package gateway

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (e *env) pairDevice(name string) string {
	e.t.Helper()
	code, _, _ := e.auth.NewCode()
	res, body := e.do("POST", "/api/pair", `{"code":"`+code+`","name":"`+name+`","device_type":"ios"}`, "")
	if res.StatusCode != 201 {
		e.t.Fatalf("pair: %d %s", res.StatusCode, body)
	}
	var pr struct{ Token string }
	json.Unmarshal(body, &pr)
	return pr.Token
}

// sseEvents reads up to n SSE event names (with data) from an open stream.
func sseEvents(t *testing.T, res *http.Response, n int, timeout time.Duration) []string {
	t.Helper()
	out := make(chan []string, 1)
	go func() {
		var evs []string
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		ev := ""
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "event: "):
				ev = strings.TrimPrefix(l, "event: ")
			case strings.HasPrefix(l, "data: "):
				evs = append(evs, ev+" "+strings.TrimPrefix(l, "data: "))
				if len(evs) >= n {
					out <- evs
					return
				}
			}
		}
		out <- evs
	}()
	select {
	case evs := <-out:
		return evs
	case <-time.After(timeout):
		res.Body.Close()
		return <-out
	}
}

func TestRoomsAndDanmaku(t *testing.T) {
	e := newEnv(t)
	guest := e.pairDevice("guest")
	res, body := e.do("POST", "/api/v1/rooms", `{"name":"晚上一起听"}`, e.token)
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var room struct{ ID, Code, Host string }
	json.Unmarshal(body, &room)
	if len(room.Code) != 6 || room.ID == "" {
		t.Fatalf("room: %s", body)
	}
	if res, _ := e.do("POST", "/api/v1/rooms/"+room.ID+"/danmaku", `{"text":"hi"}`, guest); res.StatusCode != 403 {
		t.Fatalf("non-member danmaku: %d", res.StatusCode)
	}
	if res, body := e.do("POST", "/api/v1/rooms/"+strings.ToLower(room.Code)+"/join", "", guest); res.StatusCode != 200 {
		t.Fatalf("join by code: %d %s", res.StatusCode, body)
	}
	// guest subscribes to room events
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/rooms/"+room.ID+"/events?access_token="+guest, nil)
	sres, err := http.DefaultClient.Do(req)
	if err != nil || sres.StatusCode != 200 {
		t.Fatalf("events: %v %v", err, sres)
	}
	defer sres.Body.Close()
	time.Sleep(50 * time.Millisecond)
	if res, _ := e.do("PUT", "/api/v1/rooms/"+room.ID+"/state", `{"track_id":"t1","position_ms":1000,"playing":true}`, guest); res.StatusCode != 403 {
		t.Fatalf("guest state: %d", res.StatusCode)
	}
	if res, body := e.do("PUT", "/api/v1/rooms/"+room.ID+"/state", `{"track_id":"t1","position_ms":1000,"playing":true}`, e.token); res.StatusCode != 200 {
		t.Fatalf("host state: %d %s", res.StatusCode, body)
	}
	if res, body := e.do("POST", "/api/v1/rooms/"+room.ID+"/danmaku", `{"text":" 好听！ ","color":"#ff0000"}`, guest); res.StatusCode != 201 {
		t.Fatalf("danmaku: %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("POST", "/api/v1/rooms/"+room.ID+"/danmaku", `{"text":""}`, guest); res.StatusCode != 400 {
		t.Fatalf("empty danmaku: %d", res.StatusCode)
	}
	evs := sseEvents(t, sres, 3, 3*time.Second)
	if len(evs) < 3 || !strings.HasPrefix(evs[0], "room.snapshot") || !strings.HasPrefix(evs[1], "room.state") ||
		!strings.HasPrefix(evs[2], "room.danmaku") || !strings.Contains(evs[2], `"text":"好听！"`) || !strings.Contains(evs[2], `"t_ms":1`) {
		t.Fatalf("events: %v", evs)
	}
	for i := 0; i < 4; i++ {
		e.do("POST", "/api/v1/rooms/"+room.ID+"/danmaku", `{"text":"x"}`, guest)
	}
	if res, _ := e.do("POST", "/api/v1/rooms/"+room.ID+"/danmaku", `{"text":"x"}`, guest); res.StatusCode != 429 {
		t.Fatalf("rate limit: %d", res.StatusCode)
	}
	_, body = e.do("GET", "/api/v1/rooms/"+room.ID+"/danmaku", "", e.token)
	var dl struct{ Items []any }
	json.Unmarshal(body, &dl)
	if len(dl.Items) != 5 {
		t.Fatalf("danmaku list: %s", body)
	}
	// host leaves → guest becomes host
	if res, _ := e.do("POST", "/api/v1/rooms/"+room.ID+"/leave", "", e.token); res.StatusCode != 204 {
		t.Fatalf("leave: %d", res.StatusCode)
	}
	_, body = e.do("GET", "/api/v1/rooms/"+room.ID, "", guest)
	if strings.Contains(body2s(body), `"host":"`+room.Host+`"`) {
		t.Fatalf("host not handed over: %s", body)
	}
	if res, _ := e.do("GET", "/api/v1/rooms/NOPE00", "", guest); res.StatusCode != 404 {
		t.Fatalf("missing room: %d", res.StatusCode)
	}
	if res, body := e.do("GET", "/room/"+room.Code, "", ""); res.StatusCode != 200 || !strings.Contains(string(body), "一起听") {
		t.Fatalf("room page: %d", res.StatusCode)
	}
}

func body2s(b []byte) string { return string(b) }

func TestSessionsAndHandoff(t *testing.T) {
	e := newEnv(t)
	res, body := e.do("GET", "/api/v1/sessions", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"current":true`) {
		t.Fatalf("sessions: %d %s", res.StatusCode, body)
	}
	var s struct {
		Items []struct {
			Device struct{ ID string }
		}
	}
	json.Unmarshal(body, &s)
	if len(s.Items) != 1 {
		t.Fatalf("paired device sees only itself: %s", body)
	}
	other := e.pairDevice("other") // non-admin paired device: may only hand off to itself
	if res, _ := e.do("POST", "/api/v1/sessions/handoff", `{"to":"`+s.Items[0].Device.ID+`"}`, other); res.StatusCode != 404 {
		t.Fatalf("handoff to another owner's device: %d", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/v1/sessions/handoff", `{}`, e.token); res.StatusCode != 400 {
		t.Fatalf("handoff without to: %d", res.StatusCode)
	}
}

func TestSearchStream(t *testing.T) {
	e, _ := newOnlineEnv(t)
	if res, body := e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token); res.StatusCode != 200 {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/search?q=a&stream=1", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %v %v", err, res)
	}
	defer res.Body.Close()
	evs := sseEvents(t, res, 10, 5*time.Second)
	if len(evs) < 2 || !strings.HasPrefix(evs[0], "local ") || !strings.HasPrefix(evs[len(evs)-1], "done ") {
		t.Fatalf("events: %v", evs)
	}
	online := 0
	for _, ev := range evs {
		if strings.HasPrefix(ev, "online ") {
			online++
		}
	}
	if online == 0 {
		t.Fatalf("no online group: %v", evs)
	}
}

func TestNonAdminAuthz(t *testing.T) {
	e := newEnv(t)
	other := e.pairDevice("phone")
	_, b := e.do("GET", "/api/v1/devices/me", "", e.token)
	var admin struct{ ID string }
	json.Unmarshal(b, &admin)
	if res, _ := e.do("DELETE", "/api/v1/devices/"+admin.ID, "", other); res.StatusCode != 404 {
		t.Fatalf("non-admin revoked admin device: %d", res.StatusCode)
	}
	if res, _ := e.do("GET", "/api/v1/devices/me", "", e.token); res.StatusCode != 200 {
		t.Fatalf("admin token gone: %d", res.StatusCode)
	}
	_, b = e.do("GET", "/api/v1/devices", "", other)
	if strings.Contains(string(b), admin.ID) {
		t.Fatalf("non-admin sees other devices: %s", b)
	}
	for _, c := range [][2]string{{"POST", "/api/v1/library/scan"}, {"POST", "/api/v1/metadata/enrich"}, {"PUT", "/api/v1/settings/artist-split"}} {
		if res, _ := e.do(c[0], c[1], `{}`, other); res.StatusCode != 403 {
			t.Errorf("%s %s by non-admin: %d", c[0], c[1], res.StatusCode)
		}
	}
	_, b = e.do("GET", "/api/v1/devices/me", "", other)
	var me struct{ ID string }
	json.Unmarshal(b, &me)
	if res, _ := e.do("DELETE", "/api/v1/devices/"+me.ID, "", other); res.StatusCode != 204 {
		t.Fatalf("self sign-out: %d", res.StatusCode)
	}
}
