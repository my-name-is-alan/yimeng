package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/rank"
	"github.com/my-name-is-alan/yimeng/core/internal/online/anon"
)

// Public lyrics API for paired apps (any authenticated user/device, not
// admin-only): look up lyrics for ANY song, in or out of the library.
//
//	GET /api/v1/lyrics/search?title=&artist=&album=&duration=&isrc=
//	GET /api/v1/lyrics/best?…same…
//
// Local/user-bound lyrics and every remote source (LRCLIB, NetEase, QQ,
// Kugou) are queried in parallel with per-source timeouts, scored by
// lyrics/rank, cached, and rate-limited per client on cache misses.

const (
	lyricsCacheTTL      = 6 * time.Hour
	lyricsEmptyCacheTTL = 30 * time.Minute
	lyricsCacheMax      = 2000
	lyricsNoneMessage   = "暂无歌词"
)

type lyricsCacheEntry struct {
	at    time.Time
	items []providers.Candidate
}

type lyricsAPIState struct {
	mu    sync.Mutex
	cache map[string]lyricsCacheEntry
	rl    *RateLimits
}

var lyricsAPI = &lyricsAPIState{cache: map[string]lyricsCacheEntry{}, rl: NewRateLimits(0.5, 10, 0)} // 30/min, burst 10

func (s *lyricsAPIState) get(key string) ([]providers.Candidate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	ttl := lyricsCacheTTL
	if len(e.items) == 0 {
		ttl = lyricsEmptyCacheTTL
	}
	if time.Since(e.at) > ttl {
		delete(s.cache, key)
		return nil, false
	}
	return e.items, true
}

func (s *lyricsAPIState) put(key string, items []providers.Candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= lyricsCacheMax {
		for k := range s.cache { // drop an arbitrary ~10%
			delete(s.cache, k)
			if len(s.cache) < lyricsCacheMax*9/10 {
				break
			}
		}
	}
	s.cache[key] = lyricsCacheEntry{at: time.Now(), items: items}
}

func lyricsQuery(r *http.Request) (rank.Query, error) {
	v := r.URL.Query()
	q := rank.Query{Title: strings.TrimSpace(v.Get("title")), Artist: strings.TrimSpace(v.Get("artist")),
		Album: strings.TrimSpace(v.Get("album")), ISRC: strings.ToUpper(strings.TrimSpace(v.Get("isrc")))}
	if q.Title == "" {
		return q, fmt.Errorf("title is required")
	}
	if d := v.Get("duration"); d != "" {
		f, err := strconv.ParseFloat(d, 64)
		if err != nil || f < 0 {
			return q, fmt.Errorf("duration must be seconds (number)")
		}
		q.DurationMs = int64(f * 1000)
	}
	if d := v.Get("duration_ms"); d != "" {
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil || n < 0 {
			return q, fmt.Errorf("duration_ms must be a non-negative integer")
		}
		q.DurationMs = n
	}
	return q, nil
}

func lyricsKey(q rank.Query) string {
	n := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", n(q.Title), n(q.Artist), n(q.Album), q.DurationMs/1000, q.ISRC)
}

// formatLRC rebuilds an LRC from parsed lines (for local/user lyrics).
func formatLRC(ls []lyrics.Line) string {
	var b strings.Builder
	for _, l := range ls {
		ms := max(l.TimeMs, 0)
		fmt.Fprintf(&b, "[%02d:%02d.%02d]%s\n", ms/60000, ms/1000%60, ms%1000/10, l.Text)
	}
	return b.String()
}

