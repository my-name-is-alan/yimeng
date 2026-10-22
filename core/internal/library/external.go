package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/search"
)

// External catalog items (online plugins) are materialized into the canonical
// tables with origin=<plugin id> and ids "<origin>:<external id>" so that
// favorites, playlists, history and ratings work across platforms. They are
// never touched by Merge (which only owns origin='local') and are read-only
// for metadata editing (see origin.go / decisions D29, D30).

// ExternalRef is an artist reference on an external item.
type ExternalRef struct {
	ID   string // full id "<origin>:<external id>" or "" (name only)
	Name string
}

// ExternalTrack is the normalized shape plugins hand to the library.
type ExternalTrack struct {
	ID         string // "<origin>:<external id>"
	Origin     string
	Title      string
	Artists    []ExternalRef
	AlbumID    string // "<origin>:<album id>" or ""
	AlbumTitle string
	CoverURL   string
	DurationMs int64
	TrackNo    int
	DiscNo     int
	Year       int
	ISRC       string
}

// IsExternalID reports whether id names an external item ("origin:id").
func IsExternalID(id string) bool {
	i := strings.IndexByte(id, ':')
	return i > 0 && i < len(id)-1
}

// SplitExternalID returns origin and the platform-native id.
func SplitExternalID(id string) (origin, ext string, ok bool) {
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}

// ExternalResolver fetches and materializes unknown external ids on demand.
type ExternalResolver func(ctx context.Context, ids []string) error

// SetExternalResolver installs the hook used by playlist validation.
func (s *Scanner) SetExternalResolver(f ExternalResolver) { s.extResolver = f }

// EnsureExternal materializes ids that are external and not yet stored.
func (s *Scanner) EnsureExternal(ctx context.Context, ids []string) error {
	var missing []string
	for _, id := range ids {
		if !IsExternalID(id) {
			continue
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE id=?`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 || s.extResolver == nil {
		return nil
	}
	return s.extResolver(ctx, missing)
}

// UpsertExternal stores external tracks (and their albums/artists).
func (s *Scanner) UpsertExternal(ctx context.Context, items []ExternalTrack) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	albums, artists := map[string]bool{}, map[string]bool{}
	for _, t := range items {
		if t.Origin == "" || t.Origin == OriginLocal || !strings.HasPrefix(t.ID, t.Origin+":") {
			return fmt.Errorf("%w: bad external id %q", ErrBadInput, t.ID)
		}
		names := make([]string, 0, len(t.Artists))
		for _, a := range t.Artists {
			names = append(names, a.Name)
		}
		artistName := strings.Join(names, " / ")
		if t.AlbumID != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO albums(id, title, year, cover_path, artist_name, search_text, created_at, updated_at, origin, cover_source)
				VALUES (?,?,?,?,?,?,?,?,?, 'remote')
				ON CONFLICT(id) DO UPDATE SET title=excluded.title, year=COALESCE(excluded.year, albums.year),
				cover_path=COALESCE(excluded.cover_path, albums.cover_path), updated_at=excluded.updated_at`,
				t.AlbumID, t.AlbumTitle, nzInt(t.Year), nz(t.CoverURL), nz(firstName(t.Artists)), search.Text(t.AlbumTitle, firstName(t.Artists)), now, now, t.Origin); err != nil {
				return err
			}
			albums[t.AlbumID] = true
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tracks(id, title, album_id, disc_no, track_no, duration_ms, isrc, artist_name, year, search_text, created_at, updated_at, origin)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, album_id=excluded.album_id, duration_ms=excluded.duration_ms,
			isrc=COALESCE(excluded.isrc, tracks.isrc), artist_name=excluded.artist_name, search_text=excluded.search_text, updated_at=excluded.updated_at`,
			t.ID, t.Title, nz(t.AlbumID), nzInt(t.DiscNo), nzInt(t.TrackNo), nzInt64(t.DurationMs), nz(t.ISRC), artistName, nzInt(t.Year),
			search.Text(t.Title, artistName), now, now, t.Origin); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM track_artists WHERE track_id=?`, t.ID); err != nil {
			return err
		}
		for i, a := range t.Artists {
			aid := a.ID
			if aid == "" {
				aid = t.Origin + ":name:" + normKey(a.Name)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO artists(id, name, sort_name, search_text, created_at, updated_at, origin) VALUES (?,?,?,?,?,?,?)
				ON CONFLICT(id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`,
				aid, a.Name, normKey(a.Name), search.Text(a.Name), now, now, t.Origin); err != nil {
				return err
			}
			role := "primary"
			if i > 0 {
				role = "featured"
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_artists(track_id, artist_id, role, position) VALUES (?,?,?,?)`, t.ID, aid, role, i); err != nil {
				return err
			}
			if t.AlbumID != "" && i == 0 {
				tx.ExecContext(ctx, `INSERT OR IGNORE INTO album_artists(album_id, artist_id, role, position) VALUES (?,?,?,0)`, t.AlbumID, aid, "primary")
			}
			artists[aid] = true
		}
	}
	for id := range albums {
		if _, err := tx.ExecContext(ctx, `UPDATE albums SET track_count=(SELECT count(*) FROM tracks WHERE album_id=albums.id),
			duration_ms=(SELECT COALESCE(sum(duration_ms),0) FROM tracks WHERE album_id=albums.id) WHERE id=?`, id); err != nil {
			return err
		}
	}
	for id := range artists {
		if _, err := tx.ExecContext(ctx, `UPDATE artists SET track_count=(SELECT count(DISTINCT track_id) FROM track_artists WHERE artist_id=artists.id),
			album_count=(SELECT count(DISTINCT album_id) FROM album_artists WHERE artist_id=artists.id) WHERE id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func firstName(a []ExternalRef) string {
	if len(a) == 0 {
		return ""
	}
	return a[0].Name
}

// Equivalent is another recording of the same song (other origin or local).
type Equivalent struct {
	TrackID    string  `json:"track_id"`
	Origin     string  `json:"origin"`
	Confidence float64 `json:"confidence"`
	Method     string  `json:"method"`
}

// LinkEquivalent records that a and b are the same recording (both directions).
func (s *Scanner) LinkEquivalent(ctx context.Context, a, b string, conf float64, method string) error {
	if a == b {
		return nil
	}
	now := time.Now().UnixMilli()
	for _, p := range [][2]string{{a, b}, {b, a}} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO track_equivalents(track_id, alt_track_id, confidence, method, created_at) VALUES (?,?,?,?,?)
			ON CONFLICT(track_id, alt_track_id) DO UPDATE SET confidence=max(confidence, excluded.confidence), method=excluded.method`,
			p[0], p[1], conf, method, now); err != nil {
			return err
		}
	}
	return nil
}

