package db

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func TestMigrateEmbeddedIdempotent(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "sub", "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	got, err := Migrate(ctx, d, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0] != 1 {
		t.Fatalf("applied = %v", got)
	}
	again, err := Migrate(ctx, d, migrations.FS)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v err %v", again, err)
	}
	for _, tbl := range []string{"artists", "albums", "tracks", "track_artists", "album_artists", "credits",
		"source_tracks", "track_links", "players", "sessions", "queue_items", "vault_entries", "settings", "scan_jobs"} {
		var n int
		if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing", tbl)
		}
	}
	var fk int
	d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Error("foreign_keys not enabled")
	}
}

func TestMigrateOrderAndFailure(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fsys := fstest.MapFS{
		"0002_b.sql": {Data: []byte(`ALTER TABLE a ADD COLUMN y INT;`)},
		"0001_a.sql": {Data: []byte(`CREATE TABLE a(x INT);`)},
	}
	got, err := Migrate(ctx, d, fsys)
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v err %v", got, err)
	}
	fsys["0003_bad.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE b(; garbage`)}
	if _, err := Migrate(ctx, d, fsys); err == nil {
		t.Fatal("expected error")
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n)
	if n != 2 {
		t.Fatalf("bad migration recorded: %d", n)
	}
	if _, err := loadMigrations(fstest.MapFS{"x.sql": {Data: nil}}); err == nil {
		t.Fatal("expected name error")
	}
}
