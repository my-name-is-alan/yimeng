// Package anon exposes the platform plugins' anonymous (no-login) catalog
// endpoints — search, track/album detail and lyrics — as metadata providers
// and lyrics candidate sources. Each source runs on its own throwaway,
// cookie-less client, so it works whether or not the streaming plugin is
// enabled or logged in, and never uses (or leaks) an account session.
package anon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/search"
)

// Source is one platform used anonymously.
type Source struct {
	id   metadata.ProviderID
	name string
	cat  online.Catalog
	hc   *http.Client
}

// New builds an anonymous source from a plugin factory. perMinute rate-limits
// requests to the platform (shared by matching, lyrics and the enricher).
func New(id metadata.ProviderID, f online.Factory, perMinute int, hc *http.Client) (*Source, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	p, err := f(online.Env{HTTP: online.NewClient("anon-"+string(id), perMinute, hc), Config: online.Config{}})
	if err != nil {
		return nil, err
	}
	cat, ok := p.(online.Catalog)
	if !ok {
		return nil, fmt.Errorf("anon: %s has no catalog", id)
	}
	return &Source{id: id, name: p.Manifest().Name, cat: cat, hc: hc}, nil
}

// NewCatalog wraps an existing catalog (tests).
func NewCatalog(id metadata.ProviderID, name string, cat online.Catalog, hc *http.Client) *Source {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Source{id: id, name: name, cat: cat, hc: hc}
}

func (s *Source) ID() metadata.ProviderID { return s.id }
func (s *Source) Name() string            { return s.name }

func (s *Source) Capabilities() metadata.Capability {
	return metadata.CapTrackTags | metadata.CapAlbumInfo | metadata.CapCoverArt | metadata.CapLyrics
}

func (s *Source) Health(ctx context.Context) metadata.HealthState { return metadata.HealthOK }

func wrap(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, online.ErrRiskControl):
		return fmt.Errorf("%w: %v", metadata.ErrRateLimited, err)
	case errors.Is(err, online.ErrUnavailable) || errors.Is(err, online.ErrNeedsLogin):
		return fmt.Errorf("%w: %v", metadata.ErrNotFound, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return fmt.Errorf("%w: %v", metadata.ErrUnavailable, err)
}

func trackQuery(q metadata.TrackQuery) string {
	return strings.TrimSpace(q.Title + " " + q.Artist)
}

// Score compares a candidate with the local tags (title, artist, duration);
// Chinese names are compared on toneless pinyin too (叶惠美 ≈ 葉惠美).
func Score(title, artist string, dur int64, t online.Track) float64 {
	st := Sim(title, t.Title)
	if st == 0 {
		return 0
	}
	sa := 0.7
	if artist != "" {
		sa = Sim(artist, t.ArtistName)
		for _, a := range t.Artists {
			sa = max(sa, Sim(artist, a.Name))
		}
	}
	sc := 0.6*st + 0.4*sa
	if dur > 0 && t.DurationMs > 0 {
		d := dur - t.DurationMs
		if d < 0 {
			d = -d
		}
		switch {
		case d > 10000:
			sc *= 0.6
		case d > 3000:
			sc *= 0.85
		}
	}
	return sc
}

var paren = regexp.MustCompile(`\s*[\(（\[【][^\)）\]】]*[\)）\]】]\s*`)

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

// Sim is a 0..1 name similarity (exact 1, pinyin-equal 0.95, without
// bracketed suffixes 0.8, containment 0.6).
func Sim(a, b string) float64 {
	a, b = norm(a), norm(b)
	switch {
	case a == "" || b == "":
		return 0
	case a == b:
		return 1
	}
	pa, pb := search.Pinyin(a), search.Pinyin(b)
	if (search.HasHan(a) || search.HasHan(b)) && pa != "" && pa == pb {
		return 0.95
	}
	if ca, cb := norm(paren.ReplaceAllString(a, " ")), norm(paren.ReplaceAllString(b, " ")); ca != "" && ca == cb {
		return 0.8
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 0.6
	}
	return 0
}

