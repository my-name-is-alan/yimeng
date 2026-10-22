package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func setup(t *testing.T) (*Service, *sql.DB) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lib.db")
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(context.Background(), d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO settings(key, value) VALUES ('marker','one')`)
	key := filepath.Join(dir, "master.key")
	os.WriteFile(key, bytes.Repeat([]byte{1}, 32), 0o600)
	return &Service{DB: d, DBPath: p, Dir: filepath.Join(dir, "backups"), KeyFile: key, Keep: 2}, d
}

func TestCreateListRotate(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	s.SetPolicy(Policy{Enabled: true, Keep: 1})
	for _, r := range []string{"scheduled", "auto", "pre-migrate", "manual"} {
		if _, err := s.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// only automatic backups rotate; manual / pre-migrate are kept (D54)
	list, _ := s.List()
	reasons := map[string]bool{}
	for _, b := range list {
		reasons[b.Reason] = true
	}
	if len(list) != 3 || !reasons["pre-migrate"] || !reasons["manual"] {
		t.Fatalf("rotation: %v", list)
	}
	if _, err := s.Path("../lib.db"); !errors.Is(err, ErrNotFound) {
		t.Fatal("traversal accepted")
	}
	f, _ := s.Path(list[0].Name)
	zr, err := zip.OpenReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, e := range zr.File {
		if e.Name == keyEntry {
			t.Fatal("stored backups must not include the master key")
		}
	}
}

func TestStageAndApply(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()
	var buf bytes.Buffer
	m, err := s.Snapshot(ctx, &buf, "export", true)
	if err != nil || !m.IncludesKey || m.SchemaVersion != db.Latest(migrations.FS) {
		t.Fatal(m, err)
	}
	d.Exec(`UPDATE settings SET value='two' WHERE key='marker'`)
	if _, err := s.Stage(ctx, bytes.NewReader([]byte("not a zip")), 1<<20, 99); !errors.Is(err, ErrInvalid) {
		t.Fatal("garbage accepted")
	}
	if _, err := s.Stage(ctx, bytes.NewReader(buf.Bytes()), 1<<30, 1); !errors.Is(err, ErrInvalid) {
		t.Fatal("newer schema accepted")
	}
	if _, err := s.Stage(ctx, bytes.NewReader(buf.Bytes()), 10, 99); !errors.Is(err, ErrInvalid) {
		t.Fatal("size limit")
	}
	if _, err := s.Stage(ctx, bytes.NewReader(buf.Bytes()), 1<<30, 99); err != nil {
		t.Fatal(err)
	}
	d.Close()
	ok, err := ApplyPending(s.DBPath)
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	d2, _ := db.Open(s.DBPath)
	defer d2.Close()
	var v string
	d2.QueryRow(`SELECT value FROM settings WHERE key='marker'`).Scan(&v)
	if v != "one" {
		t.Fatalf("restored value %q", v)
	}
	if ok, _ := ApplyPending(s.DBPath); ok {
		t.Fatal("applied twice")
	}
}

func TestAutoPolicyAndStageFile(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	now := time.Now()
	if !s.Due(now, 24*time.Hour) {
		t.Fatal("no backup yet: due")
	}
	s.SetPolicy(Policy{Enabled: false})
	if s.Due(now, 24*time.Hour) || s.Policy().Enabled {
		t.Fatal("disabled policy still due")
	}
	s.SetPolicy(Policy{Enabled: true, Keep: 99})
	if s.Policy().Keep != 60 {
		t.Fatalf("keep cap %d", s.Policy().Keep)
	}
	b, err := s.Create(ctx, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if s.Due(now, 24*time.Hour) || !s.Due(now.Add(25*time.Hour), 24*time.Hour) {
		t.Fatal("due after fresh auto backup")
	}
	m, err := s.StageFile(ctx, b.Name, 1<<30, 1000)
	if err != nil || m.Reason != "auto" {
		t.Fatal(m, err)
	}
	if _, err := os.Stat(PendingPath(s.DBPath)); err != nil {
		t.Fatal("not staged")
	}
	if _, err := s.StageFile(ctx, "../lib.db", 1<<30, 1000); !errors.Is(err, ErrNotFound) {
		t.Fatal("traversal staged")
	}
}
