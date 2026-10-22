package gateway

import (
	"strings"
	"testing"
)

// D58: the stream verdict corrects a listing's optimistic entitlement.
func TestEntitlementFollowsStreamVerdict(t *testing.T) {
	e := onlineReady(t)
	if _, b := e.do("GET", "/api/tracks/gw:paid", "", e.token); !strings.Contains(string(b), `"entitlement":"preview"`) {
		t.Fatalf("listing hint %s", b)
	}
	if res, b := e.do("GET", "/api/stream/gw:paid", "", e.token); res.StatusCode != 403 || !strings.Contains(string(b), "not_entitled") {
		t.Fatalf("stream %d %s", res.StatusCode, b)
	}
	_, b := e.do("GET", "/api/tracks/gw:paid", "", e.token)
	if !strings.Contains(string(b), `"entitlement":"unavailable"`) || !strings.Contains(string(b), `"playable":false`) {
		t.Fatalf("after verdict %s", b)
	}
}
