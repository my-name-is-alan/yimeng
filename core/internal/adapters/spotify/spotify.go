// Package spotify is a metadata-only provider backed by the Spotify Web API
// with the client-credentials grant (no user OAuth, no playback, no
// playlists/library import). It supplies match candidates for the local
// metadata editor: track/album/artist search, ISRC, covers, genres, release
// dates, popularity and artist images. Applying a candidate is always a local
// user edit; Spotify never writes to the database.
//
// Credentials come from a callback that reads them from the Vault (plugin
// "spotify" secret settings); the access token lives only in memory.
package spotify

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
	"github.com/my-name-is-alan/yimeng/core/internal/search"
)

const (
	DefaultAPIBase  = "https://api.spotify.com/v1"
	DefaultAuthBase = "https://accounts.spotify.com"
	searchLimit     = 10
)

// Creds returns the client id/secret; ok=false when not configured/disabled.
type Creds func() (id, secret string, ok bool)

type Config struct {
	APIBase, AuthBase string
	HTTP              *http.Client
	UserAgent         string
	Creds             Creds
	Now               func() time.Time
	// Aliases optionally returns other names of an artist (e.g. MusicBrainz
	// aliases: 周杰伦 → Jay Chou, Zhou Jielun). Used for Chinese ↔ romanised matching.
	Aliases func(ctx context.Context, name string) []string
}

type Provider struct {
	cfg Config

	mu      sync.Mutex
	token   string
	expires time.Time
	tokFor  string // client id the cached token belongs to
}

var _ metadata.MetadataProvider = (*Provider)(nil)

func New(c Config) *Provider {
	if c.APIBase == "" {
		c.APIBase = DefaultAPIBase
	}
	if c.AuthBase == "" {
		c.AuthBase = DefaultAuthBase
	}
	c.APIBase = strings.TrimRight(c.APIBase, "/")
	// Accept "https://accounts.spotify.com", ".../api" or ".../api/token".
	c.AuthBase = strings.TrimSuffix(strings.TrimRight(c.AuthBase, "/"), "/api/token")
	c.AuthBase = strings.TrimSuffix(c.AuthBase, "/api")
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &Provider{cfg: c}
}

func (p *Provider) ID() metadata.ProviderID { return metadata.Spotify }

func (p *Provider) Capabilities() metadata.Capability {
	return metadata.CapTrackTags | metadata.CapAlbumInfo | metadata.CapArtistInfo | metadata.CapCoverArt
}

// Configured reports whether credentials are present (no network).
func (p *Provider) Configured() bool {
	if p.cfg.Creds == nil {
		return false
	}
	_, _, ok := p.cfg.Creds()
	return ok
}

func (p *Provider) Health(ctx context.Context) metadata.HealthState {
	if !p.Configured() {
		return metadata.HealthDown
	}
	if _, err := p.accessToken(ctx, false); err != nil {
		return metadata.HealthDown
	}
	return metadata.HealthOK
}

