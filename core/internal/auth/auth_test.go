package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func newSvc(t *testing.T) *Service {
	d, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(context.Background(), d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return New(d, 5*time.Minute, nil)
}

func TestPairFlow(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _, err := s.NewCode()
	if err != nil || len(code) != 9 || code[4] != '-' {
		t.Fatalf("%q %v", code, err)
	}
	dev, tok, err := s.Pair(ctx, "1.1.1.1", strings.ToLower(strings.ReplaceAll(code, "-", "")), "Mac", "desktop")
	if err != nil || !strings.HasPrefix(tok, "ymd_") {
		t.Fatal(err)
	}
	// single use
	if _, _, err := s.Pair(ctx, "1.1.1.1", code, "x", ""); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("code reused")
	}
	// token hashed at rest
	var stored string
	s.db.QueryRow(`SELECT token_hash FROM players WHERE id=?`, dev.ID).Scan(&stored)
	if stored == tok || stored != HashToken(tok) {
		t.Fatal("token not hashed")
	}
	got, err := s.Authenticate(ctx, tok)
	if err != nil || got.ID != dev.ID {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok+"x"); err == nil {
		t.Fatal("bad token accepted")
	}
	devs, _ := s.ListDevices(ctx)
	if len(devs) != 1 {
		t.Fatal(devs)
	}
	if err := s.Revoke(ctx, dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok); err == nil {
		t.Fatal("revoked token accepted")
	}
}

func TestCodeExpiry(t *testing.T) {
	s := newSvc(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	code, _, _ := s.NewCode()
	now = now.Add(5*time.Minute + time.Second)
	if _, _, err := s.Pair(context.Background(), "ip", code, "x", ""); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("expired code accepted")
	}
}

func TestRateLimit(t *testing.T) {
	s := newSvc(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	code, _, _ := s.NewCode()
	for i := 0; i < ipFailLimit; i++ {
		s.Pair(context.Background(), "evil", "AAAA-AAAA", "x", "")
	}
	// even the right code is refused while limited
	if _, _, err := s.Pair(context.Background(), "evil", code, "x", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want rate limit, got %v", err)
	}
	// other IP unaffected
	if _, _, err := s.Pair(context.Background(), "good", code, "x", ""); err != nil {
		t.Fatal(err)
	}
	// escalated lockout: still locked after the 1-minute window ...
	now = now.Add(failWindow + time.Second)
	if _, _, err := s.Pair(context.Background(), "evil", "AAAA-AAAA", "x", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("lockout should persist: %v", err)
	}
	// ... and released after 15 minutes
	now = now.Add(lockout)
	if _, _, err := s.Pair(context.Background(), "evil", "AAAA-AAAA", "x", ""); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("lockout should expire: %v", err)
	}
	for i := 0; i < requestLimit; i++ {
		if _, _, err := s.RequestCode("r"); err != nil && !errors.Is(err, ErrTooManyCodes) {
			t.Fatal(err)
		}
	}
	if _, _, err := s.RequestCode("r"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("request limit")
	}
}

func TestGlobalLimitAndMaxCodes(t *testing.T) {
	s := newSvc(t)
	for i := 0; i < globalFailLimit; i++ {
		s.Pair(context.Background(), string(rune('a'+i%26))+string(rune(i)), "BAD", "x", "")
	}
	if _, _, err := s.Pair(context.Background(), "fresh", "BAD", "x", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatal("global limit")
	}
	for i := 0; i < maxActiveCodes; i++ {
		s.NewCode()
	}
	if _, _, err := s.NewCode(); !errors.Is(err, ErrTooManyCodes) {
		t.Fatal("max codes")
	}
}
