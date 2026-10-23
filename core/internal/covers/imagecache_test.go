package covers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{200, 30, 30, 255})
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func imgServer(t *testing.T, hits *atomic.Int64, fail *atomic.Bool) *httptest.Server {
	big := pngBytes(800, 800)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail != nil && fail.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/cover.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(big)
		case "/html":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("<html>not an image</html>"))
		case "/text":
			w.Header().Set("Content-Type", "text/html")
			w.Write(big)
		case "/redir":
			http.Redirect(w, r, "https://evil.example/x.png", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testCache(t *testing.T, srv *httptest.Server) *Cache {
	return &Cache{Dir: t.TempDir(), AllowHosts: []string{"127.0.0.1"}, Client: srv.Client(), TTL: time.Hour, AnyPort: true}
}

func TestRemoteFetchOnceSingleflightAndPresets(t *testing.T) {
	var hits atomic.Int64
	srv := imgServer(t, &hits, nil)
	c := testCache(t, srv)
	u := srv.URL + "/cover.png"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Remote(context.Background(), u) }()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("upstream hit %d times", hits.Load())
	}
	p, err := c.Remote(context.Background(), u)
	if err != nil || hits.Load() != 1 {
		t.Fatalf("second fetch: %v %d", err, hits.Load())
	}
	// presets rendered in the background
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := c.MetaCached(p); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := c.MetaCached(p); !ok {
		t.Fatal("meta not prewarmed")
	}
	st := c.ImageStats()
	if st.Originals != 1 || st.Fetches != 1 || st.Hits < 1 || st.Files < 5 {
		t.Fatalf("stats %+v", st)
	}
	if k, ok := c.Register(u); !ok {
		t.Fatal("register")
	} else if p2, err := c.RemoteKey(context.Background(), k); err != nil || p2 != p {
		t.Fatal("by key")
	}
}

func TestRemoteRejects(t *testing.T) {
	var hits atomic.Int64
	srv := imgServer(t, &hits, nil)
	c := testCache(t, srv)
	for _, u := range []string{srv.URL + "/html", srv.URL + "/text", srv.URL + "/redir", "https://evil.example/a.png", "file:///etc/passwd", "https://user@127.0.0.1/x"} {
		if _, err := c.Remote(context.Background(), u); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	d := &Cache{Dir: t.TempDir()}
	for u, want := range map[string]bool{"https://p1.music.126.net/a.jpg": true, "http://y.gtimg.cn/x.jpg": false,
		"https://y.gtimg.cn/x.jpg": true, "https://music.126.net.evil.com/a": false, "https://p1.music.126.net:8443/a": false,
		"https://i.scdn.co/image/ab": true, "https://169.254.169.254/latest": false} {
		if got := d.HostAllowed(u); got != want {
			t.Errorf("%s: %v", u, got)
		}
	}
	if _, ok := c.URLFor("../../etc/passwd"); ok {
		t.Fatal("traversal key accepted")
	}
}

func TestStaleServedWhenUpstreamFails(t *testing.T) {
	var hits atomic.Int64
	var fail atomic.Bool
	srv := imgServer(t, &hits, &fail)
	c := testCache(t, srv)
	u := srv.URL + "/cover.png"
	p, err := c.Remote(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(p, old, old)
	fail.Store(true) // platform risk control
	p2, err := c.Remote(context.Background(), u)
	if err != nil || p2 != p {
		t.Fatalf("stale not served: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(p); err != nil {
		t.Fatal("stale copy lost after failed refresh")
	}
	if st := c.ImageStats(); st.StaleServed != 1 || st.Refreshes != 1 {
		t.Fatalf("stats %+v", st)
	}
	// alias keeps working without the platform
	c.Alias("artist:netease:1", u)
	if a, ok := c.Aliased("artist:netease:1"); !ok || a != u {
		t.Fatal("alias")
	}
}

func TestLRUEviction(t *testing.T) {
	c := &Cache{Dir: t.TempDir(), MaxBytes: 15_000}
	var srcs []string
	for i := 0; i < 6; i++ {
		p := filepath.Join(t.TempDir(), "c.png")
		os.WriteFile(p, pngBytes(300+i, 300), 0o644)
		srcs = append(srcs, p)
	}
	var first string
	for i, s := range srcs {
		out, _, err := c.Thumb(s, 300, "png")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = out
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.evictIfNeeded()
	st := c.ImageStats()
	if st.Bytes > c.MaxBytes || st.Evictions == 0 {
		t.Fatalf("not evicted: %+v", st)
	}
	if _, err := os.Stat(first); err == nil {
		t.Fatal("oldest thumb should be evicted first")
	}
	n, _ := c.ClearImages()
	if n == 0 || c.ImageStats().Files != 0 {
		t.Fatal("clear")
	}
	_ = strings.TrimSpace
}

func TestCachePublishAccountingAndConcurrentEviction(t *testing.T) {
	c := &Cache{Dir: t.TempDir(), MaxBytes: 8192}
	s := c.st() // snapshot before any files are published
	write := func(id string, size int) error {
		tmp := filepath.Join(c.Dir, id+".tmp")
		if err := os.WriteFile(tmp, bytes.Repeat([]byte{1}, size), 0o600); err != nil {
			return err
		}
		return c.publishImage(tmp, filepath.Join(c.Dir, id+".png"))
	}
	if err := write("same", 1000); err != nil {
		t.Fatal(err)
	}
	if err := write("same", 2000); err != nil {
		t.Fatal(err)
	}
	if n := s.bytes.Load(); n != 2000 {
		t.Fatalf("replacement double counted: %d", n)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "pending.tmp"), make([]byte, 16384), 0o600); err != nil {
		t.Fatal(err)
	}
	if stats := c.ImageStats(); stats.Bytes != 2000 || stats.Files != 1 {
		t.Fatalf("unpublished bytes counted: %+v", stats)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := write(fmt.Sprint(i), 3000); err != nil {
				t.Error(err)
			}
			c.evictIfNeeded()
			c.ImageStats()
		}()
	}
	wg.Wait()
	c.evictIfNeeded()
	stats := c.ImageStats()
	if stats.Bytes > c.MaxBytes || stats.Evictions == 0 || s.bytes.Load() != stats.Bytes {
		t.Fatalf("inconsistent eviction/accounting: %+v counted=%d", stats, s.bytes.Load())
	}
}
