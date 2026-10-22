// Package metadata defines the MetadataProvider SPI used to enrich the
// library from external catalogues (MusicBrainz, QQ音乐, 网易云, Apple Music,
// Spotify, 酷狗, ...).
//
// Scope rules for every provider:
//   - metadata only: tags, cover art, lyrics, artist/album info;
//   - never audio bytes, stream URLs, or DRM / encrypted-format handling;
//   - credentials only via a vault.Scoped handed in at construction time;
//   - providers return DTOs and never write to the database themselves.
//
// MusicBrainz is registered when enabled; NetEase/QQ stay unregistered (D12).
package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

type ProviderID string

const (
	MusicBrainz ProviderID = "musicbrainz"
	QQMusic     ProviderID = "qq"
	NetEase     ProviderID = "netease"
	AppleMusic  ProviderID = "apple"
	Spotify     ProviderID = "spotify"
	Kugou       ProviderID = "kugou"
)

// Capability is a bit set of what a provider can supply.
type Capability uint32

const (
	CapTrackTags Capability = 1 << iota
	CapAlbumInfo
	CapArtistInfo
	CapCoverArt
	CapLyrics
	CapCredits
)

func (c Capability) Has(o Capability) bool { return c&o == o }

var (
	ErrNotSupported = errors.New("metadata: capability not supported by provider")
	ErrNotFound     = errors.New("metadata: no match")
	ErrRateLimited  = errors.New("metadata: rate limited")
	ErrAuthExpired  = errors.New("metadata: credentials expired") // caller asks Vault to refresh
	ErrUnavailable  = errors.New("metadata: provider unavailable")
)

type HealthState string

const (
	HealthOK       HealthState = "ok"
	HealthDegraded HealthState = "degraded"
	HealthDown     HealthState = "down"
)

// TrackQuery describes what we already know about a track; providers use
// whichever identifiers they understand (IDs beat fuzzy text).
type TrackQuery struct {
	Title, Artist, Album, AlbumArtist string
	DurationMs                        int64
	ISRC                              string
	MBRecordingID, MBReleaseID        string
	ExternalIDs                       map[ProviderID]string
}

type AlbumQuery struct {
	Title, AlbumArtist string
	Year               int
	MBReleaseID        string
	ExternalIDs        map[ProviderID]string
}

type ArtistQuery struct {
	Name        string
	MBArtistID  string
	ExternalIDs map[ProviderID]string
}

type Credit struct {
	Role, Name, MBID string
}

type TrackMatch struct {
	Provider                                 ProviderID
	ExternalID                               string
	Score                                    float64 // 0..1 provider-side confidence
	Title, Artist, Album, AlbumArtist, Genre string
	TrackNo, DiscNo, Year                    int
	DurationMs                               int64
	ISRC, MBRecordingID, MBReleaseID         string
	Credits                                  []Credit
	ReleaseDate                              string
	CoverURL                                 string // album art (provider CDN), optional
	Popularity                               int    // 0..100 provider-specific, 0 = unknown
}

type AlbumInfo struct {
	Provider    ProviderID
	ExternalID  string
	Title       string
	AlbumArtist string
	Year        int
	ReleaseDate string
	Label       string
	Genres      []string
	MBReleaseID string
	Description string
	UPC         string
	CoverURL    string
	Popularity  int
	TrackCount  int
}

type ArtistInfo struct {
	Provider   ProviderID
	ExternalID string
	Name       string
	SortName   string
	Aliases    []string
	Country    string
	Begin, End string
	Bio        string
	ImageURLs  []string
	MBArtistID string
	Genres     []string
	Popularity int
	Followers  int
}

type CoverCandidate struct {
	Provider      ProviderID
	URL           string
	MIME          string
	Width, Height int
}

type LyricLine struct {
	At   time.Duration
	Text string
}

type Lyrics struct {
	Provider    ProviderID
	Language    string
	Plain       string
	Synced      []LyricLine // empty if only plain text is available
	Translation []LyricLine // e.g. 网易云 tlyric
}

// MetadataProvider is the SPI. Methods for capabilities a provider does not
// declare must return ErrNotSupported.
type MetadataProvider interface {
	ID() ProviderID
	Capabilities() Capability
	Health(ctx context.Context) HealthState

	LookupTrack(ctx context.Context, q TrackQuery) ([]TrackMatch, error)
	LookupAlbum(ctx context.Context, q AlbumQuery) ([]AlbumInfo, error)
	LookupArtist(ctx context.Context, q ArtistQuery) ([]ArtistInfo, error)
	CoverArt(ctx context.Context, q AlbumQuery) ([]CoverCandidate, error)
	// FetchCover downloads one candidate's image bytes (images only).
	FetchCover(ctx context.Context, c CoverCandidate) (io.ReadCloser, error)
	Lyrics(ctx context.Context, q TrackQuery) (*Lyrics, error)
}

// Deps are what the Core hands a provider factory. HTTP should already carry
// timeouts and concurrency limits (docs §10.4).
type Deps struct {
	Credentials vault.Scoped // nil for providers that need none
	HTTP        *http.Client
	UserAgent   string
}

type Factory func(Deps) (MetadataProvider, error)

// Registry holds enabled MetadataProviders keyed by id.
type Registry struct {
	mu        sync.Mutex
	providers map[ProviderID]MetadataProvider
	factories map[ProviderID]Factory
}

func NewRegistry() *Registry {
	return &Registry{providers: map[ProviderID]MetadataProvider{}, factories: map[ProviderID]Factory{}}
}

func (r *Registry) RegisterFactory(id ProviderID, f Factory) {
	r.mu.Lock()
	r.factories[id] = f
	r.mu.Unlock()
}

// Enable constructs the provider (idempotent) using deps.
func (r *Registry) Enable(id ProviderID, deps Deps) (MetadataProvider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.providers[id]; ok {
		return p, nil
	}
	f, ok := r.factories[id]
	if !ok {
		return nil, fmt.Errorf("metadata: unknown provider %q", id)
	}
	p, err := f(deps)
	if err != nil {
		return nil, err
	}
	r.providers[id] = p
	return p, nil
}

func (r *Registry) Put(p MetadataProvider) {
	r.mu.Lock()
	r.providers[p.ID()] = p
	r.mu.Unlock()
}

func (r *Registry) Get(id ProviderID) (MetadataProvider, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) All() []MetadataProvider {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]MetadataProvider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}

func (r *Registry) IDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.providers))
	for id := range r.providers {
		out = append(out, string(id))
	}
	return out
}
