package online

import (
	"context"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/search"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// LibraryPort is what the Service needs from the canonical library.
type LibraryPort interface {
	UpsertExternal(ctx context.Context, items []library.ExternalTrack) error
	FindMatch(ctx context.Context, q library.MatchQuery) (string, float64, string, error)
	LinkEquivalent(ctx context.Context, a, b string, conf float64, method string) error
	Equivalents(ctx context.Context, id string) ([]library.Equivalent, error)
}

// Service is the platform-neutral facade used by the gateway.
type Service struct {
	Reg   *Registry
	Lib   LibraryPort
	Cache *Cache
	Log   *slog.Logger

	mu     sync.Mutex
	leases map[string]Resolution
	now    func() time.Time
}

func NewService(reg *Registry, lib LibraryPort, cache *Cache, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{Reg: reg, Lib: lib, Cache: cache, Log: log, leases: map[string]Resolution{}, now: time.Now}
	if cache != nil && reg != nil {
		// In a risk-control cooldown, serve stale copies without retrying.
		cache.Cooling = func(origin string) bool {
			in, err := reg.get(origin)
			return err == nil && in.client != nil && time.Now().Before(in.client.CooldownUntil())
		}
	}
	return s
}

func FullID(origin, native string) string { return origin + ":" + native }

func split(id string) (string, string, error) {
	o, n, ok := library.SplitExternalID(id)
	if !ok {
		return "", "", fmt.Errorf("%w: not an external id %q", ErrUnknown, id)
	}
	return o, n, nil
}

func (s *Service) plugin(origin string) (Plugin, Config, error) {
	return s.Reg.Enabled(origin)
}

func (s *Service) after(ctx context.Context, origin string, err error) {
	s.Reg.Persist(ctx, origin)
	if err == nil {
		if in, e := s.Reg.get(origin); e == nil {
			in.client.Reward()
		}
		return
	}
	if errors.Is(err, ErrRiskControl) {
		if in, e := s.Reg.get(origin); e == nil {
			in.client.Penalize()
		}
	}
}

func catalogOf(p Plugin) (Catalog, error) {
	c, ok := p.(Catalog)
	if !ok {
		return nil, ErrNotSupported
	}
	return c, nil
}

// ── catalog ────────────────────────────────────────────────────────────────

// OriginResult is one platform's slice of a federated search.
type OriginResult struct {
	Origin string        `json:"origin"`
	Name   string        `json:"name"`
	Result *SearchResult `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// Search queries every enabled plugin with CapSearch in parallel (6 s budget each).
func (s *Service) Search(ctx context.Context, q string, limit int, only []string) []OriginResult {
	ids := s.Reg.EnabledIDs()
	if len(only) > 0 {
		keep := map[string]bool{}
		for _, o := range only {
			keep[o] = true
		}
		var f []string
		for _, id := range ids {
			if keep[id] {
				f = append(f, id)
			}
		}
		ids = f
	}
	out := make([]OriginResult, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		p, _, err := s.plugin(id)
		out[i] = OriginResult{Origin: id}
		if err != nil {
			out[i].Error = err.Error()
			continue
		}
		out[i].Name = p.Manifest().Name
		c, ok := p.(Catalog)
		if !ok || !p.Manifest().Has(CapSearch) {
			out[i].Error = ErrNotSupported.Error()
			continue
		}
		wg.Add(1)
		go func(i int, id string, c Catalog) {
			defer wg.Done()
			cctx, cancel := withTimeout(ctx, 6*time.Second)
			defer cancel()
			r, err := cached(cctx, s.Cache, Key(id, "search", fmt.Sprintf("%d:%s", limit, strings.ToLower(q))), TTLSearch, func(ctx context.Context) (SearchResult, error) {
				return c.Search(cctx, q, limit)
			})
			s.after(ctx, id, err)
			if err != nil {
				out[i].Error = err.Error()
				return
			}
			s.rememberTracks(ctx, r.Tracks)
			out[i].Result = &r
		}(i, id, c)
	}
	wg.Wait()
	return out
}

// rememberTracks caches track DTOs so later materialization needs no fetch.
func (s *Service) rememberTracks(ctx context.Context, ts []Track) {
	for _, t := range ts {
		o, n, err := split(t.ID)
		if err == nil {
			s.Cache.Put(ctx, Key(o, "track", n), t, TTLTrack)
		}
	}
}

// Tracks returns full track DTOs for full ids (cache first).
func (s *Service) Tracks(ctx context.Context, ids []string) ([]Track, error) {
	byOrigin := map[string][]string{}
	out := map[string]Track{}
	for _, id := range ids {
		o, n, err := split(id)
		if err != nil {
			return nil, err
		}
		var t Track
		if s.Cache.Get(ctx, Key(o, "track", n), &t) {
			out[id] = t
			continue
		}
		byOrigin[o] = append(byOrigin[o], n)
	}
	for o, natives := range byOrigin {
		p, _, err := s.plugin(o)
		if err != nil {
			return nil, err
		}
		c, err := catalogOf(p)
		if err != nil {
			return nil, err
		}
		for i := 0; i < len(natives); i += 500 {
			ts, err := c.Tracks(ctx, natives[i:min(len(natives), i+500)])
			s.after(ctx, o, err)
			if err != nil {
				return nil, err
			}
			s.rememberTracks(ctx, ts)
			for _, t := range ts {
				out[t.ID] = t
			}
		}
	}
	res := make([]Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := out[id]; ok {
			res = append(res, t)
		}
	}
	return res, nil
}

func (s *Service) Track(ctx context.Context, id string) (Track, error) {
	ts, err := s.Tracks(ctx, []string{id})
	if err != nil {
		return Track{}, err
	}
	if len(ts) == 0 {
		return Track{}, fmt.Errorf("%w: track %s", ErrUnavailable, id)
	}
	return ts[0], nil
}

// ToLibrary converts DTOs for materialization.
func ToLibrary(t Track) library.ExternalTrack {
	o, _, _ := library.SplitExternalID(t.ID)
	et := library.ExternalTrack{ID: t.ID, Origin: o, Title: t.Title, CoverURL: t.CoverURL, DurationMs: t.DurationMs,
		TrackNo: t.TrackNo, DiscNo: t.DiscNo, Year: t.Year, ISRC: t.ISRC}
	for _, a := range t.Artists {
		et.Artists = append(et.Artists, library.ExternalRef{ID: a.ID, Name: a.Name})
	}
	if len(et.Artists) == 0 && t.ArtistName != "" {
		et.Artists = []library.ExternalRef{{Name: t.ArtistName}}
	}
	if t.Album != nil {
		et.AlbumID, et.AlbumTitle = t.Album.ID, t.Album.Name
	}
	return et
}

// Materialize stores external tracks in the library (ExternalResolver hook).
func (s *Service) Materialize(ctx context.Context, ids []string) error {
	ts, err := s.Tracks(ctx, ids)
	if err != nil {
		return err
	}
	if len(ts) != len(ids) {
		return fmt.Errorf("%w: %d of %d tracks not found", ErrUnavailable, len(ids)-len(ts), len(ids))
	}
	items := make([]library.ExternalTrack, len(ts))
	for i, t := range ts {
		items[i] = ToLibrary(t)
	}
	if err := s.Lib.UpsertExternal(ctx, items); err != nil {
		return err
	}
	s.annotate(ctx, ts)
	return nil
}

// annotate links tracks to equivalent local recordings (ISRC or
// title+artist+duration) and sets LocalID.
func (s *Service) annotate(ctx context.Context, ts []Track) {
	for i := range ts {
		t := &ts[i]
		t.SortLetter = search.SortLetter(t.Title)
		t.Preview = t.Playable == EntPreview
		id, conf, method, err := s.Lib.FindMatch(ctx, library.MatchQuery{ISRC: t.ISRC, Title: t.Title, Artist: t.ArtistName, DurationMs: t.DurationMs})
		if err != nil || id == "" {
			continue
		}
		t.LocalID = id
		s.Lib.LinkEquivalent(ctx, t.ID, id, conf, method)
	}
}

func (s *Service) Album(ctx context.Context, id string) (Album, error) {
	o, n, err := split(id)
	if err != nil {
		return Album{}, err
	}
	p, _, err := s.plugin(o)
	if err != nil {
		return Album{}, err
	}
	c, err := catalogOf(p)
	if err != nil {
		return Album{}, err
	}
	a, err := cached(ctx, s.Cache, Key(o, "album", n), TTLAlbum, func(ctx context.Context) (Album, error) { return c.Album(ctx, n) })
	s.after(ctx, o, err)
	if err == nil {
		s.rememberTracks(ctx, a.Tracks)
		s.annotate(ctx, a.Tracks)
	}
	return a, err
}

func (s *Service) Artist(ctx context.Context, id string) (Artist, error) {
	o, n, err := split(id)
	if err != nil {
		return Artist{}, err
	}
	p, _, err := s.plugin(o)
	if err != nil {
		return Artist{}, err
	}
	c, err := catalogOf(p)
	if err != nil {
		return Artist{}, err
	}
	a, err := cached(ctx, s.Cache, Key(o, "artist", n), TTLArtist, func(ctx context.Context) (Artist, error) { return c.Artist(ctx, n) })
	s.after(ctx, o, err)
	if err == nil {
		s.rememberTracks(ctx, a.TopTracks)
		s.annotate(ctx, a.TopTracks)
	}
	return a, err
}

// Playlist returns an external playlist with per-track local link and
// playable status.
func (s *Service) Playlist(ctx context.Context, id string) (Playlist, error) {
	o, n, err := split(id)
	if err != nil {
		return Playlist{}, err
	}
	p, _, err := s.plugin(o)
	if err != nil {
		return Playlist{}, err
	}
	c, err := catalogOf(p)
	if err != nil {
		return Playlist{}, err
	}
	pl, err := cached(ctx, s.Cache, Key(o, "playlist", n), TTLPlaylist, func(ctx context.Context) (Playlist, error) { return c.Playlist(ctx, n) })
	s.after(ctx, o, err)
	if err == nil {
		s.rememberTracks(ctx, pl.Tracks)
		s.annotate(ctx, pl.Tracks)
	}
	return pl, err
}

func (s *Service) Lyrics(ctx context.Context, id string) (Lyrics, error) {
	o, n, err := split(id)
	if err != nil {
		return Lyrics{}, err
	}
	p, _, err := s.plugin(o)
	if err != nil {
		return Lyrics{}, err
	}
	c, err := catalogOf(p)
	if err != nil {
		return Lyrics{}, err
	}
	l, err := cached(ctx, s.Cache, Key(o, "lyrics", n), TTLLyrics, func(ctx context.Context) (Lyrics, error) { return c.Lyrics(ctx, n) })
	s.after(ctx, o, err)
	return l, err
}

// ── discovery ──────────────────────────────────────────────────────────────

func (s *Service) discovery(origin string) (Discovery, error) {
	p, _, err := s.plugin(origin)
	if err != nil {
		return nil, err
	}
	d, ok := p.(Discovery)
	if !ok {
		return nil, ErrNotSupported
	}
	return d, nil
}

func (s *Service) Charts(ctx context.Context, origin string) ([]Playlist, error) {
	d, err := s.discovery(origin)
	if err != nil {
		return nil, err
	}
	v, err := cached(ctx, s.Cache, Key(origin, "charts", "all"), TTLCharts, func(ctx context.Context) ([]Playlist, error) { return d.Charts(ctx) })
	s.after(ctx, origin, err)
	return v, err
}

// userScoped caches per logged-in account (key includes the user id).
func (s *Service) userKey(origin, kind string) string {
	uid := "anon"
	if in, err := s.Reg.get(origin); err == nil {
		s.Reg.mu.Lock()
		if in.account != nil {
			uid = in.account.UserID
		}
		s.Reg.mu.Unlock()
	}
	return Key(origin, kind, uid)
}

func (s *Service) RecommendTracks(ctx context.Context, origin string) ([]Track, error) {
	d, err := s.discovery(origin)
	if err != nil {
		return nil, err
	}
	if !s.Reg.LoggedIn(origin) {
		return nil, ErrNeedsLogin
	}
	v, err := cached(ctx, s.Cache, s.userKey(origin, "rec-tracks")+":"+s.now().Format("2006-01-02"), TTLRecs, func(ctx context.Context) ([]Track, error) { return d.RecommendTracks(ctx) })
	s.after(ctx, origin, err)
	if err == nil {
		s.rememberTracks(ctx, v)
	}
	return v, err
}

func (s *Service) RecommendPlaylists(ctx context.Context, origin string) ([]Playlist, error) {
	d, err := s.discovery(origin)
	if err != nil {
		return nil, err
	}
	v, err := cached(ctx, s.Cache, s.userKey(origin, "rec-playlists")+":"+s.now().Format("2006-01-02"), TTLRecs, func(ctx context.Context) ([]Playlist, error) { return d.RecommendPlaylists(ctx) })
	s.after(ctx, origin, err)
	return v, err
}

func (s *Service) DiscoverPlaylists(ctx context.Context, origin, cat string, offset, limit int) ([]Playlist, error) {
	d, err := s.discovery(origin)
	if err != nil {
		return nil, err
	}
	v, err := cached(ctx, s.Cache, Key(origin, "discover", fmt.Sprintf("%s:%d:%d", cat, offset, limit)), TTLCharts, func(ctx context.Context) ([]Playlist, error) {
		return d.DiscoverPlaylists(ctx, cat, offset, limit)
	})
	s.after(ctx, origin, err)
	return v, err
}

func (s *Service) UserPlaylists(ctx context.Context, origin string) ([]Playlist, error) {
	p, _, err := s.plugin(origin)
	if err != nil {
		return nil, err
	}
	u, ok := p.(UserLibrary)
	if !ok {
		return nil, ErrNotSupported
	}
	if !s.Reg.LoggedIn(origin) {
		return nil, ErrNeedsLogin
	}
	v, err := cached(ctx, s.Cache, s.userKey(origin, "user-playlists"), 10*time.Minute, func(ctx context.Context) ([]Playlist, error) { return u.UserPlaylists(ctx) })
	s.after(ctx, origin, err)
	return v, err
}

// ── playback ───────────────────────────────────────────────────────────────

// Resolution is what plays for a requested track. URL stays in Core.
type Resolution struct {
	TrackID  string `json:"track_id"`
	PlayID   string `json:"play_id"`
	Origin   string `json:"origin"`
	Local    bool   `json:"local"`
	Fallback string `json:"fallback,omitempty"` // "local" or a plugin id when substituted
	Stream
}

const leaseSlack = 30 * time.Second

func (s *Service) quality(origin string, req Quality) Quality {
	if req != "" {
		return req
	}
	_, cfg, err := s.Reg.Enabled(origin)
	if err != nil {
		return QHigh
	}
	return Quality(cfg.String(KeyQuality, string(QHigh)))
}

func (s *Service) resolveOne(ctx context.Context, id string, q Quality) (Stream, error) {
	o, n, err := split(id)
	if err != nil {
		return Stream{}, err
	}
	p, _, err := s.plugin(o)
	if err != nil {
		return Stream{}, err
	}
	st, ok := p.(Streamer)
	if !ok {
		return Stream{}, ErrNotSupported
	}
	cctx, cancel := withTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := st.Resolve(cctx, n, s.quality(o, q))
	s.after(ctx, o, err)
	if err != nil {
		return res, err
	}
	if res.ExpiresAt.IsZero() {
		res.ExpiresAt = s.now().Add(10 * time.Minute)
	}
	return res, nil
}

// Resolve picks what to play: the requested platform when the account is
// fully entitled; otherwise an equivalent local file, then an equivalent on
// another platform the user is logged into with full entitlement; otherwise
// the original result (preview segment or unavailable). Never anything else.
func (s *Service) Resolve(ctx context.Context, id string, q Quality, fresh bool) (Resolution, error) {
	key := id + "|" + string(q)
	if !fresh {
		s.mu.Lock()
		r, ok := s.leases[key]
		s.mu.Unlock()
		if ok && s.now().Add(leaseSlack).Before(r.ExpiresAt) {
			return r, nil
		}
	}
	r, err := s.resolve(ctx, id, q)
	if err == nil && !r.Local && r.Entitlement != EntUnavailable {
		s.mu.Lock()
		s.leases[key] = r
		if len(s.leases) > 2000 {
			for k, v := range s.leases {
				if s.now().After(v.ExpiresAt) {
					delete(s.leases, k)
				}
			}
		}
		s.mu.Unlock()
	}
	return r, err
}

func (s *Service) resolve(ctx context.Context, id string, q Quality) (Resolution, error) {
	o, _, err := split(id)
	if err != nil {
		return Resolution{TrackID: id, PlayID: id, Origin: library.OriginLocal, Local: true}, nil
	}
	base := Resolution{TrackID: id, PlayID: id, Origin: o}
	st, err := s.resolveOne(ctx, id, q)
	if err == nil && st.Entitlement == EntFull {
		base.Stream = st
		return base, nil
	}
	primaryErr := err
	if err == nil {
		base.Stream = st
	}
	// 1) known equivalents (local first)
	eqs, _ := s.Lib.Equivalents(ctx, id)
	for _, e := range eqs {
		if e.Origin == library.OriginLocal {
			return Resolution{TrackID: id, PlayID: e.TrackID, Origin: library.OriginLocal, Local: true, Fallback: "local",
				Stream: Stream{Entitlement: EntFull}}, nil
		}
	}
	// track metadata for matching
	t, terr := s.Track(ctx, id)
	if terr == nil {
		if lid, conf, method, err := s.Lib.FindMatch(ctx, library.MatchQuery{ISRC: t.ISRC, Title: t.Title, Artist: t.ArtistName, DurationMs: t.DurationMs}); err == nil && lid != "" {
			s.Lib.LinkEquivalent(ctx, id, lid, conf, method)
			return Resolution{TrackID: id, PlayID: lid, Origin: library.OriginLocal, Local: true, Fallback: "local",
				Stream: Stream{Entitlement: EntFull}}, nil
		}
	}
	for _, e := range eqs {
		if e.Origin == o || !s.Reg.LoggedIn(e.Origin) {
			continue
		}
		if alt, err := s.resolveOne(ctx, e.TrackID, q); err == nil && alt.Entitlement == EntFull {
			return Resolution{TrackID: id, PlayID: e.TrackID, Origin: e.Origin, Fallback: e.Origin, Stream: alt}, nil
		}
	}
	// 2) search other logged-in platforms by title+artist+duration
	if terr == nil {
		for _, other := range s.Reg.EnabledIDs() {
			if other == o || !s.Reg.LoggedIn(other) {
				continue
			}
			res := s.Search(ctx, strings.TrimSpace(t.Title+" "+t.ArtistName), 10, []string{other})
			if len(res) == 0 || res[0].Result == nil {
				continue
			}
			cands := res[0].Result.Tracks
			sort.SliceStable(cands, func(i, j int) bool { return cands[i].Playable == EntFull && cands[j].Playable != EntFull })
			for _, c := range cands {
				sc, method := library.MatchScore(library.MatchQuery{ISRC: t.ISRC, Title: t.Title, Artist: t.ArtistName, DurationMs: t.DurationMs},
					c.ISRC, c.Title, c.ArtistName, c.DurationMs)
				if sc < 0.75 || c.Playable == EntUnavailable {
					continue
				}
				alt, err := s.resolveOne(ctx, c.ID, q)
				if err != nil || alt.Entitlement != EntFull {
					continue
				}
				if err := s.Materialize(ctx, []string{c.ID}); err == nil {
					s.Lib.LinkEquivalent(ctx, id, c.ID, sc, method)
				}
				return Resolution{TrackID: id, PlayID: c.ID, Origin: other, Fallback: other, Stream: alt}, nil
			}
		}
	}
	if primaryErr != nil {
		return base, primaryErr
	}
	return base, nil
}

// Invalidate drops a cached lease (e.g. upstream answered 403).
func (s *Service) Invalidate(id string) {
	s.mu.Lock()
	for k := range s.leases {
		if strings.HasPrefix(k, id+"|") {
			delete(s.leases, k)
		}
	}
	s.mu.Unlock()
}

// HTTPClient returns the plugin's client for proxying upstream audio.
func (s *Service) HTTPClient(origin string) (*Client, error) {
	in, err := s.Reg.get(origin)
	if err != nil {
		return nil, err
	}
	return in.client, nil
}

// FlushPlugin drops every cached stream lease of a plugin (account switch:
// leases were resolved with the previous account's entitlement).
func (s *Service) FlushPlugin(origin string) {
	s.Invalidate(origin + ":")
	s.mu.Lock()
	for k := range s.leases {
		if strings.HasPrefix(k, origin+":") {
			delete(s.leases, k)
		}
	}
	s.mu.Unlock()
}

// RefreshHot touches the few always-visible platform lists (charts, and the
// logged-in account's daily recommendations) so the hourly tick revalidates
// them in the background instead of a user waiting on a cold fetch. It is
// deliberately low-frequency and paced.
func (s *Service) RefreshHot(ctx context.Context) {
	for _, id := range s.Reg.EnabledIDs() {
		if ctx.Err() != nil {
			return
		}
		s.Charts(ctx, id)
		time.Sleep(2 * time.Second)
		s.RecommendTracks(ctx, id)
		time.Sleep(2 * time.Second)
	}
}

// FM returns the next batch of the account's personal FM. Not cached (each
// call is the platform's next batch); still rate-limited and cooled down.
func (s *Service) FM(ctx context.Context, origin string) ([]Track, error) {
	p, _, err := s.plugin(origin)
	if err != nil {
		return nil, err
	}
	rd, ok := p.(Radio)
	if !ok {
		return nil, ErrNotSupported
	}
	if !s.Reg.LoggedIn(origin) {
		return nil, ErrNeedsLogin
	}
	v, err := rd.FM(ctx)
	s.after(ctx, origin, err)
	if err == nil {
		s.annotate(ctx, v)
		s.rememberTracks(ctx, v)
	}
	return v, err
}

// FMFeedback forwards like|trash for an FM track ("origin:native" id).
func (s *Service) FMFeedback(ctx context.Context, origin, trackID, action string) error {
	if action != "like" && action != "trash" {
		return ErrBadInput
	}
	o, n, err := split(trackID)
	if err != nil || o != origin {
		return ErrBadInput
	}
	p, _, err := s.plugin(origin)
	if err != nil {
		return err
	}
	rd, ok := p.(Radio)
	if !ok {
		return ErrNotSupported
	}
	if !s.Reg.LoggedIn(origin) {
		return ErrNeedsLogin
	}
	err = rd.FMFeedback(ctx, n, action)
	s.after(ctx, origin, err)
	return err
}
