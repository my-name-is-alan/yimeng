package gateway

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/runtimecfg"
)

type credResp struct {
	Credential struct {
		ID     string   `json:"id"`
		Scopes []string `json:"scopes"`
	} `json:"credential"`
	Secret string `json:"secret"`
}

func (e *env) mkCred(tok, body string) credResp {
	e.t.Helper()
	res, b := e.do("POST", "/api/v1/developer/credentials", body, tok)
	if res.StatusCode != 201 {
		e.t.Fatalf("create cred %d %s", res.StatusCode, b)
	}
	var c credResp
	json.Unmarshal(b, &c)
	return c
}

func sub(e *env, path string, q url.Values) map[string]any {
	e.t.Helper()
	q.Set("f", "json")
	q.Set("v", "1.16.1")
	q.Set("c", "test")
	res, b := e.do("GET", "/rest/"+path+"?"+q.Encode(), "", "")
	if res.StatusCode != 200 {
		e.t.Fatalf("%s: %d", path, res.StatusCode)
	}
	var env map[string]map[string]any
	json.Unmarshal(b, &env)
	return env["subsonic-response"]
}

func subCode(r map[string]any) int {
	if er, ok := r["error"].(map[string]any); ok {
		return int(er["code"].(float64))
	}
	return 0
}

