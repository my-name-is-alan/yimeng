package gateway

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (e *env) raw(method, path, tok, ctype string, body []byte) *http.Response {
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", ctype)
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func TestMeAndAvatars(t *testing.T) {
	e := newEnv(t)
	admin, alice, bob := e.accounts()
	res, b := e.do("GET", "/api/me", "", alice)
	var me map[string]any
	json.Unmarshal(b, &me)
	if res.StatusCode != 200 || me["username"] != "alice" || me["nickname"] != "alice" || me["role"] != "user" || me["avatar_url"] != nil {
		t.Fatalf("me %d %s", res.StatusCode, b)
	}
	if res, b := e.do("PATCH", "/api/me", `{"nickname":"小爱"}`, alice); res.StatusCode != 200 || !strings.Contains(string(b), `"nickname":"小爱"`) {
		t.Fatalf("nickname %d %s", res.StatusCode, b)
	}
	if res, _ := e.do("PATCH", "/api/me", `{"nickname":"`+strings.Repeat("x", 40)+`"}`, alice); res.StatusCode != 400 {
		t.Fatal("long nickname")
	}
	// avatar limits
	if r := e.raw("PUT", "/api/me/avatar", alice, "image/png", []byte("not an image")); r.StatusCode != 415 {
		t.Fatalf("bad type %d", r.StatusCode)
	}
	if r := e.raw("PUT", "/api/me/avatar", alice, "image/png", make([]byte, 1<<20+10)); r.StatusCode != 413 {
		t.Fatalf("too large %d", r.StatusCode)
	}
	if r := e.raw("PUT", "/api/me/avatar", alice, "image/png", pngBytes(t, 4097, 1)); r.StatusCode != 400 {
		t.Fatalf("too wide %d", r.StatusCode)
	}
	img := pngBytes(t, 64, 64)
	r := e.raw("PUT", "/api/me/avatar", alice, "image/png", img)
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	var u struct {
		ID        string `json:"id"`
		AvatarURL string `json:"avatar_url"`
	}
	json.Unmarshal(body, &u)
	if r.StatusCode != 200 || !strings.HasPrefix(u.AvatarURL, "/api/v1/users/"+u.ID+"/avatar") {
		t.Fatalf("upload %d %s", r.StatusCode, body)
	}
	// visible to other accounts (household), via header or ?access_token
	g := e.raw("GET", "/api/users/"+u.ID+"/avatar", bob, "", nil)
	got, _ := io.ReadAll(g.Body)
	g.Body.Close()
	if g.StatusCode != 200 || g.Header.Get("Content-Type") != "image/png" || !bytes.Equal(got, img) {
		t.Fatalf("get avatar %d %s", g.StatusCode, g.Header.Get("Content-Type"))
	}
	// only admins set other users' avatars
	if r := e.raw("PUT", "/api/users/"+u.ID+"/avatar", bob, "image/png", img); r.StatusCode != 403 {
		t.Fatalf("bob set alice avatar %d", r.StatusCode)
	}
	if r := e.raw("PUT", "/api/users/"+u.ID+"/avatar", admin, "image/png", img); r.StatusCode != 200 {
		t.Fatalf("admin set avatar %d", r.StatusCode)
	}
	if _, b := e.do("GET", "/api/users", "", admin); !strings.Contains(string(b), "/avatar?v=") {
		t.Fatalf("users list lacks avatar_url %s", b)
	}
	if res, _ := e.do("DELETE", "/api/me/avatar", "", alice); res.StatusCode != 204 {
		t.Fatal("delete")
	}
	if res, _ := e.do("GET", "/api/users/"+u.ID+"/avatar", "", bob); res.StatusCode != 404 {
		t.Fatal("still there")
	}
}
