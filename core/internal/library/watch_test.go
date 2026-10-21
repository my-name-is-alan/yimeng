package library

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func waitRows(t *testing.T, s *Scanner, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		s.db.QueryRow(`SELECT count(*) FROM source_tracks`).Scan(&n)
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rows=%d want %d", n, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWatchFsnotifyDebounce(t *testing.T) {
	s, root, run := setup(t)
	copyFixture(t, "tagged.mp3", filepath.Join(root, "a", "1.mp3"))
	run("")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Watch(ctx, WatchOptions{Mode: "fsnotify", Debounce: 150 * time.Millisecond}); close(done) }()
	time.Sleep(200 * time.Millisecond)
	// new subdirectory + files: must be picked up via recursive add.
	copyFixture(t, "tagged.flac", filepath.Join(root, "b", "c", "2.flac"))
	time.Sleep(100 * time.Millisecond)
	copyFixture(t, "tagged.m4a", filepath.Join(root, "b", "c", "3.m4a"))
	waitRows(t, s, 3)
	cancel()
	<-done
	s.Wait()
}

func TestWatchPollFallback(t *testing.T) {
	s, root, run := setup(t)
	copyFixture(t, "tagged.mp3", filepath.Join(root, "1.mp3"))
	run("")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Watch(ctx, WatchOptions{Mode: "poll", Debounce: 10 * time.Millisecond, RescanInterval: 100 * time.Millisecond})
		close(done)
	}()
	copyFixture(t, "tagged.flac", filepath.Join(root, "2.flac"))
	waitRows(t, s, 2)
	cancel()
	<-done
	s.Wait()
}

func TestIgnoredPath(t *testing.T) {
	if !ignoredPath("/m/@eaDir/x.mp3") || !ignoredPath("/m/.hidden/x") || ignoredPath("/m/a/b.mp3") {
		t.Fatal()
	}
}
