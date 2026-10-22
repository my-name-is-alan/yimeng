// Package gateway is the HTTP boundary: auth, DTO mapping, error codes.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/backup"
	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/logging"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/subsonic"
	"github.com/my-name-is-alan/yimeng/core/internal/system"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

// LibraryUseCases is implemented by app.Library.
type LibraryUseCases interface {
	StartScan(ctx context.Context, req library.StartRequest) (library.Job, error)
	ScanStatus(ctx context.Context, id string) (library.Job, error)
	LatestScan(ctx context.Context) (library.Job, error)

	ListArtists(ctx context.Context, p library.ListParams) (library.Page[library.Artist], error)
	GetArtist(ctx context.Context, id string) (library.Artist, error)
	ListAlbums(ctx context.Context, p library.ListParams) (library.Page[library.Album], error)
	GetAlbum(ctx context.Context, id string) (library.Album, error)
	ListTracks(ctx context.Context, p library.ListParams) (library.Page[library.Track], error)
	GetTrack(ctx context.Context, id string) (library.Track, error)
	AlbumCover(ctx context.Context, albumID string) (string, error)
	TrackCover(ctx context.Context, trackID string) (string, error)
	TrackFile(ctx context.Context, trackID string) (library.TrackFile, error)

	AlbumCredits(ctx context.Context, albumID string) ([]library.Credit, error)
	TrackCredits(ctx context.Context, trackID string) ([]library.Credit, error)
	Lyrics(ctx context.Context, trackID string, fetch library.LyricsFetcher) (library.TrackLyrics, error)
	RadioSeeds(ctx context.Context, seedTrackID string, limit int, exclude []string) ([]string, error)
	Health(ctx context.Context) (library.HealthReport, error)
	Stats(ctx context.Context) (library.Stats, error)
	OnThisDay(ctx context.Context, month, day int) (library.OnThisDay, error)

	SplitConfig() library.SplitConfig
	SetSplitConfig(ctx context.Context, c library.SplitConfig) (library.SplitConfig, error)
}

type Deps struct {
	Library  LibraryUseCases
	Playback *playback.Orchestrator
	Hub      *events.Hub
	// Enrich, if non-nil, triggers a background metadata pass (opt-in providers).
	Enrich func()
	Auth   *auth.Service // required: every /api route except pairing needs a device token
	// Transcoder is optional (nil = original-only streaming).
	Transcoder  *transcode.Transcoder
	StreamPrefs StreamPrefs
	Log         *slog.Logger

	Backup          *backup.Service
	RestoreMaxBytes int64
	SchemaVersion   int // newest schema this binary understands
	Version         string
	// LyricsFetcher is an optional remote lyrics provider chain.
	LyricsFetcher  library.LyricsFetcher
	UserData       *userdata.Store
	Playlists      PlaylistUseCases
	System         *system.Collector
	Presence       *Presence
	Admin          AdminLibrary
	Covers         *covers.Cache
	Providers      *metadata.Registry // optional; listed by GET /api/metadata/providers
	TrustedProxies *TrustedProxies
	RateLimits     *RateLimits
	Subsonic       *subsonic.Handler // optional; mounts /rest/*
	Sources        *playback.Registry
	Vault          vault.Vault // for logout / credential delete; never exposed

	// Web admin extras (all optional).
	UserEdits    UserEdits
	LyricsSearch LyricsSearcher
	Matcher      metadata.MetadataProvider // on-demand MusicBrainz lookups for the metadata editor
	Logs         *logging.Ring
	Runtime      map[string]any // non-secret effective config snapshot
}

type contextT = context.Context

