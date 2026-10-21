package library

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Read side of the canonical layer, used by the catalog API.

type ArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SortName   string `json:"sort_name,omitempty"`
	MBID       string `json:"mb_id,omitempty"`
	AlbumCount int    `json:"album_count"`
	TrackCount int    `json:"track_count"`
}

type Album struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	ArtistName  string      `json:"artist_name"`
	Artists     []ArtistRef `json:"artists"`
	Year        int         `json:"year,omitempty"`
	Genre       string      `json:"genre,omitempty"`
	TrackCount  int         `json:"track_count"`
	DurationMs  int64       `json:"duration_ms"`
	MBReleaseID string      `json:"mb_release_id,omitempty"`
	HasCover    bool        `json:"has_cover"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Track struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	ArtistName    string      `json:"artist_name"`
	Artists       []ArtistRef `json:"artists"`
	AlbumID       string      `json:"album_id,omitempty"`
	AlbumTitle    string      `json:"album_title,omitempty"`
	DiscNo        int         `json:"disc_no,omitempty"`
	TrackNo       int         `json:"track_no,omitempty"`
	Year          int         `json:"year,omitempty"`
	Genre         string      `json:"genre,omitempty"`
	DurationMs    int64       `json:"duration_ms"`
	ISRC          string      `json:"isrc,omitempty"`
	MBRecordingID string      `json:"mb_recording_id,omitempty"`
	Format        string      `json:"format,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
}

type ListParams struct {
	Limit, Offset int
	Sort          string // entity-specific whitelist
	Desc          bool
	Q             string // substring search (case-insensitive for ASCII)
	ArtistID      string
	AlbumID       string
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

var ErrBadSort = errors.New("unsupported sort field")

func (p *ListParams) norm() {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 500 {
		p.Limit = 500
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
}

func likeAny(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(q)) + "%"
}

func orderBy(sorts map[string]string, p ListParams, def string) (string, error) {
	key := p.Sort
	if key == "" {
		key = def
	}
	col, ok := sorts[key]
	if !ok {
		return "", ErrBadSort
	}
	dir := " ASC"
	if p.Desc {
		dir = " DESC"
	}
	// apply direction to every comma-separated term
	parts := strings.Split(col, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i]) + dir
	}
	return " ORDER BY " + strings.Join(parts, ", "), nil
}

type where struct {
	conds []string
	args  []any
}

func (w *where) add(c string, a ...any) { w.conds = append(w.conds, c); w.args = append(w.args, a...) }
func (w *where) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

