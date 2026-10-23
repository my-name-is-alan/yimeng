// Package online is the plugin framework for online music platforms
// (NetEase Cloud Music, Kugou, QQ Music, ...). See docs/online-sources.md.
//
// Rules every plugin follows (enforced by review, documented in D30):
//   - credentials (cookies/tokens) live only in the Vault; plugins see them
//     through their per-account Client and never return them in DTOs;
//   - only what the logged-in account is entitled to is played: full,
//     official preview/trial segment, or unavailable. No unblocking, no
//     cross-platform URL grabbing, no decryption of protected formats;
//   - plugins return DTOs; the Service materializes them into the library.
package online

import (
	"context"
	"errors"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
	"time"
)

var (
	ErrNotSupported = errors.New("online: not supported by plugin")
	ErrNeedsLogin   = errors.New("online: login required")
	ErrUnavailable  = errors.New("online: not available for this account")
	ErrRiskControl  = errors.New("online: platform risk control / rate limited")
	ErrDisabled     = errors.New("online: plugin disabled")
	ErrUnknown      = errors.New("online: unknown plugin")
	ErrUpstream     = errors.New("online: upstream error")
	ErrBadInput     = errors.New("online: bad input")
)

// Capability is advertised in the manifest and drives which UI surfaces appear.
type Capability string

const (
	CapSearch        Capability = "search"
	CapLyrics        Capability = "lyrics"
	CapCharts        Capability = "charts"
	CapRecommend     Capability = "recommend"
	CapDiscover      Capability = "discover"
	CapUserPlaylists Capability = "user_playlists"
	CapStream        Capability = "stream"
	CapFM            Capability = "fm"
	CapHotSearch     Capability = "hot_search"
)

// LoginMethod values: "qr", "cookie", "password".
type LoginMethod string

const (
	LoginQR       LoginMethod = "qr"
	LoginCookie   LoginMethod = "cookie"
	LoginPassword LoginMethod = "password"
	LoginSMS      LoginMethod = "sms" // phone number + SMS verification code
)

// Quality is the platform-neutral quality ladder.
type Quality string

const (
	QStandard Quality = "standard" // ~128 kbps
	QHigh     Quality = "high"     // ~320 kbps
	QLossless Quality = "lossless" // 16/44.1 FLAC
	QHiRes    Quality = "hires"    // 24-bit
)

var qualityRank = map[Quality]int{QStandard: 1, QHigh: 2, QLossless: 3, QHiRes: 4}

// Rank orders qualities (0 = unknown).
func (q Quality) Rank() int { return qualityRank[q] }

// ConfigField describes one setting; the web UI renders the schema generically.
type ConfigField struct {
	Key     string         `json:"key"`
	Label   string         `json:"label"`
	Type    string         `json:"type"` // select | number | bool | text
	Options []ConfigOption `json:"options,omitempty"`
	Default any            `json:"default,omitempty"`
	Min     *float64       `json:"min,omitempty"`
	Max     *float64       `json:"max,omitempty"`
	Help    string         `json:"help,omitempty"`
}

type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Manifest is the static description of a plugin.
type Manifest struct {
	ID           string        `json:"id"` // also the origin of its items
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Homepage     string        `json:"homepage,omitempty"`
	Version      string        `json:"version"`
	Capabilities []Capability  `json:"capabilities"`
	LoginMethods []LoginMethod `json:"login_methods"`
	// QRChannels names which app scans each QR channel (UI labels / tabs).
	QRChannels  []QRChannel    `json:"qr_channels,omitempty"`
	Qualities   []Quality      `json:"qualities"`
	Config      []ConfigField  `json:"config"`
	Notice      string         `json:"notice,omitempty"`
	Skeleton    bool           `json:"skeleton,omitempty"` // partial implementation
	Credits     []string       `json:"credits,omitempty"`
	DefaultRate int            `json:"-"` // requests/minute default
	Extra       map[string]any `json:"extra,omitempty"`
}

