package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/safehttp"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
)

// OnlineLibrary is the slice of *library.Scanner the online endpoints need.
type OnlineLibrary interface {
	LocalTrackCount(ctx contextT) (int, error)
	Setting(ctx contextT, key string) (string, error)
	SetSetting(ctx contextT, key, value string) error
}

const settingMode = "app.mode" // local | online | both

// upstreamHC fetches audio from platform CDNs (no overall timeout: streams are long).
var upstreamHC = &http.Client{
	Transport: safehttp.Transport(),
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return errors.New("bad redirect")
		}
		return nil
	},
}

func (d Deps) mountOnline(mux *http.ServeMux) {
	d.mountRecognize(mux)
	if d.Online == nil {
		return
	}
	mux.HandleFunc("GET /api/plugins", d.listPlugins)
	mux.HandleFunc("GET /api/plugins/{id}", d.getPlugin)
	mux.HandleFunc("PUT /api/plugins/{id}", d.adminOnly(d.putPlugin))
	mux.HandleFunc("POST /api/plugins/{id}/login", d.personalOnly(d.pluginLogin))
	mux.HandleFunc("POST /api/plugins/{id}/login/qr", d.personalOnly(d.pluginQR))
	mux.HandleFunc("POST /api/plugins/{id}/login/sms", d.personalOnly(d.pluginSMS))
	mux.HandleFunc("GET /api/plugins/{id}/login/qr/{key}", d.personalOnly(d.pluginQRPoll))
	mux.HandleFunc("POST /api/plugins/{id}/refresh", d.personalOnly(d.pluginRefresh))
	mux.HandleFunc("DELETE /api/plugins/{id}/session", d.personalOnly(d.pluginLogout))
	mux.HandleFunc("GET /api/plugins/{id}/accounts", d.personalOnly(d.pluginAccounts))
	mux.HandleFunc("POST /api/plugins/{id}/accounts/{acct}/default", d.personalOnly(d.pluginAccountDefault))
	mux.HandleFunc("DELETE /api/plugins/{id}/accounts/{acct}", d.personalOnly(d.pluginAccountRemove))

	mux.HandleFunc("GET /api/home", d.home)
	mux.HandleFunc("GET /api/online/{origin}/search/hot", d.platformHotSearch)
	mux.HandleFunc("GET /api/online/{origin}/artists/{id}/albums", d.onlineArtistAlbums)
	mux.HandleFunc("GET /api/online/{origin}/{sub}", d.platformSub) // daily | fm
	mux.HandleFunc("POST /api/online/{origin}/fm/feedback", d.platformFMFeedback)
	mux.HandleFunc("GET /api/online/{origin}/recommend/playlists", d.platformRecPlaylists)
	mux.HandleFunc("GET /api/online/{origin}/user/playlists", d.platformUserPlaylists)
	mux.HandleFunc("GET /api/charts", d.charts)
	mux.HandleFunc("GET /api/online/resolve/{id}", d.onlineResolve)
	mux.HandleFunc("GET /api/online/favorites", d.cloudFavorites)
	mux.HandleFunc("GET /api/online/{origin}/{kind}/{id}", d.onlineItem)
	mux.HandleFunc("POST /api/online/{origin}/playlists/{id}/clone", d.clonePlaylist)
}

func (d Deps) onlineErr(w http.ResponseWriter, what string, err error) {
	if status, code, msg, ok := isolationError(err); ok {
		writeErr(w, status, code, msg)
		return
	}
	switch {
	case errors.Is(err, online.ErrUnknown):
		writeErr(w, http.StatusNotFound, "unknown_plugin", err.Error())
	case errors.Is(err, online.ErrDisabled):
		writeErr(w, http.StatusConflict, "plugin_disabled", err.Error())
	case errors.Is(err, online.ErrNeedsLogin):
		writeErr(w, http.StatusUnauthorized, "plugin_login_required", err.Error())
	case errors.Is(err, online.ErrNotSupported):
		writeErr(w, http.StatusNotImplemented, "not_supported", err.Error())
	case errors.Is(err, online.ErrUnavailable):
		writeErr(w, http.StatusNotFound, "unavailable", err.Error())
	case errors.Is(err, online.ErrRiskControl):
		writeErr(w, http.StatusTooManyRequests, "risk_control", err.Error())
	case errors.Is(err, online.ErrUpstream):
		writeErr(w, http.StatusBadGateway, "upstream", err.Error())
	case errors.Is(err, online.ErrBadInput):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		d.internal(w, what, err)
	}
}

// ---- plugin management -------------------------------------------------

