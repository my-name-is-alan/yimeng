package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/adapters/musicbrainz"
	spotifyapi "github.com/my-name-is-alan/yimeng/core/internal/adapters/spotify"
	"github.com/my-name-is-alan/yimeng/core/internal/app"
	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/backup"
	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/discovery"
	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/gateway"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/config"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/logging"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/loudness"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/online/kugou"
	"github.com/my-name-is-alan/yimeng/core/internal/online/netease"
	"github.com/my-name-is-alan/yimeng/core/internal/online/qq"
	spotifyplugin "github.com/my-name-is-alan/yimeng/core/internal/online/spotify"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/playback/sources"
	"github.com/my-name-is-alan/yimeng/core/internal/runtimecfg"
	"github.com/my-name-is-alan/yimeng/core/internal/subsonic"
	"github.com/my-name-is-alan/yimeng/core/internal/system"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

var version = "dev"

var logRing *logging.Ring

func main() {
	boot := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		boot.Error("config", "err", err)
		os.Exit(1)
	}
	log, err := logging.New(os.Stderr, cfg.Log.Format, cfg.Log.Level)
	if err != nil {
		boot.Error("log", "err", err)
		os.Exit(1)
	}
	logRing = logging.NewRing(1000)
	log = slog.New(logRing.Wrap(log.Handler())) // web admin log viewer (secrets redacted)
	slog.SetDefault(log)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(cfg.Bind))
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		os.Exit(resetPassword(cfg, os.Args[2:]))
	}
	if err := run(cfg, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if ok, err := backup.ApplyPending(cfg.DBPath); err != nil {
		return fmt.Errorf("apply staged restore: %w", err)
	} else if ok {
		log.Warn("restored database from staged backup; previous database kept as *.pre-restore-*")
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	bk := &backup.Service{DB: database, DBPath: cfg.DBPath, Dir: cfg.Backup.Dir, KeyFile: cfg.MasterKeyFile,
		Keep: cfg.Backup.Keep, CoreVersion: version, Log: log}
	if pending, fresh, err := db.Pending(ctx, database, migrations.FS); err != nil {
		return err
	} else if len(pending) > 0 && !fresh {
		if _, err := bk.Create(ctx, fmt.Sprintf("pre-migrate-%d", pending[0])); err != nil {
			return fmt.Errorf("backup before migration: %w", err)
		}
	}
	applied, err := db.Migrate(ctx, database, migrations.FS)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	log.Info("database ready", "path", cfg.DBPath, "migrations_applied", applied)

	for _, r := range cfg.LibraryRoots {
		if st, err := os.Stat(r); err != nil || !st.IsDir() {
			log.Warn("library root not accessible", "root", r, "err", err)
		}
	}
	key, warn, err := vault.LoadOrCreateKey(cfg.MasterKeyFile, os.Getenv("YIMENG_VAULT_KEY"))
	if err != nil {
		return err
	}
	if warn != "" {
		log.Warn(warn)
	}
	vlt, err := vault.NewSQLStore(database, key)
	if err != nil {
		return err
	}
	clear(key)
	streamReg := playback.NewStreamRegistry()
	// NetEase (and later QQ/Kugou) are online plugins (internal/online); only
	// the Apple Music placeholder remains on the legacy StreamSource SPI.
	for _, id := range []string{"apple"} {
		srcID := id
		methods := []vault.LoginMethod{vault.LoginCookie, vault.LoginQRCode}
		if srcID == "apple" {
			methods = []vault.LoginMethod{vault.LoginOAuth}
		}
		streamReg.RegisterFactory(srcID, func(d playback.StreamDeps) (playback.StreamSource, error) {
			return sources.NewStub(srcID, methods, d.Credentials), nil
		})
		if _, err := streamReg.Enable(srcID, playback.StreamDeps{Credentials: vault.Scope(vlt, srcID)}); err != nil {
			return err
		}
	}
	log.Info("stream sources registered", "n", len(streamReg.All()))

	hub := events.NewHub()
	reg := metadata.NewRegistry()
	reg.RegisterFactory(metadata.MusicBrainz, func(d metadata.Deps) (metadata.MetadataProvider, error) {
		return musicbrainz.New(musicbrainz.Config{BaseURL: cfg.MusicBrainz.BaseURL, UserAgent: d.UserAgent})
	})
	// NetEase/QQ metadata remain unregistered: no safe public API (docs/decisions.md D12).
	// Runtime-editable settings (web console → settings table), applied live.
	rcStore := &runtimecfg.Store{DB: database}
	rcCur, err := rcStore.Load(ctx, runtimecfg.Settings{WatchMode: cfg.WatchMode, RescanInterval: cfg.RescanInterval.String(),
		MusicBrainzEnabled: cfg.MusicBrainz.Enabled || containsCI(cfg.Providers, "musicbrainz"), MusicBrainzContact: cfg.MusicBrainz.Contact, FillPerMinute: 20})
	if err != nil {
		return err
	}
	ua := fmt.Sprintf("yimeng/%s ( %s )", version, cfg.MusicBrainz.Contact)
	if rcCur.MusicBrainzContact != "" {
		ua = fmt.Sprintf("yimeng/%s ( %s )", version, rcCur.MusicBrainzContact)
	}
	var enrichFn atomic.Pointer[func()]
	enrich := func() {
		if f := enrichFn.Load(); f != nil {
			(*f)()
		}
	}
	var mbSup runtimecfg.Supervisor
	startMB := func(s runtimecfg.Settings) {
		if !s.MusicBrainzEnabled || s.MusicBrainzContact == "" {
			mbSup.Restart(ctx, nil)
			enrichFn.Store(nil)
			return
		}
		mb, err := musicbrainz.New(musicbrainz.Config{BaseURL: cfg.MusicBrainz.BaseURL, UserAgent: fmt.Sprintf("yimeng/%s ( %s )", version, s.MusicBrainzContact)})
		if err != nil {
			log.Warn("MusicBrainz enrichment unavailable", "err", err)
			return
		}
		en := &metadata.Enricher{DB: database, P: mb, Hub: hub, CoverDir: filepath.Join(cfg.CacheDir, "covers"), Log: log}
		mbSup.Restart(ctx, en.Run)
		kick := en.Kick
		enrichFn.Store(&kick)
		en.Kick()
		log.Info("MusicBrainz enrichment enabled")
	}
	if rcCur.MusicBrainzEnabled {
		if _, err := reg.Enable(metadata.MusicBrainz, metadata.Deps{UserAgent: ua}); err != nil {
			return err
		}
	}
	anonSrc := anonSources(log)
	var lyricChain providers.Chain
	for _, name := range cfg.Providers {
		switch strings.ToLower(name) {
		case "lrclib":
			lyricChain = append(lyricChain, &providers.LRCLIB{UA: ua})
		case "netease", "qq", "kugou":
			// anonymous public lyrics endpoints (no login, no account)
			if s := anonSrc[strings.ToLower(name)]; s != nil {
				lyricChain = append(lyricChain, anonLyrics{s})
			}
		case "musicbrainz":
			// handled above via Registry
		default:
			log.Warn("unknown provider ignored", "id", name)
		}
	}
	// On-demand (admin-initiated) lookups for the web metadata/lyrics editors; never run in the background.
	lrclibSearch := &providers.LRCLIB{UA: ua}
	// Matcher = MusicBrainz + Spotify (metadata-only, client credentials read
	// from the Vault via the "spotify" plugin settings; skipped until set).
	var pluginReg *online.Registry
	var mbAliases metadata.MetadataProvider
	if mb, err := musicbrainz.New(musicbrainz.Config{BaseURL: cfg.MusicBrainz.BaseURL, UserAgent: ua}); err == nil {
		mbAliases = mb
	}
	artistAliases := metadata.AliasLookup(mbAliases)
	spotifyMeta := spotifyapi.New(spotifyapi.Config{
		Aliases: artistAliases,
		APIBase: os.Getenv("YIMENG_SPOTIFY_API_BASE_URL"), AuthBase: os.Getenv("YIMENG_SPOTIFY_AUTH_BASE_URL"),
		HTTP: &http.Client{Timeout: 15 * time.Second}, UserAgent: ua,
		Creds: func() (string, string, bool) {
			if pluginReg == nil {
				return "", "", false
			}
			id, ok1 := pluginReg.Secret(spotifyplugin.ID, spotifyplugin.KeyClientID)
			sec, ok2 := pluginReg.Secret(spotifyplugin.ID, spotifyplugin.KeySecret)
			return id, sec, ok1 && ok2
		},
	})
	matchers := metadata.Multi{}
	if mbc, err := musicbrainz.New(musicbrainz.Config{BaseURL: cfg.MusicBrainz.BaseURL, UserAgent: ua}); err == nil {
		matchers = append(matchers, mbc)
	}
	matchers = append(matchers, spotifyMeta)
	for _, id := range []string{"netease", "qq", "kugou"} {
		if s := anonSrc[id]; s != nil {
			matchers = append(matchers, s)
		}
	}
	var matcher metadata.MetadataProvider = matchers
	fillMB, _ := musicbrainz.New(musicbrainz.Config{BaseURL: cfg.MusicBrainz.BaseURL, UserAgent: ua})
	filler := &metadata.Filler{DB: database, Hub: hub, CoverDir: filepath.Join(cfg.CacheDir, "covers"), Log: log,
		PerMinute: func() int { return rcStore.Get().FillPerMinute },
		Sources: func() []metadata.MetadataProvider {
			var out []metadata.MetadataProvider
			for _, id := range rcStore.Get().FillSources {
				switch id {
				case "spotify":
					out = append(out, spotifyMeta)
				case "musicbrainz":
					if fillMB != nil {
						out = append(out, fillMB)
					}
				default:
					if s := anonSrc[id]; s != nil {
						out = append(out, s)
					}
				}
			}
			return out
		}}
	go filler.Run(ctx)
	var lyricsFetcher library.LyricsFetcher
	if len(lyricChain) > 0 {
		lyricsFetcher = lyricChain.AsFetcher()
		log.Info("lyrics providers enabled", "n", len(lyricChain))
	}
	var analyzer *loudness.Analyzer
	var restartWatch func()
	scanner := library.NewScanner(database, library.Options{
		OnMerged: func() {
			enrich()
			filler.Kick()
			if analyzer != nil {
				analyzer.Kick()
			}
		},
		Roots: cfg.LibraryRoots,
		OnRootsChanged: func() {
			if restartWatch != nil {
				restartWatch()
			}
		},
		CoverDir:   filepath.Join(cfg.CacheDir, "covers"),
		Logger:     log,
		OnProgress: func(j library.Job) { hub.Publish("scan.progress", j) },
	})
	orch, err := playback.NewOrchestrator(ctx, database, hub, app.LocalLease(scanner))
	if err != nil {
		return err
	}
	ud := &userdata.Store{DB: database}
	orch.OnListened = func(l playback.Listen) {
		if !userdata.Counts(l.PlayedMs, l.DurationMs, l.Completed) {
			return
		}
		err := ud.Record(context.Background(), userdata.Play{TrackID: l.TrackID, PlayerID: l.PlayerID,
			PlayedAt: l.At.Add(-time.Duration(l.PlayedMs) * time.Millisecond), DurationMs: l.PlayedMs, Completed: l.Completed})
		if err != nil {
			log.Warn("record play", "err", err)
			return
		}
		hub.Publish("history.played", map[string]any{"track_id": l.TrackID, "player_id": l.PlayerID})
	}
	if err := scanner.LoadSettings(ctx); err != nil {
		return err
	}
	plugins := online.NewRegistry(database, vlt, log, nil)
	for _, f := range []online.Factory{netease.New(netease.Options{}), kugou.New(kugou.Options{}), qq.New(qq.Options{}), spotifyplugin.New()} {
		if err := plugins.Register(f); err != nil {
			return err
		}
	}
	if err := plugins.Load(ctx); err != nil {
		return err
	}
	pluginReg = plugins
	// Local bootstrap: seed Spotify client credentials from the environment
	// into the Vault once (values are never logged).
	for key, env := range map[string]string{spotifyplugin.KeyClientID: "YIMENG_SPOTIFY_CLIENT_ID", spotifyplugin.KeySecret: "YIMENG_SPOTIFY_CLIENT_SECRET"} {
		if plugins.SeedSecret(ctx, spotifyplugin.ID, key, os.Getenv(env)) {
			log.Info("spotify credential seeded into vault", "field", key)
		}
	}
	onlineCache := online.NewCache(database)
	if mb, err := strconv.Atoi(os.Getenv("YIMENG_ONLINE_CACHE_MB")); err == nil && mb > 0 {
		onlineCache.MaxBytes = int64(mb) << 20
	}
	onlineSvc := online.NewService(plugins, scanner, onlineCache, log)
	scanner.SetExternalResolver(onlineSvc.Materialize)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				onlineCache.Prune(ctx)
				onlineSvc.RefreshHot(ctx)
			}
		}
	}()
	log.Info("online plugins registered", "ids", plugins.IDs(), "enabled", plugins.EnabledIDs())
	if err := scanner.RecoverInterrupted(ctx); err != nil {
		return err
	}

	tc, err := transcode.New(transcode.Options{FFmpeg: cfg.FFmpeg, CacheDir: cfg.CacheDir,
		MaxBytes: cfg.CacheMaxBytes, Concurrency: cfg.TranscodeConcurrency, Log: log})
	if err != nil {
		return err
	}
	if tc == nil {
		log.Info("ffmpeg not available: streaming originals only")
	} else {
		log.Info("transcoding enabled", "formats", len(tc.Formats())-1)
	}

	if cfg.Library.LoudnessAnalysis && tc.FFmpeg() != "" {
		analyzer = loudness.New(database, tc.FFmpeg(), log)
		analyzer.Resolve = scanner.SafePath
		analyzer.OnDone = func(n int) { hub.Publish("library.loudness", map[string]int{"analyzed": n}) }
		go analyzer.Run(ctx)
		analyzer.Kick()
	}

	authSvc := auth.New(database, cfg.PairCodeTTL, log)
	if v := strings.TrimSpace(os.Getenv("YIMENG_ADMIN_RESET")); v == "1" || strings.EqualFold(v, "true") {
		// Escape hatch: remove every web account (and its sessions) so the
		// first-run setup page appears again. Unset the variable afterwards.
		if n, err := authSvc.RemoveAllUsers(ctx); err != nil {
			return err
		} else {
			log.Warn("YIMENG_ADMIN_RESET: web accounts removed; open the web console to create a new admin, then unset the variable", "removed", n)
		}
	}
	if n, err := authSvc.DeviceCount(ctx); err == nil && n == 0 {
		// First run: print a pairing code so the owner can pair the first client.
		if code, exp, err := authSvc.NewCode(); err == nil {
			log.Warn("NO DEVICES PAIRED YET — pairing code (enter it in a client; POST /api/pair/request prints a new one)",
				"code", code, "expires_at", exp.Format(time.RFC3339))
		}
	}

	if cfg.ScanOnStart {
		if _, err := scanner.Start(ctx, library.StartRequest{}); err != nil {
			log.Warn("scan on start", "err", err)
		}
	}
	reload := config.NewReloader(cfg, log)
	reload.NotifySIGHUP()
	_ = reload
	go bk.Run(ctx, cfg.Backup.Interval)
	go func() {
		if err := discovery.Run(ctx, discovery.Options{Enabled: cfg.MDNS, Bind: cfg.Bind, Version: version, Log: log}); err != nil {
			log.Warn("mDNS", "err", err)
		}
	}()
	var watchSup runtimecfg.Supervisor
	startWatch := func() {
		s := rcStore.Get()
		watchSup.Restart(ctx, func(c context.Context) {
			scanner.Watch(c, library.WatchOptions{Mode: s.WatchMode, Debounce: cfg.WatchDebounce, RescanInterval: s.Rescan()})
		})
	}
	startWatch()
	restartWatch = startWatch
	startMB(rcCur)
	rcStore.OnChange(func(old, cur runtimecfg.Settings) {
		if old.WatchMode != cur.WatchMode || old.RescanInterval != cur.RescanInterval {
			startWatch()
			log.Info("file watching updated", "mode", cur.WatchMode, "rescan_interval", cur.RescanInterval)
		}
		if old.MusicBrainzEnabled != cur.MusicBrainzEnabled || old.MusicBrainzContact != cur.MusicBrainzContact {
			startMB(cur)
		}
		if strings.Join(old.FillSources, ",") != strings.Join(cur.FillSources, ",") {
			filler.Reset(context.Background())
		}
		filler.Kick()
	})

	var sub *subsonic.Handler
	if cfg.Subsonic {
		sub = &subsonic.Handler{
			Auth: authSvc, Library: &app.Library{Scanner: scanner, BG: ctx},
			Playlists: scanner, LyricsFetcher: lyricsFetcher, Version: version,
		}
	}
	tp, err := gateway.ParseTrustedProxies(cfg.Server.TrustedProxies)
	if err != nil {
		return fmt.Errorf("trusted_proxies: %w", err)
	}
	rl := gateway.NewRateLimits(cfg.Server.RateLimit, cfg.Server.RateBurst, cfg.Server.GlobalRate)

	srv := &http.Server{
		Addr: cfg.Bind,
		Handler: gateway.NewRouter(gateway.Deps{
			Library:  &app.Library{Scanner: scanner, BG: ctx},
			Playback: orch,
			Hub:      hub,
			Enrich:   func() { enrich(); filler.Kick() },
			Auth:     authSvc,
			Log:      log,

			Backup:         bk,
			UserData:       ud,
			Playlists:      scanner,
			Admin:          scanner,
			Covers:         &covers.Cache{Dir: filepath.Join(cfg.CacheDir, "thumbs")},
			LyricsFetcher:  lyricsFetcher,
			Providers:      reg,
			TrustedProxies: tp,
			RateLimits:     rl,
			Subsonic:       sub,
			Sources:        streamReg,
			Online:         onlineSvc,
			OnlineLib:      scanner,
			Vault:          vlt,
			UserEdits:      scanner,
			LyricsSearch:   lyricsSearch(lrclibSearch, anonSrc),
			ArtistAliases:  artistAliases,
			Matcher:        matcher,
			ServerSettings: rcStore,
			Logs:           logRing,
			Runtime: map[string]any{
				"version": version, "bind": cfg.Bind, "library_roots": cfg.LibraryRoots,
				"db_path": cfg.DBPath, "cache_dir": cfg.CacheDir, "cache_max_bytes": cfg.CacheMaxBytes,
				"scan_on_start": cfg.ScanOnStart, "watch_mode": cfg.WatchMode,
				"watch_debounce": cfg.WatchDebounce.String(), "rescan_interval": cfg.RescanInterval.String(),
				"musicbrainz_enabled":     cfg.MusicBrainz.Enabled || containsCI(cfg.Providers, "musicbrainz"),
				"musicbrainz_contact_set": cfg.MusicBrainz.Contact != "",
				"providers":               cfg.Providers, "ffmpeg": tc.FFmpeg() != "", "transcode_concurrency": cfg.TranscodeConcurrency,
				"loudness_analysis": cfg.Library.LoudnessAnalysis, "backup_dir": cfg.Backup.Dir,
				"backup_interval": cfg.Backup.Interval.String(), "backup_keep": cfg.Backup.Keep,
				"subsonic": cfg.Subsonic, "mdns": cfg.MDNS, "tls": cfg.Server.TLSCert != "",
				"pair_code_ttl": cfg.PairCodeTTL.String(), "log_level": cfg.Log.Level, "log_format": cfg.Log.Format,
			},
			RestoreMaxBytes: cfg.Backup.RestoreMaxBytes,
			SchemaVersion:   db.Latest(migrations.FS),
			Version:         version,
			Transcoder:      tc,
			StreamPrefs:     app.StreamPrefs{DB: database},
			System: &system.Collector{
				DB: database, DBPath: cfg.DBPath, Roots: scanner.Roots, CacheDir: cfg.CacheDir,
				CacheMaxBytes: cfg.CacheMaxBytes, Transcoder: tc, Backup: bk,
				LatestScan: scanner.Latest, Version: version, SchemaVersion: db.Latest(migrations.FS),
				Started: time.Now(), LoudnessEnabled: cfg.Library.LoudnessAnalysis,
				Features: map[string]bool{
					"backup": true, "lyrics": true, "history": true, "playlists": true,
					"loudness": cfg.Library.LoudnessAnalysis, "transcode": tc != nil,
					"subsonic": cfg.Subsonic, "mdns": cfg.MDNS,
					"providers":      len(reg.IDs()) > 0 || len(lyricChain) > 0,
					"tls":            cfg.Server.TLSCert != "",
					"rate_limit":     cfg.Server.RateLimit > 0 || cfg.Server.GlobalRate > 0,
					"stream_sources": true,
				},
			},
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		attrs := []any{"version", version, "bind", cfg.Bind, "roots", cfg.LibraryRoots}
		if cfg.Server.TLSCert != "" {
			attrs = append(attrs, "tls", true)
		}
		log.Info("yimeng core listening", attrs...)
		if cfg.Server.TLSCert != "" {
			errc <- srv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	watchSup.Wait()
	mbSup.Wait()
	scanner.Wait() // background scan stops via ctx cancellation
	return nil
}

// healthcheck lets distroless images run a HEALTHCHECK without curl.
func healthcheck(bind string) int {
	_, port, err := net.SplitHostPort(bind)
	if err != nil {
		return 1
	}
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil || res.StatusCode != 200 {
		return 1
	}
	return 0
}

func containsCI(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
