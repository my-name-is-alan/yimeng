package online

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Platform cache (D44). Every NetEase/QQ/Kugou catalog call goes through
// cached(): one SQLite-backed store shared by all apps and the web UI.
//
//   - per-kind TTLs (Policies), then a stale window: a stale entry is served
//     immediately and refreshed in the background (stale-while-revalidate);
//   - request coalescing: concurrent misses for one key = one upstream call;
//   - negative caching for "not available" answers (short TTL);
//   - when the platform is in risk-control cooldown or errors, a stale copy is
//     served instead of failing (the per-platform limiter + exponential
//     cooldown live in Client);
//   - size cap with LRU eviction (last_access), Stats and Clear for settings.
//
// Stream URLs are not cached here (Service leases honour their expiry).

// Policy is the freshness policy of one kind of entry.
type Policy struct {
	TTL      time.Duration // fresh for
	Stale    time.Duration // then served stale (and refreshed) for
	Negative time.Duration // "not available" answers are remembered for
}

// TTLs kept for callers that reference them directly.
const (
	TTLTrack    = 30 * 24 * time.Hour
	TTLAlbum    = 14 * 24 * time.Hour
	TTLArtist   = 7 * 24 * time.Hour
	TTLPlaylist = 6 * time.Hour
	TTLCharts   = time.Hour
	TTLRecs     = 24 * time.Hour // per account; the key carries the day
	TTLSearch   = 10 * time.Minute
	TTLLyrics   = 90 * 24 * time.Hour
)

// Policies by kind (the second segment of the key "origin:kind:id").
var Policies = map[string]Policy{
	"track":    {TTLTrack, 60 * 24 * time.Hour, 6 * time.Hour},
	"album":    {TTLAlbum, 30 * 24 * time.Hour, 6 * time.Hour},
	"artist":   {TTLArtist, 30 * 24 * time.Hour, 6 * time.Hour},
	"playlist": {TTLPlaylist, 7 * 24 * time.Hour, time.Hour},
	"charts":   {TTLCharts, 7 * 24 * time.Hour, 10 * time.Minute},
	"lyrics":   {TTLLyrics, 365 * 24 * time.Hour, 24 * time.Hour},
	"search":   {TTLSearch, time.Hour, 10 * time.Minute},
	"recs":     {TTLRecs, 24 * time.Hour, 10 * time.Minute},
	"user":     {30 * time.Minute, 24 * time.Hour, 10 * time.Minute},
	// per logged-in account; daily keys carry the date, so "stale" only bridges
	// a platform hiccup within the day
	"rec-tracks":     {TTLRecs, 6 * time.Hour, 10 * time.Minute},
	"rec-playlists":  {TTLRecs, 6 * time.Hour, 10 * time.Minute},
	"user-playlists": {10 * time.Minute, 7 * 24 * time.Hour, 5 * time.Minute},
	"discover":       {time.Hour, 7 * 24 * time.Hour, 10 * time.Minute},
	"hot-search":     {10 * time.Minute, time.Hour, 5 * time.Minute},
	"artist-albums":  {TTLArtist, 30 * 24 * time.Hour, 6 * time.Hour},
}

// DefaultPolicy applies to kinds without an entry.
var DefaultPolicy = Policy{time.Hour, 24 * time.Hour, 10 * time.Minute}

// DefaultCacheBytes is the size cap when none is configured (256 MiB).
const DefaultCacheBytes = 256 << 20

// Cache stores normalized DTOs keyed "origin:kind:id" (external_cache table).
type Cache struct {
	namespace string // immutable; isolates persisted entries and refreshes
	db        *sql.DB
	now       func() time.Time
	MaxBytes  int64
	Log       *slog.Logger
	// Cooling reports whether origin is in a risk-control cooldown; when set
	// and a stale copy exists, it is served without calling upstream.
	Cooling func(origin string) bool

	flight flightGroup
	bg     sync.WaitGroup
	evict  sync.Mutex
	stats  cacheCounters
}