func (d Deps) listPlugins(w http.ResponseWriter, r *http.Request) {
	items := d.onlineFor(r.Context()).Reg.List(r.Context(), r.URL.Query().Get("check") == "1")
	for i := range items {
		if dev, ok := DeviceFrom(r.Context()); d.Accounts != nil && ok && dev.Admin && len(items[i].Manifest.Capabilities) == 0 {
			if global, err := d.Online.Reg.Status(r.Context(), items[i].Manifest.ID); err == nil {
				items[i] = global
			}
		}
		d.decorateStatus(&items[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// decorateStatus adds Core-cached avatar previews so clients never load
// platform CDNs directly (D60).
func (d Deps) decorateStatus(st *online.Status) {
	if st == nil {
		return
	}
	if st.Account != nil {
		st.Account.AvatarPreview = d.imageURL(st.Account.Avatar)
	}
	for i := range st.Accounts {
		st.Accounts[i].AvatarPreview = d.imageURL(st.Accounts[i].Avatar)
	}
}

func (d Deps) getPlugin(w http.ResponseWriter, r *http.Request) {
	st, err := d.onlineFor(r.Context()).Reg.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		d.onlineErr(w, "plugin", err)
		return
	}
	d.decorateStatus(&st)
	writeJSON(w, http.StatusOK, st)
}

func (d Deps) putPlugin(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled *bool          `json:"enabled"`
		Config  map[string]any `json:"config"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	st, err := d.Online.Reg.Configure(r.Context(), r.PathValue("id"), b.Enabled, b.Config)
	if err != nil {
		if errors.Is(err, online.ErrUnknown) {
			d.onlineErr(w, "plugin", err)
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	if d.Accounts != nil {
		if err := d.Accounts.ConfigChanged(); err != nil {
			d.internal(w, "platform config", err)
			return
		}
		st.Account = nil
		st.Accounts = nil
		st.ActiveAccount = ""
		st.LoggedIn = false
		st.LoginMethod = ""
		st.LastError = ""
	}
	d.Hub.Publish("plugin.updated", map[string]any{"id": r.PathValue("id")})
	writeJSON(w, http.StatusOK, st)
}

func (d Deps) pluginLogin(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Method   string `json:"method"` // cookie | password
		Cookie   string `json:"cookie"`
		Username string `json:"username"`
		Password string `json:"password"`
		Phone    string `json:"phone"`
		Code     string `json:"code"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	id := r.PathValue("id")
	var err error
	switch b.Method {
	case "cookie":
		if strings.TrimSpace(b.Cookie) == "" {
			writeErr(w, http.StatusBadRequest, "invalid_body", "cookie required")
			return
		}
		_, err = d.onlineFor(r.Context()).Reg.LoginCookie(r.Context(), id, b.Cookie)
	case "password":
		if b.Username == "" || b.Password == "" {
			writeErr(w, http.StatusBadRequest, "invalid_body", "username and password required")
			return
		}
		_, err = d.onlineFor(r.Context()).Reg.LoginPassword(r.Context(), id, b.Username, b.Password)
	case "sms":
		if b.Phone == "" || b.Code == "" {
			writeErr(w, http.StatusBadRequest, "invalid_body", "phone and code required")
			return
		}
		_, err = d.onlineFor(r.Context()).Reg.LoginSMS(r.Context(), id, b.Phone, b.Code)
	default:
		writeErr(w, http.StatusBadRequest, "invalid_body", "method must be cookie, password or sms (QR: POST …/login/qr)")
		return
	}
	if err != nil {
		d.onlineErr(w, "plugin login", err)
		return
	}
	d.pluginDone(w, r, id)
}

func (d Deps) pluginSMS(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Phone string `json:"phone"`
	}
	if err := decodeBody(r, &b); err != nil || strings.TrimSpace(b.Phone) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_body", "phone required")
		return
	}
	if err := d.onlineFor(r.Context()).Reg.SendSMS(r.Context(), r.PathValue("id"), strings.TrimSpace(b.Phone)); err != nil {
		d.onlineErr(w, "plugin sms", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

func (d Deps) pluginDone(w http.ResponseWriter, r *http.Request, id string) {
	st, err := d.onlineFor(r.Context()).Reg.Status(r.Context(), id)
	if err != nil {
		d.onlineErr(w, "plugin", err)
		return
	}
	d.pluginEvent(r.Context(), id)
	writeJSON(w, http.StatusOK, st)
}

func (d Deps) pluginQR(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Channel string `json:"channel"`
	}
	if r.ContentLength > 0 {
		if err := decodeBody(r, &b); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
	}
	if b.Channel != "" {
		st, err := d.onlineFor(r.Context()).Reg.Status(r.Context(), r.PathValue("id"))
		if err != nil {
			d.onlineErr(w, "qr", err)
			return
		}
		known := false
		for _, c := range st.Manifest.QRChannels {
			known = known || c.ID == b.Channel
		}
		if !known {
			writeErr(w, http.StatusBadRequest, "unsupported_channel", "this plugin has no QR channel "+b.Channel)
			return
		}
	}
	ch, err := d.onlineFor(r.Context()).Reg.StartQR(r.Context(), r.PathValue("id"))
	if err != nil {
		d.onlineErr(w, "qr", err)
		return
	}
	if d.Accounts != nil {
		d.Accounts.RememberQR(owner.From(r.Context()).ID, r.PathValue("id"), ch.Key, ch.ExpiresAt)
	}
	writeJSON(w, http.StatusOK, ch)
}

func (d Deps) pluginQRPoll(w http.ResponseWriter, r *http.Request) {
	if d.Accounts != nil && !d.Accounts.OwnQR(owner.From(r.Context()).ID, r.PathValue("id"), r.PathValue("key")) {
		writeErr(w, 404, "qr_not_found", "QR challenge not found or expired")
		return
	}
	p, err := d.onlineFor(r.Context()).Reg.PollQR(r.Context(), r.PathValue("id"), r.PathValue("key"))
	if err != nil {
		d.onlineErr(w, "qr poll", err)
		return
	}
	if p.Status == online.QRConfirmed {
		d.pluginEvent(r.Context(), r.PathValue("id"))
	}
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) pluginRefresh(w http.ResponseWriter, r *http.Request) {
	st, err := d.onlineFor(r.Context()).Reg.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		d.onlineErr(w, "refresh", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (d Deps) pluginLogout(w http.ResponseWriter, r *http.Request) {
	if err := d.onlineFor(r.Context()).Reg.Logout(r.Context(), r.PathValue("id")); err != nil {
		d.onlineErr(w, "logout", err)
		return
	}
	d.pluginEvent(r.Context(), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) pluginAccounts(w http.ResponseWriter, r *http.Request) {
	accs, err := d.onlineFor(r.Context()).Reg.Accounts(r.Context(), r.PathValue("id"))
	if err != nil {
		d.onlineErr(w, "accounts", err)
		return
	}
	for i := range accs {
		accs[i].AvatarPreview = d.imageURL(accs[i].Avatar)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": accs})
}

func (d Deps) accountErr(w http.ResponseWriter, op string, err error) {
	if errors.Is(err, online.ErrUnknownAccount) {
		writeErr(w, http.StatusNotFound, "unknown_account", err.Error())
		return
	}
	d.onlineErr(w, op, err)
}

func (d Deps) pluginAccountDefault(w http.ResponseWriter, r *http.Request) {
	st, err := d.onlineFor(r.Context()).Reg.SwitchAccount(r.Context(), r.PathValue("id"), r.PathValue("acct"))
	if err != nil {
		d.accountErr(w, "switch account", err)
		return
	}
	d.onlineFor(r.Context()).FlushPlugin(r.PathValue("id"))
	d.pluginEvent(r.Context(), r.PathValue("id"))
	writeJSON(w, http.StatusOK, st)
}

func (d Deps) pluginAccountRemove(w http.ResponseWriter, r *http.Request) {
	st, err := d.onlineFor(r.Context()).Reg.RemoveAccount(r.Context(), r.PathValue("id"), r.PathValue("acct"))
	if err != nil {
		d.accountErr(w, "remove account", err)
		return
	}
	d.onlineFor(r.Context()).FlushPlugin(r.PathValue("id"))
	d.pluginEvent(r.Context(), r.PathValue("id"))
	writeJSON(w, http.StatusOK, st)
}

// ---- mode ---------------------------------------------------------------

func (d Deps) getMode(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"mode": "", "local_tracks": 0, "online_enabled": []string{}}
	if d.OnlineLib != nil {
		m, err := d.OnlineLib.Setting(r.Context(), settingMode)
		if err != nil {
			d.internal(w, "mode", err)
			return
		}
		n, _ := d.OnlineLib.LocalTrackCount(r.Context())
		out["mode"], out["local_tracks"] = m, n
	}
	if d.onlineFor(r.Context()) != nil {
		if ids := d.onlineFor(r.Context()).Reg.EnabledIDs(); ids != nil {
			out["online_enabled"] = ids
		}
	}
	out["configured"] = out["mode"] != ""
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) putMode(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Mode string `json:"mode"`
	}
	if err := decodeBody(r, &b); err != nil || (b.Mode != "local" && b.Mode != "online" && b.Mode != "both") {
		writeErr(w, http.StatusBadRequest, "invalid_body", "mode must be local, online or both")
		return
	}
	if d.OnlineLib == nil {
		writeErr(w, http.StatusNotImplemented, "not_supported", "settings unavailable")
		return
	}
	if err := d.OnlineLib.SetSetting(r.Context(), settingMode, b.Mode); err != nil {
		d.internal(w, "mode", err)
		return
	}
	d.getMode(w, r)
}

// ---- discovery ----------------------------------------------------------

type homeSection struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Origin    string            `json:"origin"`
	Kind      string            `json:"kind"` // tracks | playlists
	Tracks    []online.Track    `json:"tracks,omitempty"`
	Playlists []online.Playlist `json:"playlists,omitempty"`
	Error     string            `json:"error,omitempty"`
}

func errCode(err error) string {
	switch {
	case errors.Is(err, online.ErrNeedsLogin):
		return "plugin_login_required"
	case errors.Is(err, online.ErrRiskControl):
		return "risk_control"
	case errors.Is(err, online.ErrNotSupported):
		return "not_supported"
	}
	return "upstream"
}

// home builds per-platform sections in parallel. Failing sections carry an
// error code instead of failing the whole page.
func (d Deps) home(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	var secs []homeSection
	order := map[string]int{}
	add := func(s homeSection, rank int) {
		mu.Lock()
		order[s.ID] = rank
		secs = append(secs, s)
		mu.Unlock()
	}
	for i, id := range d.onlineFor(r.Context()).Reg.EnabledIDs() {
		st, _ := d.onlineFor(r.Context()).Reg.Status(ctx, id)
		name := st.Manifest.Name
		logged := d.onlineFor(r.Context()).Reg.LoggedIn(id)
		has := func(c online.Capability) bool {
			for _, x := range st.Manifest.Capabilities {
				if x == c {
					return true
				}
			}
			return false
		}
		base := i * 10
		run := func(rank int, s homeSection, f func(*homeSection) error) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := f(&s); err != nil {
					s.Error = errCode(err)
					if errors.Is(err, online.ErrNeedsLogin) {
						return // hide login-only sections silently
					}
				}
				add(s, rank)
			}()
		}
		if logged && has(online.CapRecommend) {
			run(base+1, homeSection{ID: id + ".daily", Title: name + " · 每日推荐", Origin: id, Kind: "tracks"}, func(s *homeSection) (err error) {
				s.Tracks, err = d.onlineFor(r.Context()).RecommendTracks(ctx, id)
				return
			})
		}
		if logged && has(online.CapUserPlaylists) {
			run(base+2, homeSection{ID: id + ".mine", Title: name + " · 我的歌单", Origin: id, Kind: "playlists"}, func(s *homeSection) (err error) {
				s.Playlists, err = d.onlineFor(r.Context()).UserPlaylists(ctx, id)
				return
			})
		}
		if has(online.CapRecommend) {
			run(base+3, homeSection{ID: id + ".playlists", Title: name + " · 推荐歌单", Origin: id, Kind: "playlists"}, func(s *homeSection) (err error) {
				s.Playlists, err = d.onlineFor(r.Context()).RecommendPlaylists(ctx, id)
				return
			})
		}
		if has(online.CapCharts) {
			run(base+4, homeSection{ID: id + ".charts", Title: name + " · 排行榜", Origin: id, Kind: "playlists"}, func(s *homeSection) (err error) {
				s.Playlists, err = d.onlineFor(r.Context()).Charts(ctx, id)
				if len(s.Playlists) > 8 {
					s.Playlists = s.Playlists[:8]
				}
				return
			})
		}
	}
	wg.Wait()
	sortSections(secs, order)
	for i := range secs {
		d.decorateOnlineTracks(secs[i].Tracks)
		d.decorateOnlinePlaylists(secs[i].Playlists)
	}
	out := map[string]any{"sections": secs}
	if d.OnlineLib != nil {
		n, _ := d.OnlineLib.LocalTrackCount(r.Context())
		m, _ := d.OnlineLib.Setting(r.Context(), settingMode)
		out["local_tracks"], out["mode"] = n, m
	}
	if secs == nil {
		out["sections"] = []homeSection{}
	}
	for k, v := range d.homeLibrary(r.Context()) {
		out[k] = v
	}
	b, err := json.Marshal(out)
	if err != nil {
		d.internal(w, "home", err)
		return
	}
	writeETagged(w, r, b)
}

func sortSections(s []homeSection, order map[string]int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && order[s[j].ID] < order[s[j-1].ID]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func (d Deps) charts(w http.ResponseWriter, r *http.Request) {
	origins := d.onlineFor(r.Context()).Reg.EnabledIDs()
	if o := r.URL.Query().Get("origin"); o != "" {
		origins = []string{o}
	}
	type group struct {
		Origin string            `json:"origin"`
		Items  []online.Playlist `json:"items"`
		Error  string            `json:"error,omitempty"`
	}
	out := make([]group, len(origins))
	var wg sync.WaitGroup
	for i, o := range origins {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := d.onlineFor(r.Context()).Charts(r.Context(), o)
			out[i] = group{Origin: o, Items: items}
			if err != nil {
				out[i].Error = errCode(err)
				if errors.Is(err, online.ErrUnknown) || errors.Is(err, online.ErrDisabled) {
					out[i].Error = "plugin_disabled"
				}
			}
		}()
	}
	wg.Wait()
	resp := map[string]any{"items": out, "origins": d.onlineFor(r.Context()).Reg.EnabledIDs()}
	// App shape: ?origin=x → "charts" is that platform's list.
	charts := []online.Playlist{}
	for i := range out {
		d.decorateOnlinePlaylists(out[i].Items)
		if len(origins) == 1 {
			charts = out[i].Items
		}
	}
	resp["charts"] = charts
	writeJSON(w, http.StatusOK, resp)
}

// listPlaylistScope serves /api/playlists?scope=mine|recommend|discover:
// external playlists grouped by platform ("local" scope = classic list).
func (d Deps) listPlaylistScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if scope == "" || scope == "local" || (d.onlineFor(r.Context()) == nil && scope != "all") {
		return false
	}
	if d.onlineFor(r.Context()) == nil { // all = local only
		return false
	}
	platform := scope
	if scope == "all" || scope == "online" {
		platform = "mine"
	}
	q := r.URL.Query()
	origins := d.onlineFor(r.Context()).Reg.EnabledIDs()
	if o := q.Get("origin"); o != "" {
		origins = []string{o}
	}
	type group struct {
		Origin string            `json:"origin"`
		Items  []online.Playlist `json:"items"`
		Error  string            `json:"error,omitempty"`
	}
	out := make([]group, len(origins))
	var wg sync.WaitGroup
	for i, o := range origins {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var items []online.Playlist
			var err error
			switch platform {
			case "mine":
				items, err = d.onlineFor(r.Context()).UserPlaylists(r.Context(), o)
			case "recommend":
				items, err = d.onlineFor(r.Context()).RecommendPlaylists(r.Context(), o)
			case "discover":
				items, err = d.onlineFor(r.Context()).DiscoverPlaylists(r.Context(), o, q.Get("category"), intQ(r, "offset"), intQ(r, "limit"))
			case "charts":
				items, err = d.onlineFor(r.Context()).Charts(r.Context(), o)
			default:
				err = online.ErrBadInput
			}
			out[i] = group{Origin: o, Items: items}
			if err != nil {
				out[i].Error = errCode(err)
			}
		}()
	}
	wg.Wait()
	switch scope {
	case "all", "online", "mine", "recommend", "discover", "charts":
	default:
		writeErr(w, http.StatusBadRequest, "invalid_query", "scope must be all, local, online, mine, recommend, discover or charts")
		return true
	}
	// flat items too: scope=all puts the local playlists first
	var flat []any
	if scope == "all" && d.Playlists != nil {
		if l, err := d.Playlists.ListPlaylists(r.Context()); err == nil {
			d.decoratePlaylists(r, l)
			for _, p := range l {
				flat = append(flat, p)
			}
		}
	}
	for _, g := range out {
		d.decorateOnlinePlaylists(g.Items)
		for _, p := range g.Items {
			flat = append(flat, p)
		}
	}
	if flat == nil {
		flat = []any{}
	}
	page, total, off, next := pageSlice(r, flat)
	writeJSON(w, http.StatusOK, map[string]any{"scope": scope, "groups": out, "items": page, "total": total, "offset": off,
		"cursor": encodeCursor(off, listFingerprint(r)), "next_cursor": next})
	return true
}

