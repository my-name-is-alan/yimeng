package online

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// TTLs for the external catalog cache.
const (
	TTLTrack    = 7 * 24 * time.Hour
	TTLAlbum    = 3 * 24 * time.Hour
	TTLArtist   = 24 * time.Hour
	TTLPlaylist = 30 * time.Minute
	TTLCharts   = time.Hour
	TTLRecs     = time.Hour
	TTLSearch   = 10 * time.Minute
	TTLLyrics   = 30 * 24 * time.Hour
)

// Cache stores normalized DTOs keyed "origin:kind:id" (external_cache table).
type Cache struct {
	db  *sql.DB
	now func() time.Time
}

func NewCache(db *sql.DB) *Cache { return &Cache{db: db, now: time.Now} }

func Key(origin, kind, id string) string { return origin + ":" + kind + ":" + id }

// Get decodes a fresh entry into v.
func (c *Cache) Get(ctx context.Context, key string, v any) bool {
	if c == nil {
		return false
	}
	var body string
	err := c.db.QueryRowContext(ctx, `SELECT body FROM external_cache WHERE key=? AND expires_at>?`, key, c.now().UnixMilli()).Scan(&body)
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(body), v) == nil
}

func (c *Cache) Put(ctx context.Context, key string, v any, ttl time.Duration) error {
	if c == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	now := c.now()
	_, err = c.db.ExecContext(ctx, `INSERT INTO external_cache(key, body, fetched_at, expires_at) VALUES (?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET body=excluded.body, fetched_at=excluded.fetched_at, expires_at=excluded.expires_at`,
		key, string(b), now.UnixMilli(), now.Add(ttl).UnixMilli())
	return err
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM external_cache WHERE key=?`, key)
	return err
}

// Prune removes expired entries.
func (c *Cache) Prune(ctx context.Context) (int64, error) {
	res, err := c.db.ExecContext(ctx, `DELETE FROM external_cache WHERE expires_at<=?`, c.now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// cached wraps a fetch with the cache.
func cached[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	var v T
	if c.Get(ctx, key, &v) {
		return v, nil
	}
	v, err := fetch()
	if err != nil {
		return v, err
	}
	if perr := c.Put(ctx, key, v, ttl); perr != nil && !errors.Is(perr, context.Canceled) {
		return v, nil
	}
	return v, nil
}
