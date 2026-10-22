package library

import (
	"context"
	"database/sql"
	"errors"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/search"
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
	Origin     string `json:"origin"`
	// ImageUpdatedAt is set when a user artist image exists (GET /api/artists/{id}/image?v=…).
	ImageUpdatedAt int64     `json:"image_updated_at,omitempty"`
	SortLetter     string    `json:"sort_letter,omitempty"` // set by the gateway
	ImageURL       string    `json:"image_url,omitempty"`   // signed, set by the gateway
	Cover          *CoverRef `json:"cover,omitempty"`       // artist image, else an album cover
}

type Album struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	ArtistName  string      `json:"artist_name"`
	Artists     []ArtistRef `json:"artists"`
	Year        int         `json:"year,omitempty"`
	Genre       string      `json:"genre,omitempty"`
	TrackCount  int         `json:"track_count"`
	DiscCount   int         `json:"disc_count,omitempty"`
	DurationMs  int64       `json:"duration_ms"`
	MBReleaseID string      `json:"mb_release_id,omitempty"`
	HasCover    bool        `json:"has_cover"`
	Favorite    bool        `json:"favorite"`
	Rating      int         `json:"rating,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	Origin      string      `json:"origin"`
	SortLetter  string      `json:"sort_letter,omitempty"` // set by the gateway
	Cover       *CoverRef   `json:"cover,omitempty"`       // set by the gateway
	// App aliases (set by the gateway): name = title, artist = first artist.
	Name   string     `json:"name,omitempty"`
	Artist *ArtistRef `json:"artist,omitempty"`
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
	Stream        *StreamInfo `json:"stream,omitempty"`
	PlayCount     int         `json:"play_count"`
	LastPlayedAt  *time.Time  `json:"last_played_at,omitempty"`
	// SortLetter is the A–Z index key (pinyin initial for Chinese titles).
	SortLetter string    `json:"sort_letter"`
	Favorite   bool      `json:"favorite"`
	Rating     int       `json:"rating,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Origin     string    `json:"origin"`
	// App fields (D45). Library tracks are playable (online entries resolve
	// their entitlement when streamed).
	Playable  bool        `json:"playable"`
	Album     *AlbumRef   `json:"album,omitempty"`
	Preview   bool        `json:"preview"`
	HasLyrics bool        `json:"has_lyrics"`
	Audio     *StreamInfo `json:"audio,omitempty"`      // same as stream; app name
	Cover     *CoverRef   `json:"cover,omitempty"`      // set by the gateway
	CoverPath string      `json:"-"`                    // album cover file or URL
	StreamURL string      `json:"stream_url,omitempty"` // signed, set by the gateway
}

// AlbumRef is {id,name} on tracks.
type AlbumRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CoverRef is a ready-to-use cover: a signed URL plus placeholder data.
type CoverRef struct {
	URL      string   `json:"url"`
	Blurhash string   `json:"blurhash,omitempty"`
	Colors   []string `json:"colors,omitempty"`
}

// StreamInfo describes the best source file (Roon-like signal path).
type StreamInfo struct {
	Format      string `json:"format"` // container: flac, mp3, m4a, ...
	Codec       string `json:"codec"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	BitDepth    int    `json:"bit_depth,omitempty"`
	Channels    int    `json:"channels,omitempty"`
	BitrateKbps int    `json:"bitrate_kbps,omitempty"`
	Bitrate     int    `json:"bitrate,omitempty"` // = bitrate_kbps (app name)
	Lossless    bool   `json:"lossless"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	ReplayGain  *Gain  `json:"replaygain,omitempty"`
}

// Gain is ReplayGain in dB relative to -18 LUFS (peaks linear).
type Gain struct {
	TrackGain *float64 `json:"track_gain,omitempty"`
	TrackPeak *float64 `json:"track_peak,omitempty"`
	AlbumGain *float64 `json:"album_gain,omitempty"`
	AlbumPeak *float64 `json:"album_peak,omitempty"`
	Source    string   `json:"source"` // tag | analyzed
}