func (m Manifest) Has(c Capability) bool {
	for _, x := range m.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// Ref is a lightweight id+name reference. IDs are full ("origin:native").
type Ref struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// Entitlement says what the current account may play.
type Entitlement string

const (
	EntFull        Entitlement = "full"
	EntPreview     Entitlement = "preview"
	EntUnavailable Entitlement = "unavailable"
	EntUnknown     Entitlement = ""
)

type Track struct {
	ID         string      `json:"id"` // "origin:native"
	Origin     string      `json:"origin"`
	Title      string      `json:"title"`
	Artists    []Ref       `json:"artists"`
	ArtistName string      `json:"artist_name"`
	Album      *Ref        `json:"album,omitempty"`
	CoverURL   string      `json:"cover_url,omitempty"`
	DurationMs int64       `json:"duration_ms"`
	ISRC       string      `json:"isrc,omitempty"`
	TrackNo    int         `json:"track_no,omitempty"`
	DiscNo     int         `json:"disc_no,omitempty"`
	Year       int         `json:"year,omitempty"`
	Playable   Entitlement `json:"playable,omitempty"` // account entitlement hint from listing
	VIP        bool        `json:"vip,omitempty"`      // platform marks the song as VIP/paid
	MaxQuality Quality     `json:"max_quality,omitempty"`
	// Filled by the Service:
	LocalID    string `json:"local_id,omitempty"`    // equivalent local track
	SortLetter string `json:"sort_letter,omitempty"` // A–Z index key
	Preview    bool   `json:"preview"`               // playable == "preview"
	// Set by the gateway: placeholder data and the platform cover URL.
	Cover *CoverRef `json:"cover,omitempty"`
}

// CoverRef mirrors library.CoverRef (url + blurhash + colours).
type CoverRef struct {
	URL       string   `json:"url"`                  // Core image-cache URL (/api/v1/images/…) when the host is allow-listed, else the CDN URL
	SourceURL string   `json:"source_url,omitempty"` // original CDN URL (D60)
	Blurhash  string   `json:"blurhash,omitempty"`
	Colors    []string `json:"colors,omitempty"`
}

type Album struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Title       string    `json:"title"`
	Artists     []Ref     `json:"artists"`
	ArtistName  string    `json:"artist_name"`
	CoverURL    string    `json:"cover_url,omitempty"`
	Year        int       `json:"year,omitempty"`
	TrackCount  int       `json:"track_count"`
	Description string    `json:"description,omitempty"`
	Tracks      []Track   `json:"tracks,omitempty"`
	Cover       *CoverRef `json:"cover,omitempty"` // set by the gateway (D60)
}

type Artist struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Name        string    `json:"name"`
	PictureURL  string    `json:"picture_url,omitempty"`
	Description string    `json:"description,omitempty"`
	AlbumCount  int       `json:"album_count,omitempty"`
	TrackCount  int       `json:"track_count,omitempty"`
	TopTracks   []Track   `json:"top_tracks,omitempty"`
	Cover       *CoverRef `json:"cover,omitempty"` // picture via Core's image cache, set by the gateway (D60)
}

type Playlist struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Name        string    `json:"name"`
	CoverURL    string    `json:"cover_url,omitempty"`
	Description string    `json:"description,omitempty"`
	TrackCount  int       `json:"track_count"`
	Owner       string    `json:"owner,omitempty"`
	PlayCount   int64     `json:"play_count,omitempty"`
	Kind        string    `json:"kind,omitempty"` // playlist | chart | liked
	Subtitle    string    `json:"subtitle,omitempty"`
	Mine        bool      `json:"mine,omitempty"` // created by the logged-in user
	Tracks      []Track   `json:"tracks,omitempty"`
	Cover       *CoverRef `json:"cover,omitempty"` // set by the gateway
}

// HotKeyword is one platform hot-search entry (keyword + optional score/url).
type HotKeyword struct {
	Keyword string `json:"keyword"`
	Score   int64  `json:"score,omitempty"`
	URL     string `json:"url,omitempty"`
}

type SearchResult struct {
	Tracks    []Track    `json:"tracks"`
	Albums    []Album    `json:"albums"`
	Artists   []Artist   `json:"artists"`
	Playlists []Playlist `json:"playlists"`
}

type Lyrics struct {
	Synced      bool   `json:"synced"`
	LRC         string `json:"lrc"`
	Translation string `json:"translation,omitempty"`
	Roma        string `json:"roma,omitempty"` // romanisation LRC
	// Words is word-timed lyrics when the platform has them (KRC/QRC/YRC).
	Words []lyrics.WordLine `json:"words,omitempty"`
}

// Account describes the logged-in user. Never contains secrets.
type Account struct {
	UserID        string `json:"user_id"`
	Nickname      string `json:"nickname"`
	Avatar        string `json:"avatar,omitempty"`
	AvatarPreview string `json:"avatar_preview,omitempty"` // D60: avatar via Core's image cache (set by the gateway)
	VIP           bool   `json:"vip"`
	Tier          string `json:"tier,omitempty"`
}

// QRChannel describes one QR login channel: which app must scan the code.
type QRChannel struct {
	ID    string `json:"id"`    // e.g. "qq", "wechat", "app"
	Label string `json:"label"` // tab label, e.g. "QQ 扫码"
	App   string `json:"app"`   // the app to scan with, e.g. "手机 QQ"
	Hint  string `json:"hint"`  // full instruction shown under the code
}

type QRStatus string

