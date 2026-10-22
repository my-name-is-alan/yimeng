package gateway

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

// 一起听 (listen together) rooms with 弹幕 (D48). In-memory and ephemeral:
// a room disappears 30 min after its last member leaves or on restart.
// The host's state is authoritative; members follow it using server_time.

type roomMember struct {
	DeviceID string    `json:"device_id"`
	Name     string    `json:"name"`
	Host     bool      `json:"host"`
	JoinedAt time.Time `json:"joined_at"`
}

type RoomState struct {
	TrackID    string    `json:"track_id"`
	PositionMs int64     `json:"position_ms"`
	Playing    bool      `json:"playing"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Danmaku struct {
	ID       string    `json:"id"`
	DeviceID string    `json:"device_id"`
	Name     string    `json:"name"`
	Text     string    `json:"text"`
	TrackID  string    `json:"track_id,omitempty"`
	TMs      int64     `json:"t_ms"` // position in the track when sent
	Color    string    `json:"color,omitempty"`
	At       time.Time `json:"at"`
}

type Room struct {
	ID        string                 `json:"id"`
	Code      string                 `json:"code"`
	Name      string                 `json:"name"`
	Host      string                 `json:"host"`
	CreatedAt time.Time              `json:"created_at"`
	State     RoomState              `json:"state"`
	Members   map[string]*roomMember `json:"-"`

	danmaku []Danmaku
	subs    map[chan roomEvt]string
	emptyAt time.Time
}

type roomEvt struct {
	Type string
	Data any
}

type Rooms struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

func NewRooms() *Rooms { return &Rooms{rooms: map[string]*Room{}} }

func (d Deps) roomsSvc() *Rooms { return d.Rooms }

const roomCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randCode(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = roomCodeAlphabet[int(b[i])%len(roomCodeAlphabet)]
	}
	return string(b)
}

func (rs *Rooms) gcLocked() {
	for id, r := range rs.rooms {
		if len(r.Members) == 0 && !r.emptyAt.IsZero() && time.Since(r.emptyAt) > 30*time.Minute {
			delete(rs.rooms, id)
		}
	}
}

// find accepts the room id or its 6-char code (case-insensitive).
func (rs *Rooms) findLocked(key string) *Room {
	if r := rs.rooms[key]; r != nil {
		return r
	}
	up := strings.ToUpper(key)
	for _, r := range rs.rooms {
		if r.Code == up {
			return r
		}
	}
	return nil
}

func (r *Room) view() map[string]any {
	ms := make([]*roomMember, 0, len(r.Members))
	for _, m := range r.Members {
		ms = append(ms, m)
	}
	return map[string]any{"id": r.ID, "code": r.Code, "name": r.Name, "host": r.Host, "created_at": r.CreatedAt,
		"state": r.State, "members": ms, "server_time": time.Now().UTC(), "web_url": "/room/" + r.Code}
}

func (r *Room) broadcastLocked(typ string, data any) {
	for ch := range r.subs {
		select {
		case ch <- roomEvt{typ, data}:
		default: // slow subscriber drops events; it resyncs from room.state
		}
	}
}

func devName(dv auth.Device) string {
	if dv.Username != "" {
		return dv.Username
	}
	if dv.Name != "" {
		return dv.Name
	}
	return dv.ID
}

// POST /api/rooms {name?}
func (d Deps) createRoom(w http.ResponseWriter, r *http.Request) {
	me, _ := DeviceFrom(r.Context())
	var b struct {
		Name string `json:"name"`
	}
	_ = decodeBody(r, &b)
	rs := d.roomsSvc()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.gcLocked()
	if len(rs.rooms) >= 100 {
		writeErr(w, http.StatusTooManyRequests, "too_many_rooms", "room limit reached")
		return
	}
	code := randCode(6)
	for rs.findLocked(code) != nil {
		code = randCode(6)
	}
	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = devName(me) + " 的房间"
	}
	room := &Room{ID: "room_" + strings.ToLower(randCode(10)), Code: code, Name: truncRunes(name, 40), Host: me.ID,
		CreatedAt: time.Now().UTC(), Members: map[string]*roomMember{}, subs: map[chan roomEvt]string{}}
	room.Members[me.ID] = &roomMember{DeviceID: me.ID, Name: devName(me), Host: true, JoinedAt: time.Now().UTC()}
	rs.rooms[room.ID] = room
	writeJSON(w, http.StatusCreated, room.view())
}

// GET /api/rooms — rooms the caller is in.
func (d Deps) listRooms(w http.ResponseWriter, r *http.Request) {
	me, _ := DeviceFrom(r.Context())
	rs := d.roomsSvc()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	items := []map[string]any{}
	for _, room := range rs.rooms {
		if room.Members[me.ID] != nil {
			items = append(items, room.view())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) withRoom(w http.ResponseWriter, r *http.Request, member bool, f func(me auth.Device, room *Room)) {
	me, _ := DeviceFrom(r.Context())
	rs := d.roomsSvc()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	room := rs.findLocked(r.PathValue("id"))
	if room == nil {
		writeErr(w, http.StatusNotFound, "room_not_found", "room not found")
		return
	}
	if member && room.Members[me.ID] == nil {
		writeErr(w, http.StatusForbidden, "not_a_member", "join the room first")
		return
	}
	f(me, room)
}

// GET /api/rooms/{id} (id or code)
func (d Deps) getRoom(w http.ResponseWriter, r *http.Request) {
	d.withRoom(w, r, false, func(_ auth.Device, room *Room) { writeJSON(w, http.StatusOK, room.view()) })
}

// POST /api/rooms/{id}/join
func (d Deps) joinRoom(w http.ResponseWriter, r *http.Request) {
	d.withRoom(w, r, false, func(me auth.Device, room *Room) {
		if room.Members[me.ID] == nil {
			if len(room.Members) >= 50 {
				writeErr(w, http.StatusConflict, "room_full", "room is full")
				return
			}
			m := &roomMember{DeviceID: me.ID, Name: devName(me), JoinedAt: time.Now().UTC()}
			room.Members[me.ID] = m
			room.emptyAt = time.Time{}
			room.broadcastLocked("room.member_joined", m)
		}
		writeJSON(w, http.StatusOK, room.view())
	})
}

// POST /api/rooms/{id}/leave — the host leaving hands host to the oldest member.
func (d Deps) leaveRoom(w http.ResponseWriter, r *http.Request) {
	d.withRoom(w, r, true, func(me auth.Device, room *Room) {
		delete(room.Members, me.ID)
		room.broadcastLocked("room.member_left", map[string]any{"device_id": me.ID})
		if room.Host == me.ID {
			var next *roomMember
			for _, m := range room.Members {
				if next == nil || m.JoinedAt.Before(next.JoinedAt) {
					next = m
				}
			}
			room.Host = ""
			if next != nil {
				next.Host, room.Host = true, next.DeviceID
				room.broadcastLocked("room.host_changed", map[string]any{"host": next.DeviceID})
			}
		}
		if len(room.Members) == 0 {
			room.emptyAt = time.Now()
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// PUT /api/rooms/{id}/state {track_id, position_ms, playing} — host only.
func (d Deps) putRoomState(w http.ResponseWriter, r *http.Request) {
	var b RoomState
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	d.withRoom(w, r, true, func(me auth.Device, room *Room) {
		if room.Host != me.ID {
			writeErr(w, http.StatusForbidden, "not_host", "only the host controls playback")
			return
		}
		b.UpdatedAt = time.Now().UTC()
		room.State = b
		room.broadcastLocked("room.state", map[string]any{"state": b, "server_time": b.UpdatedAt})
		writeJSON(w, http.StatusOK, room.view())
	})
}

// POST /api/rooms/{id}/danmaku {text, t_ms?, color?}; GET lists the last 200.
func (d Deps) postDanmaku(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Text  string `json:"text"`
		TMs   *int64 `json:"t_ms"`
		Color string `json:"color"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	b.Text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, b.Text))
	if b.Text == "" || utf8.RuneCountInString(b.Text) > 100 {
		writeErr(w, http.StatusBadRequest, "invalid_text", "text must be 1-100 characters")
		return
	}
	if b.Color != "" && !validHexColor(b.Color) {
		b.Color = ""
	}
	d.withRoom(w, r, true, func(me auth.Device, room *Room) {
		now := time.Now().UTC()
		recent := 0
		for i := len(room.danmaku) - 1; i >= 0 && now.Sub(room.danmaku[i].At) < 10*time.Second; i-- {
			if room.danmaku[i].DeviceID == me.ID {
				recent++
			}
		}
		if recent >= 5 {
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "slow down")
			return
		}
		t := room.State.PositionMs
		if room.State.Playing {
			t += now.Sub(room.State.UpdatedAt).Milliseconds()
		}
		if b.TMs != nil {
			t = *b.TMs
		}
		dm := Danmaku{ID: "dm_" + strings.ToLower(randCode(10)), DeviceID: me.ID, Name: devName(me), Text: b.Text,
			TrackID: room.State.TrackID, TMs: t, Color: b.Color, At: now}
		room.danmaku = append(room.danmaku, dm)
		if len(room.danmaku) > 200 {
			room.danmaku = room.danmaku[len(room.danmaku)-200:]
		}
		room.broadcastLocked("room.danmaku", dm)
		writeJSON(w, http.StatusCreated, dm)
	})
}

func (d Deps) listDanmaku(w http.ResponseWriter, r *http.Request) {
	d.withRoom(w, r, true, func(_ auth.Device, room *Room) {
		writeJSON(w, http.StatusOK, map[string]any{"items": append([]Danmaku{}, room.danmaku...)})
	})
}

func validHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// GET /api/rooms/{id}/events — SSE for members: room.snapshot first, then
// room.state, room.danmaku, room.member_joined/left, room.host_changed.
func (d Deps) roomEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	ch := make(chan roomEvt, 64)
	var room *Room
	var snap map[string]any
	d.withRoom(w, r, true, func(me auth.Device, rm *Room) {
		room = rm
		rm.subs[ch] = me.ID
		snap = rm.view()
	})
	if room == nil {
		return
	}
	rs := d.roomsSvc()
	defer func() { rs.mu.Lock(); delete(room.subs, ch); rs.mu.Unlock() }()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(typ string, v any) error {
		b, _ := json.Marshal(v)
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b)
		fl.Flush()
		return err
	}
	send("room.snapshot", snap)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		case e := <-ch:
			if send(e.Type, e.Data) != nil {
				return
			}
		}
	}
}
