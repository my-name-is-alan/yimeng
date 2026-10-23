package gateway

import "testing"

func TestAPIV1Alias(t *testing.T) {
	e := newEnv(t)
	res, body := e.do("GET", "/api/v1/devices/me", "", e.token)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	res2, _ := e.do("GET", "/api/devices/me", "", e.token)
	if res2.StatusCode != 200 {
		t.Fatal(res2.StatusCode)
	}
}
