package library

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
)

var ErrNoLyrics = errors.New("no lyrics")

type TrackLyrics struct {
	TrackID string `json:"track_id"`
	Source  string `json:"source"` // sidecar | embedded | <provider>
	lyrics.Lyrics
}

// LyricsFetcher is an optional remote provider consulted when no local lyrics exist.
type LyricsFetcher func(ctx context.Context, t Track) (string, string, error)

// Lyrics resolves lyrics: sidecar .lrc next to the best file (synced, edit
// without rescan) → embedded tag → optional fetcher. Synced lyrics win over
// plain ones from a lower tier only if the higher tier is plain.
func (s *Scanner) Lyrics(ctx context.Context, trackID string, fetch LyricsFetcher) (TrackLyrics, error) {
	out := TrackLyrics{TrackID: trackID}
	var embedded sql.NullString
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(st.path,''), st.lyrics FROM track_links l
		JOIN source_tracks st ON st.id=l.source_track_id WHERE l.track_id=?
		ORDER BY st.lyrics IS NULL, l.confidence DESC, st.path LIMIT 1`, trackID).Scan(&path, &embedded)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	var cands []TrackLyrics
	if f, err := s.TrackFile(ctx, trackID); err == nil {
		if txt, ok := s.sidecar(f.Path); ok {
			cands = append(cands, TrackLyrics{TrackID: trackID, Source: "sidecar", Lyrics: lyrics.Parse(txt)})
		}
	}
	if embedded.Valid && strings.TrimSpace(embedded.String) != "" {
		cands = append(cands, TrackLyrics{TrackID: trackID, Source: "embedded", Lyrics: lyrics.Parse(embedded.String)})
	}
	for _, c := range cands {
		if c.Synced {
			return c, nil
		}
	}
	if len(cands) == 0 && fetch != nil {
		if t, err := s.GetTrack(ctx, trackID); err == nil {
			if txt, src, err := fetch(ctx, t); err == nil && strings.TrimSpace(txt) != "" {
				return TrackLyrics{TrackID: trackID, Source: src, Lyrics: lyrics.Parse(txt)}, nil
			}
		}
	}
	if len(cands) > 0 {
		return cands[0], nil
	}
	return out, ErrNoLyrics
}

// sidecar reads <basename>.lrc (or .LRC / .txt) beside the audio file, inside library roots, ≤1 MiB.
func (s *Scanner) sidecar(audio string) (string, bool) {
	base := strings.TrimSuffix(audio, filepath.Ext(audio))
	for _, ext := range []string{".lrc", ".LRC", ".Lrc"} {
		p, err := s.safeFile(base+ext, false)
		if err != nil {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, 1<<20))
		f.Close()
		if err == nil && len(b) > 0 {
			return lyrics.Decode(b), true
		}
	}
	return "", false
}
