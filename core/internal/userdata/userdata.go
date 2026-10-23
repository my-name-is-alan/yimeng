// Package userdata stores listening history, play counts, favorites and ratings.
package userdata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrBad      = errors.New("bad request")
)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Counts reports whether a listen qualifies as a play (Last.fm rule): the
// track ended, or ≥ half its length / 4 minutes was heard; tracks ≤30s need
// to complete.
func Counts(playedMs, durationMs int64, completed bool) bool {
	if completed {
		return true
	}
	if durationMs > 0 && durationMs <= 30_000 {
		return false
	}
	need := int64(240_000)
	if durationMs > 0 && durationMs/2 < need {
		need = durationMs / 2
	}
	return playedMs >= need
}

type Play struct {
	TrackID    string    `json:"track_id"`
	PlayerID   string    `json:"player_id,omitempty"`
	PlayedAt   time.Time `json:"played_at"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	Completed  bool      `json:"completed"`
	Source     string    `json:"source"`
}

// Record stores a qualifying play and bumps track_stats.
func (s *Store) Record(ctx context.Context, p Play) error {
	if p.TrackID == "" {
		return fmt.Errorf("%w: track_id required", ErrBad)
	}
	if p.PlayedAt.IsZero() {
		p.PlayedAt = s.now()
	}
	if p.PlayedAt.After(s.now().Add(time.Hour)) {
		return fmt.Errorf("%w: played_at in the future", ErrBad)
	}
	if p.Source == "" {
		p.Source = "core"
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE id=?`, p.TrackID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	at := p.PlayedAt.UnixMilli()
	var pid any
	if p.PlayerID != "" {
		pid = p.PlayerID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO plays(track_id, player_id, played_at, duration_ms, completed, source) VALUES (?,?,?,?,?,?)`,
		p.TrackID, pid, at, p.DurationMs, p.Completed, p.Source); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO track_stats(track_id, play_count, last_played_at) VALUES (?,1,?)
		ON CONFLICT(track_id) DO UPDATE SET play_count=play_count+1, last_played_at=max(COALESCE(last_played_at,0), excluded.last_played_at)`,
		p.TrackID, at); err != nil {
		return err
	}
	return tx.Commit()
}

type HistoryItem struct {
	Play
	Title      string `json:"title"`
	ArtistName string `json:"artist_name"`
	AlbumID    string `json:"album_id,omitempty"`
	AlbumTitle string `json:"album_title,omitempty"`
	Available  bool   `json:"available"` // track still in the library
}

