// Package musicbrainz is a metadata-only provider backed by the MusicBrainz
// web service (ws/2, JSON) and the Cover Art Archive. It honours the
// MusicBrainz API policy: ≤1 request/second and a descriptive User-Agent with
// contact information. No credentials are needed.
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

type Config struct {
	BaseURL   string        // default https://musicbrainz.org
	CAAURL    string        // default https://coverartarchive.org
	UserAgent string        // required: "yimeng/<ver> ( contact )"
	Interval  time.Duration // min spacing between MB requests; default 1s
	HTTP      *http.Client  // default: 15s timeout
}

type Provider struct {
	cfg  Config
	mu   sync.Mutex
	next time.Time
}

var _ metadata.MetadataProvider = (*Provider)(nil)

func New(c Config) (*Provider, error) {
	if !strings.Contains(c.UserAgent, "(") {
		return nil, errors.New("musicbrainz: User-Agent must include contact info, e.g. \"yimeng/0.1 ( you@example.com )\"")
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://musicbrainz.org"
	}
	if c.CAAURL == "" {
		c.CAAURL = "https://coverartarchive.org"
	}
	if c.Interval <= 0 {
		c.Interval = time.Second
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	c.CAAURL = strings.TrimRight(c.CAAURL, "/")
	return &Provider{cfg: c}, nil
}

func (p *Provider) ID() metadata.ProviderID { return metadata.MusicBrainz }
func (p *Provider) Capabilities() metadata.Capability {
	return metadata.CapTrackTags | metadata.CapAlbumInfo | metadata.CapArtistInfo | metadata.CapCoverArt | metadata.CapCredits
}

func (p *Provider) Health(ctx context.Context) metadata.HealthState {
	if _, err := p.get(ctx, "/ws/2/artist/89ad4ac3-39f7-470e-963a-56509c546377", nil, nil); err != nil {
		return metadata.HealthDegraded
	}
	return metadata.HealthOK
}

// wait enforces the request interval across all goroutines.
func (p *Provider) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.cfg.Interval)
	p.mu.Unlock()
	if d := time.Until(at); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

func (p *Provider) do(ctx context.Context, u string, limited bool) (*http.Response, error) {
	if limited {
		if err := p.wait(ctx); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	res, err := p.cfg.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", metadata.ErrUnavailable, err)
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		res.Body.Close()
		return nil, metadata.ErrNotFound
	case res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusTooManyRequests:
		res.Body.Close()
		return nil, metadata.ErrRateLimited
	case res.StatusCode >= 400:
		res.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d", metadata.ErrUnavailable, res.StatusCode)
	}
	return res, nil
}

func (p *Provider) get(ctx context.Context, path string, q url.Values, out any) ([]byte, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("fmt", "json")
	res, err := p.do(ctx, p.cfg.BaseURL+path+"?"+q.Encode(), true)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return nil, fmt.Errorf("musicbrainz: decode %s: %w", path, err)
		}
	}
	return b, nil
}

// ── wire types ──────────────────────────────────────────────────────────────

type mbArtist struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SortName string `json:"sort-name"`
	Country  string `json:"country"`
	Score    int    `json:"score"`
	LifeSpan struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
	} `json:"life-span"`
	Aliases []struct {
		Name string `json:"name"`
	} `json:"aliases"`
	Disambiguation string `json:"disambiguation"`
}

type mbCredit []struct {
	Name       string   `json:"name"`
	JoinPhrase string   `json:"joinphrase"`
	Artist     mbArtist `json:"artist"`
}

func (c mbCredit) String() string {
	var b strings.Builder
	for _, a := range c {
		b.WriteString(a.Name + a.JoinPhrase)
	}
	return b.String()
}

