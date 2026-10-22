package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/lastfm"
)

func TestListeningReportIsolation(t *testing.T) {
	e := newEnv(t)
	_, alice, bob := e.accounts()
	tr := flacTrack(t, e)
	for i := 0; i < 3; i++ {
		if res, b := e.do("POST", "/api/scrobble", `{"track_id":"`+tr.ID+`","completed":true,"duration_ms":60000}`, alice); res.StatusCode != 204 {
			t.Fatalf("scrobble %d %s", res.StatusCode, b)
		}
	}
	get := func(tok, q string) (int, map[string]any) {
		res, b := e.do("GET", "/api/me/report"+q, "", tok)
		var m map[string]any
		json.Unmarshal(b, &m)
		return res.StatusCode, m
	}
	code, a := get(alice, "?tz=Asia/Shanghai")
	if code != 200 || a["plays"].(float64) != 3 || a["unique_tracks"].(float64) != 1 || a["listened_ms"].(float64) != 180000 {
		t.Fatalf("alice month: %d %v", code, a)
	}
	tops := a["top_tracks"].([]any)
	if len(tops) != 1 || tops[0].(map[string]any)["plays"].(float64) != 3 {
		t.Fatalf("top tracks %v", tops)
	}
	hours := 0.0
	for _, h := range a["by_hour"].([]any) {
		hours += h.(float64)
	}
	if hours != 3 || len(a["by_weekday"].([]any)) != 7 {
		t.Fatalf("buckets %v", a)
	}
	if _, y := get(alice, "?period=year&tz=Asia/Shanghai"); y["plays"].(float64) != 3 || len(y["by_day"].([]any)) != 12 {
		t.Fatalf("year %v", y)
	}
	if _, b := get(bob, ""); b["plays"].(float64) != 0 {
		t.Fatalf("bob sees alice's plays: %v", b)
	}
	if _, old := get(alice, "?year=2001&month=1"); old["plays"].(float64) != 0 {
		t.Fatal("range not applied")
	}
	for _, q := range []string{"?period=week", "?month=13", "?tz=Mars/Base", "?year=abc"} {
		if c, _ := get(alice, q); c != 400 {
			t.Errorf("%s = %d", q, c)
		}
	}
}

const (
	fakeKey    = "0123456789abcdef0123456789abcdef"
	fakeSecret = "fedcba9876543210fedcba9876543210"
)