// accessToken returns a cached client-credentials token, fetching a new one
// when missing/expired (or force). The token is never logged or returned.
func (p *Provider) accessToken(ctx context.Context, force bool) (string, error) {
	if p.cfg.Creds == nil {
		return "", metadata.ErrUnavailable
	}
	id, secret, ok := p.cfg.Creds()
	if !ok || id == "" || secret == "" {
		return "", metadata.ErrUnavailable
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !force && p.token != "" && p.tokFor == id && p.cfg.Now().Before(p.expires) {
		return p.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.AuthBase+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	p.ua(req)
	res, err := p.cfg.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: token request failed", metadata.ErrUnavailable)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return "", metadata.ErrRateLimited
	case res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnauthorized:
		return "", fmt.Errorf("%w: Spotify rejected the client credentials", metadata.ErrAuthExpired)
	case res.StatusCode != http.StatusOK:
		return "", fmt.Errorf("%w: token endpoint HTTP %d", metadata.ErrUnavailable, res.StatusCode)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&t); err != nil || t.AccessToken == "" {
		return "", fmt.Errorf("%w: bad token response", metadata.ErrUnavailable)
	}
	if t.ExpiresIn <= 0 {
		t.ExpiresIn = 3600
	}
	p.token, p.tokFor = t.AccessToken, id
	p.expires = p.cfg.Now().Add(time.Duration(t.ExpiresIn)*time.Second - time.Minute)
	return p.token, nil
}

func (p *Provider) ua(r *http.Request) {
	if p.cfg.UserAgent != "" {
		r.Header.Set("User-Agent", p.cfg.UserAgent)
	}
}

// get calls GET {api}{path}?q and decodes JSON, refreshing the token once on 401.
func (p *Provider) get(ctx context.Context, path string, q url.Values, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := p.accessToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		u := p.cfg.APIBase + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		p.ua(req)
		res, err := p.cfg.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %s", metadata.ErrUnavailable, redact(err))
		}
		func() { defer res.Body.Close(); err = p.decode(res, out) }()
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		return err
	}
	return metadata.ErrAuthExpired
}

func (p *Provider) decode(res *http.Response, out any) error {
	switch res.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
	case http.StatusNotFound:
		return metadata.ErrNotFound
	case http.StatusTooManyRequests:
		return metadata.ErrRateLimited
	case http.StatusUnauthorized:
		return metadata.ErrAuthExpired
	case http.StatusForbidden:
		return fmt.Errorf("%w: Spotify HTTP 403 (endpoint not available for this app)", metadata.ErrNotSupported)
	default:
		return fmt.Errorf("%w: Spotify HTTP %d", metadata.ErrUnavailable, res.StatusCode)
	}
}

// redact strips the URL (which never carries secrets, but keep logs short).
func redact(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}

// ---- wire types ----

type image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type simpleArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type album struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	AlbumType            string            `json:"album_type"`
	Artists              []simpleArtist    `json:"artists"`
	ReleaseDate          string            `json:"release_date"`
	ReleaseDatePrecision string            `json:"release_date_precision"`
	TotalTracks          int               `json:"total_tracks"`
	Images               []image           `json:"images"`
	Label                string            `json:"label"`
	Genres               []string          `json:"genres"`
	Popularity           int               `json:"popularity"`
	ExternalIDs          map[string]string `json:"external_ids"`
}

type track struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Artists     []simpleArtist    `json:"artists"`
	Album       album             `json:"album"`
	DurationMs  int64             `json:"duration_ms"`
	TrackNumber int               `json:"track_number"`
	DiscNumber  int               `json:"disc_number"`
	Popularity  int               `json:"popularity"`
	ExternalIDs map[string]string `json:"external_ids"`
}

type artist struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Genres     []string `json:"genres"`
	Images     []image  `json:"images"`
	Popularity int      `json:"popularity"`
	Followers  struct {
		Total int `json:"total"`
	} `json:"followers"`
}

type searchResp struct {
	Tracks  struct{ Items []track }  `json:"tracks"`
	Albums  struct{ Items []album }  `json:"albums"`
	Artists struct{ Items []artist } `json:"artists"`
}

func joinNames(as []simpleArtist) string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Name)
	}
	return strings.Join(out, ", ")
}