// Equivalents lists known alternatives for id, best first. Stale rows whose
// track no longer exists are skipped.
func (s *Scanner) Equivalents(ctx context.Context, id string) ([]Equivalent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.alt_track_id, t.origin, e.confidence, e.method FROM track_equivalents e
		JOIN tracks t ON t.id=e.alt_track_id WHERE e.track_id=? ORDER BY (t.origin='local') DESC, e.confidence DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Equivalent{}
	for rows.Next() {
		var e Equivalent
		if err := rows.Scan(&e.TrackID, &e.Origin, &e.Confidence, &e.Method); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MatchQuery describes a recording to find among stored tracks.
type MatchQuery struct {
	ISRC, Title, Artist string
	DurationMs          int64
	Origin              string // restrict to this origin ("" = local)
}

// MatchScore scores a candidate against q: 1.0 for an ISRC hit, otherwise
// normalized title+artist equality with a duration tolerance (≤3 s full,
// ≤8 s reduced). 0 means no match.
func MatchScore(q MatchQuery, isrc, title, artist string, durMs int64) (float64, string) {
	if q.ISRC != "" && strings.EqualFold(q.ISRC, isrc) {
		return 1, "isrc"
	}
	if normKey(q.Title) != normKey(title) || q.Title == "" {
		return 0, ""
	}
	qa, ca := normKey(q.Artist), normKey(artist)
	if qa == "" || ca == "" || !(strings.Contains(ca, qa) || strings.Contains(qa, ca)) {
		return 0, ""
	}
	if q.DurationMs <= 0 || durMs <= 0 {
		return 0.7, "meta"
	}
	d := math.Abs(float64(q.DurationMs - durMs))
	switch {
	case d <= 3000:
		return 0.9, "meta"
	case d <= 8000:
		return 0.75, "meta"
	}
	return 0, ""
}

// FindMatch returns the best stored track matching q (score ≥ 0.75).
func (s *Scanner) FindMatch(ctx context.Context, q MatchQuery) (string, float64, string, error) {
	origin := q.Origin
	if origin == "" {
		origin = OriginLocal
	}
	var rows *sql.Rows
	var err error
	if q.ISRC != "" {
		var id string
		err = s.db.QueryRowContext(ctx, `SELECT id FROM tracks WHERE origin=? AND isrc=? COLLATE NOCASE LIMIT 1`, origin, q.ISRC).Scan(&id)
		if err == nil {
			return id, 1, "isrc", nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", 0, "", err
		}
	}
	if q.Title == "" {
		return "", 0, "", nil
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id, COALESCE(isrc,''), title, COALESCE(artist_name,''), COALESCE(duration_ms,0) FROM tracks
		WHERE origin=? AND lower(title)=lower(?) LIMIT 50`, origin, strings.TrimSpace(q.Title))
	if err != nil {
		return "", 0, "", err
	}
	defer rows.Close()
	best, bestScore, bestMethod := "", 0.0, ""
	for rows.Next() {
		var id, isrc, title, artist string
		var dur int64
		if err := rows.Scan(&id, &isrc, &title, &artist, &dur); err != nil {
			return "", 0, "", err
		}
		if sc, m := MatchScore(q, isrc, title, artist, dur); sc > bestScore {
			best, bestScore, bestMethod = id, sc, m
		}
	}
	if bestScore < 0.75 {
		return "", 0, "", rows.Err()
	}
	return best, bestScore, bestMethod, rows.Err()
}

// LocalTrackCount counts tracks from library roots (origin local).
func (s *Scanner) LocalTrackCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE origin='local'`).Scan(&n)
	return n, err
}