func count(ctx context.Context, db *sql.DB, q string, args []any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// ── artists ─────────────────────────────────────────────────────────────────

var artistSorts = map[string]string{"name": "a.sort_name, a.id", "albums": "a.album_count, a.sort_name", "tracks": "a.track_count, a.sort_name"}

func (s *Scanner) ListArtists(ctx context.Context, p ListParams) (Page[Artist], error) {
	p.norm()
	ob, err := orderBy(artistSorts, p, "name")
	if err != nil {
		return Page[Artist]{}, err
	}
	var w where
	if p.Q != "" {
		w.add(`a.name LIKE ? ESCAPE '\'`, likeAny(p.Q))
	}
	out := Page[Artist]{Items: []Artist{}, Limit: p.Limit, Offset: p.Offset}
	if out.Total, err = count(ctx, s.db, `SELECT count(*) FROM artists a`+w.sql(), w.args); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.name, COALESCE(a.sort_name,''), COALESCE(a.mb_id,''), a.album_count, a.track_count
		FROM artists a`+w.sql()+ob+` LIMIT ? OFFSET ?`, append(w.args, p.Limit, p.Offset)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.AlbumCount, &a.TrackCount); err != nil {
			return out, err
		}
		out.Items = append(out.Items, a)
	}
	return out, rows.Err()
}

func (s *Scanner) GetArtist(ctx context.Context, id string) (Artist, error) {
	var a Artist
	err := s.db.QueryRowContext(ctx, `SELECT id, name, COALESCE(sort_name,''), COALESCE(mb_id,''), album_count, track_count
		FROM artists WHERE id=?`, id).Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.AlbumCount, &a.TrackCount)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// ── albums ──────────────────────────────────────────────────────────────────

var albumSorts = map[string]string{
	"title": "lower(al.title), al.id", "artist": "lower(al.artist_name), al.year, lower(al.title)",
	"year": "al.year, lower(al.title)", "added": "al.created_at, al.id",
}

const albumCols = `al.id, al.title, COALESCE(al.artist_name,''), COALESCE(al.year,0), COALESCE(al.genre,''),
	al.track_count, al.duration_ms, COALESCE(al.mb_release_id,''), al.cover_path IS NOT NULL, al.created_at`

func scanAlbum(r interface{ Scan(...any) error }) (Album, error) {
	var a Album
	var created int64
	err := r.Scan(&a.ID, &a.Title, &a.ArtistName, &a.Year, &a.Genre, &a.TrackCount, &a.DurationMs, &a.MBReleaseID, &a.HasCover, &created)
	a.CreatedAt = time.UnixMilli(created).UTC()
	return a, err
}

func (s *Scanner) ListAlbums(ctx context.Context, p ListParams) (Page[Album], error) {
	p.norm()
	ob, err := orderBy(albumSorts, p, "artist")
	if err != nil {
		return Page[Album]{}, err
	}
	var w where
	if p.Q != "" {
		w.add(`(al.title LIKE ? ESCAPE '\' OR al.artist_name LIKE ? ESCAPE '\')`, likeAny(p.Q), likeAny(p.Q))
	}
	if p.ArtistID != "" {
		w.add(`al.id IN (SELECT album_id FROM album_artists WHERE artist_id=?
			UNION SELECT t.album_id FROM track_artists ta JOIN tracks t ON t.id=ta.track_id WHERE ta.artist_id=?)`, p.ArtistID, p.ArtistID)
	}
	out := Page[Album]{Items: []Album{}, Limit: p.Limit, Offset: p.Offset}
	if out.Total, err = count(ctx, s.db, `SELECT count(*) FROM albums al`+w.sql(), w.args); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumCols+` FROM albums al`+w.sql()+ob+` LIMIT ? OFFSET ?`,
		append(w.args, p.Limit, p.Offset)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, a)
	}
	rows.Close()
	for i := range out.Items {
		if out.Items[i].Artists, err = s.refs(ctx, `album_artists`, `album_id`, out.Items[i].ID); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Scanner) GetAlbum(ctx context.Context, id string) (Album, error) {
	a, err := scanAlbum(s.db.QueryRowContext(ctx, `SELECT `+albumCols+` FROM albums al WHERE al.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Artists, err = s.refs(ctx, `album_artists`, `album_id`, id)
	return a, err
}

func (s *Scanner) refs(ctx context.Context, table, col, id string) ([]ArtistRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.name, j.role FROM `+table+` j JOIN artists a ON a.id=j.artist_id
		WHERE j.`+col+`=? ORDER BY j.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArtistRef{}
	for rows.Next() {
		var r ArtistRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Role); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── tracks ──────────────────────────────────────────────────────────────────

var trackSorts = map[string]string{
	"title": "lower(t.title), t.id", "artist": "lower(t.artist_name), lower(t.title)",
	"album": "lower(al.title), t.disc_no, t.track_no, t.id", "added": "t.created_at, t.id",
	"duration": "t.duration_ms, t.id", "year": "t.year, t.id",
	"track": "t.disc_no, t.track_no, lower(t.title)",
}

const trackCols = `t.id, t.title, COALESCE(t.artist_name,''), COALESCE(t.album_id,''), COALESCE(al.title,''),
	COALESCE(t.disc_no,0), COALESCE(t.track_no,0), COALESCE(t.year,0), COALESCE(t.genre,''), COALESCE(t.duration_ms,0),
	COALESCE(t.isrc,''), COALESCE(t.mb_recording_id,''),
	COALESCE((SELECT st.format FROM track_links l JOIN source_tracks st ON st.id=l.source_track_id
		WHERE l.track_id=t.id ORDER BY l.confidence DESC LIMIT 1),''), t.created_at`

func scanTrack(r interface{ Scan(...any) error }) (Track, error) {
	var t Track
	var created int64
	err := r.Scan(&t.ID, &t.Title, &t.ArtistName, &t.AlbumID, &t.AlbumTitle, &t.DiscNo, &t.TrackNo, &t.Year, &t.Genre,
		&t.DurationMs, &t.ISRC, &t.MBRecordingID, &t.Format, &created)
	t.CreatedAt = time.UnixMilli(created).UTC()
	return t, err
}

func (s *Scanner) ListTracks(ctx context.Context, p ListParams) (Page[Track], error) {
	p.norm()
	def := "title"
	if p.AlbumID != "" {
		def = "track"
	}
	ob, err := orderBy(trackSorts, p, def)
	if err != nil {
		return Page[Track]{}, err
	}
	var w where
	if p.Q != "" {
		w.add(`(t.title LIKE ? ESCAPE '\' OR t.artist_name LIKE ? ESCAPE '\' OR al.title LIKE ? ESCAPE '\')`,
			likeAny(p.Q), likeAny(p.Q), likeAny(p.Q))
	}
	if p.AlbumID != "" {
		w.add(`t.album_id=?`, p.AlbumID)
	}
	if p.ArtistID != "" {
		w.add(`t.id IN (SELECT track_id FROM track_artists WHERE artist_id=?)`, p.ArtistID)
	}
	from := ` FROM tracks t LEFT JOIN albums al ON al.id=t.album_id`
	out := Page[Track]{Items: []Track{}, Limit: p.Limit, Offset: p.Offset}
	if out.Total, err = count(ctx, s.db, `SELECT count(*)`+from+w.sql(), w.args); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+trackCols+from+w.sql()+ob+` LIMIT ? OFFSET ?`, append(w.args, p.Limit, p.Offset)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, t)
	}
	rows.Close()
	for i := range out.Items {
		if out.Items[i].Artists, err = s.refs(ctx, `track_artists`, `track_id`, out.Items[i].ID); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Scanner) GetTrack(ctx context.Context, id string) (Track, error) {
	t, err := scanTrack(s.db.QueryRowContext(ctx, `SELECT `+trackCols+` FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	t.Artists, err = s.refs(ctx, `track_artists`, `track_id`, id)
	return t, err
}

// ── files: covers & streams (paths come from the DB only) ──────────────────

// AlbumCover returns the cover file for an album, or for a track's album.
func (s *Scanner) AlbumCover(ctx context.Context, albumID string) (string, error) {
	var p sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT cover_path FROM albums WHERE id=?`, albumID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.Valid) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s.safeFile(p.String, true)
}

func (s *Scanner) TrackCover(ctx context.Context, trackID string) (string, error) {
	var p sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT st.cover_path FROM track_links l JOIN source_tracks st ON st.id=l.source_track_id
		WHERE l.track_id=? AND st.cover_path IS NOT NULL ORDER BY l.confidence DESC LIMIT 1`, trackID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s.safeFile(p.String, true)
}

type TrackFile struct {
	Path   string
	Format string
	Size   int64
}

// TrackFile returns the best local file for a canonical track.
func (s *Scanner) TrackFile(ctx context.Context, trackID string) (TrackFile, error) {
	var f TrackFile
	err := s.db.QueryRowContext(ctx, `SELECT st.path, COALESCE(st.format,'') FROM track_links l
		JOIN source_tracks st ON st.id=l.source_track_id
		WHERE l.track_id=? AND st.source=? AND st.path IS NOT NULL
		ORDER BY l.confidence DESC, CASE st.format WHEN 'flac' THEN 0 ELSE 1 END, st.path LIMIT 1`, trackID, SourceLocal).Scan(&f.Path, &f.Format)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	f.Path, err = s.safeFile(f.Path, false)
	return f, err
}

// safeFile resolves symlinks and verifies the file is a regular file inside a
// configured library root (or the cover cache when allowCache).
func (s *Scanner) safeFile(p string, allowCache bool) (string, error) {
	real, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		return "", ErrNotFound
	}
	bases := append([]string{}, s.opts.Roots...)
	if allowCache && s.opts.CoverDir != "" {
		bases = append(bases, s.opts.CoverDir)
	}
	ok := false
	for _, b := range bases {
		rb, err := filepath.EvalSymlinks(b)
		if err != nil {
			continue
		}
		if strings.HasPrefix(real, rb+string(filepath.Separator)) {
			ok = true
			break
		}
	}
	if !ok {
		return "", ErrNotFound
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return real, nil
}
