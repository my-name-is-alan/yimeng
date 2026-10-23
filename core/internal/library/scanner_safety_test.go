package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanFailurePreservesExistingCatalog(t *testing.T) {
	s, root, run := setup(t)
	keep := filepath.Join(root, "keep.mp3")
	removed := filepath.Join(root, "removed.flac")
	copyFixture(t, "tagged.mp3", keep)
	copyFixture(t, "tagged.flac", removed)
	if j := run(""); j.Status != "completed" || j.Added != 2 {
		t.Fatalf("initial scan: %+v", j)
	}
	var before int
	if err := s.db.QueryRow(`SELECT count(*) FROM tracks`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	// Inject an update failure without removing or corrupting the audio file.
	if _, err := s.db.Exec(`CREATE TRIGGER fail_scan_update BEFORE UPDATE ON source_tracks
		WHEN NEW.last_scan_id <> OLD.last_scan_id
		BEGIN SELECT RAISE(ABORT, 'temporary scan update failure'); END`); err != nil {
		t.Fatal(err)
	}
	j := run("")
	if j.Status != "failed" || j.Failed != 1 || j.Removed != 0 || !strings.Contains(j.Error, "cleanup and merge skipped") {
		t.Fatalf("incomplete scan must fail without cleanup: %+v", j)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
	var sources, tracks int
	if err := s.db.QueryRow(`SELECT count(*) FROM source_tracks`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM tracks`).Scan(&tracks); err != nil {
		t.Fatal(err)
	}
	if sources != 2 || tracks != before {
		t.Fatalf("catalog changed after failed scan: sources=%d tracks=%d (before=%d)", sources, tracks, before)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_scan_update`); err != nil {
		t.Fatal(err)
	}
	if j := run(""); j.Status != "completed" || j.Removed != 1 || j.Unchanged != 1 {
		t.Fatalf("successful retry should reconcile deleted files: %+v", j)
	}
}

func TestCancelledEmptyScanDoesNotCleanCatalog(t *testing.T) {
	s, root, run := setup(t)
	path := filepath.Join(root, "song.mp3")
	copyFixture(t, "tagged.mp3", path)
	run("")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	s.opts.OnProgress = func(Job) {
		<-started
		cancel()
	}
	j, err := s.Start(ctx, StartRequest{})
	close(started)
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	j, err = s.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM source_tracks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if j.Status != "failed" || j.Removed != 0 || n != 1 {
		t.Fatalf("cancelled scan must preserve catalog: rows=%d job=%+v", n, j)
	}
}

func TestScanKeepsConfiguredRootSnapshot(t *testing.T) {
	s, root, run := setup(t)
	sub := filepath.Join(root, "album")
	path := filepath.Join(sub, "song.mp3")
	copyFixture(t, "tagged.mp3", path)
	changed := false
	s.opts.OnProgress = func(j Job) {
		if changed || j.Total == 0 || j.Processed != 0 {
			return
		}
		changed = true
		if err := s.SetLibraryRoots(context.Background(), []string{sub}); err != nil {
			t.Error(err)
		}
	}
	if j := run(sub); j.Status != "completed" || j.Added != 1 {
		t.Fatalf("scan: %+v", j)
	}
	var storedRoot string
	if err := s.db.QueryRow(`SELECT root FROM source_tracks WHERE path=?`, path).Scan(&storedRoot); err != nil {
		t.Fatal(err)
	}
	if !changed || storedRoot != root || s.Roots()[0] != sub {
		t.Fatalf("mixed root snapshots: changed=%v stored=%q current=%v", changed, storedRoot, s.Roots())
	}
}