func TestAPIKeyScopes(t *testing.T) {
	e := newEnv(t)
	admin, alice, bob := e.accounts()
	// read-only key for alice
	k := e.mkCred(alice, `{"kind":"api_key","name":"reader","scopes":["read"]}`)
	if !strings.HasPrefix(k.Secret, "yk_") {
		t.Fatalf("secret %q", k.Secret)
	}
	if res, b := e.do("GET", "/api/v1/albums", "", k.Secret); res.StatusCode != 200 || res.Header.Get("X-Yimeng-API-Version") == "" {
		t.Fatalf("read with key: %d %s", res.StatusCode, b)
	}
	tr := flacTrack(t, e)
	if res, _ := e.do("GET", "/api/v1/stream/"+tr.ID, "", k.Secret); res.StatusCode != 403 {
		t.Errorf("stream with read key: %d", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/v1/playlists", `{"name":"x"}`, k.Secret); res.StatusCode != 403 {
		t.Fatalf("write with read key: %d", res.StatusCode)
	}
	for _, p := range []string{"/api/v1/developer/credentials", "/api/v1/devices"} {
		if res, _ := e.do("GET", p, "", k.Secret); res.StatusCode != 403 {
			t.Errorf("%s via key: %d", p, res.StatusCode)
		}
	}
	// admin scope is refused for non-admins, and a key with write works
	if res, _ := e.do("POST", "/api/v1/developer/credentials", `{"kind":"api_key","name":"x","scopes":["admin"]}`, alice); res.StatusCode != 400 {
		t.Errorf("non-admin admin scope: %d", res.StatusCode)
	}
	w := e.mkCred(alice, `{"kind":"api_key","name":"writer","scopes":["library-write"]}`)
	if res, b := e.do("POST", "/api/v1/playlists", `{"name":"via key"}`, w.Secret); res.StatusCode != 201 {
		t.Fatalf("write key: %d %s", res.StatusCode, b)
	}
	// admin's key without admin scope can't do admin things
	ak := e.mkCred(admin, `{"kind":"api_key","name":"a","scopes":["read"]}`)
	if res, _ := e.do("GET", "/api/v1/users", "", ak.Secret); res.StatusCode != 403 {
		t.Errorf("admin w/o scope: %d", res.StatusCode)
	}
	ak2 := e.mkCred(admin, `{"kind":"api_key","name":"a2","scopes":["admin"]}`)
	if res, _ := e.do("GET", "/api/v1/users", "", ak2.Secret); res.StatusCode != 200 {
		t.Errorf("admin scope: %d", res.StatusCode)
	}
	// isolation: bob can't list or revoke alice's
	res, b := e.do("GET", "/api/v1/developer/credentials", "", bob)
	if res.StatusCode != 200 || strings.Contains(string(b), k.Credential.ID) {
		t.Fatalf("bob list: %s", b)
	}
	if res, _ := e.do("DELETE", "/api/v1/developer/credentials/"+k.Credential.ID, "", bob); res.StatusCode != 404 {
		t.Errorf("bob revoke: %d", res.StatusCode)
	}
	if strings.Contains(string(b), "yk_") && strings.Contains(string(b), k.Secret) {
		t.Fatal("secret leaked in list")
	}
	if res, _ := e.do("DELETE", "/api/v1/developer/credentials/"+k.Credential.ID, "", alice); res.StatusCode != 204 {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if res, _ := e.do("GET", "/api/v1/albums", "", k.Secret); res.StatusCode != 401 {
		t.Errorf("revoked key: %d", res.StatusCode)
	}
}

func TestCORS(t *testing.T) {
	e := newEnvWith(t, nil)
	pre := func(origin string) (int, string) {
		res, _ := e.do("OPTIONS", "/api/v1/albums", "", "", "Origin", origin, "Access-Control-Request-Method", "GET")
		return res.StatusCode, res.Header.Get("Access-Control-Allow-Origin")
	}
	if c, _ := pre("https://app.example.com"); c != 403 {
		t.Fatalf("default should deny: %d", c)
	}
	var g struct{ Settings runtimecfg.Settings }
	_, b := e.do("GET", "/api/v1/settings/server", "", e.token)
	json.Unmarshal(b, &g)
	s := g.Settings
	s.CORSOrigins = []string{"https://app.example.com/"}
	body, _ := json.Marshal(s)
	if res, b := e.do("PUT", "/api/v1/settings/server", string(body), e.token); res.StatusCode != 200 {
		t.Fatalf("put settings %d %s", res.StatusCode, b)
	}
	if c, o := pre("https://app.example.com"); c != 204 || o != "https://app.example.com" {
		t.Fatalf("allowed origin: %d %q", c, o)
	}
	if c, _ := pre("https://evil.example"); c != 403 {
		t.Fatalf("other origin: %d", c)
	}
	res, _ := e.do("GET", "/api/v1/albums", "", e.token, "Origin", "https://app.example.com")
	if res.Header.Get("Access-Control-Allow-Credentials") != "" || res.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("cors headers on simple request")
	}
	s.CORSOrigins = []string{"javascript:alert(1)"}
	body, _ = json.Marshal(s)
	if res, _ := e.do("PUT", "/api/v1/settings/server", string(body), e.token); res.StatusCode != 400 {
		t.Errorf("bad origin accepted: %d", res.StatusCode)
	}
}

func TestSubsonicAuthAndEndpoints(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	_, alice, bob := e.accounts()
	key := e.mkCred(alice, `{"kind":"api_key","name":"sub","scopes":["read","playback","library-write"]}`)
	q := func(kv ...string) url.Values {
		v := url.Values{"apiKey": {key.Secret}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Add(kv[i], kv[i+1])
		}
		return v
	}
	if r := sub(e, "ping", q()); r["status"] != "ok" || r["openSubsonic"] != true {
		t.Fatalf("ping %v", r)
	}
	if r := sub(e, "ping", url.Values{"apiKey": {key.Secret}, "u": {"alice"}}); subCode(r) != 43 {
		t.Errorf("apiKey+u: %v", r)
	}
	if r := sub(e, "ping", url.Values{"apiKey": {"yk_bogus"}}); subCode(r) != 44 {
		t.Errorf("bad key: %v", r)
	}
	ext := sub(e, "getOpenSubsonicExtensions", q())
	if !strings.Contains(mustJSON(ext), "apiKeyAuthentication") || !strings.Contains(mustJSON(ext), "songLyrics") {
		t.Errorf("extensions %v", ext)
	}
	// login password must never work for t/s or p
	salt := "abc123"
	tok := md5hex("Little-Listens-42" + salt)
	if r := sub(e, "ping", url.Values{"u": {"alice"}, "t": {tok}, "s": {salt}}); subCode(r) != 40 {
		t.Fatalf("login password via t/s accepted: %v", r)
	}
	// app password: t+s works, p refused unless allow_plain
	ap := e.mkCred(alice, `{"kind":"app_password","name":"DSub"}`)
	if r := sub(e, "ping", url.Values{"u": {"alice"}, "t": {md5hex(ap.Secret + salt)}, "s": {salt}}); r["status"] != "ok" {
		t.Fatalf("app password t/s: %v", r)
	}
	if r := sub(e, "ping", url.Values{"u": {"bob"}, "t": {md5hex(ap.Secret + salt)}, "s": {salt}}); subCode(r) != 40 {
		t.Fatalf("app password for wrong user: %v", r)
	}
	if r := sub(e, "ping", url.Values{"u": {"alice"}, "p": {ap.Secret}}); subCode(r) != 41 {
		t.Fatalf("plaintext p accepted: %v", r)
	}
	pl := e.mkCred(alice, `{"kind":"app_password","name":"old","allow_plain":true}`)
	if r := sub(e, "ping", url.Values{"u": {"alice"}, "p": {"enc:" + hex.EncodeToString([]byte(pl.Secret))}}); r["status"] != "ok" {
		t.Fatalf("allowed plain: %v", r)
	}
	// app passwords don't work on /api
	if res, _ := e.do("GET", "/api/v1/albums", "", ap.Secret); res.StatusCode != 401 {
		t.Errorf("app password on /api: %d", res.StatusCode)
	}
	// browse
	if r := sub(e, "getSong", q("id", tr.ID)); r["status"] != "ok" {
		t.Fatalf("getSong %v", r)
	}
	if r := sub(e, "getAlbum", q("id", tr.AlbumID)); r["status"] != "ok" {
		t.Fatalf("getAlbum %v", r)
	}
	if r := sub(e, "getArtist", q("id", tr.Artists[0].ID)); r["status"] != "ok" {
		t.Fatalf("getArtist %v", r)
	}
	// playlists are per account with the real owner
	r := sub(e, "createPlaylist", q("name", "sub mix", "songId", tr.ID))
	p := r["playlist"].(map[string]any)
	if p["owner"] != "alice" || len(p["entry"].([]any)) != 1 {
		t.Fatalf("createPlaylist %v", r)
	}
	pid := p["id"].(string)
	if r := sub(e, "updatePlaylist", q("playlistId", pid, "name", "renamed", "songIndexToRemove", "0")); r["status"] != "ok" {
		t.Fatalf("update %v", r)
	}
	if r := sub(e, "getPlaylist", q("id", pid)); r["playlist"].(map[string]any)["name"] != "renamed" {
		t.Fatalf("getPlaylist %v", r)
	}
	if res, b := e.do("GET", "/api/v1/playlists", "", bob); strings.Contains(string(b), pid) || res.StatusCode != 200 {
		t.Fatal("playlist visible to bob")
	}
	if r := sub(e, "deletePlaylist", q("id", pid)); r["status"] != "ok" {
		t.Fatalf("delete %v", r)
	}
	// stars
	sub(e, "star", q("id", tr.ID, "albumId", tr.AlbumID))
	st := sub(e, "getStarred2", q())["starred2"].(map[string]any)
	if len(st["song"].([]any)) != 1 || len(st["album"].([]any)) != 1 {
		t.Fatalf("starred %v", st)
	}
	sub(e, "unstar", q("id", tr.ID))
	if st := sub(e, "getStarred2", q())["starred2"].(map[string]any); len(st["song"].([]any)) != 0 {
		t.Fatalf("unstar %v", st)
	}
	// scrobble + queue + user + lyrics
	if r := sub(e, "scrobble", q("id", tr.ID, "submission", "true")); r["status"] != "ok" {
		t.Fatalf("scrobble %v", r)
	}
	if r := sub(e, "savePlayQueue", q("id", tr.ID, "current", tr.ID, "position", "1234")); r["status"] != "ok" {
		t.Fatalf("save queue %v", r)
	}
	pq := sub(e, "getPlayQueue", q())["playQueue"].(map[string]any)
	if pq["position"].(float64) != 1234 || len(pq["entry"].([]any)) != 1 {
		t.Fatalf("queue %v", pq)
	}
	u := sub(e, "getUser", q("username", "alice"))["user"].(map[string]any)
	if u["adminRole"] != false || u["streamRole"] != true {
		t.Fatalf("user %v", u)
	}
	if r := sub(e, "getUser", q("username", "bob")); subCode(r) != 50 {
		t.Errorf("other user: %v", r)
	}
	if r := sub(e, "getLyricsBySongId", q("id", tr.ID)); r["status"] != "ok" || r["lyricsList"] == nil {
		t.Fatalf("lyrics %v", r)
	}
	// stream with the key (playback scope)
	res, _ := e.do("GET", "/rest/stream?"+q("id", tr.ID).Encode(), "", "")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "audio/") {
		t.Fatalf("stream %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	res, _ = e.do("POST", "/rest/stream.view", q("id", tr.ID).Encode(), "", "Content-Type", "application/x-www-form-urlencoded")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "audio/") {
		t.Fatalf("form-post stream %d", res.StatusCode)
	}
	// read-only key cannot stream or write
	ro := e.mkCred(alice, `{"kind":"api_key","name":"ro","scopes":["read"]}`)
	if r := sub(e, "stream", url.Values{"apiKey": {ro.Secret}, "id": {tr.ID}}); subCode(r) != 50 {
		t.Errorf("ro stream: %v", r)
	}
	if r := sub(e, "createPlaylist", url.Values{"apiKey": {ro.Secret}, "name": {"x"}}); subCode(r) != 50 {
		t.Errorf("ro write: %v", r)
	}
	// XML default + formPost
	res, b := e.do("POST", "/rest/ping.view", "apiKey="+url.QueryEscape(key.Secret), "", "Content-Type", "application/x-www-form-urlencoded")
	if res.StatusCode != 200 || !strings.Contains(string(b), `status="ok"`) {
		t.Fatalf("form post xml: %s", b)
	}
}

func TestSubsonicLockout(t *testing.T) {
	e := newEnv(t)
	e.accounts()
	for i := 0; i < 10; i++ {
		sub(e, "ping", url.Values{"u": {"alice"}, "t": {"00"}, "s": {"x"}})
	}
	ap := e.mkCredAs(t)
	if r := sub(e, "ping", url.Values{"u": {"alice"}, "t": {md5hex(ap + "s")}, "s": {"s"}}); subCode(r) != 40 {
		t.Fatalf("not locked: %v", r)
	}
}

func (e *env) mkCredAs(t *testing.T) string {
	res, b := e.do("POST", "/api/auth/login", `{"username":"alice","password":"Little-Listens-42"}`, "")
	var k struct{ Token string }
	json.Unmarshal(b, &k)
	if res.StatusCode != 200 {
		t.Fatal(string(b))
	}
	return e.mkCred(k.Token, `{"kind":"app_password","name":"x"}`).Secret
}

func md5hex(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
