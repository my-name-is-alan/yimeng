package covers

// Image cache (D60): remote album covers and artist images are fetched once
// from an allow-listed CDN, kept on disk with pre-rendered sizes and
// blurhash/colours, evicted LRU under a byte cap, refreshed after a TTL in the
// background, and served stale when the platform is unreachable (risk control).

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultHosts are the image CDNs Core may fetch from (suffix match).
var DefaultHosts = []string{
	"music.126.net",                                                             // NetEase
	"y.gtimg.cn", "qpic.cn", "qpic.y.qq.com", "gtimg.cn", "music-file.y.qq.com", // QQ Music
	"kugou.com", "kgimg.com", // Kugou
	"i.scdn.co", "mosaic.scdn.co", "spotifycdn.com", // Spotify (metadata)
	"coverartarchive.org", "archive.org", // MusicBrainz Cover Art Archive
}

// PresetSizes are rendered right after an image is fetched.
var PresetSizes = []int{120, 300, 600, 1200}

const (
	maxRemoteBytes = 12 << 20
	maxPixels      = 40_000_000
	maxEdge        = 10000
)

var (
	ErrHostNotAllowed = errors.New("image host not allowed")
	keyRe             = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Stats is the admin view of the image cache.
type Stats struct {
	Dir         string  `json:"-"`
	Files       int     `json:"files"`
	Bytes       int64   `json:"bytes"`
	MaxBytes    int64   `json:"max_bytes"` // 0 = unlimited
	Originals   int     `json:"originals"`
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	Fetches     int64   `json:"fetches"`
	FetchErrors int64   `json:"fetch_errors"`
	StaleServed int64   `json:"stale_served"`
	Refreshes   int64   `json:"refreshes"`
	Evictions   int64   `json:"evictions"`
	HitRate     float64 `json:"hit_rate"`
	TTLHours    int     `json:"ttl_hours"`
}

type counters struct {
	hits, misses, fetches, fetchErrs, stale, refreshes, evictions atomic.Int64
}

type flight struct {
	wg   sync.WaitGroup
	path string
	err  error
}

type state struct {
	once    sync.Once
	mu      sync.Mutex
	flights map[string]*flight
	access  map[string]time.Time
	bytes   atomic.Int64
	evictMu sync.Mutex
	prewarm chan string
	c       counters
}

func (c *Cache) st() *state {
	c.stOnce.Do(func() {
		c.state = &state{flights: map[string]*flight{}, access: map[string]time.Time{}, prewarm: make(chan string, 128)}
		c.state.bytes.Store(dirSize(c.Dir))
		go c.prewarmLoop()
	})
	return c.state
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// HostAllowed reports whether u is https on an allow-listed host.
func (c *Cache) HostAllowed(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.User != nil || p.Host == "" {
		return false
	}
	h := strings.ToLower(p.Hostname())
	if !c.AnyPort && p.Port() != "" && p.Port() != "443" {
		return false
	}
	hosts := c.AllowHosts
	if len(hosts) == 0 {
		hosts = DefaultHosts
	}
	for _, s := range hosts {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && (h == s || strings.HasSuffix(h, "."+s)) {
			return true
		}
	}
	return false
}

func normURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "http://") {
		u = "https://" + u[len("http://"):]
	}
	return u
}

// Key is the cache key of a remote URL (hex, 32 chars).
func Key(u string) string {
	h := sha1.Sum([]byte(normURL(u)))
	return hex.EncodeToString(h[:16])
}

func (c *Cache) remotePath(key string) string {
	return filepath.Join(c.Dir, "remote", key[:2], key+".img")
}

// Register remembers u so it can later be served by key (GET /api/images/{key})
// even if the platform is unreachable. Only allow-listed hosts register.
func (c *Cache) Register(u string) (string, bool) {
	u = normURL(u)
	if !c.HostAllowed(u) {
		return "", false
	}
	key := Key(u)
	p := filepath.Join(c.Dir, "remote", key[:2], key+".url")
	if _, err := os.Stat(p); err != nil {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(u), 0o644)
	}
	return key, true
}

