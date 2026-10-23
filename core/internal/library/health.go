package library

import (
	"context"
	"fmt"
)

type HealthReport struct {
	Tracks          int            `json:"tracks"`
	Albums          int            `json:"albums"`
	Artists         int            `json:"artists"`
	MissingFiles    int            `json:"missing_files"`
	NoCoverAlbums   int            `json:"no_cover_albums"`
	NoGenreTracks   int            `json:"no_genre_tracks"`
	DuplicatePaths  int            `json:"duplicate_paths"`
	OrphanOverrides int            `json:"orphan_overrides"`
	ByFormat        map[string]int `json:"by_format"`
}

func (s *Scanner) Health(ctx context.Context) (HealthReport, error) {
	var h HealthReport
	h.ByFormat = map[string]int{}
	q := func(dst *int, sqls string, args ...any) error {
		return s.db.QueryRowContext(ctx, sqls, args...).Scan(dst)
	}
	if err := q(&h.Tracks, `SELECT count(*) FROM tracks`); err != nil {
		return h, err
	}
	if err := q(&h.Albums, `SELECT count(*) FROM albums`); err != nil {
		return h, err
	}
	if err := q(&h.Artists, `SELECT count(*) FROM artists`); err != nil {
		return h, err
	}
	_ = q(&h.NoCoverAlbums, `SELECT count(*) FROM albums WHERE cover_path IS NULL`)
	_ = q(&h.NoGenreTracks, `SELECT count(*) FROM tracks WHERE genre IS NULL OR genre=''`)
	_ = q(&h.OrphanOverrides, `SELECT count(*) FROM metadata_overrides o
		WHERE (o.entity_type='track' AND NOT EXISTS(SELECT 1 FROM tracks t WHERE t.id=o.entity_id))
		OR (o.entity_type='album' AND NOT EXISTS(SELECT 1 FROM albums a WHERE a.id=o.entity_id))
		OR (o.entity_type='artist' AND NOT EXISTS(SELECT 1 FROM artists a WHERE a.id=o.entity_id))`)
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(format,''), count(*) FROM source_tracks WHERE source=? GROUP BY format`, SourceLocal)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var f string
			var n int
			rows.Scan(&f, &n)
			if f == "" {
				f = "unknown"
			}
			h.ByFormat[f] = n
		}
	}
	// missing files: source path gone — best-effort cheap check via count where path empty
	_ = q(&h.MissingFiles, `SELECT count(*) FROM source_tracks WHERE source=? AND (path IS NULL OR path='')`, SourceLocal)
	return h, nil
}

type Stats struct {
	Tracks     int            `json:"tracks"`
	Albums     int            `json:"albums"`
	Artists    int            `json:"artists"`
	PlayCount  int            `json:"play_count"`
	Favorites  int            `json:"favorites"`
	Playlists  int            `json:"playlists"`
	TopArtists []StatItem     `json:"top_artists"`
	TopTracks  []StatItem     `json:"top_tracks"`
	ByYear     map[string]int `json:"plays_by_year,omitempty"`
}

type StatItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type OnThisDay struct {
	MonthDay string     `json:"month_day"` // MM-DD
	Items    []StatItem `json:"items"`
}

func (s *Scanner) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	q := func(dst *int, sqls string) error { return s.db.QueryRowContext(ctx, sqls).Scan(dst) }
	_ = q(&st.Tracks, `SELECT count(*) FROM tracks`)
	_ = q(&st.Albums, `SELECT count(*) FROM albums`)
	_ = q(&st.Artists, `SELECT count(*) FROM artists`)
	_ = q(&st.PlayCount, `SELECT COALESCE(sum(play_count),0) FROM track_stats`)
	_ = q(&st.Favorites, `SELECT count(*) FROM favorites`)
	_ = q(&st.Playlists, `SELECT count(*) FROM playlists`)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.title, COALESCE(ts.play_count,0) FROM tracks t
		JOIN track_stats ts ON ts.track_id=t.id ORDER BY ts.play_count DESC LIMIT 10`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var it StatItem
			rows.Scan(&it.ID, &it.Name, &it.Count)
			st.TopTracks = append(st.TopTracks, it)
		}
	}
	rows2, err := s.db.QueryContext(ctx, `SELECT a.id, a.name, count(*) FROM plays h
		JOIN tracks t ON t.id=h.track_id JOIN track_artists ta ON ta.track_id=t.id
		JOIN artists a ON a.id=ta.artist_id GROUP BY a.id ORDER BY count(*) DESC LIMIT 10`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var it StatItem
			rows2.Scan(&it.ID, &it.Name, &it.Count)
			st.TopArtists = append(st.TopArtists, it)
		}
	}
	if st.TopTracks == nil {
		st.TopTracks = []StatItem{}
	}
	if st.TopArtists == nil {
		st.TopArtists = []StatItem{}
	}
	return st, nil
}

func (s *Scanner) OnThisDay(ctx context.Context, month, day int) (OnThisDay, error) {
	out := OnThisDay{MonthDay: "", Items: []StatItem{}}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return out, fmt.Errorf("%w: invalid month/day", ErrBadSort)
	}
	out.MonthDay = sprintfMD(month, day)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.title, count(*) FROM plays h
		JOIN tracks t ON t.id=h.track_id
		WHERE CAST(strftime('%m', datetime(h.played_at/1000, 'unixepoch')) AS INTEGER)=?
		  AND CAST(strftime('%d', datetime(h.played_at/1000, 'unixepoch')) AS INTEGER)=?
		GROUP BY t.id ORDER BY count(*) DESC LIMIT 20`, month, day)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var it StatItem
		rows.Scan(&it.ID, &it.Name, &it.Count)
		out.Items = append(out.Items, it)
	}
	return out, nil
}

func sprintfMD(m, d int) string { return fmt.Sprintf("%02d-%02d", m, d) }