func (s *Source) LookupTrack(ctx context.Context, q metadata.TrackQuery) ([]metadata.TrackMatch, error) {
	if q.Title == "" {
		return nil, metadata.ErrNotSupported // MBID/ISRC-only lookups are MusicBrainz's job
	}
	res, err := s.cat.Search(ctx, trackQuery(q), 10)
	if err != nil {
		return nil, wrap(err)
	}
	var out []metadata.TrackMatch
	for _, t := range res.Tracks {
		sc := Score(q.Title, q.Artist, q.DurationMs, t)
		if sc == 0 {
			continue
		}
		m := metadata.TrackMatch{Provider: s.id, ExternalID: t.ID, Score: sc, Title: t.Title, Artist: t.ArtistName,
			TrackNo: t.TrackNo, DiscNo: t.DiscNo, Year: t.Year, DurationMs: t.DurationMs, ISRC: t.ISRC, CoverURL: httpsURL(t.CoverURL)}
		if t.Album != nil {
			m.Album = t.Album.Name
			if q.Album != "" && Sim(q.Album, t.Album.Name) >= 0.9 {
				m.Score = min(1, m.Score+0.05)
			}
		}
		out = append(out, m)
	}
	sortMatches(out)
	if len(out) == 0 {
		return nil, metadata.ErrNotFound
	}
	return out, nil
}

func sortMatches(ms []metadata.TrackMatch) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].Score > ms[j-1].Score; j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

func (s *Source) LookupAlbum(ctx context.Context, q metadata.AlbumQuery) ([]metadata.AlbumInfo, error) {
	if q.Title == "" {
		return nil, metadata.ErrNotSupported
	}
	res, err := s.cat.Search(ctx, strings.TrimSpace(q.Title+" "+q.AlbumArtist), 10)
	if err != nil {
		return nil, wrap(err)
	}
	var out []metadata.AlbumInfo
	for _, a := range res.Albums {
		st := Sim(q.Title, a.Title)
		if st == 0 {
			continue
		}
		sa := 0.7
		if q.AlbumArtist != "" {
			sa = Sim(q.AlbumArtist, a.ArtistName)
			for _, r := range a.Artists {
				sa = max(sa, Sim(q.AlbumArtist, r.Name))
			}
		}
		sc := 0.6*st + 0.4*sa
		if q.Year > 0 && a.Year > 0 && q.Year != a.Year {
			sc *= 0.9
		}
		out = append(out, metadata.AlbumInfo{Provider: s.id, ExternalID: a.ID, Title: a.Title, AlbumArtist: a.ArtistName,
			Year: a.Year, Description: a.Description, Score: sc, CoverURL: httpsURL(a.CoverURL), TrackCount: a.TrackCount})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score > out[j-1].Score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) == 0 {
		return nil, metadata.ErrNotFound
	}
	return out, nil
}

// LookupArtist returns name-matching artists with their platform picture.
func (s *Source) LookupArtist(ctx context.Context, q metadata.ArtistQuery) ([]metadata.ArtistInfo, error) {
	if strings.TrimSpace(q.Name) == "" {
		return nil, metadata.ErrNotSupported
	}
	res, err := s.cat.Search(ctx, q.Name, 10)
	if err != nil {
		return nil, wrap(err)
	}
	var out []metadata.ArtistInfo
	seen := map[string]bool{}
	add := func(id, name, pic string) {
		if seen[id] || Sim(q.Name, name) < 0.9 {
			return
		}
		seen[id] = true
		in := metadata.ArtistInfo{Provider: s.id, ExternalID: id, Name: name}
		if pic != "" {
			in.ImageURLs = []string{httpsURL(pic)}
		}
		out = append(out, in)
	}
	for _, a := range res.Artists {
		add(a.ID, a.Name, a.PictureURL)
	}
	if len(out) == 0 {
		return nil, metadata.ErrNotFound
	}
	return out, nil
}