func (c *Cache) storageKey(key string) string {
	if c.namespace == "" {
		return key
	}
	return c.namespace + "/" + key
}

type cacheCounters struct {
	hits, stale, misses, coalesced, negative, errors, staleOnError, evicted atomic.Int64
}

func NewCache(db *sql.DB) *Cache {
	return &Cache{db: db, now: time.Now, MaxBytes: DefaultCacheBytes, Log: slog.Default()}
}

func Key(origin, kind, id string) string { return origin + ":" + kind + ":" + id }

func keyParts(key string) (origin, kind string) {
	p := strings.SplitN(key, ":", 3)
	if len(p) < 2 {
		return "", ""
	}
	return p[0], p[1]
}

// PolicyFor returns the policy for a key.
func PolicyFor(key string) Policy {
	_, kind := keyParts(key)
	if p, ok := Policies[kind]; ok {
		return p
	}
	return DefaultPolicy
}

type entry struct {
	body     string
	negative string
	fresh    bool
	usable   bool // within the stale window
}

func (c *Cache) load(ctx context.Context, key string) (entry, bool) {
	var e entry
	var exp, stale, access int64
	err := c.db.QueryRowContext(ctx, `SELECT body, negative, expires_at, stale_until, last_access FROM external_cache WHERE key=?`, c.storageKey(key)).
		Scan(&e.body, &e.negative, &exp, &stale, &access)
	if err != nil {
		return e, false
	}
	now := c.now().UnixMilli()
	e.fresh = exp > now
	e.usable = e.fresh || stale > now
	if !e.usable {
		return e, false
	}
	if now-access > int64(time.Hour/time.Millisecond) { // cheap LRU touch, at most hourly
		c.db.ExecContext(context.WithoutCancel(ctx), `UPDATE external_cache SET last_access=? WHERE key=?`, now, c.storageKey(key))
	}
	return e, true
}

// Get decodes a fresh entry into v.
func (c *Cache) Get(ctx context.Context, key string, v any) bool {
	if c == nil {
		return false
	}
	e, ok := c.load(ctx, key)
	if !ok || !e.fresh || e.negative != "" {
		return false
	}
	return json.Unmarshal([]byte(e.body), v) == nil
}

// Put stores v under key with the key's policy (ttl overrides the fresh time).
func (c *Cache) Put(ctx context.Context, key string, v any, ttl time.Duration) error {
	if c == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.store(ctx, key, string(b), "", ttl)
}

func (c *Cache) store(ctx context.Context, key, body, negative string, ttl time.Duration) error {
	p := PolicyFor(key)
	if ttl <= 0 {
		ttl = p.TTL
	}
	stale := p.Stale
	if negative != "" {
		stale = 0
	}
	now := c.now()
	_, err := c.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO external_cache(key, body, negative, fetched_at, expires_at, stale_until, size, last_access) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET body=excluded.body, negative=excluded.negative, fetched_at=excluded.fetched_at,
		expires_at=excluded.expires_at, stale_until=excluded.stale_until, size=excluded.size, last_access=excluded.last_access`,
		c.storageKey(key), body, negative, now.UnixMilli(), now.Add(ttl).UnixMilli(), now.Add(ttl+stale).UnixMilli(), len(body)+len(key), now.UnixMilli())
	if err == nil {
		c.maybeEvict(ctx)
	}
	return err
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM external_cache WHERE key=?`, c.storageKey(key))
	return err
}

