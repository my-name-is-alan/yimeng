// Package gateway is the HTTP boundary: auth, DTO mapping, error codes.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/my-name-is-alan/yimeng/core/internal/lastfm"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"github.com/my-name-is-alan/yimeng/core/internal/runtimecfg"
	"github.com/my-name-is-alan/yimeng/core/internal/secalert"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/backup"
	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/logging"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
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
	// Rooms holds 一起听 rooms (nil = package default).
	Rooms    *Rooms
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
	ServerName      string // shown by /api/v1/server/info and mDNS
	// LyricsFetcher is an optional remote lyrics provider chain.
	LyricsFetcher  library.LyricsFetcher
	UserData       *userdata.Store
	Alerts         *secalert.Store // security alerts (D54), optional
	BackupInterval time.Duration   // automatic backup interval (0 = off)
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
	LastFM         *lastfm.Service // optional per-account scrobbling (D56)
	Vault          vault.Vault     // for logout / credential delete; never exposed

	// Web admin extras (all optional).
	UserEdits    UserEdits
	LyricsSearch LyricsSearcher
	// ArtistAliases resolves other names of an artist (MusicBrainz), used by
	// the lyrics scorer; optional.
	ArtistAliases func(ctx context.Context, name string) []string
	Matcher       metadata.MetadataProvider // on-demand MusicBrainz lookups for the metadata editor
	// ServerSettings are runtime-editable server settings (watcher, rescan, MusicBrainz, background fill).
	ServerSettings *runtimecfg.Store
	Logs           *logging.Ring
	Runtime        map[string]any // non-secret effective config snapshot

	// Online source plugins (optional).
	Online    *online.Service
	OnlineLib OnlineLibrary
}

type contextT = context.Context