func NewRouter(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Presence == nil {
		d.Presence = NewPresence(d.Hub)
	}
	mux := http.NewServeMux()
	d.mountWebUI(mux)
	d.mountWebAdminAPI(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "yimeng-core"})
	})

	// pairing (public, rate-limited)
	mux.HandleFunc("POST /api/pair/request", d.pairRequest)
	mux.HandleFunc("POST /api/pair", d.pair)
	// device management (auth)
	mux.HandleFunc("POST /api/pair/codes", d.newPairCode)
	mux.HandleFunc("GET /api/devices", d.listDevices)
	mux.HandleFunc("GET /api/devices/me", d.me)
	mux.HandleFunc("GET /api/devices/me/stream-quality", d.getStreamQuality)
	mux.HandleFunc("PUT /api/devices/me/stream-quality", d.putStreamQuality)
	mux.HandleFunc("DELETE /api/devices/{id}", d.revokeDevice)

	mux.HandleFunc("POST /api/library/scan", d.startScan)
	mux.HandleFunc("GET /api/library/scan", d.latestScan)
	mux.HandleFunc("GET /api/library/scan/{id}", d.scanStatus)

	mux.HandleFunc("GET /api/artists", d.listArtists)
	mux.HandleFunc("GET /api/artists/{id}", d.getArtist)
	mux.HandleFunc("GET /api/albums", d.listAlbums)
	mux.HandleFunc("GET /api/albums/{id}", d.getAlbum)
	mux.HandleFunc("GET /api/albums/{id}/cover", d.albumCover)
	mux.HandleFunc("GET /api/tracks", d.listTracks)
	mux.HandleFunc("GET /api/tracks/{id}", d.getTrack)
	mux.HandleFunc("GET /api/tracks/{id}/cover", d.trackCover)
	mux.HandleFunc("GET /api/search", d.search)
	mux.HandleFunc("GET /api/library/health", d.libraryHealth)
	mux.HandleFunc("GET /api/stats", d.libraryStats)
	mux.HandleFunc("GET /api/stats/on-this-day", d.onThisDay)
	mux.HandleFunc("GET /api/stream/{id}", d.stream)
	mux.HandleFunc("GET /api/tracks/{id}/download", d.download)

	mux.HandleFunc("GET /api/players", d.listPlayers)
	mux.HandleFunc("GET /api/players/{id}", d.playerState)
	mux.HandleFunc("GET /api/players/{id}/queue", d.playerQueue)
	for _, a := range []string{"play", "pause", "stop", "next", "prev", "seek", "repeat", "shuffle", "queue", "transfer", "sleep"} {
		mux.HandleFunc("POST /api/players/{id}/"+a, d.playerAction(a))
	}
	mux.HandleFunc("POST /api/players/{id}/radio", d.playerRadio)
	mux.HandleFunc("GET /ws", d.ws)

	mux.HandleFunc("GET /api/tracks/{id}/credits", d.trackCredits)
	mux.HandleFunc("GET /api/tracks/{id}/lyrics", d.trackLyrics)
	mux.HandleFunc("POST /api/metadata/enrich", d.enrich)
	mux.HandleFunc("GET /api/metadata/providers", d.listProviders)

	mux.HandleFunc("GET /api/settings/artist-split", d.getSplit)
	mux.HandleFunc("PUT /api/settings/artist-split", d.putSplit)

	if d.System != nil {
		mux.HandleFunc("GET /api/system", d.getSystem)
	}
	mux.HandleFunc("PATCH /api/devices/{id}", d.adminOnly(d.patchDevice))
	if d.Admin != nil {
		mux.HandleFunc("GET /api/settings/library-roots", d.adminOnly(d.getRoots))
		mux.HandleFunc("PUT /api/settings/library-roots", d.adminOnly(d.putRoots))
		mux.HandleFunc("GET /api/overrides/{type}/{id}", d.getOverride)
		mux.HandleFunc("PUT /api/overrides/{type}/{id}", d.adminOnly(d.putOverride))
		mux.HandleFunc("DELETE /api/overrides/{type}/{id}", d.adminOnly(d.deleteOverride))
		mux.HandleFunc("POST /api/albums/merge", d.adminOnly(d.mergeAlbums))
		mux.HandleFunc("POST /api/albums/split", d.adminOnly(d.splitAlbum))
	}
	if d.Playlists != nil {
		mux.HandleFunc("GET /api/playlists", d.listPlaylists)
		mux.HandleFunc("POST /api/playlists", d.createPlaylist)
		mux.HandleFunc("POST /api/playlists/import", d.importM3U)
		mux.HandleFunc("GET /api/playlists/{id}", d.getPlaylist)
		mux.HandleFunc("PATCH /api/playlists/{id}", d.updatePlaylist)
		mux.HandleFunc("DELETE /api/playlists/{id}", d.deletePlaylist)
		mux.HandleFunc("POST /api/playlists/{id}/items", d.editPlaylist)
		mux.HandleFunc("GET /api/playlists/{id}/m3u", d.exportM3U)
	}
	if d.UserData != nil {
		mux.HandleFunc("POST /api/scrobble", d.scrobble)
		mux.HandleFunc("GET /api/history", d.history)
		mux.HandleFunc("GET /api/history/top", d.historyTop)
		mux.HandleFunc("GET /api/favorites", d.favorites)
		mux.HandleFunc("PUT /api/favorites/{type}/{id}", d.setFavorite(true))
		mux.HandleFunc("DELETE /api/favorites/{type}/{id}", d.setFavorite(false))
		mux.HandleFunc("PUT /api/ratings/{type}/{id}", d.setRating)
	}
	if d.Sources != nil {
		mux.HandleFunc("GET /api/sources", d.listSources)
		mux.HandleFunc("GET /api/sources/{id}", d.getSource)
		mux.HandleFunc("POST /api/sources/{id}/login", d.adminOnly(d.loginSource))
		mux.HandleFunc("POST /api/sources/{id}/login/qr", d.adminOnly(d.startQR))
		mux.HandleFunc("GET /api/sources/{id}/login/qr/{challenge}", d.adminOnly(d.pollQR))
		mux.HandleFunc("DELETE /api/sources/{id}/credentials", d.adminOnly(d.logoutSource))
		mux.HandleFunc("POST /api/sources/{id}/sync", d.adminOnly(d.syncSource))
	}
	if d.Subsonic != nil {
		d.Subsonic.Stream = d.stream
		d.Subsonic.Mount(mux)
	}
	if d.Backup != nil {
		mux.HandleFunc("GET /api/system/backups", d.adminOnly(d.listBackups))
		mux.HandleFunc("POST /api/system/backups", d.adminOnly(d.createBackup))
		mux.HandleFunc("GET /api/system/backups/{name}", d.adminOnly(d.downloadBackup))
		mux.HandleFunc("DELETE /api/system/backups/{name}", d.adminOnly(d.deleteBackup))
		mux.HandleFunc("GET /api/system/export", d.adminOnly(d.exportBackup))
		mux.HandleFunc("POST /api/system/restore", d.adminOnly(d.restoreBackup))
	}

	h := securityHeaders(d.requireAuth(mux))
	h = rateLimit(d.RateLimits, h)
	h = realIP(d.TrustedProxies, h)
	h = apiV1Alias(h)
	return accessLog(d.Log, h)
}