// Prune removes entries past their stale window.
func (c *Cache) Prune(ctx context.Context) (int64, error) {
	res, err := c.db.ExecContext(ctx, `DELETE FROM external_cache WHERE stale_until<=? AND expires_at<=?`, c.now().UnixMilli(), c.now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// maybeEvict trims the least recently used entries when over MaxBytes.
func (c *Cache) maybeEvict(ctx context.Context) {
	if c.MaxBytes <= 0 || !c.evict.TryLock() {
		return
	}
	defer c.evict.Unlock()
	ctx = context.WithoutCancel(ctx)
	var total int64
	if c.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM external_cache`).Scan(&total) != nil || total <= c.MaxBytes {
		return
	}
	c.Prune(ctx)
	target := c.MaxBytes * 9 / 10
	for total > target {
		res, err := c.db.ExecContext(ctx, `DELETE FROM external_cache WHERE key IN (SELECT key FROM external_cache ORDER BY last_access ASC LIMIT 200)`)
		if err != nil {
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return
		}
		c.stats.evicted.Add(n)
		if c.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM external_cache`).Scan(&total) != nil {
			return
		}
	}
}

// negativeErr maps answers worth remembering to a stored code.
func negativeCode(err error) string {
	switch {
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	case errors.Is(err, ErrBadInput):
		return "bad_input"
	}
	return ""
}

func negativeErr(code string) error {
	switch code {
	case "bad_input":
		return ErrBadInput
	}
	return ErrUnavailable
}

// cached returns key's value: fresh from the cache, else stale (refreshing
// in the background), else from fetch (coalesced). fetch receives a context
// that outlives the request when it runs as a background refresh.
func cached[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, fetch func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	if c == nil {
		return fetch(ctx)
	}
	decode := func(body string) (T, bool) {
		var v T
		return v, json.Unmarshal([]byte(body), &v) == nil
	}
	e, ok := c.load(ctx, key)
	if ok && e.negative != "" && e.fresh {
		c.stats.negative.Add(1)
		return zero, negativeErr(e.negative)
	}
	if ok && e.negative == "" {
		if v, good := decode(e.body); good {
			if e.fresh {
				c.stats.hits.Add(1)
				return v, nil
			}
			c.stats.stale.Add(1)
			origin, _ := keyParts(key)
			if c.Cooling == nil || !c.Cooling(origin) {
				c.refresh(key, ttl, func(ctx context.Context) (any, error) { return fetch(ctx) })
			}
			return v, nil
		}
	}
	c.stats.misses.Add(1)
	origin, _ := keyParts(key)
	v, err, shared := c.flight.do(key, func() (any, error) {
		// Detached from the first caller so a cancelled request doesn't fail
		// the others waiting on the same key.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		v, err := fetch(fctx)
		if err != nil {
			if code := negativeCode(err); code != "" {
				c.store(fctx, key, "", code, PolicyFor(key).Negative)
			}
			return nil, err
		}
		if b, merr := json.Marshal(v); merr == nil {
			c.store(fctx, key, string(b), "", ttl)
		}
		return v, nil
	})
	if shared {
		c.stats.coalesced.Add(1)
	}
	if err != nil {
		c.stats.errors.Add(1)
		_ = origin
		return zero, err
	}
	return v.(T), nil
}

// refresh revalidates key in the background (coalesced with any fetch).
func (c *Cache) refresh(key string, ttl time.Duration, fetch func(ctx context.Context) (any, error)) {
	if !c.flight.claim(key) {
		return // already being fetched
	}
	c.bg.Add(1)
	go func() {
		defer c.bg.Done()
		defer c.flight.release(key)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		v, err := fetch(ctx)
		if err != nil {
			c.stats.staleOnError.Add(1)
			return // keep serving the stale copy
		}
		if b, merr := json.Marshal(v); merr == nil {
			c.store(ctx, key, string(b), "", ttl)
		}
	}()
}

// Wait blocks until background refreshes finish (tests, shutdown).
func (c *Cache) Wait() { c.bg.Wait() }

// CacheKindStats is one row of Stats.
type CacheKindStats struct {
	Origin  string `json:"origin"`
	Kind    string `json:"kind"`
	Entries int64  `json:"entries"`
	Bytes   int64  `json:"bytes"`
	Fresh   int64  `json:"fresh"`
	Stale   int64  `json:"stale"`
}