const (
	QRWaiting   QRStatus = "waiting"
	QRScanned   QRStatus = "scanned"
	QRConfirmed QRStatus = "confirmed"
	QRExpired   QRStatus = "expired"
)

type QRChallenge struct {
	Key       string    `json:"key"`
	Content   string    `json:"content"`         // text to encode in the QR code
	ImageURL  string    `json:"image,omitempty"` // filled by the registry (data: URL)
	ExpiresAt time.Time `json:"expires_at"`
}

type QRPoll struct {
	Status   QRStatus `json:"status"`
	Nickname string   `json:"nickname,omitempty"`
	Message  string   `json:"message,omitempty"`
	Account  *Account `json:"account,omitempty"`
}

// Stream is a just-in-time resolved upstream URL. Header stays in Core (injected when
// proxying). When Header is empty, Core may 302 / return play_url so the client hits the CDN directly.
type Stream struct {
	URL            string            `json:"-"`
	Alternates     []string          `json:"-"` // same file on other CDN hosts, tried in order when URL fails
	Header         map[string]string `json:"-"`
	Entitlement    Entitlement       `json:"entitlement"`
	Quality        Quality           `json:"quality,omitempty"`
	Format         string            `json:"format,omitempty"`
	BitrateKbps    int               `json:"bitrate_kbps,omitempty"`
	SizeBytes      int64             `json:"size_bytes,omitempty"`
	DurationMs     int64             `json:"duration_ms,omitempty"` // full track duration
	PreviewStartMs int64             `json:"preview_start_ms,omitempty"`
	PreviewEndMs   int64             `json:"preview_end_ms,omitempty"`
	ExpiresAt      time.Time         `json:"expires_at"`
	Reason         string            `json:"reason,omitempty"`
}

// Plugin is the minimum: a manifest. Capabilities are optional interfaces.
type Plugin interface {
	Manifest() Manifest
}

// Authenticator handles account login. Session state lives in the plugin's
// Client cookie jar, which the Registry persists to the Vault.
type Authenticator interface {
	StartQR(ctx context.Context) (QRChallenge, error)
	PollQR(ctx context.Context, key string) (QRPoll, error)
	LoginCookie(ctx context.Context, cookie string) (Account, error)
	LoginPassword(ctx context.Context, username, password string) (Account, error)
	// Account validates the current session (ErrNeedsLogin when logged out).
	Account(ctx context.Context) (Account, error)
	Refresh(ctx context.Context) error
}

// SMSAuthenticator is optional: phone-number login with an SMS code. The
// platform may answer SendSMS with ErrRiskControl when it wants a captcha
// that only its own app can show.
type SMSAuthenticator interface {
	SendSMS(ctx context.Context, phone string) error
	LoginSMS(ctx context.Context, phone, code string) (Account, error)
}

type Catalog interface {
	Search(ctx context.Context, q string, limit int) (SearchResult, error)
	Tracks(ctx context.Context, nativeIDs []string) ([]Track, error)
	Album(ctx context.Context, nativeID string) (Album, error)
	Artist(ctx context.Context, nativeID string) (Artist, error)
	Playlist(ctx context.Context, nativeID string) (Playlist, error)
	Lyrics(ctx context.Context, nativeID string) (Lyrics, error)
}

// HotSearcher is optional: platform search-box hot keywords (no login usually).
type HotSearcher interface {
	HotSearch(ctx context.Context) ([]HotKeyword, error)
}

type Discovery interface {
	Charts(ctx context.Context) ([]Playlist, error)
	RecommendTracks(ctx context.Context) ([]Track, error)
	RecommendPlaylists(ctx context.Context) ([]Playlist, error)
	DiscoverPlaylists(ctx context.Context, category string, offset, limit int) ([]Playlist, error)
}

type UserLibrary interface {
	UserPlaylists(ctx context.Context) ([]Playlist, error)
}

// Radio is the logged-in account's personal FM as the platform serves it.
type Radio interface {
	FM(ctx context.Context) ([]Track, error)
	// FMFeedback reports like|trash for a native track id.
	FMFeedback(ctx context.Context, nativeID, action string) error
}

type Streamer interface {
	Resolve(ctx context.Context, nativeID string, q Quality) (Stream, error)
}

// Env is handed to a plugin factory.
type Env struct {
	HTTP   *Client
	Config Config
}

type Factory func(Env) (Plugin, error)

// Config is the decoded, validated plugin configuration.
type Config map[string]any

func (c Config) String(k, def string) string {
	if v, ok := c[k].(string); ok && v != "" {
		return v
	}
	return def
}

func (c Config) Int(k string, def int) int {
	switch v := c[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

func (c Config) Bool(k string, def bool) bool {
	if v, ok := c[k].(bool); ok {
		return v
	}
	return def
}