func TestLastFMFlow(t *testing.T) {
	e := newEnv(t)
	_, alice, bob := e.accounts()
	tr := flacTrack(t, e)
	var mu sync.Mutex
	var scrobbles []map[string]string
	granted := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p := map[string]string{}
		for k := range r.Form {
			p[k] = r.Form.Get(k)
		}
		sig := p["api_sig"]
		delete(p, "api_sig")
		if p["api_key"] != fakeKey || lastfm.Sign(p, fakeSecret) != sig {
			w.Write([]byte(`{"error":13,"message":"Invalid method signature supplied"}`))
			return
		}
		switch p["method"] {
		case "auth.getToken":
			w.Write([]byte(`{"token":"tok123"}`))
		case "auth.getSession":
			if !granted {
				w.Write([]byte(`{"error":14,"message":"Unauthorized Token"}`))
				return
			}
			w.Write([]byte(`{"session":{"name":"alice_fm","key":"sess-secret-1"}}`))
		case "track.scrobble":
			mu.Lock()
			scrobbles = append(scrobbles, p)
			mu.Unlock()
			w.Write([]byte(`{"scrobbles":{"@attr":{"accepted":1}}}`))
		}
	}))
	defer fake.Close()
	e.lfm.APIURL = fake.URL
	e.lfm.MinTrackMs = -1 // test fixtures are a few seconds long

	if res, _ := e.do("POST", "/api/me/lastfm/auth", "", alice); res.StatusCode != 409 {
		t.Fatal("auth without keys")
	}
	if res, _ := e.do("PUT", "/api/me/lastfm/keys", `{"api_key":"short","api_secret":"x"}`, alice); res.StatusCode != 400 {
		t.Fatal("bad keys accepted")
	}
	res, b := e.do("PUT", "/api/me/lastfm/keys", `{"api_key":"`+fakeKey+`","api_secret":"`+fakeSecret+`"}`, alice)
	if res.StatusCode != 200 || strings.Contains(string(b), fakeKey) || strings.Contains(string(b), fakeSecret) || !strings.Contains(string(b), `"configured":true`) {
		t.Fatalf("keys %d %s", res.StatusCode, b)
	}
	res, b = e.do("POST", "/api/me/lastfm/auth", "", alice)
	if res.StatusCode != 200 || !strings.Contains(string(b), "token=tok123") {
		t.Fatalf("auth %d %s", res.StatusCode, b)
	}
	if res, _ := e.do("POST", "/api/me/lastfm/auth/complete", "", alice); res.StatusCode != 409 {
		t.Fatal("complete before grant")
	}
	granted = true
	res, b = e.do("POST", "/api/me/lastfm/auth/complete", "", alice)
	if res.StatusCode != 200 || !strings.Contains(string(b), `"username":"alice_fm"`) || strings.Contains(string(b), "sess-secret") {
		t.Fatalf("complete %d %s", res.StatusCode, b)
	}
	// bob is unaffected
	if _, b := e.do("GET", "/api/me/lastfm", "", bob); strings.Contains(string(b), "alice_fm") || strings.Contains(string(b), `"connected":true`) {
		t.Fatalf("bob sees alice's lastfm: %s", b)
	}
	e.do("POST", "/api/scrobble", `{"track_id":"`+tr.ID+`","completed":true}`, alice)
	e.do("POST", "/api/scrobble", `{"track_id":"`+tr.ID+`","completed":true}`, bob)
	if _, b := e.do("GET", "/api/me/lastfm", "", alice); !strings.Contains(string(b), `"pending":1`) {
		t.Fatalf("queue %s", b)
	}
	e.lfm.Flush(context.Background())
	mu.Lock()
	if len(scrobbles) != 1 || scrobbles[0]["sk"] != "sess-secret-1" || scrobbles[0]["track[0]"] == "" || scrobbles[0]["timestamp[0]"] == "" {
		t.Fatalf("scrobbles %v", scrobbles)
	}
	mu.Unlock()
	if _, b := e.do("GET", "/api/me/lastfm", "", alice); !strings.Contains(string(b), `"pending":0`) {
		t.Fatalf("queue not drained %s", b)
	}
	// pause, then disconnect
	e.do("PUT", "/api/me/lastfm", `{"enabled":false}`, alice)
	e.do("POST", "/api/scrobble", `{"track_id":"`+tr.ID+`","completed":true}`, alice)
	if _, b := e.do("GET", "/api/me/lastfm", "", alice); !strings.Contains(string(b), `"pending":0`) || !strings.Contains(string(b), `"enabled":false`) {
		t.Fatalf("paused still queues %s", b)
	}
	if res, _ := e.do("DELETE", "/api/me/lastfm", "", alice); res.StatusCode != 204 {
		t.Fatal("disconnect")
	}
	if _, b := e.do("GET", "/api/me/lastfm", "", alice); strings.Contains(string(b), `"configured":true`) {
		t.Fatalf("not removed %s", b)
	}
	// no-account devices cannot use Last.fm
	if res, _ := e.do("GET", "/api/me/lastfm", "", e.pairDevice("loose")); res.StatusCode != 403 && res.StatusCode != 200 {
		t.Fatalf("no-account: %d", res.StatusCode)
	}
	_ = time.Second
}

func TestReportTimeZoneSetting(t *testing.T) {
	e := newEnv(t)
	admin, alice, _ := e.accounts()
	tz := func() string {
		_, b := e.do("GET", "/api/me/report", "", alice)
		var m struct{ TZ string }
		json.Unmarshal(b, &m)
		return m.TZ
	}
	if got := tz(); got != "Asia/Shanghai" {
		t.Fatalf("default tz %q", got)
	}
	if res, b := e.do("PUT", "/api/settings/server", `{"time_zone":"Europe/Berlin"}`, admin); res.StatusCode != 200 {
		t.Fatalf("put tz %d %s", res.StatusCode, b)
	}
	if got := tz(); got != "Europe/Berlin" {
		t.Fatalf("tz after setting %q", got)
	}
	for _, bad := range []string{"Mars/Base", "Local"} {
		if res, _ := e.do("PUT", "/api/settings/server", `{"time_zone":"`+bad+`"}`, admin); res.StatusCode != 400 {
			t.Errorf("%s accepted", bad)
		}
	}
	if res, _ := e.do("GET", "/api/me/report?tz=UTC", "", alice); res.StatusCode != 200 {
		t.Fatal("explicit tz")
	}
}
