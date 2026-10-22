package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
)

func TestPresence(t *testing.T) {
	h := events.NewHub()
	p := NewPresence(h)
	ch, cancel := h.Subscribe()
	defer cancel()
	p.Connect("a")
	if e := <-ch; e.Type != "device.online" {
		t.Fatal(e)
	}
	p.Connect("a") // second tab
	select {
	case e := <-ch:
		t.Fatalf("unexpected %v", e)
	case <-time.After(20 * time.Millisecond):
	}
	p.Disconnect("a")
	select {
	case e := <-ch:
		t.Fatalf("still online: %v", e)
	default:
	}
	p.Disconnect("a")
	if e := <-ch; e.Type != "device.offline" {
		t.Fatal(e)
	}
}

func TestTransferAPI(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	other := e.pairAnother("phone")
	var me, them struct{ ID string }
	_, body := e.do("GET", "/api/devices/me", "", e.token)
	json.Unmarshal(body, &me)
	// pairAnother returns token; get its device via a privileged list
	_, body = e.do("GET", "/api/devices", "", e.token)
	var devices struct {
		Items []struct{ ID, Name string }
	}
	// listDevices may return array directly
	var raw json.RawMessage
	json.Unmarshal(body, &raw)
	if raw[0] == '[' {
		json.Unmarshal(body, &devices.Items)
	} else {
		json.Unmarshal(body, &devices)
	}
	for _, d := range devices.Items {
		if d.Name == "phone" {
			them.ID = d.ID
		}
	}
	if them.ID == "" {
		t.Fatalf("phone not found: %s", body)
	}
	_, _ = other, me
	res, body := e.do("POST", "/api/players/"+me.ID+"/play", `{"track_ids":["`+tr.ID+`"]}`, e.token)
	if res.StatusCode != 200 {
		t.Fatalf("play %d %s", res.StatusCode, body)
	}
	res, body = e.do("POST", "/api/players/"+me.ID+"/transfer", `{"to":"`+them.ID+`"}`, e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"player_id":"`+them.ID+`"`) {
		t.Fatalf("transfer %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("POST", "/api/players/"+me.ID+"/transfer", `{}`, e.token); res.StatusCode != 400 {
		t.Fatal("bad body")
	}
}

func TestWSOnlineOffline(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/ws?access_token="+e.token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"type":"hello"`) || !strings.Contains(string(b), `"online"`) {
		t.Fatalf("%s", b)
	}
	var me struct{ ID string }
	_, body := e.do("GET", "/api/devices/me", "", e.token)
	json.Unmarshal(body, &me)
	res, body := e.do("GET", "/api/players/"+me.ID, "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"online":true`) {
		t.Fatalf("%s", body)
	}
	c.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, body = e.do("GET", "/api/players/"+me.ID, "", e.token)
		if strings.Contains(string(body), `"online":false`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never offline: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = http.StatusOK
}