func year(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

func bestImage(im []image) string {
	best, area := "", -1
	for _, i := range im {
		if a := i.Width * i.Height; a > area {
			best, area = i.URL, a
		}
	}
	return best
}

func norm(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("（", "(", "）", ")", "’", "'", "‘", "'")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

func sim(a, b string) float64 {
	a, b = norm(a), norm(b)
	switch {
	case a == "" || b == "":
		return 0
	case a == b:
		return 1
	}
	// 叶惠美 vs 葉惠美, 晴天 vs 晴天 (Live): compare toneless pinyin keys
	pa, pb := search.Pinyin(a), search.Pinyin(b)
	if (search.HasHan(a) || search.HasHan(b)) && pa != "" && pa == pb {
		return 0.95
	}
	if strings.Contains(a, b) || strings.Contains(b, a) || (pa != "" && pb != "" && (strings.Contains(pa, pb) || strings.Contains(pb, pa))) {
		return 0.6
	}
	return 0
}

// artistNames maps candidate names of the queried artist to a confidence:
// the name itself (1), external aliases (1) and, for Chinese names, the top
// Spotify artist search hit (0.85, e.g. 周杰伦 → Jay Chou).
func (p *Provider) artistNames(ctx context.Context, name string) map[string]float64 {
	out := map[string]float64{}
	name = strings.TrimSpace(name)
	if name == "" {
		return out
	}
	out[name] = 1
	if !search.HasHan(name) {
		return out
	}
	if p.cfg.Aliases != nil {
		for _, a := range p.cfg.Aliases(ctx, name) {
			if a = strings.TrimSpace(a); a != "" {
				out[a] = 1
			}
		}
	}
	var r searchResp
	if err := p.get(ctx, "/search", url.Values{"q": {name}, "type": {"artist"}, "limit": {"3"}}, &r); err == nil && len(r.Artists.Items) > 0 {
		top := r.Artists.Items[0]
		if _, ok := out[top.Name]; !ok && !search.HasHan(top.Name) && top.Followers.Total >= 10000 {
			out[top.Name] = 0.85
		}
	}
	return out
}

// artistSim is the best match between any known name and any credited artist.
func artistSim(names map[string]float64, credited string) float64 {
	best := 0.0
	for _, c := range strings.Split(credited, ", ") {
		for n, w := range names {
			if v := w * sim(n, c); v > best {
				best = v
			}
		}
	}
	return best
}

// latinAlias picks the strongest non-Chinese alias (for the search query).
func latinAlias(names map[string]float64) string {
	best, bw := "", 0.0
	for n, w := range names {
		if !search.HasHan(n) && w > bw {
			best, bw = n, w
		}
	}
	return best
}

// quote escapes a value for Spotify's field filter syntax.
func quote(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), `"`, "") }

func (p *Provider) search(ctx context.Context, q, typ string) (searchResp, error) {
	var r searchResp
	err := p.get(ctx, "/search", url.Values{"q": {q}, "type": {typ}, "limit": {strconv.Itoa(searchLimit)}}, &r)
	return r, err
}

// ---- MetadataProvider ----

func (p *Provider) LookupTrack(ctx context.Context, q metadata.TrackQuery) ([]metadata.TrackMatch, error) {
	var items []track
	if id := q.ExternalIDs[metadata.Spotify]; id != "" {
		var t track
		if err := p.get(ctx, "/tracks/"+url.PathEscape(id), nil, &t); err != nil {
			return nil, err
		}
		items = []track{t}
	}
	if len(items) == 0 && q.ISRC != "" {
		r, err := p.search(ctx, "isrc:"+quote(q.ISRC), "track")
		if err != nil {
			return nil, err
		}
		items = r.Tracks.Items
	}
	names := map[string]float64{}
	if len(items) == 0 {
		if strings.TrimSpace(q.Title) == "" {
			return nil, nil
		}
		names = p.artistNames(ctx, q.Artist)
		queries := []string{}
		if alias := latinAlias(names); alias != "" {
			queries = append(queries, `track:"`+quote(q.Title)+`" artist:"`+quote(alias)+`"`)
		}
		s := `track:"` + quote(q.Title) + `"`
		if a := quote(q.Artist); a != "" {
			s += ` artist:"` + a + `"`
		}
		queries = append(queries, s)
		seen := map[string]bool{}
		for _, qs := range queries {
			r, err := p.search(ctx, qs, "track")
			if err != nil {
				return nil, err
			}
			for _, t := range r.Tracks.Items {
				if !seen[t.ID] {
					seen[t.ID] = true
					items = append(items, t)
				}
			}
		}
		if len(items) == 0 { // field filters are strict; fall back to free text
			r, err := p.search(ctx, strings.TrimSpace(q.Title+" "+q.Artist), "track")
			if err != nil {
				return nil, err
			}
			items = r.Tracks.Items
		}
	} else {
		names = map[string]float64{q.Artist: 1}
	}
	out := make([]metadata.TrackMatch, 0, len(items))
	for _, t := range items {
		m := metadata.TrackMatch{Provider: metadata.Spotify, ExternalID: t.ID, Title: t.Name, Artist: joinNames(t.Artists),
			Album: t.Album.Name, AlbumArtist: joinNames(t.Album.Artists), TrackNo: t.TrackNumber, DiscNo: t.DiscNumber,
			Year: year(t.Album.ReleaseDate), ReleaseDate: t.Album.ReleaseDate, DurationMs: t.DurationMs,
			ISRC: strings.ToUpper(t.ExternalIDs["isrc"]), CoverURL: bestImage(t.Album.Images), Popularity: t.Popularity}
		m.Score = trackScore(q, m, names)
		out = append(out, m)
	}
	// Genre lives on artists in Spotify's model: fill it for the best match only.
	if best := bestIdx(out); best >= 0 && len(items[best].Artists) > 0 {
		var a artist
		if err := p.get(ctx, "/artists/"+url.PathEscape(items[best].Artists[0].ID), nil, &a); err == nil && len(a.Genres) > 0 {
			out[best].Genre = a.Genres[0]
		}
	}
	return out, nil
}