// localLyricsCandidates returns lyrics already known to this Core: user-bound,
// sidecar or embedded, for library tracks matching the title (or ISRC).
func (d Deps) localLyricsCandidates(ctx context.Context, q rank.Query) []providers.Candidate {
	if d.Library == nil {
		return nil
	}
	page, err := d.Library.ListTracks(ctx, library.ListParams{Q: q.Title, Limit: 10})
	if err != nil {
		return nil
	}
	var out []providers.Candidate
	for _, t := range page.Items {
		isrcHit := q.ISRC != "" && strings.EqualFold(t.ISRC, q.ISRC)
		if !isrcHit && anon.Sim(q.Title, t.Title) < 0.6 {
			continue
		}
		l, err := d.Library.Lyrics(ctx, t.ID, nil)
		if err != nil || len(l.Lines) == 0 {
			continue
		}
		prov := "local"
		if strings.HasPrefix(l.Source, "user") {
			prov = "user"
		}
		c := providers.Candidate{Ref: "track:" + t.ID, Provider: prov, Title: t.Title, Artist: t.ArtistName,
			Album: t.AlbumTitle, DurationMs: t.DurationMs, Synced: l.Synced}
		if l.Synced {
			c.SyncedLyrics = formatLRC(l.Lines)
		} else {
			c.PlainLyrics = l.Text
		}
		out = append(out, c)
	}
	return out
}

// rankedLyrics runs (or serves from cache) the full lookup.
func (d Deps) rankedLyrics(w http.ResponseWriter, r *http.Request) (rank.Query, []providers.Candidate, bool) {
	q, err := lyricsQuery(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return q, nil, false
	}
	key := lyricsKey(q)
	if items, ok := lyricsAPI.get(key); ok {
		w.Header().Set("X-Yimeng-Cache", "hit")
		return q, items, true
	}
	if ok, wait := lyricsAPI.rl.allow(rateKey(r)); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many lyrics lookups; retry later")
		return q, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var (
		wg            sync.WaitGroup
		local, remote []providers.Candidate
	)
	if q.Artist != "" && d.ArtistAliases != nil {
		wg.Add(1)
		go func() { defer wg.Done(); q.ArtistAliases = d.ArtistAliases(ctx, q.Artist) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); local = d.localLyricsCandidates(ctx, q) }()
	if d.LyricsSearch != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			remote, _ = d.LyricsSearch(ctx, "", q.Title, q.Artist) // partial results are fine
		}()
	}
	wg.Wait()
	items := append(local, remote...)
	kept := items[:0]
	for _, c := range items {
		if c.Instrumental || (strings.TrimSpace(c.SyncedLyrics) == "" && strings.TrimSpace(c.PlainLyrics) == "") {
			continue
		}
		kept = append(kept, c)
	}
	items = rank.CapPerProvider(rank.Rank(q, kept), 5)
	if items == nil {
		items = []providers.Candidate{}
	}
	lyricsAPI.put(key, items)
	w.Header().Set("X-Yimeng-Cache", "miss")
	return q, items, true
}

// lyricsCandidateView is a search hit without the full lyric text (keeps the
// list small); preview holds the first few lines.
type lyricsCandidateView struct {
	ID             string                    `json:"id"`
	Provider       string                    `json:"provider"`
	Title          string                    `json:"title"`
	Artist         string                    `json:"artist"`
	Album          string                    `json:"album,omitempty"`
	DurationMs     int64                     `json:"duration_ms,omitempty"`
	Synced         bool                      `json:"synced"`
	HasTranslation bool                      `json:"has_translation"`
	Score          int                       `json:"score"`
	Breakdown      *providers.ScoreBreakdown `json:"score_breakdown"`
	Preview        []string                  `json:"preview"`
}

func candidateID(c providers.Candidate) string {
	if c.Ref != "" {
		if strings.HasPrefix(c.Ref, "track:") || strings.Contains(c.Ref, ":") {
			return c.Ref
		}
		return c.Provider + ":" + c.Ref
	}
	return fmt.Sprintf("lrclib:%d", c.ID)
}

