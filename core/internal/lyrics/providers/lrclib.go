package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// LRCLIB talks to https://lrclib.net (public, no auth, rate-limited by the host).
type LRCLIB struct {
	Base   string
	Client *http.Client
	UA     string
}

func (l *LRCLIB) ID() string { return "lrclib" }

func (l *LRCLIB) client() *http.Client {
	if l.Client != nil {
		return l.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (l *LRCLIB) base() string {
	if l.Base != "" {
		return strings.TrimRight(l.Base, "/")
	}
	return "https://lrclib.net"
}

func (l *LRCLIB) Fetch(ctx context.Context, t library.Track) (string, error) {
	q := url.Values{}
	q.Set("track_name", t.Title)
	q.Set("artist_name", t.ArtistName)
	if t.AlbumTitle != "" {
		q.Set("album_name", t.AlbumTitle)
	}
	if t.DurationMs > 0 {
		q.Set("duration", strconv.FormatInt((t.DurationMs+500)/1000, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base()+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	ua := l.UA
	if ua == "" {
		ua = "yimeng-core/dev (lyrics; +https://github.com/my-name-is-alan/yimeng)"
	}
	req.Header.Set("User-Agent", ua)
	res, err := l.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("lrclib: HTTP %d", res.StatusCode)
	}
	var body struct {
		SyncedLyrics string `json:"syncedLyrics"`
		PlainLyrics  string `json:"plainLyrics"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.SyncedLyrics != "" {
		return body.SyncedLyrics, nil
	}
	if body.PlainLyrics != "" {
		return body.PlainLyrics, nil
	}
	return "", ErrNotFound
}

// Candidate is one LRCLIB search hit shown to an admin for manual binding.
type Candidate struct {
	ID           int64  `json:"id"`
	Ref          string `json:"ref,omitempty"` // platform track id (non-LRCLIB sources)
	Provider     string `json:"provider"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	DurationMs   int64  `json:"duration_ms"`
	Instrumental bool   `json:"instrumental"`
	Synced       bool   `json:"synced"`
	SyncedLyrics string `json:"synced_lyrics,omitempty"`
	PlainLyrics  string `json:"plain_lyrics,omitempty"`
}

// Search queries /api/search (free text q, or title+artist). At most 20 hits.
func (l *LRCLIB) Search(ctx context.Context, q, title, artist string) ([]Candidate, error) {
	v := url.Values{}
	if strings.TrimSpace(q) != "" {
		v.Set("q", q)
	} else {
		v.Set("track_name", title)
		if artist != "" {
			v.Set("artist_name", artist)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base()+"/api/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	ua := l.UA
	if ua == "" {
		ua = "yimeng-core/dev (lyrics; +https://github.com/my-name-is-alan/yimeng)"
	}
	req.Header.Set("User-Agent", ua)
	res, err := l.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("lrclib: HTTP %d", res.StatusCode)
	}
	var hits []struct {
		ID           int64   `json:"id"`
		TrackName    string  `json:"trackName"`
		ArtistName   string  `json:"artistName"`
		AlbumName    string  `json:"albumName"`
		Duration     float64 `json:"duration"`
		Instrumental bool    `json:"instrumental"`
		PlainLyrics  string  `json:"plainLyrics"`
		SyncedLyrics string  `json:"syncedLyrics"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&hits); err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, min(len(hits), 20))
	for _, h := range hits {
		if len(out) == 20 {
			break
		}
		out = append(out, Candidate{ID: h.ID, Provider: "lrclib", Title: h.TrackName, Artist: h.ArtistName, Album: h.AlbumName,
			DurationMs: int64(h.Duration * 1000), Instrumental: h.Instrumental, Synced: h.SyncedLyrics != "",
			SyncedLyrics: h.SyncedLyrics, PlainLyrics: h.PlainLyrics})
	}
	return out, nil
}
