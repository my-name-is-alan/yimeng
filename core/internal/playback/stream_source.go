// Package playback: StreamSource SPI for future user-account playback sources
// (Apple Music via MusicKit on the client, 网易云 / QQ音乐 via the user's own
// account and the platform's normal play URLs).
//
// Rules for every StreamSource:
//   - credentials live only in the Vault (encrypted at rest) and are reached
//     through vault.Scoped; they are never sent to clients;
//   - only content the user's own account may play through the platform's
//     ordinary play endpoints; no DRM circumvention and no decryption of
//     encrypted formats anywhere in Core;
//   - when the platform requires its own SDK on the device (Apple MusicKit),
//     the lease says so (DeliveryClientSDK) and Core only coordinates;
//   - adapters return DTOs and never write canonical tables.
//
// Interface only; no implementations in P0.
package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

var (
	ErrNotSupported        = errors.New("stream: not supported by source")
	ErrAuthExpired         = errors.New("stream: credentials expired")
	ErrNeedsLogin          = errors.New("stream: source needs login")
	ErrPlayableUnavailable = errors.New("stream: track not playable for this account")
	ErrSourceUnavailable   = errors.New("stream: source unavailable")
)

// Delivery describes how a client obtains audio for a lease.
type Delivery string

const (
	DeliveryDirect    Delivery = "direct"     // client fetches URL (local file range endpoint)
	DeliveryProxy     Delivery = "proxy"      // Core proxies bytes; upstream URL never leaves Core
	DeliveryClientSDK Delivery = "client_sdk" // client plays via vendor SDK (MusicKit); Core passes catalog id only
)

// StreamLease is short-lived; the Orchestrator renews before ExpiresAt.
type StreamLease struct {
	Delivery    Delivery
	URL         string // client-facing URL (Core proxy or local); never an upstream signed URL for proxy
	ExpiresAt   time.Time
	Format      string // mp3 | flac | aac | ...
	BitrateKbps int
	SampleRate  int
	CatalogID   string // for DeliveryClientSDK
}

// LoginMethodInfo advertises which login flows a source supports.
type LoginMethodInfo struct {
	Method vault.LoginMethod `json:"method"`
}

// QRChallenge is returned to the UI to render; polling happens in Core.
type QRChallenge struct {
	ID        string
	ImageURL  string // or data: URL / payload to render
	ExpiresAt time.Time
}

type QRStatus string

const (
	QRPending   QRStatus = "pending"
	QRScanned   QRStatus = "scanned"
	QRConfirmed QRStatus = "confirmed"
	QRExpired   QRStatus = "expired"
)

// LoginInput carries user-entered login material (cookie or username+password).
// It goes straight to the adapter, which converts it to a vault.Credential;
// it is never persisted or logged in plain text.
type LoginInput struct {
	Method   vault.LoginMethod `json:"method"`
	Cookie   string            `json:"cookie,omitempty"`
	Username string            `json:"username,omitempty"`
	Password string            `json:"password,omitempty"`
}

type AccountInfo struct {
	DisplayName string `json:"display_name"`
	Tier        string `json:"tier,omitempty"` // e.g. free / vip; informs ErrPlayableUnavailable
}

type SourceHealth string

const (
	SourceOK          SourceHealth = "ok"
	SourceDegraded    SourceHealth = "degraded"
	SourceDown        SourceHealth = "down"
	SourceNeedsReauth SourceHealth = "needs_reauth"
)

type LibraryItem struct {
	SourceTrackID                     string
	Title, Artist, Album, AlbumArtist string
	DurationMs                        int64
	ISRC                              string
}

type SyncPage struct {
	Items      []LibraryItem
	NextCursor string // empty = done
}

// StreamSource is the playback-side SPI (doc "SourcePort").
type StreamSource interface {
	ID() string
	Health(ctx context.Context) SourceHealth

	// Login
	LoginMethods() []LoginMethodInfo
	StartQRLogin(ctx context.Context) (QRChallenge, error)
	PollQRLogin(ctx context.Context, challengeID string) (QRStatus, error) // on confirm, stores credential via Vault
	Login(ctx context.Context, in LoginInput) (AccountInfo, error)         // cookie / password; stores via Vault
	Account(ctx context.Context) (AccountInfo, error)
	RefreshCredentials(ctx context.Context) error
	Logout(ctx context.Context) error

	// User library
	SyncLibrary(ctx context.Context, cursor string) (SyncPage, error)

	// Playback
	ResolveStream(ctx context.Context, sourceTrackID string) (StreamLease, error)
	// OpenStream is used for DeliveryProxy: Core streams upstream bytes to the
	// client as-is (no decryption/transcoding). rangeHeader may be empty.
	OpenStream(ctx context.Context, sourceTrackID, rangeHeader string) (body io.ReadCloser, contentType string, err error)
}

type StreamDeps struct {
	Credentials vault.Scoped
}

type StreamFactory func(StreamDeps) (StreamSource, error)

// Registry holds enabled StreamSources.
type Registry struct {
	mu        sync.Mutex
	providers map[string]StreamSource
	factories map[string]StreamFactory
}

func NewStreamRegistry() *Registry {
	return &Registry{providers: map[string]StreamSource{}, factories: map[string]StreamFactory{}}
}

func (r *Registry) RegisterFactory(id string, f StreamFactory) {
	r.mu.Lock()
	r.factories[id] = f
	r.mu.Unlock()
}

func (r *Registry) Enable(id string, deps StreamDeps) (StreamSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.providers[id]; ok {
		return p, nil
	}
	f, ok := r.factories[id]
	if !ok {
		return nil, fmt.Errorf("stream: unknown source %q", id)
	}
	p, err := f(deps)
	if err != nil {
		return nil, err
	}
	r.providers[id] = p
	return p, nil
}

func (r *Registry) Put(p StreamSource) {
	r.mu.Lock()
	r.providers[p.ID()] = p
	r.mu.Unlock()
}

func (r *Registry) Get(id string) (StreamSource, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) All() []StreamSource {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StreamSource, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}