// History lists plays newest first.
func (s *Store) History(ctx context.Context, playerID string, limit, offset int) ([]HistoryItem, int, error) {
	limit, offset = clamp(limit, offset)
	where, args := "", []any{}
	if playerID != "" {
		where, args = " WHERE p.player_id=?", append(args, playerID)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM plays p`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.track_id, COALESCE(p.player_id,''), p.played_at, COALESCE(p.duration_ms,0), p.completed, p.source,
		COALESCE(t.title,''), COALESCE(t.artist_name,''), COALESCE(t.album_id,''), COALESCE(al.title,''), t.id IS NOT NULL
		FROM plays p LEFT JOIN tracks t ON t.id=p.track_id LEFT JOIN albums al ON al.id=t.album_id`+where+`
		ORDER BY p.played_at DESC, p.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []HistoryItem{}
	for rows.Next() {
		var h HistoryItem
		var at int64
		if err := rows.Scan(&h.TrackID, &h.PlayerID, &at, &h.DurationMs, &h.Completed, &h.Source,
			&h.Title, &h.ArtistName, &h.AlbumID, &h.AlbumTitle, &h.Available); err != nil {
			return nil, 0, err
		}
		h.PlayedAt = time.UnixMilli(at).UTC()
		out = append(out, h)
	}
	return out, total, rows.Err()
}

type TopItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ArtistName string `json:"artist_name,omitempty"`
	Plays      int    `json:"plays"`
}

// Top returns the most played tracks/albums/artists since a time (zero = all time).
func (s *Store) Top(ctx context.Context, kind string, since time.Time, limit int) ([]TopItem, error) {
	limit, _ = clamp(limit, 0)
	var q string
	switch kind {
	case "", "track":
		q = `SELECT t.id, t.title, COALESCE(t.artist_name,''), count(*) c FROM plays p JOIN tracks t ON t.id=p.track_id
			WHERE p.played_at>=? GROUP BY t.id ORDER BY c DESC, t.title LIMIT ?`
	case "album":
		q = `SELECT al.id, al.title, COALESCE(al.artist_name,''), count(*) c FROM plays p JOIN tracks t ON t.id=p.track_id
			JOIN albums al ON al.id=t.album_id WHERE p.played_at>=? GROUP BY al.id ORDER BY c DESC, al.title LIMIT ?`
	case "artist":
		q = `SELECT a.id, a.name, '', count(*) c FROM plays p JOIN track_artists ta ON ta.track_id=p.track_id AND ta.role='primary'
			JOIN artists a ON a.id=ta.artist_id WHERE p.played_at>=? GROUP BY a.id ORDER BY c DESC, a.name LIMIT ?`
	default:
		return nil, fmt.Errorf("%w: type must be track|album|artist", ErrBad)
	}
	rows, err := s.DB.QueryContext(ctx, q, since.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopItem{}
	for rows.Next() {
		var t TopItem
		if err := rows.Scan(&t.ID, &t.Name, &t.ArtistName, &t.Plays); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

var tables = map[string]string{"track": "tracks", "album": "albums", "artist": "artists"}

func (s *Store) exists(ctx context.Context, typ, id string) error {
	tbl, ok := tables[typ]
	if !ok {
		return fmt.Errorf("%w: type must be track|album|artist", ErrBad)
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM `+tbl+` WHERE id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetFavorite(ctx context.Context, typ, id string, fav bool) error {
	if !fav {
		if _, ok := tables[typ]; !ok {
			return fmt.Errorf("%w: type must be track|album|artist", ErrBad)
		}
		_, err := s.DB.ExecContext(ctx, `DELETE FROM favorites WHERE entity_type=? AND entity_id=?`, typ, id)
		return err
	}
	if err := s.exists(ctx, typ, id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO favorites(entity_type, entity_id, created_at) VALUES (?,?,?)`,
		typ, id, s.now().UnixMilli())
	return err
}

// SetRating sets 1..5; 0 clears.
func (s *Store) SetRating(ctx context.Context, typ, id string, rating int) error {
	if rating < 0 || rating > 5 {
		return fmt.Errorf("%w: rating must be 0..5", ErrBad)
	}
	if rating == 0 {
		if _, ok := tables[typ]; !ok {
			return fmt.Errorf("%w: type must be track|album|artist", ErrBad)
		}
		_, err := s.DB.ExecContext(ctx, `DELETE FROM ratings WHERE entity_type=? AND entity_id=?`, typ, id)
		return err
	}
	if err := s.exists(ctx, typ, id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ratings(entity_type, entity_id, rating, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET rating=excluded.rating, updated_at=excluded.updated_at`,
		typ, id, rating, s.now().UnixMilli())
	return err
}

type Favorite struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Available bool      `json:"available"`
}

func (s *Store) Favorites(ctx context.Context, typ string, limit, offset int) ([]Favorite, error) {
	limit, offset = clamp(limit, offset)
	args := []any{}
	where := ""
	if typ != "" {
		if _, ok := tables[typ]; !ok {
			return nil, fmt.Errorf("%w: type must be track|album|artist", ErrBad)
		}
		where, args = " WHERE f.entity_type=?", append(args, typ)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT f.entity_type, f.entity_id, f.created_at,
		COALESCE(t.title, al.title, a.name, ''), COALESCE(t.id, al.id, a.id) IS NOT NULL
		FROM favorites f
		LEFT JOIN tracks t ON f.entity_type='track' AND t.id=f.entity_id
		LEFT JOIN albums al ON f.entity_type='album' AND al.id=f.entity_id
		LEFT JOIN artists a ON f.entity_type='artist' AND a.id=f.entity_id`+where+`
		ORDER BY f.created_at DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		var at int64
		if err := rows.Scan(&f.Type, &f.ID, &at, &f.Name, &f.Available); err != nil {
			return nil, err
		}
		f.CreatedAt = time.UnixMilli(at).UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

func clamp(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	return limit, max(offset, 0)
}