// ---- items ----------------------------------------------------------------

func (d Deps) onlineItem(w http.ResponseWriter, r *http.Request) {
	id := online.FullID(r.PathValue("origin"), r.PathValue("id"))
	ctx := r.Context()
	var v any
	var err error
	switch r.PathValue("kind") {
	case "tracks":
		v, err = d.onlineFor(r.Context()).Track(ctx, id)
	case "albums":
		var a online.Album
		a, err = d.onlineFor(r.Context()).Album(ctx, id)
		a.Cover = d.onlineCover(a.CoverURL)
		d.decorateOnlineTracks(a.Tracks)
		v = a
	case "artists":
		var a online.Artist
		a, err = d.onlineFor(r.Context()).Artist(ctx, id)
		as := []online.Artist{a}
		d.decorateOnlineArtists(as)
		d.decorateOnlineTracks(as[0].TopTracks)
		v = as[0]
	case "playlists":
		var p online.Playlist
		p, err = d.onlineFor(r.Context()).Playlist(ctx, id)
		if err == nil {
			d.decorateOnlinePlaylist(&p)
			v = playlistView(p)
		}
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown kind")
		return
	}
	if err != nil {
		d.onlineErr(w, "online item", err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// playlistView adds per-status counts so clients can show "n playable / m preview".
func playlistView(p online.Playlist) map[string]any {
	counts := map[string]int{"full": 0, "preview": 0, "unavailable": 0, "local": 0}
	for _, t := range p.Tracks {
		if t.LocalID != "" {
			counts["local"]++
		}
		switch t.Playable {
		case online.EntPreview:
			counts["preview"]++
		case online.EntUnavailable:
			counts["unavailable"]++
		default:
			counts["full"]++
		}
	}
	return map[string]any{"playlist": p, "counts": counts}
}

func (d Deps) clonePlaylist(w http.ResponseWriter, r *http.Request) {
	if d.Playlists == nil {
		writeErr(w, http.StatusNotImplemented, "not_supported", "playlists unavailable")
		return
	}
	var b struct {
		Name        string `json:"name"`
		PreferLocal *bool  `json:"prefer_local"` // default true
		SkipUnavail bool   `json:"skip_unavailable"`
		Into        string `json:"into"` // append to this 译梦 playlist instead of creating one
	}
	if r.ContentLength != 0 {
		if err := decodeBody(r, &b); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
	}
	p, err := d.onlineFor(r.Context()).Playlist(r.Context(), online.FullID(r.PathValue("origin"), r.PathValue("id")))
	if err != nil {
		d.onlineErr(w, "clone", err)
		return
	}
	prefer := b.PreferLocal == nil || *b.PreferLocal
	// Existing tracks of the target (owner-checked by GetPlaylist) seed the
	// de-duplication set, so re-importing into the same playlist is a no-op.
	var seen dedupSet
	if b.Into != "" {
		for off := 0; ; off += 500 {
			pg, err := d.Playlists.GetPlaylist(r.Context(), b.Into, 500, off)
			if err != nil {
				d.plErr(w, err)
				return
			}
			for _, t := range pg.Tracks.Items {
				seen.add(t.ID, t.ISRC, t.Title, t.ArtistName, t.DurationMs)
			}
			if len(pg.Tracks.Items) < 500 {
				break
			}
		}
	}
	ids := make([]string, 0, len(p.Tracks))
	linked, skipped, dups, unavailable := 0, 0, 0, 0
	for _, t := range p.Tracks {
		id := t.ID
		if prefer && t.LocalID != "" {
			id = t.LocalID
		} else if b.SkipUnavail && t.Playable == online.EntUnavailable {
			skipped++
			continue
		}
		if seen.has(id, t.LocalID, t.ISRC, t.Title, t.ArtistName, t.DurationMs) {
			dups++
			continue
		}
		seen.add(id, t.ISRC, t.Title, t.ArtistName, t.DurationMs)
		if t.LocalID != "" {
			seen.add(t.LocalID, "", "", "", 0)
		}
		ids = append(ids, id)
		if id == t.LocalID {
			linked++
		} else if t.Playable == online.EntUnavailable {
			unavailable++
		}
	}
	var pl library.Playlist
	status := http.StatusCreated
	if b.Into != "" { // merge into an existing playlist (owner-checked)
		if len(ids) == 0 {
			var pg library.PlaylistPage
			pg, err = d.Playlists.GetPlaylist(r.Context(), b.Into, 1, 0)
			pl = pg.Playlist
		} else {
			pl, err = d.Playlists.EditPlaylist(r.Context(), b.Into, library.PlaylistOp{Op: "append", TrackIDs: ids})
		}
		status = http.StatusOK
	} else {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			name = p.Name
		}
		comment := "从 " + p.Origin + " 克隆 · " + p.ID
		pl, err = d.Playlists.CreatePlaylist(r.Context(), library.PlaylistInput{Name: &name, Comment: &comment, TrackIDs: ids})
	}
	if err != nil {
		d.plErr(w, err)
		return
	}
	d.Hub.Publish("playlist.updated", map[string]string{"id": pl.ID})
	writeJSON(w, status, map[string]any{"playlist": pl, "tracks": len(ids), "added": len(ids), "skipped_duplicates": dups,
		"linked_local": linked, "skipped": skipped, "unavailable": unavailable})
}

// cloudFavorites returns the platform "liked songs" playlist of each logged-in account.
func (d Deps) cloudFavorites(w http.ResponseWriter, r *http.Request) {
	origins := d.onlineFor(r.Context()).Reg.EnabledIDs()
	if o := r.URL.Query().Get("origin"); o != "" {
		origins = []string{o}
	}
	type group struct {
		Origin   string           `json:"origin"`
		Playlist *online.Playlist `json:"playlist,omitempty"`
		Error    string           `json:"error,omitempty"`
	}
	var out []group
	for _, o := range origins {
		g := group{Origin: o}
		if !d.onlineFor(r.Context()).Reg.LoggedIn(o) {
			g.Error = "plugin_login_required"
			out = append(out, g)
			continue
		}
		pls, err := d.onlineFor(r.Context()).UserPlaylists(r.Context(), o)
		if err != nil {
			g.Error = errCode(err)
			out = append(out, g)
			continue
		}
		for _, p := range pls {
			if p.Kind == "liked" {
				full, err := d.onlineFor(r.Context()).Playlist(r.Context(), p.ID)
				if err != nil {
					g.Error = errCode(err)
				} else {
					full.Kind = "liked"
					d.decorateOnlinePlaylist(&full)
					g.Playlist = &full
				}
				break
			}
		}
		if g.Playlist == nil && g.Error == "" {
			g.Error = "not_supported"
		}
		out = append(out, g)
	}
	if out == nil {
		out = []group{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ---- playback ---------------------------------------------------------------


// canDirectCDN reports whether the client can GET the resolved CDN URL without
// Core injecting headers (NetEase-style tokens-in-query). Non-empty Header means
// proxy (QQ/Kugou Referer/UA). http and https both qualify — NetEase often
// returns http://m*.music.126.net links that work with a bare GET.
func canDirectCDN(res online.Resolution) bool {
	if res.URL == "" || len(res.Header) != 0 {
		return false
	}
	u, err := url.Parse(res.URL)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func qualityQ(r *http.Request) online.Quality {
	switch q := online.Quality(r.URL.Query().Get("quality")); q {
	case online.QStandard, online.QHigh, online.QLossless, online.QHiRes:
		return q
	}
	return ""
}

// onlineResolve tells clients what /api/stream/{id} will play (entitlement,
// preview window, fallback). stream_url is always the Core path; play_url is the
// absolute CDN URL when Header is empty so clients can play App-direct.
func (d Deps) onlineResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !library.IsExternalID(id) {
		writeJSON(w, http.StatusOK, map[string]any{"track_id": id, "play_id": id, "origin": "local", "local": true, "entitlement": "full", "stream_url": "/api/stream/" + url.PathEscape(id)})
		return
	}
	res, err := d.onlineFor(r.Context()).Resolve(d.switchCtx(r.Context()), id, qualityQ(r), r.URL.Query().Get("fresh") == "1")
	if err != nil {
		d.onlineErr(w, "resolve", err)
		return
	}
	out := struct {
		online.Resolution
		StreamURL string `json:"stream_url"`
		PlayURL   string `json:"play_url,omitempty"`
	}{Resolution: res, StreamURL: "/api/stream/" + url.PathEscape(id)}
	if canDirectCDN(res) {
		out.PlayURL = res.URL
	}
	writeJSON(w, http.StatusOK, out)
}

// streamExternal serves an online track. Default is App-direct: 302 to the CDN
// when Resolve needs no injected headers. Proxy remains for sources that require
// Referer/UA/cookies, for transcode, and for /download. Cookies never leave Core.
func (d Deps) streamExternal(w http.ResponseWriter, r *http.Request, id string) bool {
	if d.onlineFor(r.Context()) == nil || !library.IsExternalID(id) {
		return false
	}
	q := qualityQ(r)
	for attempt := 0; attempt < 2; attempt++ {
		res, err := d.onlineFor(r.Context()).Resolve(d.switchCtx(r.Context()), id, q, attempt > 0)
		if err != nil {
			d.onlineErr(w, "resolve", err)
			return true
		}
		if res.Local {
			r.SetPathValue("id", res.PlayID)
			w.Header().Set("X-Yimeng-Play-Id", res.PlayID)
			w.Header().Set("X-Yimeng-Entitlement", "full")
			return false // fall through to local streaming
		}
		if res.Entitlement == online.EntPreview && r.Header.Get("X-Yimeng-Full-Only") == "1" {
			// Subsonic clients can't render a preview window: refuse honestly (D59).
			writeErr(w, http.StatusForbidden, "preview_only", "only a preview of this track is available")
			return true
		}
		if res.Entitlement == online.EntUnavailable || res.URL == "" {
			msg := res.Reason
			if msg == "" {
				msg = "当前账号无权播放此曲目"
			}
			writeErr(w, http.StatusForbidden, "not_entitled", msg)
			return true
		}
		u, err := url.Parse(res.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			writeErr(w, http.StatusBadGateway, "upstream", "invalid upstream url")
			return true
		}
		// Optional transcode (explicit ?format= or the device default); the
		// original passthrough stays the default.
		prof := d.onlineProfile(r, res.Format)
		method := r.Method
		if !prof.IsOriginal() {
			method = http.MethodGet
		}
		if d.Accounts != nil {
			ctx, closeStream, err := d.Accounts.Guard(r.Context(), res)
			if err != nil {
				d.onlineErr(w, "stream grant", err)
				return true
			}
			defer closeStream()
			r = r.WithContext(ctx)
		}
		// App-direct CDN: bare GET works → 302 so audio bytes skip the VPS.
		if canDirectCDN(res) && prof.IsOriginal() && !strings.HasSuffix(r.URL.Path, "/download") {
			h := w.Header()
			d.setOnlineHeaders(h, res)
			h.Set("X-Stream-Profile", transcode.Original)
			http.Redirect(w, r, res.URL, http.StatusFound)
			return true
		}
		resp, status := d.fetchUpstream(r, method, res, prof.IsOriginal())
		if resp == nil {
			if status == http.StatusForbidden && attempt == 0 {
				d.onlineFor(r.Context()).Invalidate(id)
				continue // URL expired / host refused: re-resolve once
			}
			writeErr(w, http.StatusBadGateway, "upstream_rejected", fmt.Sprintf("音源服务器拒绝了请求（HTTP %d）。可能是登录失效或该曲目暂不可播放，请稍后重试或重新登录平台账号。", status))
			return true
		}
		defer resp.Body.Close()
		h := w.Header()
		if !prof.IsOriginal() {
			d.pipeExternal(w, r, resp.Body, prof, res)
			return true
		}
		h.Set("X-Stream-Profile", transcode.Original)
		setAudioHeaders(h, formatCodec[res.Format], 0, 0, 0, res.BitrateKbps)
		for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
			if v := resp.Header.Get(k); v != "" {
				h.Set(k, v)
			}
		}
		if h.Get("Content-Type") == "" || strings.HasPrefix(h.Get("Content-Type"), "application/octet") {
			if ct := audioTypes[res.Format]; ct != "" {
				h.Set("Content-Type", ct)
			}
		}
		h.Set("Cache-Control", "private, no-store")
		if strings.HasSuffix(r.URL.Path, "/download") {
			h.Set("Content-Disposition", contentDisposition(downloadFilename(library.Track{ID: id}, res.Format)))
		}
		h.Set("X-Yimeng-Entitlement", string(res.Entitlement))
		h.Set("X-Yimeng-Play-Id", res.PlayID)
		if res.Fallback != "" {
			h.Set("X-Yimeng-Fallback", res.Fallback)
		}
		if res.Entitlement == online.EntPreview {
			h.Set("X-Yimeng-Preview-Start-Ms", strconv.FormatInt(res.PreviewStartMs, 10))
			h.Set("X-Yimeng-Preview-End-Ms", strconv.FormatInt(res.PreviewEndMs, 10))
		}
		w.WriteHeader(resp.StatusCode)
		if r.Method != http.MethodHead {
			if _, err := io.Copy(w, resp.Body); err != nil {
				panic(http.ErrAbortHandler)
			}
		}
		return true
	}
	writeErr(w, http.StatusBadGateway, "upstream", "upstream url expired")
	return true
}

// fetchUpstream GETs the resolved URL, then each alternate host, and
// returns the first response that is actually audio (CDNs sometimes answer
// 200 with an HTML "Forbidden" page). On failure it returns nil and the
// last upstream status (403 for HTML/JSON error pages).
func (d Deps) fetchUpstream(r *http.Request, method string, res online.Resolution, passRange bool) (*http.Response, int) {
	status := http.StatusBadGateway
	for _, raw := range append([]string{res.URL}, res.Alternates...) {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		req, _ := http.NewRequestWithContext(r.Context(), method, raw, nil)
		for k, v := range res.Header {
			req.Header.Set(k, v)
		}
		if rg := r.Header.Get("Range"); rg != "" && passRange {
			req.Header.Set("Range", rg)
		}
		resp, err := upstreamHC.Do(req)
		if err != nil {
			if r.Context().Err() != nil {
				return nil, status
			}
			continue
		}
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if resp.StatusCode < 400 && !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "json") {
			if cr := resp.Header.Get("Content-Range"); method == http.MethodHead || (cr != "" && !strings.HasPrefix(cr, "bytes 0-")) {
				return resp, resp.StatusCode
			}
			// some CDNs label an HTML error page audio/*: sniff the body
			br := bufio.NewReaderSize(resp.Body, 512)
			head, _ := br.Peek(64)
			if !looksLikeErrorPage(head) {
				resp.Body = struct {
					io.Reader
					io.Closer
				}{br, resp.Body}
				return resp, resp.StatusCode
			}
		}
		status = resp.StatusCode
		if status < 400 {
			status = http.StatusForbidden // error page served as 200
		}
		// QQ's primary CDN hosts answer 200 with an empty body and no
		// Content-Type for some files; the next host serves the audio.
		peek := make([]byte, 64)
		n, _ := io.ReadFull(resp.Body, peek)
		resp.Body.Close()
		d.Log.Info("online upstream skipped, trying next host", "origin", res.Origin, "host", u.Host, "status", resp.StatusCode,
			"content_type", ct, "body_bytes", n)
	}
	return nil, status
}

// looksLikeErrorPage reports whether a supposed audio body starts like an
// HTML/XML/JSON document.
func looksLikeErrorPage(b []byte) bool {
	t := strings.ToLower(strings.TrimLeft(string(b), " \t\r\n\ufeff"))
	for _, p := range []string{"<html", "<!doctype", "<?xml", "<head", "<body", "{", "["} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// onlineProfile picks the stream profile for an online track: explicit
// query, else the device default; anything unsupported or pointless
// (lossless target from a lossy source, same codec) falls back to original.
func (d Deps) onlineProfile(r *http.Request, srcFormat string) transcode.Profile {
	orig := transcode.Profile{Format: transcode.Original}
	p, _, err := d.streamProfile(r)
	if err != nil || p.IsOriginal() || d.Transcoder == nil || !d.Transcoder.Supports(p) {
		return orig
	}
	src := strings.ToLower(srcFormat)
	if p.Lossless() && src != "flac" {
		return orig
	}
	if (p.Format == "mp3" && src == "mp3") || (p.Format == "aac" && (src == "aac" || src == "m4a")) {
		return orig // same codec: re-encoding only loses quality
	}
	return p
}

func (d Deps) setOnlineHeaders(h http.Header, res online.Resolution) {
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Yimeng-Entitlement", string(res.Entitlement))
	h.Set("X-Yimeng-Play-Id", res.PlayID)
	if res.Fallback != "" {
		h.Set("X-Yimeng-Fallback", res.Fallback)
	}
	if res.Entitlement == online.EntPreview {
		h.Set("X-Yimeng-Preview-Start-Ms", strconv.FormatInt(res.PreviewStartMs, 10))
		h.Set("X-Yimeng-Preview-End-Ms", strconv.FormatInt(res.PreviewEndMs, 10))
	}
}

// pipeExternal streams an ffmpeg transcode of the upstream body (no cache).
func (d Deps) pipeExternal(w http.ResponseWriter, r *http.Request, body io.Reader, p transcode.Profile, res online.Resolution) {
	h := w.Header()
	d.setOnlineHeaders(h, res)
	h.Set("Content-Type", p.ContentType())
	h.Set("Accept-Ranges", "none")
	h.Set("X-Stream-Profile", p.Key())
	setAudioHeaders(h, formatCodec[p.Format], 0, 0, 0, p.BitrateKbps)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	rc, err := d.Transcoder.Pipe(r.Context(), body, p)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "transcode_failed", "transcode could not be started")
		return
	}
	defer rc.Close()
	w.WriteHeader(http.StatusOK)
	rcw := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			rcw.Flush()
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			if r.Context().Err() == nil {
				d.Log.Warn("online transcode", "profile", p.Key(), "err", err)
			}
			panic(http.ErrAbortHandler)
		}
	}
}

