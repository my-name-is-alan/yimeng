package config

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Reloader holds the live Config and applies safe hot-reload fields on SIGHUP
// or when Watch is used. Bind, DBPath, VaultPath, MasterKeyFile never reload.
type Reloader struct {
	mu       sync.RWMutex
	cfg      Config
	log      *slog.Logger
	OnChange func(old, new Config)
}

func NewReloader(cfg Config, log *slog.Logger) *Reloader {
	if log == nil {
		log = slog.Default()
	}
	return &Reloader{cfg: cfg, log: log}
}

func (r *Reloader) Get() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

// ApplySafe copies hot-reloadable fields from next into the live config.
func (r *Reloader) ApplySafe(next Config) {
	r.mu.Lock()
	old := r.cfg
	r.cfg.Log = next.Log
	r.cfg.Server.RateLimit = next.Server.RateLimit
	r.cfg.Server.RateBurst = next.Server.RateBurst
	r.cfg.Server.GlobalRate = next.Server.GlobalRate
	r.cfg.Server.TrustedProxies = next.Server.TrustedProxies
	r.cfg.MDNS = next.MDNS
	r.cfg.Subsonic = next.Subsonic
	r.cfg.Providers = next.Providers
	r.cfg.Library.LoudnessAnalysis = next.Library.LoudnessAnalysis
	r.cfg.ScanOnStart = next.ScanOnStart
	r.cfg.WatchMode = next.WatchMode
	r.cfg.WatchDebounce = next.WatchDebounce
	r.cfg.RescanInterval = next.RescanInterval
	newc := r.cfg
	r.mu.Unlock()
	r.log.Info("config reloaded", "log_level", newc.Log.Level, "rate_limit", newc.Server.RateLimit)
	if r.OnChange != nil {
		r.OnChange(old, newc)
	}
}

// NotifySIGHUP reloads from the same Load() path on SIGHUP.
func (r *Reloader) NotifySIGHUP() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			next, err := Load()
			if err != nil {
				r.log.Warn("config reload failed", "err", err)
				continue
			}
			r.ApplySafe(next)
		}
	}()
}
