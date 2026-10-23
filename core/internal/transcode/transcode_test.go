package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseProfile(t *testing.T) {
	ok := map[[2]string]string{
		{"", ""}: "original", {"original", ""}: "original", {"mp3", ""}: "mp3-320", {"mp3", "128"}: "mp3-128",
		{"aac", ""}: "aac-256", {"opus", "96"}: "opus-96", {"flac", ""}: "flac",
	}
	for in, want := range ok {
		p, err := ParseProfile(in[0], in[1])
		if err != nil || p.Key() != want {
			t.Errorf("%v: %v %v", in, p.Key(), err)
		}
	}
	for _, in := range [][2]string{{"wav", ""}, {"mp3", "999"}, {"mp3", "128k"}, {"flac", "320"},
		{"original", "128"}, {"-f", ""}, {"mp3", "-1"}, {"mp3;rm", ""}} {
		if _, err := ParseProfile(in[0], in[1]); !errors.Is(err, ErrBadProfile) {
			t.Errorf("%v accepted", in)
		}
	}
	a := Profile{Format: "mp3", BitrateKbps: 192}.args("/m/-x.flac", "/c/o.part")
	if !strings.Contains(strings.Join(a, " "), "-i file:/m/-x.flac") || a[len(a)-1] != "file:/c/o.part" {
		t.Fatal(a)
	}
}

func TestNilTranscoder(t *testing.T) {
	var tr *Transcoder
	if tr.Supports(Profile{Format: "mp3", BitrateKbps: 320}) || len(tr.Formats()) != 1 {
		t.Fatal("nil transcoder must be original-only")
	}
	if _, err := tr.Open(context.Background(), Source{}, Profile{Format: "mp3"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if tr, err := New(Options{FFmpeg: "off"}); tr != nil || err != nil {
		t.Fatal("off must disable")
	}
	if _, err := New(Options{FFmpeg: "/nonexistent/ffmpeg"}); err == nil {
		t.Fatal("explicit missing ffmpeg must error")
	}
}

func fixture(t *testing.T, maxBytes int64) (*Transcoder, Source) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "-in put.wav") // leading dash / space must be harmless
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=3", "-ac", "2", "-ar", "44100", "file:"+in).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	st, _ := os.Stat(in)
	tr, err := New(Options{CacheDir: filepath.Join(dir, "cache"), MaxBytes: maxBytes, Concurrency: 2})
	if err != nil || tr == nil {
		t.Fatal(err)
	}
	return tr, Source{TrackID: "t1", Path: in, Size: st.Size(), Mtime: st.ModTime().UnixNano()}
}

func TestTranscodeProgressiveThenCached(t *testing.T) {
	tr, src := fixture(t, 0)
	p := Profile{Format: "mp3", BitrateKbps: 128}
	if !tr.Supports(p) {
		t.Skip("no libmp3lame")
	}
	if _, ok := tr.Cached(src, p); ok {
		t.Fatal("cold cache")
	}
	var wg sync.WaitGroup
	outs := make([][]byte, 3)
	for i := range outs { // concurrent requests share one ffmpeg run
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc, err := tr.Open(context.Background(), src, p)
			if err != nil {
				t.Error(err)
				return
			}
			defer rc.Close()
			outs[i], err = io.ReadAll(rc)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(outs[0]) < 10000 || string(outs[0]) != string(outs[1]) || string(outs[1]) != string(outs[2]) {
		t.Fatalf("sizes %d %d %d", len(outs[0]), len(outs[1]), len(outs[2]))
	}
	if !strings.HasPrefix(string(outs[0]), "ID3") && outs[0][0] != 0xFF {
		t.Fatal("not mp3")
	}
	path, ok := tr.Cached(src, p)
	if !ok {
		t.Fatal("not cached")
	}
	if b, _ := os.ReadFile(path); string(b) != string(outs[0]) {
		t.Fatal("cache differs from streamed bytes")
	}
	// a changed source (mtime) misses the cache
	src2 := src
	src2.Mtime++
	if _, ok := tr.Cached(src2, p); ok {
		t.Fatal("stale cache hit")
	}
}

func TestTranscodeCancelOnLastReader(t *testing.T) {
	tr, src := fixture(t, 0)
	p := Profile{Format: "flac"}
	rc, err := tr.Open(context.Background(), src, p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rc, buf); err != nil || string(buf[:4]) != "fLaC" {
		t.Fatalf("%q %v", buf, err)
	}
	rc.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tr.mu.Lock()
		n := len(tr.jobs)
		tr.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job not finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ents, _ := os.ReadDir(filepath.Join(tr.dir, "tmp")); len(ents) != 0 {
		t.Fatal("part file left behind")
	}
}

func TestEvictLRU(t *testing.T) {
	tr, src := fixture(t, 1)
	tr.max.Store(1 << 30)
	read := func(p Profile) {
		rc, err := tr.Open(context.Background(), src, p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 200; i++ {
			if _, ok := tr.Cached(src, p); ok {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("not cached", p.Key())
	}
	a, b := Profile{Format: "flac"}, Profile{Format: "aac", BitrateKbps: 128}
	read(a)
	read(b)
	old := time.Now().Add(-time.Hour)
	pa, _ := tr.Cached(src, a)
	os.Chtimes(pa, old, old) // a is least recently used
	pb, _ := tr.Cached(src, b)
	sb, _ := os.Stat(pb)
	tr.max.Store(sb.Size())
	tr.evict()
	if _, err := os.Stat(pa); !os.IsNotExist(err) {
		t.Fatal("LRU entry not evicted")
	}
	if _, err := os.Stat(pb); err != nil {
		t.Fatal("recent entry evicted")
	}
}

func TestConcurrencyLimitHonoursContext(t *testing.T) {
	tr, src := fixture(t, 0)
	tr.sem = make(chan struct{}, 1)
	tr.sem <- struct{}{} // slot taken
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := tr.Open(ctx, src, Profile{Format: "flac"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// Exercise the file sharing contract without requiring ffmpeg or a timing race.
func TestProgressiveFileAllowsRenameAndDelete(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "audio.part")
	final := filepath.Join(dir, "audio.mp3")
	want := []byte("audio bytes still readable after rename")
	if err := os.WriteFile(part, want, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openTranscodeFile(part)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(part, final); err != nil {
		t.Fatalf("rename with progressive reader: %v", err)
	}
	got, err := io.ReadAll(f)
	if err != nil || string(got) != string(want) {
		t.Fatalf("drain renamed file: %q %v", got, err)
	}
	if err := os.Remove(final); err != nil {
		t.Fatalf("delete with progressive reader: %v", err)
	}
}

func TestFailedCachePublishRemovesPartialFile(t *testing.T) {
	tr, src := fixture(t, 0)
	p := Profile{Format: "flac"}
	final := tr.finalPath(Key(src, p), p)
	// A directory in place of the cache file makes publication fail after ffmpeg succeeds.
	if err := os.MkdirAll(final, 0o750); err != nil {
		t.Fatal(err)
	}
	rc, err := tr.Open(context.Background(), src, p)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, rc)
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if readErr == nil {
		t.Fatal("cache publication failure was reported as clean EOF")
	}
	entries, err := os.ReadDir(filepath.Join(tr.dir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed publication left temporary files: %v", entries)
	}
}
