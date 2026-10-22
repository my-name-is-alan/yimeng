package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebAccountsFlow(t *testing.T) {
	e := newEnv(t)
	res, body := e.do("GET", "/api/v1/auth/setup", "", "")
	if res.StatusCode != 200 || !strings.Contains(string(body), `"needs_setup":true`) {
		t.Fatal(res.StatusCode, string(body))
	}
	if res, _ := e.do("POST", "/api/auth/login", `{"username":"edy","password":"x"}`, ""); res.StatusCode != 401 {
		t.Fatal("login before setup", res.StatusCode)
	}
	if res, b := e.do("POST", "/api/auth/setup", `{"username":"edy","password":"123"}`, ""); res.StatusCode != 400 || !strings.Contains(string(b), "weak_password") {
		t.Fatal(res.StatusCode, string(b))
	}
	res, body = e.do("POST", "/api/auth/setup", `{"username":"edy","password":"Str0ng-Passw0rd"}`, "")
	var s struct {
		Token string `json:"token"`
		User  struct{ Role string }
	}
	json.Unmarshal(body, &s)
	if res.StatusCode != 201 || s.Token == "" || s.User.Role != "admin" || strings.Contains(string(body), "Passw0rd") {
		t.Fatal(res.StatusCode, string(body))
	}
	if res, _ := e.do("POST", "/api/auth/setup", `{"username":"eve","password":"Str0ng-Passw0rd"}`, ""); res.StatusCode != 409 {
		t.Fatal("setup must close after first account", res.StatusCode)
	}
	// admin APIs work with the login token
	if res, _ := e.do("GET", "/api/users", "", s.Token); res.StatusCode != 200 {
		t.Fatal("users", res.StatusCode)
	}
	res, body = e.do("POST", "/api/users", `{"username":"kid","password":"Little-Listens-42","role":"user"}`, s.Token)
	if res.StatusCode != 201 {
		t.Fatal(string(body))
	}
	res, body = e.do("POST", "/api/auth/login", `{"username":"kid","password":"Little-Listens-42"}`, "")
	var k struct{ Token string }
	json.Unmarshal(body, &k)
	if res.StatusCode != 200 {
		t.Fatal(string(body))
	}
	for _, p := range []string{"/api/users", "/api/pair/codes"} {
		m := "GET"
		if strings.HasSuffix(p, "codes") {
			m = "POST"
		}
		if res, _ := e.do(m, p, "", k.Token); res.StatusCode != 403 {
			t.Fatalf("%s for user role: %d", p, res.StatusCode)
		}
	}
	if res, _ := e.do("GET", "/api/albums", "", k.Token); res.StatusCode != 200 {
		t.Fatal("user can browse")
	}
	// change password, logout
	if res, _ := e.do("POST", "/api/auth/password", `{"old_password":"bad","new_password":"Little-Listens-43"}`, k.Token); res.StatusCode != 403 {
		t.Fatal("wrong old password", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/auth/password", `{"old_password":"Little-Listens-42","new_password":"Little-Listens-43"}`, k.Token); res.StatusCode != 204 {
		t.Fatal("change password", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/auth/logout", "", k.Token); res.StatusCode != 204 {
		t.Fatal("logout")
	}
	if res, _ := e.do("GET", "/api/albums", "", k.Token); res.StatusCode != 401 {
		t.Fatal("token must die on logout")
	}
	// lockout after repeated failures
	var last int
	for i := 0; i < 6; i++ {
		res, _ := e.do("POST", "/api/auth/login", `{"username":"edy","password":"Wrong-Passw0rd"}`, "")
		last = res.StatusCode
	}
	if last != 429 {
		t.Fatal("expected lockout, got", last)
	}
}
