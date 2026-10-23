package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIPTrusted(t *testing.T) {
	tp, err := ParseTrustedProxies([]string{"10.0.0.0/8", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	h := realIP(tp, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen", clientIP(r))
	}))
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.RemoteAddr = "10.1.2.3:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.1.2.3")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Seen"); got != "203.0.113.9" {
		t.Fatalf("got %q", got)
	}
	// untrusted peer: ignore XFF
	req2 := httptest.NewRequest("GET", "/healthz", nil)
	req2.RemoteAddr = "203.0.113.1:1"
	req2.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if got := rec2.Header().Get("X-Seen"); got != "203.0.113.1" {
		t.Fatalf("untrusted got %q", got)
	}
}

func TestRateLimit(t *testing.T) {
	rl := NewRateLimits(1, 1, 0) // 1 rps, burst 1
	var hits int
	h := rateLimit(rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.RemoteAddr = "192.0.2.1:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 429 {
		t.Fatalf("want 429 got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	// healthz exempt
	hr := httptest.NewRequest("GET", "/healthz", nil)
	hr.RemoteAddr = "192.0.2.1:1"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, hr)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}

func TestParseTrustedProxiesBad(t *testing.T) {
	if _, err := ParseTrustedProxies([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected error")
	}
}