type ListParams struct {
	Limit, Offset int
	Sort          string // entity-specific whitelist
	Desc          bool
	Q             string // substring search (case-insensitive for ASCII)
	ArtistID      string
	AlbumID       string
	Favorite      bool   // only favorites
	Origin        string // "" or "all" = every origin; "local", "netease", ...
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

// addSearch appends OR-ed LIKE predicates over cols for every ExpandQuery form of p.Q.
func addSearch(w *where, p ListParams, cols ...string) {
	if p.Q == "" || len(cols) == 0 {
		return
	}
	forms := search.ExpandQuery(p.Q)
	var parts []string
	var args []any
	for _, f := range forms {
		pat := likeAny(f)
		for _, c := range cols {
			parts = append(parts, c+` LIKE ? ESCAPE '\'`)
			args = append(args, pat)
		}
	}
	w.add(`(`+strings.Join(parts, ` OR `)+`)`, args...)
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
	parts := strings.Split(col, ", ") // terms are ", "-separated; expressions may contain ","
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
	addSearch(&w, p, `a.name`, `a.search_text`)
	if p.Origin != "" && p.Origin != "all" {
		w.add(`a.origin=?`, p.Origin)
	}
	out := Page[Artist]{Items: []Artist{}, Limit: p.Limit, Offset: p.Offset}
	if out.Total, err = count(ctx, s.db, `SELECT count(*) FROM artists a`+w.sql(), w.args); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.name, COALESCE(a.sort_name,''), COALESCE(a.mb_id,''), a.album_count, a.track_count, a.origin,
		COALESCE((SELECT updated_at FROM artist_images i WHERE i.artist_id=a.id),0)
		FROM artists a`+w.sql()+ob+` LIMIT ? OFFSET ?`, append(w.args, p.Limit, p.Offset)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.AlbumCount, &a.TrackCount, &a.Origin, &a.ImageUpdatedAt); err != nil {
			return out, err
		}
		out.Items = append(out.Items, a)
	}
	return out, rows.Err()
}

func (s *Scanner) GetArtist(ctx context.Context, id string) (Artist, error) {
	var a Artist
	err := s.db.QueryRowContext(ctx, `SELECT id, name, COALESCE(sort_name,''), COALESCE(mb_id,''), album_count, track_count, origin,
		COALESCE((SELECT updated_at FROM artist_images i WHERE i.artist_id=artists.id),0)
		FROM artists WHERE id=?`, id).Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.AlbumCount, &a.TrackCount, &a.Origin, &a.ImageUpdatedAt)
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

// albumCols selects an album with the caller's favorite flag and rating (D49).
func albumCols(ctx context.Context) string {
	o := ownerLit(ctx)
	return `al.id, al.title, COALESCE(al.artist_name,''), COALESCE(al.year,0), COALESCE(al.genre,''),
	al.track_count, COALESCE(al.disc_count,1), al.duration_ms, COALESCE(al.mb_release_id,''), al.cover_path IS NOT NULL, al.created_at,
	EXISTS(SELECT 1 FROM favorites f WHERE f.user_id=` + o + ` AND f.entity_type='album' AND f.entity_id=al.id),
	COALESCE((SELECT rating FROM ratings r WHERE r.user_id=` + o + ` AND r.entity_type='album' AND r.entity_id=al.id),0), al.origin`
}

// ownerLit is the caller's owner id as an SQL string literal (ids are
// server-generated; quoting still escapes any quote).
func ownerLit(ctx context.Context) string {
	return "'" + strings.ReplaceAll(owner.From(ctx).ID, "'", "''") + "'"
}

func scanAlbum(r interface{ Scan(...any) error }) (Album, error) {
	var a Album
	var created int64
	err := r.Scan(&a.ID, &a.Title, &a.ArtistName, &a.Year, &a.Genre, &a.TrackCount, &a.DiscCount, &a.DurationMs, &a.MBReleaseID, &a.HasCover, &created, &a.Favorite, &a.Rating, &a.Origin)
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
	addSearch(&w, p, `al.title`, `al.artist_name`, `al.search_text`)
	if p.Origin != "" && p.Origin != "all" {
		w.add(`al.origin=?`, p.Origin)
	}
	if p.Favorite {
		w.add(`al.id IN (SELECT entity_id FROM favorites WHERE user_id=? AND entity_type='album')`, owner.From(ctx).ID)
	}
	if p.ArtistID != "" {
		w.add(`al.id IN (SELECT album_id FROM album_artists WHERE artist_id=?
			UNION SELECT t.album_id FROM track_artists ta JOIN tracks t ON t.id=ta.track_id WHERE ta.artist_id=?)`, p.ArtistID, p.ArtistID)
	}
	out := Page[Album]{Items: []Album{}, Limit: p.Limit, Offset: p.Offset}
	if out.Total, err = count(ctx, s.db, `SELECT count(*) FROM albums al`+w.sql(), w.args); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumCols(ctx)+` FROM albums al`+w.sql()+ob+` LIMIT ? OFFSET ?`,
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
	a, err := scanAlbum(s.db.QueryRowContext(ctx, `SELECT `+albumCols(ctx)+` FROM albums al WHERE al.id=?`, id))
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
	"plays": "COALESCE(ts.play_count,0), t.id", "last_played": "COALESCE(ts.last_played_at,0), t.id",
	"rating": "COALESCE(rt.rating,0), t.id",
}

const trackCols = `t.id, t.title, COALESCE(t.artist_name,''), COALESCE(t.album_id,''), COALESCE(al.title,''),
	COALESCE(t.disc_no,0), COALESCE(t.track_no,0), COALESCE(t.year,0), COALESCE(t.genre,''), COALESCE(t.duration_ms,0),
	COALESCE(t.isrc,''), COALESCE(t.mb_recording_id,''), COALESCE(st.format,''), t.created_at,
	COALESCE(st.codec,''), COALESCE(st.sample_rate,0), COALESCE(st.bit_depth,0), COALESCE(st.channels,0),
	COALESCE(st.bitrate_kbps,0), COALESCE(st.lossless,0), COALESCE(st.size,0),
	COALESCE(ts.play_count,0), COALESCE(ts.last_played_at,0), fv.entity_id IS NOT NULL, COALESCE(rt.rating,0),
	st.rg_track_gain, st.rg_track_peak, st.rg_album_gain, st.rg_album_peak, COALESCE(st.rg_source,''), t.origin,
	(EXISTS(SELECT 1 FROM user_lyrics ul WHERE ul.track_id=t.id) OR COALESCE(st.lyrics,'')<>''), COALESCE(al.cover_path,'')`

// bestSource picks the same file TrackFile streams: highest confidence, FLAC first, then path.
const bestSource = `(SELECT l.source_track_id FROM track_links l JOIN source_tracks s2 ON s2.id=l.source_track_id
	WHERE l.track_id=t.id ORDER BY l.confidence DESC, CASE s2.format WHEN 'flac' THEN 0 ELSE 1 END, s2.path LIMIT 1)`

// trackFrom joins the caller's favorite/rating (D49).
func trackFrom(ctx context.Context) string {
	o := ownerLit(ctx)
	return ` FROM tracks t LEFT JOIN albums al ON al.id=t.album_id
	LEFT JOIN track_stats ts ON ts.track_id=t.id
	LEFT JOIN favorites fv ON fv.user_id=` + o + ` AND fv.entity_type='track' AND fv.entity_id=t.id
	LEFT JOIN ratings rt ON rt.user_id=` + o + ` AND rt.entity_type='track' AND rt.entity_id=t.id
	LEFT JOIN source_tracks st ON st.id=` + bestSource
}

func scanTrack(r interface{ Scan(...any) error }) (Track, error) {
	var t Track
	var created int64
	var si StreamInfo
	var lastPlayed int64
	var g Gain
	err := r.Scan(&t.ID, &t.Title, &t.ArtistName, &t.AlbumID, &t.AlbumTitle, &t.DiscNo, &t.TrackNo, &t.Year, &t.Genre,
		&t.DurationMs, &t.ISRC, &t.MBRecordingID, &t.Format, &created,
		&si.Codec, &si.SampleRate, &si.BitDepth, &si.Channels, &si.BitrateKbps, &si.Lossless, &si.SizeBytes,
		&t.PlayCount, &lastPlayed, &t.Favorite, &t.Rating,
		&g.TrackGain, &g.TrackPeak, &g.AlbumGain, &g.AlbumPeak, &g.Source, &t.Origin, &t.HasLyrics, &t.CoverPath)
	if g.Source != "" {
		si.ReplayGain = &g
	}
	t.CreatedAt = time.UnixMilli(created).UTC()
	t.SortLetter = search.SortLetter(t.Title)
	if lastPlayed > 0 {
		lp := time.UnixMilli(lastPlayed).UTC()
		t.LastPlayedAt = &lp
	}
	if t.Format != "" {
		si.Format = t.Format
		t.Stream = &si
		t.Audio = &si
	}
	t.Playable = true
	if t.AlbumID != "" {
		t.Album = &AlbumRef{ID: t.AlbumID, Name: t.AlbumTitle}
	}
	if si.BitrateKbps > 0 {
		si.Bitrate = si.BitrateKbps
	}
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
	addSearch(&w, p, `t.title`, `t.artist_name`, `al.title`, `t.search_text`)
	if p.Origin != "" && p.Origin != "all" {
		w.add(`t.origin=?`, p.Origin)
	}
	if p.AlbumID != "" {
		w.add(`t.album_id=?`, p.AlbumID)
	}
	if p.ArtistID != "" {
		w.add(`t.id IN (SELECT track_id FROM track_artists WHERE artist_id=?)`, p.ArtistID)
	}
	if p.Favorite {
		w.add(`t.id IN (SELECT entity_id FROM favorites WHERE user_id=? AND entity_type='track')`, owner.From(ctx).ID)
	}
	from := trackFrom(ctx)
	out := Page[Track]{Items: []Track{}, Limit: p.Limit, Offset: p.Offset}
	// counting needs no per-row source/stat joins
	if out.Total, err = count(ctx, s.db, `SELECT count(*) FROM tracks t LEFT JOIN albums al ON al.id=t.album_id`+w.sql(), w.args); err != nil {
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
	t, err := scanTrack(s.db.QueryRowContext(ctx, `SELECT `+trackCols+trackFrom(ctx)+` WHERE t.id=?`, id))
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
	if strings.HasPrefix(p.String, "https://") || strings.HasPrefix(p.String, "http://") {
		return p.String, nil // external album: remote cover URL (public CDN)
	}
	return s.safeFile(p.String, true)
}

func (s *Scanner) TrackCover(ctx context.Context, trackID string) (string, error) {
	if IsExternalID(trackID) {
		var al sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT album_id FROM tracks WHERE id=?`, trackID).Scan(&al); err != nil || !al.Valid {
			return "", ErrNotFound
		}
		return s.AlbumCover(ctx, al.String)
	}
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
	Mtime  int64 // unix ns (from the last scan)
	Stream StreamInfo
}

