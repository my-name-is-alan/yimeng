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
	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
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

	SplitConfig() library.SplitConfig
	SetSplitConfig(ctx context.Context, c library.SplitConfig) (library.SplitConfig, error)
}

type Deps struct {
	Library  LibraryUseCases
	Playback *playback.Orchestrator
	Hub      *events.Hub
	Auth     *auth.Service // required: every /api route except pairing needs a device token
	Log      *slog.Logger
}

func NewRouter(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	mux := http.NewServeMux()
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
	mux.HandleFunc("GET /api/stream/{id}", d.stream)

	mux.HandleFunc("GET /api/players", d.listPlayers)
	mux.HandleFunc("GET /api/players/{id}", d.playerState)
	mux.HandleFunc("GET /api/players/{id}/queue", d.playerQueue)
	for _, a := range []string{"play", "pause", "stop", "next", "prev", "seek", "repeat", "queue"} {
		mux.HandleFunc("POST /api/players/{id}/"+a, d.playerAction(a))
	}
	mux.HandleFunc("GET /ws", d.ws)

	mux.HandleFunc("GET /api/settings/artist-split", d.getSplit)
	mux.HandleFunc("PUT /api/settings/artist-split", d.putSplit)

	return securityHeaders(d.requireAuth(mux))
}

type ctxKey struct{}

func DeviceFrom(ctx context.Context) (auth.Device, bool) {
	dv, ok := ctx.Value(ctxKey{}).(auth.Device)
	return dv, ok
}

func isPublic(r *http.Request) bool {
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
	return p == "/ws" || strings.HasPrefix(p, "/api/stream/") || (strings.HasSuffix(p, "/cover") &&
		(strings.HasPrefix(p, "/api/albums/") || strings.HasPrefix(p, "/api/tracks/")))
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

// clientIP uses the socket peer only; X-Forwarded-For is not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
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
