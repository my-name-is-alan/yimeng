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
	"syscall"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/adapters/musicbrainz"
	"github.com/my-name-is-alan/yimeng/core/internal/app"
	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/backup"
	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/gateway"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/config"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/logging"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/loudness"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/system"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

var version = "dev"

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
	slog.SetDefault(log)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(cfg.Bind))
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
	_ = vlt // handed to StreamSource / MetadataProvider factories (vault.Scope) as they land

	hub := events.NewHub()
	var enrich func()
	if cfg.MusicBrainz.Enabled {
		mb, err := musicbrainz.New(musicbrainz.Config{
			BaseURL:   cfg.MusicBrainz.BaseURL,
			UserAgent: fmt.Sprintf("yimeng/%s ( %s )", version, cfg.MusicBrainz.Contact),
		})
		if err != nil {
			return err
		}
		en := &metadata.Enricher{DB: database, P: mb, Hub: hub, CoverDir: filepath.Join(cfg.CacheDir, "covers"), Log: log}
		go en.Run(ctx)
		enrich = en.Kick
		log.Info("MusicBrainz enrichment enabled")
	}
	var analyzer *loudness.Analyzer
	scanner := library.NewScanner(database, library.Options{
		OnMerged: func() {
			if enrich != nil {
				enrich()
			}
			if analyzer != nil {
				analyzer.Kick()
			}
		},
		Roots:      cfg.LibraryRoots,
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
	go bk.Run(ctx, cfg.Backup.Interval)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		scanner.Watch(ctx, library.WatchOptions{Mode: cfg.WatchMode, Debounce: cfg.WatchDebounce, RescanInterval: cfg.RescanInterval})
	}()

	srv := &http.Server{
		Addr: cfg.Bind,
		Handler: gateway.NewRouter(gateway.Deps{
			Library:  &app.Library{Scanner: scanner, BG: ctx},
			Playback: orch,
			Hub:      hub,
			Enrich:   enrich,
			Auth:     authSvc,
			Log:      log,

			Backup:          bk,
			UserData:        ud,
			Playlists:       scanner,
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
				},
			},
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("yimeng core listening", "version", version, "bind", cfg.Bind, "roots", cfg.LibraryRoots)
		errc <- srv.ListenAndServe()
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
	<-watchDone
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