// TrackFile returns the best local file for a canonical track.
func (s *Scanner) TrackFile(ctx context.Context, trackID string) (TrackFile, error) {
	var f TrackFile
	err := s.db.QueryRowContext(ctx, `SELECT st.path, COALESCE(st.format,''), COALESCE(st.size,0), COALESCE(st.mtime,0),
		COALESCE(st.codec,''), COALESCE(st.sample_rate,0), COALESCE(st.bit_depth,0), COALESCE(st.channels,0),
		COALESCE(st.bitrate_kbps,0), COALESCE(st.lossless,0) FROM track_links l
		JOIN source_tracks st ON st.id=l.source_track_id
		WHERE l.track_id=? AND st.source=? AND st.path IS NOT NULL
		ORDER BY l.confidence DESC, CASE st.format WHEN 'flac' THEN 0 ELSE 1 END, st.path LIMIT 1`, trackID, SourceLocal).
		Scan(&f.Path, &f.Format, &f.Size, &f.Mtime, &f.Stream.Codec, &f.Stream.SampleRate, &f.Stream.BitDepth,
			&f.Stream.Channels, &f.Stream.BitrateKbps, &f.Stream.Lossless)
	f.Stream.Format, f.Stream.SizeBytes = f.Format, f.Size
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

type Credit struct {
	TrackID string `json:"track_id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	MBID    string `json:"mb_id,omitempty"`
	Source  string `json:"source,omitempty"`
}

// AlbumCredits returns credits for every track on an album (disc/track order).
func (s *Scanner) AlbumCredits(ctx context.Context, albumID string) ([]Credit, error) {
	return s.credits(ctx, `JOIN tracks t ON t.id=c.track_id WHERE t.album_id=? ORDER BY t.disc_no, t.track_no, c.role, c.name`, albumID)
}

func (s *Scanner) TrackCredits(ctx context.Context, trackID string) ([]Credit, error) {
	return s.credits(ctx, `WHERE c.track_id=? ORDER BY c.role, c.name`, trackID)
}

func (s *Scanner) credits(ctx context.Context, tail string, arg string) ([]Credit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.track_id, c.role, c.name, COALESCE(c.mb_id,''), COALESCE(c.source,'') FROM credits c `+tail, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credit{}
	for rows.Next() {
		var c Credit
		if err := rows.Scan(&c.TrackID, &c.Role, &c.Name, &c.MBID, &c.Source); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DB exposes the handle for tests and wiring.
func (s *Scanner) DB() *sql.DB { return s.db }

// SafePath validates that p is a regular file inside a library root (symlinks resolved).
func (s *Scanner) SafePath(p string) (string, error) { return s.safeFile(p, false) }

// UpdatedAt is the newest catalog change (artists, albums, tracks).
func (s *Scanner) UpdatedAt(ctx context.Context) time.Time {
	var ms sql.NullInt64
	s.db.QueryRowContext(ctx, `SELECT MAX(m) FROM (SELECT MAX(updated_at) m FROM tracks UNION ALL SELECT MAX(updated_at) FROM albums UNION ALL SELECT MAX(updated_at) FROM artists)`).Scan(&ms)
	if !ms.Valid {
		return time.Time{}
	}
	return time.UnixMilli(ms.Int64).UTC()
}
