package secalert

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/migrations"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
)

func TestNewIPAndLockout(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(context.Background(), d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := &Store{DB: d}
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO users(id,username,pw_hash,created_at,updated_at) VALUES ('u1','alice','x',0,0)`); err != nil {
		t.Fatal(err)
	}
	s.LoginOK(ctx, "u1", "alice", "d1", "10.0.0.1")
	s.LoginOK(ctx, "u1", "alice", "d1", "10.0.0.1")
	if a, _, _ := s.List(ctx, "u1", false, 0); len(a) != 0 {
		t.Fatalf("first/known IP alerted: %v", a)
	}
	s.LoginOK(ctx, "u1", "alice", "d1", "198.51.100.7")
	a, _, _ := s.List(ctx, "u1", false, 0)
	if len(a) != 1 || a[0].Kind != KindNewIPLogin {
		t.Fatalf("new ip: %v", a)
	}
	s.LoginFailed(ctx, "alice", "1.2.3.4", true)
	s.LoginFailed(ctx, "alice", "1.2.3.4", true)
	n := 0
	all, _, _ := s.List(ctx, "", true, 0)
	for _, x := range all {
		if x.Kind == KindLockout {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("lockout alerts %d", n)
	}
	if o, _, _ := s.List(ctx, "u2", false, 0); len(o) != 0 {
		t.Fatalf("other user sees alerts: %v", o)
	}
}
