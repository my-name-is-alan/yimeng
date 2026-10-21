package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
)

type wsMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func dialWS(t *testing.T, e *env, tok string) *websocket.Conn {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/ws?access_token="+tok, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

// next reads messages until one of type typ arrives.
func next(t *testing.T, c *websocket.Conn, typ string) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", typ, err)
		}
		var m wsMsg
		json.Unmarshal(b, &m)
		if m.Type == typ {
			return m.Data
		}
	}
}

func TestPlayersAndWebSocket(t *testing.T) {
	e := newEnv(t)
	tok := e.token
	e.do("POST", "/api/library/scan", "", tok)
	e.sc.Wait()
	var tracks library.Page[library.Track]
	_, body := e.do("GET", "/api/tracks", "", tok)
	json.Unmarshal(body, &tracks)
	if tracks.Total != 2 {
		t.Fatalf("tracks %s", body)
	}
	_, body = e.do("GET", "/api/devices/me", "", tok)
	var me struct{ ID string }
	json.Unmarshal(body, &me)

	if res, _ := e.do("GET", "/ws", "", ""); res.StatusCode != 401 {
		t.Fatal("ws without token")
	}
	// second device observes the first device's player
	code, _, _ := e.auth.NewCode()
	_, body = e.do("POST", "/api/pair", `{"code":"`+code+`","name":"phone"}`, "")
	var p2 struct{ Token string }
	json.Unmarshal(body, &p2)

	a := dialWS(t, e, tok)
	b := dialWS(t, e, p2.Token)
	var hello struct {
		DeviceID string           `json:"device_id"`
		Players  []playback.State `json:"players"`
	}
	json.Unmarshal(next(t, a, "hello"), &hello)
	next(t, b, "hello")
	if hello.DeviceID != me.ID || len(hello.Players) != 2 {
		t.Fatalf("hello %+v", hello)
	}

	ids := `["` + tracks.Items[0].ID + `","` + tracks.Items[1].ID + `"]`
	res, body := e.do("POST", "/api/players/"+me.ID+"/play", `{"track_ids":`+ids+`}`, tok)
	if res.StatusCode != 200 {
		t.Fatalf("play %d %s", res.StatusCode, body)
	}
	for _, c := range []*websocket.Conn{a, b} {
		var st playback.State
		json.Unmarshal(next(t, c, "player.state"), &st)
		if st.Transport != "buffering" || st.Lease == nil || !strings.HasPrefix(st.Lease.URL, "/api/stream/") {
			t.Fatalf("state %+v", st)
		}
		next(t, c, "queue.updated")
	}
	// client A reports progress -> both observe playing
	a.Write(context.Background(), websocket.MessageText, []byte(`{"type":"player.progress","data":{"position_ms":1200}}`))
	var st playback.State
	json.Unmarshal(next(t, b, "player.state"), &st)
	if st.Transport != "playing" || st.PlayerID != me.ID {
		t.Fatalf("observer %+v", st)
	}
	a.Write(context.Background(), websocket.MessageText, []byte(`{"type":"player.ended","data":{"track_id":"`+tracks.Items[0].ID+`"}}`))
	json.Unmarshal(next(t, b, "player.state"), &st)
	if st.CurrentIndex != 1 {
		t.Fatalf("ended->next %+v", st)
	}

	for _, c := range []struct {
		path, body string
		code       int
	}{
		{"pause", "", 200}, {"seek", `{"position_ms":500}`, 200}, {"seek", `{}`, 400},
		{"prev", "", 200}, {"next", "", 200}, {"repeat", `{"mode":"all"}`, 200}, {"repeat", `{"mode":"x"}`, 400},
		{"shuffle", `{"enabled":true}`, 200}, {"shuffle", `{}`, 400}, {"shuffle", `{"enabled":false}`, 200},
		{"queue", `{"op":"remove","index":0}`, 200}, {"queue", `{"op":"bogus"}`, 400}, {"stop", "", 200},
	} {
		if res, body := e.do("POST", "/api/players/"+me.ID+"/"+c.path, c.body, tok); res.StatusCode != c.code {
			t.Errorf("%s %s: %d %s", c.path, c.body, res.StatusCode, body)
		}
	}
	if res, _ := e.do("POST", "/api/players/nope/play", "", tok); res.StatusCode != 404 {
		t.Fatal("unknown player")
	}
	if res, _ := e.do("GET", "/api/players/"+me.ID+"/queue", "", tok); res.StatusCode != 200 {
		t.Fatal("queue")
	}
	if res, body := e.do("GET", "/api/players", "", tok); res.StatusCode != 200 || !strings.Contains(string(body), me.ID) {
		t.Fatal("players")
	}
}