// URLFor returns the registered URL of key.
func (c *Cache) URLFor(key string) (string, bool) {
	if !keyRe.MatchString(key) {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(c.Dir, "remote", key[:2], key+".url"))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// RemoteKey serves a registered image by key.
func (c *Cache) RemoteKey(ctx context.Context, key string) (string, error) {
	u, ok := c.URLFor(key)
	if !ok {
		return "", ErrRemote
	}
	return c.Remote(ctx, u)
}

// Alias binds a stable id (e.g. "artist:netease:123") to an image URL so the
// image stays servable when the platform API is rate-limited.
func (c *Cache) Alias(id, u string) {
	if _, ok := c.Register(u); !ok {
		return
	}
	k := Key("alias:" + id)
	p := filepath.Join(c.Dir, "alias", k[:2], k)
	if b, err := os.ReadFile(p); err == nil && string(b) == normURL(u) {
		return
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(normURL(u)), 0o644)
}

// Aliased returns the URL last bound to id.
func (c *Cache) Aliased(id string) (string, bool) {
	k := Key("alias:" + id)
	b, err := os.ReadFile(filepath.Join(c.Dir, "alias", k[:2], k))
	return string(b), err == nil && len(b) > 0
}

// Remote returns the local copy of an allow-listed remote image, fetching it
// once (single-flight). Expired copies are served while a refresh runs; a
// failed fetch falls back to the stale copy.
func (c *Cache) Remote(ctx context.Context, u string) (string, error) {
	u = normURL(u)
	if !c.HostAllowed(u) {
		return "", ErrHostNotAllowed
	}
	s := c.st()
	key, _ := c.Register(u)
	out := c.remotePath(key)
	if fi, err := os.Stat(out); err == nil {
		s.c.hits.Add(1)
		c.touch(out)
		if c.TTL > 0 && time.Since(fi.ModTime()) > c.TTL {
			s.c.stale.Add(1)
			go c.fetch(context.Background(), u, out, true)
		}
		return out, nil
	}
	s.c.misses.Add(1)
	return c.fetch(ctx, u, out, false)
}

func (c *Cache) fetch(ctx context.Context, u, out string, refresh bool) (string, error) {
	s := c.st()
	s.mu.Lock()
	if f, ok := s.flights[out]; ok {
		s.mu.Unlock()
		if refresh {
			return out, nil
		}
		f.wg.Wait()
		return f.path, f.err
	}
	f := &flight{}
	f.wg.Add(1)
	s.flights[out] = f
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.flights, out)
		s.mu.Unlock()
		f.wg.Done()
	}()
	if refresh {
		s.c.refreshes.Add(1)
	}
	s.c.fetches.Add(1)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	f.path, f.err = c.download(ctx, u, out)
	if f.err != nil {
		s.c.fetchErrs.Add(1)
		if _, err := os.Stat(out); err == nil { // keep serving the old copy
			f.path, f.err = out, nil
		}
		return f.path, f.err
	}
	select {
	case s.prewarm <- out:
	default:
	}
	c.evictIfNeeded()
	return f.path, nil
}

func (c *Cache) client() *http.Client {
	base := remoteClient
	if c.Client != nil {
		base = c.Client
	}
	cl := *base
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !c.HostAllowed(req.URL.String()) {
			return ErrHostNotAllowed
		}
		return nil
	}
	return &cl
}

var imageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true, "image/jpg": true}

func (c *Cache) download(ctx context.Context, u, out string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", ErrRemote
	}
	req.Header.Set("User-Agent", "yimeng-core/1 (image cache)")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", ErrRemote
	}
	defer resp.Body.Close()
	ct, _, _ := strings.Cut(strings.ToLower(resp.Header.Get("Content-Type")), ";")
	if resp.StatusCode != http.StatusOK || !imageTypes[strings.TrimSpace(ct)] || resp.ContentLength > maxRemoteBytes {
		return "", ErrRemote
	}
	os.MkdirAll(filepath.Dir(out), 0o755)
	tmp := fmt.Sprintf("%s.%d.tmp", out, time.Now().UnixNano())
	fh, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(fh, io.LimitReader(resp.Body, maxRemoteBytes+1))
	fh.Close()
	if err != nil || n == 0 || n > maxRemoteBytes || !decodable(tmp) {
		os.Remove(tmp)
		return "", ErrRemote
	}
	var old int64
	if fi, err := os.Stat(out); err == nil {
		old = fi.Size()
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	c.st().bytes.Add(n - old)
	c.touch(out)
	return out, nil
}

