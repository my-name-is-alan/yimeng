package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func copyFixture(t *testing.T, name, dst string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "adapters", "local", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (*Scanner, string, func(string) Job) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "music")
	d, err := db.Open(filepath.Join(tmp, "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(context.Background(), d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := NewScanner(d, Options{Roots: []string{root}, CoverDir: filepath.Join(tmp, "covers")})
	run := func(r string) Job {
		t.Helper()
		j, err := s.Start(context.Background(), StartRequest{Root: r})
		if err != nil {
			t.Fatal(err)
		}
		s.Wait()
		j, err = s.Get(context.Background(), j.ID)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	return s, root, run
}

func TestScanIncremental(t *testing.T) {
	s, root, run := setup(t)
	album := filepath.Join(root, "Artist", "Album")
	copyFixture(t, "tagged.mp3", filepath.Join(album, "01.mp3"))
	copyFixture(t, "tagged.flac", filepath.Join(album, "02.flac"))
	copyFixture(t, "notags.mp3", filepath.Join(root, "Loose", "notags.mp3"))
	copyFixture(t, "cover.png", filepath.Join(root, "Loose", "Folder.PNG"))
	copyFixture(t, "tagged.mp3", filepath.Join(root, "@eaDir", "x.mp3")) // Synology thumbnails dir
	os.WriteFile(filepath.Join(album, "readme.txt"), []byte("x"), 0o644)

	j := run("")
	if j.Status != "completed" || j.Total != 3 || j.Added != 3 || j.Failed != 0 || j.Progress != 1 {
		t.Fatalf("first scan: %+v", j)
	}

	var title, artist, albumArtist, genre, cover, coverSrc string
	var trackNo, discNo, year int
	var dur int64
	err := s.db.QueryRow(`SELECT title, artist, album_artist, genre, track_no, disc_no, year, duration_ms, cover_path, cover_source
		FROM source_tracks WHERE path=?`, filepath.Join(album, "02.flac")).
		Scan(&title, &artist, &albumArtist, &genre, &trackNo, &discNo, &year, &dur, &cover, &coverSrc)
	if err != nil {
		t.Fatal(err)
	}
	if title != "梦里花" || artist != "测试艺人" || albumArtist != "Various Artists" || genre != "Pop" ||
		trackNo != 3 || discNo != 1 || year != 2024 || dur < 900 || dur > 1100 || coverSrc != "embedded" {
		t.Fatalf("row: %s %s %s %s %d %d %d %d %s", title, artist, albumArtist, genre, trackNo, discNo, year, dur, coverSrc)
	}
	if _, err := os.Stat(cover); err != nil {
		t.Fatalf("cover not extracted: %v", err)
	}
	s.db.QueryRow(`SELECT title, cover_source FROM source_tracks WHERE path=?`, filepath.Join(root, "Loose", "notags.mp3")).Scan(&title, &coverSrc)
	if title != "notags" || coverSrc != "folder" {
		t.Fatalf("notags: %s %s", title, coverSrc)
	}

	// Unchanged re-scan.
	j = run("")
	if j.Unchanged != 3 || j.Added+j.Updated+j.Removed != 0 {
		t.Fatalf("rescan: %+v", j)
	}

	// Modify one (mtime change), delete one, add one.
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(album, "01.mp3"), future, future)
	os.Remove(filepath.Join(root, "Loose", "notags.mp3"))
	copyFixture(t, "tagged.m4a", filepath.Join(album, "03.m4a"))
	j = run("")
	if j.Updated != 1 || j.Removed != 1 || j.Added != 1 || j.Unchanged != 1 {
		t.Fatalf("incremental: %+v", j)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM source_tracks`).Scan(&n)
	if n != 3 {
		t.Fatalf("rows=%d", n)
	}

	// Sub-root scan only removes within that subtree.
	j = run(album)
	if j.Total != 3 || j.Removed != 0 {
		t.Fatalf("subroot: %+v", j)
	}
	latest, err := s.Latest(context.Background())
	if err != nil || latest.ID != j.ID {
		t.Fatalf("latest %v %v", latest.ID, err)
	}
}

func TestStartValidation(t *testing.T) {
	s, root, _ := setup(t)
	if _, err := s.Start(context.Background(), StartRequest{Root: "/etc"}); !errors.Is(err, ErrUnknownRoot) {
		t.Fatalf("want ErrUnknownRoot, got %v", err)
	}
	if _, err := s.Start(context.Background(), StartRequest{Root: root + "/../.."}); !errors.Is(err, ErrUnknownRoot) {
		t.Fatalf("traversal: %v", err)
	}
	// Missing root directory -> job fails cleanly.
	j, err := s.Start(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	j, _ = s.Get(context.Background(), j.ID)
	if j.Status != "failed" || j.Error == "" {
		t.Fatalf("%+v", j)
	}
	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	s, _, _ := setup(t)
	s.db.Exec(`INSERT INTO scan_jobs(id, root, status, started_at) VALUES ('x','[]','running',1)`)
	if err := s.RecoverInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := s.Get(context.Background(), "x")
	if j.Status != "failed" {
		t.Fatal(j.Status)
	}
}