type ctxKey struct{}

func DeviceFrom(ctx context.Context) (auth.Device, bool) {
	dv, ok := ctx.Value(ctxKey{}).(auth.Device)
	return dv, ok
}

func isPublic(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/rest/") {
		return true // Subsonic handler does its own auth
	}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/ws" {
		return true // /healthz and 404s
	}
	return r.Method == http.MethodPost && (r.URL.Path == "/api/pair" || r.URL.Path == "/api/pair/request")
}

// allowsQueryToken: <audio>/<img>/WebSocket cannot send headers, so these GETs may pass
// ?access_token=. Core never logs request URLs.
func allowsQueryToken(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	return p == "/ws" || strings.HasPrefix(p, "/api/stream/") || strings.HasSuffix(p, "/download") ||
		(strings.HasSuffix(p, "/cover") && (strings.HasPrefix(p, "/api/albums/") || strings.HasPrefix(p, "/api/tracks/")))
}

func (d Deps) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}
		tok := ""
		if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			tok = strings.TrimSpace(h[7:])
		} else if allowsQueryToken(r) {
			tok = r.URL.Query().Get("access_token")
		}
		if tok == "" || d.Auth == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="yimeng"`)
			writeErr(w, http.StatusUnauthorized, "unauthorized", "device token required")
			return
		}
		dev, err := d.Auth.Authenticate(r.Context(), tok)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="yimeng", error="invalid_token"`)
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked device token")
			return
		}
		noteDevice(r.Context(), dev.ID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, dev)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// apiV1Alias rewrites /api/v1/... to /api/... so clients can pin a versioned prefix.
func apiV1Alias(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/api/" + strings.TrimPrefix(r.URL.Path, "/api/v1/")
			next.ServeHTTP(w, r2)
			return
		}
		if r.URL.Path == "/api/v1" {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/api"
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the peer host. When TrustedProxies rewrote RemoteAddr via
// realIP, this is the original client.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func decodeBody(r *http.Request, v any) error { return decodeBodyMax(r, v, 64<<10) }

func decodeBodyMax(r *http.Request, v any, maxBytes int64) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, kind, msg string) {
	writeJSON(w, code, map[string]string{"error": kind, "message": msg})
}

func (d Deps) internal(w http.ResponseWriter, what string, err error) {
	d.Log.Error(what, "err", err)
	writeErr(w, http.StatusInternalServerError, "internal", what+" failed")
}