type mbRelease struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Date         string   `json:"date"`
	Score        int      `json:"score"`
	ArtistCredit mbCredit `json:"artist-credit"`
	LabelInfo    []struct {
		Label struct {
			Name string `json:"name"`
		} `json:"label"`
	} `json:"label-info"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
}

type mbRelation struct {
	Type       string   `json:"type"`
	TargetType string   `json:"target-type"`
	Attributes []string `json:"attributes"`
	Artist     *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
	Work *struct {
		ID        string       `json:"id"`
		Title     string       `json:"title"`
		Relations []mbRelation `json:"relations"`
	} `json:"work"`
}

type mbRecording struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Length       int64        `json:"length"`
	Score        int          `json:"score"`
	ArtistCredit mbCredit     `json:"artist-credit"`
	Releases     []mbRelease  `json:"releases"`
	ISRCs        []string     `json:"isrcs"`
	Relations    []mbRelation `json:"relations"`
}

func year(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

func norm(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func (p *Provider) toMatch(r mbRecording, score float64, album string) metadata.TrackMatch {
	m := metadata.TrackMatch{Provider: metadata.MusicBrainz, ExternalID: r.ID, Score: score, Title: r.Title,
		Artist: r.ArtistCredit.String(), DurationMs: r.Length, MBRecordingID: r.ID}
	if len(r.ISRCs) > 0 {
		m.ISRC = r.ISRCs[0]
	}
	// prefer the release whose title matches the local album
	var rel *mbRelease
	for i := range r.Releases {
		if album != "" && norm(r.Releases[i].Title) == norm(album) {
			rel = &r.Releases[i]
			break
		}
	}
	if rel == nil && len(r.Releases) > 0 {
		rel = &r.Releases[0]
	}
	if rel != nil {
		m.Album, m.MBReleaseID, m.Year = rel.Title, rel.ID, year(rel.Date)
		m.AlbumArtist = rel.ArtistCredit.String()
	}
	m.Credits = credits(r.Relations)
	return m
}

func relRole(r mbRelation) string {
	role := r.Type
	if len(r.Attributes) > 0 {
		role += " (" + strings.Join(r.Attributes, ", ") + ")"
	}
	return role
}

// credits flattens recording-level artist relations (producer, engineer,
// mix, vocal, instrument, ...) and work-level ones (composer, lyricist, ...).
func credits(rels []mbRelation) []metadata.Credit {
	var out []metadata.Credit
	seen := map[string]bool{}
	add := func(r mbRelation) {
		if r.Artist == nil {
			return
		}
		c := metadata.Credit{Role: relRole(r), Name: r.Artist.Name, MBID: r.Artist.ID}
		if k := c.Role + "\x00" + c.MBID; !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	for _, r := range rels {
		if r.TargetType == "artist" || r.Artist != nil {
			add(r)
		}
		if r.Work != nil {
			for _, wr := range r.Work.Relations {
				add(wr)
			}
		}
	}
	return out
}

// lucene quotes a value for a MusicBrainz search field.
func lucene(field, v string) string {
	v = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.TrimSpace(v))
	return field + `:"` + v + `"`
}

// ── SPI ─────────────────────────────────────────────────────────────────────

const recInc = "artists+releases+isrcs+artist-rels+work-rels+work-level-rels"

func (p *Provider) LookupTrack(ctx context.Context, q metadata.TrackQuery) ([]metadata.TrackMatch, error) {
	if q.MBRecordingID != "" {
		var r mbRecording
		if _, err := p.get(ctx, "/ws/2/recording/"+url.PathEscape(q.MBRecordingID), url.Values{"inc": {recInc}}, &r); err != nil {
			return nil, err
		}
		return []metadata.TrackMatch{p.toMatch(r, 1, q.Album)}, nil
	}
	if q.ISRC != "" {
		var res struct {
			Recordings []mbRecording `json:"recordings"`
		}
		_, err := p.get(ctx, "/ws/2/isrc/"+url.PathEscape(q.ISRC), url.Values{"inc": {"artists+releases"}}, &res)
		if err == nil && len(res.Recordings) > 0 {
			out := make([]metadata.TrackMatch, 0, len(res.Recordings))
			for _, r := range res.Recordings {
				out = append(out, p.toMatch(r, 0.95, q.Album))
			}
			return out, nil
		}
		if err != nil && !errors.Is(err, metadata.ErrNotFound) {
			return nil, err
		}
	}
	if strings.TrimSpace(q.Title) == "" {
		return nil, metadata.ErrNotFound
	}
	parts := []string{lucene("recording", q.Title)}
	if q.Artist != "" {
		parts = append(parts, lucene("artist", q.Artist))
	}
	if q.Album != "" {
		parts = append(parts, lucene("release", q.Album))
	}
	var res struct {
		Recordings []mbRecording `json:"recordings"`
	}
	if _, err := p.get(ctx, "/ws/2/recording", url.Values{"query": {strings.Join(parts, " AND ")}, "limit": {"5"}}, &res); err != nil {
		return nil, err
	}
	if len(res.Recordings) == 0 {
		return nil, metadata.ErrNotFound
	}
	out := make([]metadata.TrackMatch, 0, len(res.Recordings))
	for _, r := range res.Recordings {
		out = append(out, p.toMatch(r, float64(r.Score)/100, q.Album))
	}
	return out, nil
}

func (p *Provider) toAlbum(r mbRelease, score float64) metadata.AlbumInfo {
	a := metadata.AlbumInfo{Provider: metadata.MusicBrainz, ExternalID: r.ID, Title: r.Title,
		AlbumArtist: r.ArtistCredit.String(), Year: year(r.Date), ReleaseDate: r.Date, MBReleaseID: r.ID}
	if len(r.LabelInfo) > 0 {
		a.Label = r.LabelInfo[0].Label.Name
	}
	for _, g := range r.Genres {
		a.Genres = append(a.Genres, g.Name)
	}
	_ = score
	return a
}

func (p *Provider) LookupAlbum(ctx context.Context, q metadata.AlbumQuery) ([]metadata.AlbumInfo, error) {
	if q.MBReleaseID != "" {
		var r mbRelease
		if _, err := p.get(ctx, "/ws/2/release/"+url.PathEscape(q.MBReleaseID), url.Values{"inc": {"artists+labels+genres"}}, &r); err != nil {
			return nil, err
		}
		return []metadata.AlbumInfo{p.toAlbum(r, 1)}, nil
	}
	if q.Title == "" {
		return nil, metadata.ErrNotFound
	}
	parts := []string{lucene("release", q.Title)}
	if q.AlbumArtist != "" {
		parts = append(parts, lucene("artist", q.AlbumArtist))
	}
	var res struct {
		Releases []mbRelease `json:"releases"`
	}
	if _, err := p.get(ctx, "/ws/2/release", url.Values{"query": {strings.Join(parts, " AND ")}, "limit": {"5"}}, &res); err != nil {
		return nil, err
	}
	if len(res.Releases) == 0 {
		return nil, metadata.ErrNotFound
	}
	var out []metadata.AlbumInfo
	for _, r := range res.Releases {
		out = append(out, p.toAlbum(r, float64(r.Score)/100))
	}
	return out, nil
}

func toArtist(a mbArtist) metadata.ArtistInfo {
	out := metadata.ArtistInfo{Provider: metadata.MusicBrainz, ExternalID: a.ID, Name: a.Name, SortName: a.SortName,
		Country: a.Country, Begin: a.LifeSpan.Begin, End: a.LifeSpan.End, MBArtistID: a.ID, Bio: a.Disambiguation}
	for _, al := range a.Aliases {
		out.Aliases = append(out.Aliases, al.Name)
	}
	return out
}

func (p *Provider) LookupArtist(ctx context.Context, q metadata.ArtistQuery) ([]metadata.ArtistInfo, error) {
	if q.MBArtistID != "" {
		var a mbArtist
		if _, err := p.get(ctx, "/ws/2/artist/"+url.PathEscape(q.MBArtistID), url.Values{"inc": {"aliases"}}, &a); err != nil {
			return nil, err
		}
		return []metadata.ArtistInfo{toArtist(a)}, nil
	}
	if q.Name == "" {
		return nil, metadata.ErrNotFound
	}
	var res struct {
		Artists []mbArtist `json:"artists"`
	}
	if _, err := p.get(ctx, "/ws/2/artist", url.Values{"query": {lucene("artist", q.Name)}, "limit": {"5"}}, &res); err != nil {
		return nil, err
	}
	if len(res.Artists) == 0 {
		return nil, metadata.ErrNotFound
	}
	var out []metadata.ArtistInfo
	for _, a := range res.Artists {
		out = append(out, toArtist(a))
	}
	return out, nil
}

// CoverArt lists Cover Art Archive images for a release (front first).
func (p *Provider) CoverArt(ctx context.Context, q metadata.AlbumQuery) ([]metadata.CoverCandidate, error) {
	if q.MBReleaseID == "" {
		return nil, metadata.ErrNotFound
	}
	res, err := p.do(ctx, p.cfg.CAAURL+"/release/"+url.PathEscape(q.MBReleaseID), false)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var body struct {
		Images []struct {
			Image      string            `json:"image"`
			Front      bool              `json:"front"`
			Thumbnails map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	var front, rest []metadata.CoverCandidate
	for _, im := range body.Images {
		u, w := im.Image, 0
		for _, k := range []string{"1200", "500", "large"} { // a sensible size, not the multi-MB original
			if t := im.Thumbnails[k]; t != "" {
				u = t
				if n, err := strconv.Atoi(k); err == nil {
					w = n
				}
				break
			}
		}
		c := metadata.CoverCandidate{Provider: metadata.MusicBrainz, URL: u, MIME: "image/jpeg", Width: w, Height: w}
		if im.Front {
			front = append(front, c)
		} else {
			rest = append(rest, c)
		}
	}
	out := append(front, rest...)
	if len(out) == 0 {
		return nil, metadata.ErrNotFound
	}
	return out, nil
}

func (p *Provider) FetchCover(ctx context.Context, c metadata.CoverCandidate) (io.ReadCloser, error) {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("musicbrainz: bad cover url")
	}
	res, err := p.do(ctx, c.URL, false)
	if err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		res.Body.Close()
		return nil, fmt.Errorf("musicbrainz: cover is %s, not an image", ct)
	}
	return res.Body, nil
}

func (p *Provider) Lyrics(context.Context, metadata.TrackQuery) (*metadata.Lyrics, error) {
	return nil, metadata.ErrNotSupported
}
