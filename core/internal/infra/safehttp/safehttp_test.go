package safehttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPublic(t *testing.T) {
	for s, want := range map[string]bool{"8.8.8.8": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.1.1": false, "0.0.0.0": false, "::1": false, "fe80::1": false, "fd00::1": false,
		"::ffff:127.0.0.1": false, "2606:4700::1111": true} {
		if got := Public(netip.MustParseAddr(s)); got != want {
			t.Errorf("%s: %v", s, got)
		}
	}
}

func TestTransportBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := &http.Client{Transport: Transport()}
	_, err := c.Get(srv.URL)
	if err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback not blocked: %v", err)
	}
	AllowPrivate(true)
	defer AllowPrivate(false)
	if res, err := c.Get(srv.URL); err != nil {
		t.Fatal(err)
	} else {
		res.Body.Close()
	}
}