func bestIdx(ms []metadata.TrackMatch) int {
	b := -1
	for i, m := range ms {
		if b < 0 || m.Score > ms[b].Score {
			b = i
		}
	}
	return b
}

func trackScore(q metadata.TrackQuery, m metadata.TrackMatch, names map[string]float64) float64 {
	if q.ISRC != "" && strings.EqualFold(q.ISRC, m.ISRC) {
		return 1
	}
	if len(names) == 0 && q.Artist != "" {
		names = map[string]float64{q.Artist: 1}
	}
	s := 0.5*sim(q.Title, m.Title) + 0.3*artistSim(names, m.Artist)
	if q.Album != "" {
		s += 0.1 * sim(q.Album, m.Album)
	} else {
		s += 0.05
	}
	if q.DurationMs > 0 && m.DurationMs > 0 {
		d := q.DurationMs - m.DurationMs
		if d < 0 {
			d = -d
		}
		switch {
		case d <= 3000:
			s += 0.1
		case d > 15000:
			s -= 0.1
		}
	} else {
		s += 0.05
	}
	if s < 0 {
		s = 0
	}
	if s > 0.99 {
		s = 0.99
	}
	return s
}

func (p *Provider) albumInfo(a album) metadata.AlbumInfo {
	return metadata.AlbumInfo{Provider: metadata.Spotify, ExternalID: a.ID, Title: a.Name, AlbumArtist: joinNames(a.Artists),
		Year: year(a.ReleaseDate), ReleaseDate: a.ReleaseDate, Label: a.Label, Genres: a.Genres,
		UPC: a.ExternalIDs["upc"], CoverURL: bestImage(a.Images), Popularity: a.Popularity, TrackCount: a.TotalTracks}
}

func (p *Provider) LookupAlbum(ctx context.Context, q metadata.AlbumQuery) ([]metadata.AlbumInfo, error) {
	var items []album
	names := map[string]float64{q.AlbumArtist: 1}
	if id := q.ExternalIDs[metadata.Spotify]; id != "" {
		items = []album{{ID: id}}
	} else {
		if strings.TrimSpace(q.Title) == "" {
			return nil, nil
		}
		names = p.artistNames(ctx, q.AlbumArtist)
		artist := quote(q.AlbumArtist)
		if alias := latinAlias(names); alias != "" {
			artist = quote(alias)
		}
		s := `album:"` + quote(q.Title) + `"`
		if artist != "" {
			s += ` artist:"` + artist + `"`
		}
		r, err := p.search(ctx, s, "album")
		if err != nil {
			return nil, err
		}
		items = r.Albums.Items
		if len(items) == 0 {
			if r, err = p.search(ctx, strings.TrimSpace(q.Title+" "+q.AlbumArtist), "album"); err != nil {
				return nil, err
			}
			items = r.Albums.Items
		}
	}
	out := make([]metadata.AlbumInfo, 0, len(items))
	for i, a := range items {
		// Full album objects add label/UPC/popularity; fetch for the top few only.
		if i < 3 {
			var full album
			if err := p.get(ctx, "/albums/"+url.PathEscape(a.ID), url.Values{"market": {"US"}}, &full); err == nil && full.ID != "" {
				a = full
			} else if errors.Is(err, metadata.ErrRateLimited) {
				return nil, err
			}
		}
		if a.Name == "" {
			continue
		}
		info := p.albumInfo(a)
		info.Score = albumScoreQ(q, info, names)
		out = append(out, info)
	}
	return out, nil
}