// CacheStats is the settings view of the platform cache.
type CacheStats struct {
	Entries  int64            `json:"entries"`
	Bytes    int64            `json:"bytes"`
	MaxBytes int64            `json:"max_bytes"`
	Kinds    []CacheKindStats `json:"kinds"`
	Counters map[string]int64 `json:"counters"` // since start
}

func (c *Cache) Stats(ctx context.Context) (CacheStats, error) {
	st := CacheStats{MaxBytes: c.MaxBytes, Kinds: []CacheKindStats{}, Counters: map[string]int64{
		"hits": c.stats.hits.Load(), "stale_served": c.stats.stale.Load(), "misses": c.stats.misses.Load(),
		"coalesced": c.stats.coalesced.Load(), "negative_hits": c.stats.negative.Load(), "errors": c.stats.errors.Load(),
		"stale_on_error": c.stats.staleOnError.Load(), "evicted": c.stats.evicted.Load(),
	}}
	now := c.now().UnixMilli()
	rows, err := c.db.QueryContext(ctx, `SELECT key, size, expires_at FROM external_cache WHERE key LIKE ?`, c.storageKey("%"))
	if err != nil {
		return st, err
	}
	defer rows.Close()
	idx := map[string]int{}
	for rows.Next() {
		var key string
		var size, exp int64
		if rows.Scan(&key, &size, &exp) != nil {
			continue
		}
		logical := strings.TrimPrefix(key, c.namespace+"/")
		if c.namespace == "" {
			parts := strings.SplitN(key, "/", 3)
			if len(parts) == 3 && len(parts[0]) == 33 && strings.HasPrefix(parts[0], "u") {
				logical = parts[2]
			}
		}
		o, k := keyParts(logical)
		i, ok := idx[o+"\x00"+k]
		if !ok {
			i = len(st.Kinds)
			idx[o+"\x00"+k] = i
			st.Kinds = append(st.Kinds, CacheKindStats{Origin: o, Kind: k})
		}
		ks := &st.Kinds[i]
		ks.Entries++
		ks.Bytes += size
		if exp > now {
			ks.Fresh++
		} else {
			ks.Stale++
		}
		st.Entries++
		st.Bytes += size
	}
	return st, rows.Err()
}

// Clear deletes entries for origin and/or kind ("" = any). Returns the count.
func (c *Cache) Clear(ctx context.Context, origin, kind string) (int64, error) {
	pat := "%"
	switch {
	case origin != "" && kind != "":
		pat = origin + ":" + kind + ":%"
	case origin != "":
		pat = origin + ":%"
	case kind != "":
		pat = "%:" + kind + ":%"
	}
	q, args := `DELETE FROM external_cache WHERE key LIKE ? ESCAPE '\'`, []any{c.storageKey(pat)}
	if c.namespace == "" {
		q += ` OR key LIKE ? ESCAPE '\'`
		args = append(args, "%/"+pat)
	}
	res, err := c.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ── coalescing ───────────────────────────────────────────────────────────

type flightCall struct {
	done chan struct{}
	val  any
	err  error
}

type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

// do runs fn once per key at a time; concurrent callers share the result.
func (g *flightGroup) do(key string, fn func() (any, error)) (any, error, bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = map[string]*flightCall{}
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.val, c.err, true
	}
	c := &flightCall{done: make(chan struct{})}
	g.m[key] = c
	g.mu.Unlock()
	c.val, c.err = fn()
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	close(c.done)
	return c.val, c.err, false
}

// claim marks key in flight for a background refresh (false if already).
func (g *flightGroup) claim(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[string]*flightCall{}
	}
	if _, ok := g.m[key]; ok {
		return false
	}
	g.m[key] = &flightCall{done: make(chan struct{})}
	return true
}

func (g *flightGroup) release(key string) {
	g.mu.Lock()
	c := g.m[key]
	delete(g.m, key)
	g.mu.Unlock()
	if c != nil {
		close(c.done)
	}
}