func (d Deps) externalLyrics(w http.ResponseWriter, r *http.Request, id string) bool {
	if d.onlineFor(r.Context()) == nil || !library.IsExternalID(id) {
		return false
	}
	l, err := d.onlineFor(r.Context()).Lyrics(r.Context(), id)
	if err != nil {
		d.onlineErr(w, "lyrics", err)
		return true
	}
	if strings.TrimSpace(l.LRC) == "" {
		writeErr(w, http.StatusNotFound, "no_lyrics", "no lyrics for this track")
		return true
	}
	o, _, _ := library.SplitExternalID(id)
	writeJSON(w, http.StatusOK, struct {
		library.TrackLyrics
		Translation *lyrics.Lyrics `json:"translation,omitempty"`
	}{library.TrackLyrics{TrackID: id, Source: o, Lyrics: lyrics.Parse(l.LRC)}, translation(l.Translation)})
	return true
}

func translation(s string) *lyrics.Lyrics {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	l := lyrics.Parse(s)
	return &l
}

// ensureExternal materializes an online track before favorites/ratings.
func (d Deps) ensureExternal(r *http.Request) error {
	id := r.PathValue("id")
	if d.onlineFor(r.Context()) == nil || r.PathValue("type") != "track" || !library.IsExternalID(id) {
		return nil
	}
	return d.onlineFor(r.Context()).Materialize(r.Context(), []string{id})
}