func preview(c providers.Candidate, n int) []string {
	src := c.SyncedLyrics
	if src == "" {
		src = c.PlainLyrics
	}
	out := []string{}
	for _, l := range lyrics.Parse(src).Lines {
		if t := strings.TrimSpace(l.Text); t != "" {
			out = append(out, t)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

func (d Deps) lyricsSearchAPI(w http.ResponseWriter, r *http.Request) {
	q, items, ok := d.rankedLyrics(w, r)
	if !ok {
		return
	}
	views := make([]lyricsCandidateView, 0, len(items))
	for _, c := range items {
		views = append(views, lyricsCandidateView{ID: candidateID(c), Provider: c.Provider, Title: c.Title, Artist: c.Artist,
			Album: c.Album, DurationMs: c.DurationMs, Synced: c.Synced, HasTranslation: c.Translation != "",
			Score: c.Score, Breakdown: c.Breakdown, Preview: preview(c, 3)})
	}
	resp := map[string]any{"query": q, "found": len(views) > 0, "items": views}
	if len(views) == 0 {
		resp["message"] = lyricsNoneMessage
	}
	writeJSON(w, http.StatusOK, resp)
}

// LyricsBestLine is one timed line with its translation (if any).
type LyricsBestLine struct {
	TMs   int64  `json:"t_ms"`
	Text  string `json:"text"`
	Trans string `json:"trans,omitempty"`
}

// fillLyricsQuery turns ?track_id= into title/artist/album/duration.
func (d Deps) fillLyricsQuery(r *http.Request) {
	v := r.URL.Query()
	id := v.Get("track_id")
	if id == "" || v.Get("title") != "" {
		return
	}
	set := func(title, artist, album string, dur int64) {
		v.Set("title", title)
		v.Set("artist", artist)
		if album != "" {
			v.Set("album", album)
		}
		if dur > 0 {
			v.Set("duration_ms", strconv.FormatInt(dur, 10))
		}
		r.URL.RawQuery = v.Encode()
	}
	if t, err := d.Library.GetTrack(r.Context(), id); err == nil {
		set(t.Title, t.ArtistName, t.AlbumTitle, t.DurationMs)
		return
	}
	if d.Online != nil && library.IsExternalID(id) {
		if t, err := d.Online.Track(r.Context(), id); err == nil {
			al := ""
			if t.Album != nil {
				al = t.Album.Name
			}
			set(t.Title, t.ArtistName, al, t.DurationMs)
		}
	}
}

// GET /api/v1/lyrics/best?track_id= | ?title=&artist=…
//
// Merged lyrics (D46): text from the best-scoring candidate; words, trans and
// roma each from the best source that has them among candidates that match
// the song (score ≥ min_score), aligned by timestamp. Word priority: Kugou
// KRC > QQ QRC > NetEase YRC.
func (d Deps) lyricsBestAPI(w http.ResponseWriter, r *http.Request) {
	d.fillLyricsQuery(r)
	if r.URL.Query().Get("title") == "" && r.URL.Query().Get("track_id") != "" {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "message": lyricsNoneMessage})
		return
	}
	q, items, ok := d.rankedLyrics(w, r)
	if !ok {
		return
	}
	minScore := 50
	if v := r.URL.Query().Get("min_score"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100 {
			minScore = n
		}
	}
	if len(items) == 0 || items[0].Score < minScore {
		writeJSON(w, http.StatusOK, map[string]any{"query": q, "found": false, "message": lyricsNoneMessage})
		return
	}
	c := items[0]
	parsed := lyrics.Parse(firstNonEmpty(c.SyncedLyrics, c.PlainLyrics))
	var srcs []lyrics.Source
	for _, it := range items {
		if it.Score < minScore {
			continue
		}
		srcs = append(srcs, lyrics.Source{Provider: it.Provider, Lines: lyrics.Parse(firstNonEmpty(it.SyncedLyrics, it.PlainLyrics)).Lines,
			Trans: lyrics.Parse(it.Translation).Lines, Roma: lyrics.Parse(it.Roma).Lines, Words: it.Words})
	}
	m := lyrics.Merge(srcs)
	writeJSON(w, http.StatusOK, map[string]any{
		"query": q, "found": true, "source": c.Provider, "sources": m.Sources,
		"id": candidateID(c), "provider": c.Provider, "title": c.Title, "artist": c.Artist, "album": c.Album,
		"duration_ms": c.DurationMs, "synced": parsed.Synced, "score": c.Score, "score_breakdown": c.Breakdown,
		"offset_ms": 0, "lrc": c.SyncedLyrics, "plain": firstNonEmpty(c.PlainLyrics, parsed.Text),
		"translation": c.Translation, "romanization": c.Roma, "has_words": m.Sources["words"] != "", "lines": m.Lines,
	})
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
