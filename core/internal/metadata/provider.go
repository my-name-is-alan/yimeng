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
// No implementations exist yet (P0).
package metadata

import (
	"context"
	"errors"
	"io"
	"net/http"
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