// onlineSearch adds federated results when ?online=1.
func (d Deps) onlineSearch(r *http.Request, q string, limit int) []online.OriginResult {
	if d.onlineFor(r.Context()) == nil || r.URL.Query().Get("online") != "1" {
		return nil
	}
	var only []string
	if o := r.URL.Query().Get("origins"); o != "" {
		only = strings.Split(o, ",")
	}
	res := d.onlineFor(r.Context()).Search(r.Context(), q, limit, only)
	if res == nil {
		res = []online.OriginResult{}
	}
	for _, g := range res {
		if g.Result != nil {
			d.decorateOnlineResult(g.Result)
		}
	}
	return res
}

// dedupSet recognises the same song by track id, ISRC, or a strong
// title/artist/duration match (library.MatchScore ≥ 0.9, as in D50).
type dedupSet struct {
	ids   map[string]bool
	metas []library.MatchQuery
}

func (s *dedupSet) add(id, isrc, title, artist string, dur int64) {
	if s.ids == nil {
		s.ids = map[string]bool{}
	}
	if id != "" {
		s.ids[id] = true
	}
	if title != "" || isrc != "" {
		s.metas = append(s.metas, library.MatchQuery{ISRC: isrc, Title: title, Artist: artist, DurationMs: dur})
	}
}

func (s *dedupSet) has(id, localID, isrc, title, artist string, dur int64) bool {
	if s.ids[id] || (localID != "" && s.ids[localID]) {
		return true
	}
	if title == "" && isrc == "" {
		return false
	}
	for _, m := range s.metas {
		if sc, _ := library.MatchScore(m, isrc, title, artist, dur); sc >= 0.9 {
			return true
		}
	}
	return false
}