// CoverArt returns the best-matching album's cover (query by title/artist).
func (s *Source) CoverArt(ctx context.Context, q metadata.AlbumQuery) ([]metadata.CoverCandidate, error) {
	as, err := s.LookupAlbum(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []metadata.CoverCandidate
	for _, a := range as {
		if a.CoverURL != "" && a.Score >= 0.85 {
			out = append(out, metadata.CoverCandidate{Provider: s.id, URL: a.CoverURL})
		}
	}
	if len(out) == 0 {
		return nil, metadata.ErrNotFound
	}
	return out, nil
}

const maxImage = 10 << 20

func (s *Source) FetchCover(ctx context.Context, c metadata.CoverCandidate) (io.ReadCloser, error) {
	if !allowedImageHost(c.URL) {
		return nil, fmt.Errorf("%w: image host not allowed", metadata.ErrNotFound)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.hc.Do(req)
	if err != nil {
		return nil, wrap(err)
	}
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		res.Body.Close()
		return nil, fmt.Errorf("%w: cover HTTP %d", metadata.ErrNotFound, res.StatusCode)
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(res.Body, maxImage), res.Body}, nil
}

var lrcTag = regexp.MustCompile(`^\[(\d+):(\d+)(?:[.:](\d+))?\]`)

// ParseLRC turns LRC text into synced lines (empty if none are timed).
func ParseLRC(s string) []metadata.LyricLine {
	var out []metadata.LyricLine
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		var tags []time.Duration
		for {
			m := lrcTag.FindStringSubmatch(ln)
			if m == nil {
				break
			}
			mi, _ := strconv.Atoi(m[1])
			se, _ := strconv.Atoi(m[2])
			fr := 0
			if m[3] != "" {
				f := (m[3] + "000")[:3]
				fr, _ = strconv.Atoi(f)
			}
			tags = append(tags, time.Duration(mi)*time.Minute+time.Duration(se)*time.Second+time.Duration(fr)*time.Millisecond)
			ln = ln[len(m[0]):]
		}
		for _, t := range tags {
			out = append(out, metadata.LyricLine{At: t, Text: strings.TrimSpace(ln)})
		}
	}
	return out
}

func (s *Source) Lyrics(ctx context.Context, q metadata.TrackQuery) (*metadata.Lyrics, error) {
	ms, err := s.LookupTrack(ctx, q)
	if err != nil {
		return nil, err
	}
	if ms[0].Score < 0.85 {
		return nil, metadata.ErrNotFound
	}
	l, err := s.cat.Lyrics(ctx, nativeID(ms[0].ExternalID))
	if err != nil {
		return nil, wrap(err)
	}
	out := &metadata.Lyrics{Provider: s.id, Synced: ParseLRC(l.LRC), Translation: ParseLRC(l.Translation)}
	if len(out.Synced) == 0 {
		out.Plain = l.LRC
	}
	return out, nil
}

func nativeID(id string) string {
	if _, n, ok := strings.Cut(id, ":"); ok {
		return n
	}
	return id
}

// LyricsCandidate is one lyrics search hit (for the lyrics workbench).
type LyricsCandidate struct {
	Ref         string
	Provider    string
	Title       string
	Artist      string
	Album       string
	DurationMs  int64
	Synced      bool
	LRC         string
	Plain       string
	Translation string
	Score       float64
}

// SearchLyrics returns up to n lyrics candidates for a free-text query (or
// title + artist), fetching each hit's lyrics concurrently.
func (s *Source) SearchLyrics(ctx context.Context, q, title, artist string, n int) ([]LyricsCandidate, error) {
	if strings.TrimSpace(q) == "" {
		q = strings.TrimSpace(title + " " + artist)
	}
	res, err := s.cat.Search(ctx, q, 10)
	if err != nil {
		return nil, wrap(err)
	}
	tracks := res.Tracks
	if len(tracks) > n {
		tracks = tracks[:n]
	}
	out := make([]*LyricsCandidate, len(tracks))
	var wg sync.WaitGroup
	for i, t := range tracks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := s.cat.Lyrics(ctx, nativeID(t.ID))
			if err != nil || strings.TrimSpace(l.LRC) == "" {
				return
			}
			c := &LyricsCandidate{Ref: t.ID, Provider: string(s.id), Title: t.Title, Artist: t.ArtistName, DurationMs: t.DurationMs, Translation: strings.TrimSpace(l.Translation)}
			if t.Album != nil {
				c.Album = t.Album.Name
			}
			if len(ParseLRC(l.LRC)) > 0 {
				c.Synced, c.LRC = true, l.LRC
			} else {
				c.Plain = l.LRC
			}
			if title != "" {
				c.Score = Score(title, artist, 0, t)
			}
			out[i] = c
		}()
	}
	wg.Wait()
	var list []LyricsCandidate
	for _, c := range out {
		if c != nil {
			list = append(list, *c)
		}
	}
	return list, nil
}

// httpsURL upgrades platform CDN image links (the web UI's CSP only allows https images).
func httpsURL(u string) string {
	if strings.HasPrefix(u, "http://") {
		return "https://" + u[len("http://"):]
	}
	return u
}

// imageHosts are the platforms' image CDNs; covers/pictures are fetched only
// from these (the URL may come from an admin request).
var imageHosts = []string{"music.126.net", "gtimg.cn", "qpic.cn", "qlogo.cn", "kugou.com", "kgimg.com"}

func allowedImageHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, d := range imageHosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