func albumScoreQ(q metadata.AlbumQuery, a metadata.AlbumInfo, names map[string]float64) float64 {
	s := 0.6*sim(q.Title, a.Title) + 0.3*artistSim(names, a.AlbumArtist)
	if q.Year != 0 && q.Year == a.Year {
		s += 0.1
	}
	return s
}

func (p *Provider) LookupArtist(ctx context.Context, q metadata.ArtistQuery) ([]metadata.ArtistInfo, error) {
	var items []artist
	if id := q.ExternalIDs[metadata.Spotify]; id != "" {
		var a artist
		if err := p.get(ctx, "/artists/"+url.PathEscape(id), nil, &a); err != nil {
			return nil, err
		}
		items = []artist{a}
	} else {
		if strings.TrimSpace(q.Name) == "" {
			return nil, nil
		}
		r, err := p.search(ctx, quote(q.Name), "artist")
		if err != nil {
			return nil, err
		}
		items = r.Artists.Items
	}
	out := make([]metadata.ArtistInfo, 0, len(items))
	for _, a := range items {
		in := metadata.ArtistInfo{Provider: metadata.Spotify, ExternalID: a.ID, Name: a.Name, Genres: a.Genres,
			Popularity: a.Popularity, Followers: a.Followers.Total}
		if u := bestImage(a.Images); u != "" {
			in.ImageURLs = []string{u}
		}
		out = append(out, in)
	}
	return out, nil
}

func (p *Provider) CoverArt(ctx context.Context, q metadata.AlbumQuery) ([]metadata.CoverCandidate, error) {
	as, err := p.LookupAlbum(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []metadata.CoverCandidate
	for _, a := range as {
		if a.CoverURL != "" {
			out = append(out, metadata.CoverCandidate{Provider: metadata.Spotify, URL: a.CoverURL, MIME: "image/jpeg", Width: 640, Height: 640})
		}
	}
	return out, nil
}

// AllowedImage reports whether u points at Spotify's image CDN.
func AllowedImage(u string) bool {
	x, err := url.Parse(u)
	if err != nil || x.Scheme != "https" {
		return false
	}
	h := strings.ToLower(x.Hostname())
	return h == "i.scdn.co" || strings.HasSuffix(h, ".scdn.co") || strings.HasSuffix(h, ".spotifycdn.com")
}

// FetchCover downloads an image from Spotify's CDN only (no SSRF to other hosts).
func (p *Provider) FetchCover(ctx context.Context, c metadata.CoverCandidate) (io.ReadCloser, error) {
	if !AllowedImage(c.URL) {
		return nil, fmt.Errorf("spotify: cover url not on Spotify's image CDN")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	p.ua(req)
	res, err := p.cfg.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", metadata.ErrUnavailable, redact(err))
	}
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		res.Body.Close()
		return nil, fmt.Errorf("%w: cover HTTP %d", metadata.ErrUnavailable, res.StatusCode)
	}
	return res.Body, nil
}

func (p *Provider) Lyrics(context.Context, metadata.TrackQuery) (*metadata.Lyrics, error) {
	return nil, metadata.ErrNotSupported
}
