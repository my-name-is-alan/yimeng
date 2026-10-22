package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// accounts sets up an admin plus two user accounts and returns login tokens.
func (e *env) accounts() (admin, alice, bob string) {
	e.t.Helper()
	res, body := e.do("POST", "/api/auth/setup", `{"username":"root","password":"Str0ng-Passw0rd"}`, "")
	if res.StatusCode != 201 {
		e.t.Fatalf("setup %d %s", res.StatusCode, body)
	}
	var s struct{ Token string }
	json.Unmarshal(body, &s)
	login := func(u string) string {
		if res, b := e.do("POST", "/api/users", `{"username":"`+u+`","password":"Little-Listens-42","role":"user"}`, s.Token); res.StatusCode != 201 {
			e.t.Fatalf("create %s: %s", u, b)
		}
		res, b := e.do("POST", "/api/auth/login", `{"username":"`+u+`","password":"Little-Listens-42"}`, "")
		if res.StatusCode != 200 {
			e.t.Fatalf("login %s: %s", u, b)
		}
		var k struct{ Token string }
		json.Unmarshal(b, &k)
		return k.Token
	}
	return s.Token, login("alice"), login("bob")
}

func TestPerAccountIsolation(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	admin, alice, bob := e.accounts()

	// playlists
	res, body := e.do("POST", "/api/playlists", `{"name":"alice mix"}`, alice)
	if res.StatusCode != 201 {
		t.Fatalf("create playlist %d %s", res.StatusCode, body)
	}
	var pl struct {
		ID      string `json:"id"`
		OwnerID string `json:"owner_id"`
	}
	json.Unmarshal(body, &pl)
	if pl.OwnerID == "" {
		t.Fatalf("no owner_id: %s", body)
	}
	for _, c := range [][3]string{
		{"GET", "/api/playlists/" + pl.ID, ""},
		{"GET", "/api/playlists/" + pl.ID + "/tracks", ""},
		{"PATCH", "/api/playlists/" + pl.ID, `{"name":"pwned"}`},
		{"POST", "/api/playlists/" + pl.ID + "/tracks", `{"track_ids":["` + tr.ID + `"]}`},
		{"GET", "/api/playlists/" + pl.ID + "/m3u", ""},
		{"DELETE", "/api/playlists/" + pl.ID, ""},
	} {
		if res, _ := e.do(c[0], c[1], c[2], bob); res.StatusCode != 404 {
			t.Errorf("bob %s %s = %d, want 404", c[0], c[1], res.StatusCode)
		}
	}
	if _, b := e.do("GET", "/api/playlists", "", bob); strings.Contains(string(b), pl.ID) {
		t.Fatalf("bob lists alice's playlist: %s", b)
	}
	if res, _ := e.do("GET", "/api/playlists/"+pl.ID, "", admin); res.StatusCode != 200 {
		t.Fatalf("admin cannot manage playlist: %d", res.StatusCode)
	}

	// favorites, ratings, scrobbles, history
	e.do("PUT", "/api/favorites/track/"+tr.ID, "", alice)
	e.do("PUT", "/api/ratings/track/"+tr.ID, `{"rating":5}`, alice)
	if res, b := e.do("POST", "/api/scrobble", `{"track_id":"`+tr.ID+`","completed":true}`, alice); res.StatusCode != 204 {
		t.Fatalf("scrobble %d %s", res.StatusCode, b)
	}
	for _, p := range []string{"/api/favorites?type=track", "/api/history", "/api/history/top"} {
		_, a := e.do("GET", p, "", alice)
		_, b := e.do("GET", p, "", bob)
		if !strings.Contains(string(a), tr.ID) {
			t.Errorf("alice %s missing own data: %s", p, a)
		}
		if strings.Contains(string(b), tr.ID) {
			t.Errorf("bob %s sees alice's data: %s", p, b)
		}
	}
	if _, b := e.do("GET", "/api/tracks/"+tr.ID, "", bob); strings.Contains(string(b), `"favorite":true`) {
		t.Errorf("favorite flag leaks to bob: %s", b)
	}

	// progress
	e.do("PUT", "/api/progress/"+tr.ID, `{"position_ms":1000,"duration_ms":5000}`, alice)
	if _, b := e.do("GET", "/api/progress", "", bob); strings.Contains(string(b), tr.ID) {
		t.Errorf("bob sees alice's progress: %s", b)
	}

	// devices / sessions / players
	_, b := e.do("GET", "/api/devices/me", "", alice)
	var ad struct{ ID string }
	json.Unmarshal(b, &ad)
	if _, b := e.do("GET", "/api/sessions", "", bob); strings.Contains(string(b), ad.ID) {
		t.Errorf("bob sees alice's session")
	}
	if _, b := e.do("GET", "/api/players", "", bob); strings.Contains(string(b), ad.ID) {
		t.Errorf("bob lists alice's player")
	}
	for _, c := range [][3]string{
		{"GET", "/api/players/" + ad.ID, ""},
		{"GET", "/api/players/" + ad.ID + "/queue", ""},
		{"POST", "/api/players/" + ad.ID + "/pause", ""},
		{"DELETE", "/api/devices/" + ad.ID, ""},
	} {
		if res, _ := e.do(c[0], c[1], c[2], bob); res.StatusCode != 404 {
			t.Errorf("bob %s %s = %d, want 404", c[0], c[1], res.StatusCode)
		}
	}
	_, b = e.do("GET", "/api/devices/me", "", bob)
	var bd struct{ ID string }
	json.Unmarshal(b, &bd)
	if res, _ := e.do("POST", "/api/sessions/handoff", `{"from":"`+ad.ID+`","to":"`+bd.ID+`"}`, bob); res.StatusCode != 404 {
		t.Errorf("bob hands off alice's session: %d", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/players/"+bd.ID+"/transfer", `{"to":"`+ad.ID+`"}`, bob); res.StatusCode != 404 {
		t.Errorf("bob transfers to alice's player: %d", res.StatusCode)
	}
	if res, _ := e.do("GET", "/api/players/"+ad.ID, "", admin); res.StatusCode == 404 {
		t.Errorf("admin cannot see player")
	}

	// system: non-admins get no host paths
	if _, b := e.do("GET", "/api/system", "", bob); strings.Contains(string(b), `"path":"/`) || strings.Contains(string(b), "uptime") ||
		strings.Contains(string(b), `"library"`) || strings.Contains(string(b), `"os"`) || !strings.Contains(string(b), `"api_version":"`+APIVersion+`"`) {
		t.Errorf("system leaks detail to user: %s", b)
	}
}

// Online tracks are scrobblable: a snapshot row is stored so history works.
func TestScrobbleOnlineTrack(t *testing.T) {
	e := onlineReady(t)
	if res, b := e.do("POST", "/api/scrobble", `{"track_id":"gw:full","completed":true}`, e.token); res.StatusCode != 204 {
		t.Fatalf("scrobble online %d %s", res.StatusCode, b)
	}
	_, b := e.do("GET", "/api/history", "", e.token)
	if !strings.Contains(string(b), "gw:full") || !strings.Contains(string(b), `"title"`) {
		t.Fatalf("history %s", b)
	}
	if res, _ := e.do("POST", "/api/scrobble", `{"track_id":"gw:nope-404"}`, e.token); res.StatusCode != 404 {
		t.Fatalf("unknown online id should 404, got %d", res.StatusCode)
	}
}

func TestPlaylistsPaging(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		e.do("POST", "/api/playlists", `{"name":"`+n+`"}`, e.token)
	}
	for _, base := range []string{"/api/playlists?limit=5", "/api/playlists?scope=all&limit=5"} {
		_, b := e.do("GET", base, "", e.token)
		var p struct {
			Items      []json.RawMessage `json:"items"`
			Total      int               `json:"total"`
			NextCursor *string           `json:"next_cursor"`
		}
		json.Unmarshal(b, &p)
		if len(p.Items) != 5 || p.Total < 7 || p.NextCursor == nil {
			t.Fatalf("%s page1 %s", base, b)
		}
		_, b = e.do("GET", base+"&cursor="+*p.NextCursor, "", e.token)
		p.Items, p.NextCursor = nil, nil
		json.Unmarshal(b, &p)
		if len(p.Items) != p.Total-5 || p.NextCursor != nil {
			t.Fatalf("%s page2 %s", base, b)
		}
	}
}
