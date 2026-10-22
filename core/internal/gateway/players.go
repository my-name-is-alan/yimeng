package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/my-name-is-alan/yimeng/core/internal/playback"
)

func (d Deps) playbackResult(w http.ResponseWriter, v any, err error) {
	switch {
	case errors.Is(err, playback.ErrPlayerNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "player not found")
	case errors.Is(err, playback.ErrBadRequest):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, playback.ErrEmptyQueue):
		writeErr(w, http.StatusConflict, "empty_queue", "queue is empty")
	case err != nil:
		d.internal(w, "playback", err)
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

func (d Deps) listPlayers(w http.ResponseWriter, r *http.Request) {
	st, err := d.Playback.States(r.Context())
	for i := range st {
		st[i].Online = d.Presence.Online(st[i].PlayerID)
	}
	d.playbackResult(w, map[string]any{"items": st, "online": d.Presence.Snapshot()}, err)
}

func (d Deps) playerState(w http.ResponseWriter, r *http.Request) {
	st, err := d.Playback.State(r.Context(), r.PathValue("id"))
	st.Online = d.Presence.Online(st.PlayerID)
	d.playbackResult(w, d.withOnline(st), err)
}

func (d Deps) withOnline(st playback.State) playback.State {
	st.Online = d.Presence.Online(st.PlayerID)
	return st
}

func (d Deps) playerQueue(w http.ResponseWriter, r *http.Request) {
	q, err := d.Playback.Queue(r.Context(), r.PathValue("id"))
	d.playbackResult(w, q, err)
}

func (d Deps) playerAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, pid := r.Context(), r.PathValue("id")
		var (
			st  playback.State
			err error
		)
		switch action {
		case "play":
			var b playback.PlayRequest
			if err := decodeBody(r, &b); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			st, err = d.Playback.Play(ctx, pid, b)
		case "pause":
			st, err = d.Playback.Pause(ctx, pid)
		case "stop":
			st, err = d.Playback.Stop(ctx, pid)
		case "next":
			st, err = d.Playback.Next(ctx, pid)
		case "prev":
			st, err = d.Playback.Prev(ctx, pid)
		case "seek":
			var b struct {
				PositionMs *int64 `json:"position_ms"`
			}
			if err := decodeBody(r, &b); err != nil || b.PositionMs == nil {
				writeErr(w, http.StatusBadRequest, "invalid_body", "position_ms required")
				return
			}
			st, err = d.Playback.Seek(ctx, pid, *b.PositionMs)
		case "repeat":
			var b struct {
				Mode string `json:"mode"`
			}
			if err := decodeBody(r, &b); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			st, err = d.Playback.SetRepeat(ctx, pid, b.Mode)
		case "shuffle":
			var b struct {
				Enabled   *bool `json:"enabled"`
				Reshuffle bool  `json:"reshuffle"`
			}
			if err := decodeBody(r, &b); err != nil || b.Enabled == nil {
				writeErr(w, http.StatusBadRequest, "invalid_body", "enabled required")
				return
			}
			st, err = d.Playback.SetShuffle(ctx, pid, *b.Enabled, b.Reshuffle)
		case "transfer":
			var b struct {
				To string `json:"to"`
			}
			if err := decodeBody(r, &b); err != nil || b.To == "" {
				writeErr(w, http.StatusBadRequest, "invalid_body", "to (destination player id) required")
				return
			}
			st, err = d.Playback.Transfer(ctx, pid, b.To)
		case "queue":
			var b playback.QueueOp
			if err := decodeBody(r, &b); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			st, err = d.Playback.EditQueue(ctx, pid, b)
		}
		d.playbackResult(w, d.withOnline(st), err)
	}
}

// ── WebSocket /ws ───────────────────────────────────────────────────────────

type wsIn struct {
	Type string `json:"type"`
	Data struct {
		PlayerID   string `json:"player_id"`
		PositionMs int64  `json:"position_ms"`
		TrackID    string `json:"track_id"`
	} `json:"data"`
}

// ws streams server events to every authenticated client and accepts
// player.progress / player.ended reports. On connect it sends a "hello"
// snapshot with all player states.
func (d Deps) ws(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Auth is the device token, not cookies, so cross-origin pages cannot
		// ride an ambient session; allow any origin (dev UIs on other ports).
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(16 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	events, unsub := d.Hub.Subscribe()
	defer unsub()
	d.Presence.Connect(dev.ID)
	defer d.Presence.Disconnect(dev.ID)

	states, err := d.Playback.States(ctx)
	if err != nil {
		c.Close(websocket.StatusInternalError, "state")
		return
	}
	if err := writeWS(ctx, c, "hello", map[string]any{
		"device_id": dev.ID, "players": states, "online": d.Presence.Snapshot(),
	}); err != nil {
		return
	}

	go func() {
		defer cancel()
		for {
			var in wsIn
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if json.Unmarshal(b, &in) != nil {
				continue
			}
			pid := in.Data.PlayerID
			if pid == "" {
				pid = dev.ID // a device reports for its own player by default
			}
			switch in.Type {
			case "player.progress":
				_ = d.Playback.Progress(ctx, pid, in.Data.PositionMs)
			case "player.ended":
				_, _ = d.Playback.Ended(ctx, pid, in.Data.TrackID)
			case "ping":
				_ = writeWS(ctx, c, "pong", nil)
			}
		}
	}()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case e, ok := <-events:
			if !ok {
				c.Close(websocket.StatusTryAgainLater, "slow consumer; reconnect")
				return
			}
			if err := writeWS(ctx, c, e.Type, e.Data); err != nil {
				return
			}
		case <-ping.C:
			pctx, pc := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pc()
			if err != nil {
				return
			}
		}
	}
}

func writeWS(ctx context.Context, c *websocket.Conn, typ string, data any) error {
	b, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b)
}
