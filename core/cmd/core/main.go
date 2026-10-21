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

	"github.com/my-name-is-alan/yimeng/core/internal/app"
	"github.com/my-name-is-alan/yimeng/core/internal/gateway"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/config"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
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

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
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
	scanner := library.NewScanner(database, library.Options{
		Roots:    cfg.LibraryRoots,
		CoverDir: filepath.Join(cfg.CacheDir, "covers"),
		Logger:   log,
	})
	if err := scanner.RecoverInterrupted(ctx); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           gateway.NewRouter(gateway.Deps{Library: &app.Library{Scanner: scanner, BG: ctx}}),
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