func NewRouter(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Rooms == nil {
		d.Rooms = NewRooms()
	}
	if d.Presence == nil {
		d.Presence = NewPresence(d.Hub)
	}
	mux := http.NewServeMux()
	d.mountWebUI(mux)
	d.mountWebAdminAPI(mux)
	d.mountOnline(mux)
	mux.HandleFunc("GET /api/settings/mode", d.getMode)
	mux.HandleFunc("PUT /api/settings/mode", d.adminOnly(d.putMode))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "yimeng-core"})
	})

	// pairing (public, rate-limited)
	mux.HandleFunc("POST /api/pair/request", d.pairRequest)
	mux.HandleFunc("POST /api/pair", d.pair)
	// web accounts (setup/login public + rate-limited/lockout; rest authenticated)
	mux.HandleFunc("GET /api/auth/setup", d.authStatus)
	mux.HandleFunc("POST /api/auth/setup", d.authSetup)
	mux.HandleFunc("POST /api/auth/login", d.authLogin)
	mux.HandleFunc("POST /api/auth/refresh", d.authRefresh)
	mux.HandleFunc("GET /api/server/info", d.serverInfo)
	mux.HandleFunc("GET /api/events", d.events)
	mux.HandleFunc("GET /api/search/suggest", d.searchSuggest)
	mux.HandleFunc("GET /api/covers/{id}", d.coverV1)
	mux.HandleFunc("GET /api/covers/{id}/meta", d.coverMeta)
	if d.UserData != nil {
		mux.HandleFunc("GET /api/me/source-switch", d.getSourceSwitch)
		mux.HandleFunc("GET /api/me/playback", d.getPlaybackPrefs)
		mux.HandleFunc("GET /api/me/report", d.getReport)
		mux.HandleFunc("GET /api/me/lastfm", d.getLastFM)
		mux.HandleFunc("PUT /api/me/lastfm", d.putLastFM)
		mux.HandleFunc("DELETE /api/me/lastfm", d.deleteLastFM)
		mux.HandleFunc("PUT /api/me/lastfm/keys", d.putLastFMKeys)
		mux.HandleFunc("POST /api/me/lastfm/auth", d.startLastFMAuth)
		mux.HandleFunc("POST /api/me/lastfm/auth/complete", d.completeLastFMAuth)
		mux.HandleFunc("PUT /api/me/playback", d.putPlaybackPrefs)
		mux.HandleFunc("PUT /api/me/source-switch", d.putSourceSwitch)
		mux.HandleFunc("GET /api/progress", d.getProgress)
		mux.HandleFunc("PUT /api/progress/{id}", d.putProgress)
		mux.HandleFunc("DELETE /api/progress/{id}", d.deleteProgress)
	}
	mux.HandleFunc("POST /api/auth/logout", d.authLogout)
	mux.HandleFunc("POST /api/auth/password", d.authPassword)
	mux.HandleFunc("GET /api/users", d.adminOnly(d.listUsers))
	mux.HandleFunc("GET /api/me", d.getMe)
	mux.HandleFunc("PATCH /api/me", d.patchMe)
	mux.HandleFunc("PUT /api/me/avatar", d.putMyAvatar)
	mux.HandleFunc("DELETE /api/me/avatar", d.deleteMyAvatar)
	mux.HandleFunc("GET /api/users/{id}/avatar", d.getUserAvatar)
	mux.HandleFunc("PUT /api/users/{id}/avatar", d.adminOnly(d.putUserAvatar))
	mux.HandleFunc("DELETE /api/users/{id}/avatar", d.adminOnly(d.deleteUserAvatar))
	mux.HandleFunc("POST /api/users", d.adminOnly(d.createUser))
	mux.HandleFunc("PATCH /api/users/{id}", d.adminOnly(d.patchUser))
	mux.HandleFunc("DELETE /api/users/{id}", d.adminOnly(d.deleteUser))
	// device management (auth)
	mux.HandleFunc("POST /api/pair/codes", d.adminOnly(d.newPairCode))
	mux.HandleFunc("GET /api/devices", d.listDevices)
	mux.HandleFunc("GET /api/devices/me", d.me)
	mux.HandleFunc("GET /api/devices/me/stream-quality", d.getStreamQuality)
	mux.HandleFunc("PUT /api/devices/me/stream-quality", d.putStreamQuality)
	mux.HandleFunc("DELETE /api/devices/{id}", d.revokeDevice)

	mux.HandleFunc("POST /api/library/scan", d.adminOnly(d.startScan))
	mux.HandleFunc("GET /api/library/scan", d.latestScan)
	mux.HandleFunc("GET /api/library/scan/{id}", d.scanStatus)

	mux.HandleFunc("GET /api/artists", d.listArtists)
	mux.HandleFunc("GET /api/artists/{id}/albums", d.artistAlbums)
	mux.HandleFunc("GET /api/artists/{id}/tracks", d.artistTracks)
	mux.HandleFunc("GET /api/artists/{id}", d.getArtist)
	if d.UserEdits != nil {
		mux.HandleFunc("GET /api/artists/{id}/image", d.artistImage)
		mux.HandleFunc("PUT /api/artists/{id}/image", d.adminOnly(d.putArtistImage))
		mux.HandleFunc("DELETE /api/artists/{id}/image", d.adminOnly(d.deleteArtistImage))
		mux.HandleFunc("GET /api/artists/{id}/image/candidates", d.adminOnly(d.artistImageCandidates))
		mux.HandleFunc("POST /api/artists/{id}/image/from-match", d.adminOnly(d.artistImageFromMatch))
	}
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
	mux.HandleFunc("GET /api/sessions", d.listSessions)
	if d.Alerts != nil {
		mux.HandleFunc("GET /api/security/alerts", d.listAlerts)
		mux.HandleFunc("POST /api/security/alerts/read", d.readAlerts)
	}
	mux.HandleFunc("GET /api/rooms", d.listRooms)
	mux.HandleFunc("POST /api/rooms", d.createRoom)
	mux.HandleFunc("GET /api/rooms/{id}", d.getRoom)
	mux.HandleFunc("POST /api/rooms/{id}/join", d.joinRoom)
	mux.HandleFunc("POST /api/rooms/{id}/leave", d.leaveRoom)
	mux.HandleFunc("PUT /api/rooms/{id}/state", d.putRoomState)
	mux.HandleFunc("GET /api/rooms/{id}/danmaku", d.listDanmaku)
	mux.HandleFunc("POST /api/rooms/{id}/danmaku", d.postDanmaku)
	mux.HandleFunc("GET /api/rooms/{id}/events", d.roomEvents)
	mux.HandleFunc("GET /room/{code}", d.roomPage)
	mux.HandleFunc("POST /api/sessions/handoff", d.handoff)
	mux.HandleFunc("GET /api/players/{id}", d.ownPlayer(d.playerState))
	mux.HandleFunc("GET /api/players/{id}/queue", d.ownPlayer(d.playerQueue))
	for _, a := range []string{"play", "pause", "stop", "next", "prev", "seek", "repeat", "shuffle", "queue", "transfer", "sleep"} {
		mux.HandleFunc("POST /api/players/{id}/"+a, d.ownPlayer(d.playerAction(a)))
	}
	mux.HandleFunc("POST /api/players/{id}/radio", d.ownPlayer(d.playerRadio))
	mux.HandleFunc("GET /ws", d.ws)

	mux.HandleFunc("GET /api/tracks/{id}/dream", d.trackDream)
	mux.HandleFunc("GET /api/tracks/{id}/credits", d.trackCredits)
	mux.HandleFunc("GET /api/tracks/{id}/alternatives", d.trackAlternatives)
	mux.HandleFunc("GET /api/tracks/{id}/lyrics", d.trackLyrics)
	mux.HandleFunc("GET /api/lyrics/search", d.lyricsSearchAPI)
	mux.HandleFunc("GET /api/lyrics/best", d.lyricsBestAPI)
	mux.HandleFunc("POST /api/metadata/enrich", d.adminOnly(d.enrich))
	mux.HandleFunc("GET /api/metadata/providers", d.listProviders)

	mux.HandleFunc("GET /api/settings/artist-split", d.getSplit)
	mux.HandleFunc("PUT /api/settings/artist-split", d.adminOnly(d.putSplit))

	if d.System != nil {
		mux.HandleFunc("GET /api/system", d.getSystem)
	}
	mux.HandleFunc("PATCH /api/devices/{id}", d.adminOnly(d.patchDevice))
	if d.Admin != nil {
		mux.HandleFunc("GET /api/settings/library-roots", d.adminOnly(d.getRoots))
		mux.HandleFunc("GET /api/settings/server", d.adminOnly(d.getServerSettings))
		mux.HandleFunc("PUT /api/settings/server", d.adminOnly(d.putServerSettings))
		mux.HandleFunc("GET /api/settings/cache", d.adminOnly(d.getCacheStats))
		mux.HandleFunc("DELETE /api/settings/cache", d.adminOnly(d.clearCache))
		mux.HandleFunc("PUT /api/settings/library-roots", d.adminOnly(d.putRoots))
		mux.HandleFunc("GET /api/overrides/{type}/{id}", d.getOverride)
		mux.HandleFunc("PUT /api/overrides/{type}/{id}", d.adminOnly(d.localOnly("", d.putOverride)))
		mux.HandleFunc("DELETE /api/overrides/{type}/{id}", d.adminOnly(d.localOnly("", d.deleteOverride)))
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
		mux.HandleFunc("GET /api/playlists/{id}/tracks", d.playlistTracks)
		mux.HandleFunc("POST /api/playlists/{id}/tracks", d.playlistTracksEdit)
		mux.HandleFunc("PUT /api/playlists/{id}/tracks", d.playlistTracksEdit)
		mux.HandleFunc("DELETE /api/playlists/{id}/tracks", d.playlistTracksEdit)
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
		mux.HandleFunc("POST /api/system/backups/{name}/restore", d.adminOnly(d.restoreStored))
		mux.HandleFunc("GET /api/system/backups/policy", d.adminOnly(d.getBackupPolicy))
		mux.HandleFunc("PUT /api/system/backups/policy", d.adminOnly(d.putBackupPolicy))
	}

	h := securityHeaders(d.requireAuth(mux))
	h = rateLimit(d.RateLimits, h)
	h = realIP(d.TrustedProxies, h)
	h = apiV1Alias(h)
	return accessLog(d.Log, h)
}