// decodable checks dimensions first (decompression bombs), then decodes.
func decodable(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxEdge || cfg.Height > maxEdge || cfg.Width*cfg.Height > maxPixels {
		return false
	}
	_, err = decodeFile(p)
	return err == nil
}

func (c *Cache) prewarmLoop() {
	for p := range c.state.prewarm {
		for _, sz := range PresetSizes {
			c.Thumb(p, sz, "jpeg")
		}
		c.Meta(p)
		c.evictIfNeeded()
	}
}

func (c *Cache) touch(p string) {
	s := c.st()
	s.mu.Lock()
	s.access[p] = time.Now()
	if len(s.access) > 200_000 {
		s.access = map[string]time.Time{}
	}
	s.mu.Unlock()
}

func (c *Cache) noteWrite(p string) {
	if fi, err := os.Stat(p); err == nil {
		c.st().bytes.Add(fi.Size())
	}
	c.touch(p)
}

// evictIfNeeded removes least-recently-used files until the cache is under
// 90 % of MaxBytes. URL and alias records are tiny and kept.
func (c *Cache) evictIfNeeded() {
	s := c.st()
	if c.MaxBytes <= 0 || s.bytes.Load() <= c.MaxBytes {
		return
	}
	if !s.evictMu.TryLock() {
		return
	}
	defer s.evictMu.Unlock()
	type ent struct {
		p    string
		size int64
		at   time.Time
	}
	var all []ent
	var total int64
	filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		total += fi.Size()
		if strings.HasSuffix(p, ".url") || strings.Contains(p, string(filepath.Separator)+"alias"+string(filepath.Separator)) || strings.HasSuffix(p, ".tmp") {
			return nil
		}
		at := fi.ModTime()
		s.mu.Lock()
		if t, ok := s.access[p]; ok && t.After(at) {
			at = t
		}
		s.mu.Unlock()
		all = append(all, ent{p, fi.Size(), at})
		return nil
	})
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	target := c.MaxBytes * 9 / 10
	for _, e := range all {
		if total <= target {
			break
		}
		if os.Remove(e.p) == nil {
			total -= e.size
			s.c.evictions.Add(1)
			s.mu.Lock()
			delete(s.access, e.p)
			s.mu.Unlock()
		}
	}
	s.bytes.Store(total)
}

// ImageStats walks the cache and returns counters.
func (c *Cache) ImageStats() Stats {
	s := c.st()
	st := Stats{MaxBytes: c.MaxBytes, TTLHours: int(c.TTL / time.Hour)}
	filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			st.Files++
			st.Bytes += fi.Size()
			if strings.HasSuffix(p, ".img") {
				st.Originals++
			}
		}
		return nil
	})
	s.bytes.Store(st.Bytes)
	st.Hits, st.Misses = s.c.hits.Load(), s.c.misses.Load()
	st.Fetches, st.FetchErrors = s.c.fetches.Load(), s.c.fetchErrs.Load()
	st.StaleServed, st.Refreshes, st.Evictions = s.c.stale.Load(), s.c.refreshes.Load(), s.c.evictions.Load()
	if t := st.Hits + st.Misses; t > 0 {
		st.HitRate = float64(st.Hits) / float64(t)
	}
	return st
}

// ClearImages deletes every cached image (originals, sizes, meta) but keeps
// URL/alias records so images can be fetched again on demand.
func (c *Cache) ClearImages() (int, error) {
	s := c.st()
	s.evictMu.Lock()
	defer s.evictMu.Unlock()
	n := 0
	err := filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".url") || strings.Contains(p, string(filepath.Separator)+"alias"+string(filepath.Separator)) {
			return nil
		}
		if os.Remove(p) == nil {
			n++
		}
		return nil
	})
	s.bytes.Store(dirSize(c.Dir))
	s.mu.Lock()
	s.access = map[string]time.Time{}
	s.mu.Unlock()
	return n, err
}