type ctxKey struct{}

// withDevice attaches the device and its owning account (D49).
func withDevice(ctx context.Context, dev auth.Device) context.Context {
	return owner.With(context.WithValue(ctx, ctxKey{}, dev), dev.UserID, dev.Admin)
}

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
	switch r.URL.Path {
	case "/api/auth/setup":
		return r.Method == http.MethodGet || r.Method == http.MethodPost
	case "/api/auth/login", "/api/auth/refresh":
		return r.Method == http.MethodPost
	case "/api/server/info":
		return r.Method == http.MethodGet || r.Method == http.MethodHead
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
	return p == "/ws" || p == "/api/events" || (strings.HasPrefix(p, "/api/rooms/") && strings.HasSuffix(p, "/events")) || strings.HasPrefix(p, "/api/stream/") || strings.HasPrefix(p, "/api/covers/") || strings.HasSuffix(p, "/download") ||
		(strings.HasSuffix(p, "/cover") && (strings.HasPrefix(p, "/api/albums/") || strings.HasPrefix(p, "/api/tracks/"))) ||
		(strings.HasSuffix(p, "/image") && strings.HasPrefix(p, "/api/artists/")) ||
		(strings.HasSuffix(p, "/avatar") && strings.HasPrefix(p, "/api/users/"))
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
			if tok == "" {
				tok = r.URL.Query().Get("token") // app alias
			}
		}
		if tok == "" && d.Auth != nil && allowsQueryToken(r) && r.URL.Query().Get("sig") != "" {
			// signed media URL (path-scoped HMAC, expiring)
			if id, ok := d.Auth.VerifyURL(r.URL.Path, r.URL.Query()); ok {
				if dev, err := d.Auth.DeviceByID(r.Context(), id); err == nil {
					next.ServeHTTP(w, r.WithContext(withDevice(r.Context(), dev)))
					return
				}
			}
			writeErr(w, http.StatusUnauthorized, "invalid_signature", "signed URL is invalid or expired")
			return
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
		next.ServeHTTP(w, r.WithContext(withDevice(r.Context(), dev)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if h.Get("Content-Security-Policy") == "" {
			h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		}
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
	writeJSON(w, code, map[string]string{"error": kind, "code": kind, "message": msg})
}

func (d Deps) internal(w http.ResponseWriter, what string, err error) {
	d.Log.Error(what, "err", err)
	writeErr(w, http.StatusInternalServerError, "internal", what+" failed")
}
