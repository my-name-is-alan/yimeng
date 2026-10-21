// Package config loads Core configuration: built-in defaults, then an optional
// TOML file (YIMENG_CONFIG), then environment variable overrides.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Bind          string   // server.bind
	LibraryRoots  []string // library.roots (absolute paths)
	DBPath        string   // db.path
	VaultPath     string   // vault.path
	CacheDir      string   // cache.dir
	CacheMaxBytes int64    // cache.max_bytes

	ScanOnStart    bool          // library.scan_on_start
	WatchMode      string        // library.watch: auto | fsnotify | poll | off
	WatchDebounce  time.Duration // library.watch_debounce
	RescanInterval time.Duration // library.rescan_interval (poll / fallback); 0 = never

	PairCodeTTL time.Duration // auth.pair_code_ttl_seconds

	MusicBrainz MusicBrainzConfig // metadata.musicbrainz (opt-in)
}

type MusicBrainzConfig struct {
	Enabled bool   `toml:"enabled"`
	Contact string `toml:"contact"`  // email or URL; required by the MB API policy
	BaseURL string `toml:"base_url"` // default https://musicbrainz.org (or a local mirror)
}

type fileConfig struct {
	Server  struct{ Bind string } `toml:"server"`
	Library struct {
		Roots          []string `toml:"roots"`
		ScanOnStart    *bool    `toml:"scan_on_start"`
		Watch          string   `toml:"watch"`
		WatchDebounce  string   `toml:"watch_debounce"`
		RescanInterval string   `toml:"rescan_interval"`
	} `toml:"library"`
	Auth struct {
		PairCodeTTLSeconds int `toml:"pair_code_ttl_seconds"`
	} `toml:"auth"`
	Metadata struct {
		MusicBrainz MusicBrainzConfig `toml:"musicbrainz"`
	} `toml:"metadata"`
	DB    struct{ Path string } `toml:"db"`
	Vault struct{ Path string } `toml:"vault"`
	Cache struct {
		Dir      string `toml:"dir"`
		MaxBytes int64  `toml:"max_bytes"`
	} `toml:"cache"`
}

func Defaults() Config {
	return Config{
		Bind:          "0.0.0.0:8741",
		LibraryRoots:  []string{"/music"},
		DBPath:        "/var/yimeng/library.db",
		VaultPath:     "/var/yimeng/vault",
		CacheDir:      "/var/yimeng/cache",
		CacheMaxBytes: 2 << 30,

		ScanOnStart:    true,
		WatchMode:      "auto",
		WatchDebounce:  5 * time.Second,
		RescanInterval: time.Hour,
		PairCodeTTL:    5 * time.Minute,
	}
}

// Load reads configuration. Environment variables:
//
//	YIMENG_CONFIG          path to a TOML file (see configs/default.toml)
//	YIMENG_BIND            listen address (legacy alias: ZHILE_BIND)
//	YIMENG_LIBRARY_ROOTS   comma-separated music root directories
//	YIMENG_DB_PATH, YIMENG_VAULT_PATH, YIMENG_CACHE_DIR, YIMENG_CACHE_MAX_BYTES
//	YIMENG_SCAN_ON_START   true|false (default true)
//	YIMENG_WATCH           auto|fsnotify|poll|off (default auto)
//	YIMENG_WATCH_DEBOUNCE  Go duration, default 5s
//	YIMENG_RESCAN_INTERVAL Go duration, default 1h (poll mode / watch fallback; 0 = off)
//	YIMENG_PAIR_CODE_TTL   Go duration, default 5m (30s..15m)
//	YIMENG_MB_ENABLED      true|false (default false) — MusicBrainz enrichment
//	YIMENG_MB_CONTACT      email/URL for the MusicBrainz User-Agent (required when enabled)
//	YIMENG_MB_BASE_URL     optional mirror
func Load() (Config, error) { return load(os.Getenv) }

func load(getenv func(string) string) (Config, error) {
	cfg := Defaults()
	if p := getenv("YIMENG_CONFIG"); p != "" {
		var fc fileConfig
		if _, err := toml.DecodeFile(p, &fc); err != nil {
			return cfg, fmt.Errorf("config %s: %w", p, err)
		}
		setStr(&cfg.Bind, fc.Server.Bind)
		if len(fc.Library.Roots) > 0 {
			cfg.LibraryRoots = fc.Library.Roots
		}
		setStr(&cfg.DBPath, fc.DB.Path)
		setStr(&cfg.VaultPath, fc.Vault.Path)
		setStr(&cfg.CacheDir, fc.Cache.Dir)
		if fc.Cache.MaxBytes > 0 {
			cfg.CacheMaxBytes = fc.Cache.MaxBytes
		}
		if fc.Library.ScanOnStart != nil {
			cfg.ScanOnStart = *fc.Library.ScanOnStart
		}
		setStr(&cfg.WatchMode, fc.Library.Watch)
		if err := setDur(&cfg.WatchDebounce, fc.Library.WatchDebounce, "library.watch_debounce"); err != nil {
			return cfg, err
		}
		if err := setDur(&cfg.RescanInterval, fc.Library.RescanInterval, "library.rescan_interval"); err != nil {
			return cfg, err
		}
		cfg.MusicBrainz = fc.Metadata.MusicBrainz
		if fc.Auth.PairCodeTTLSeconds > 0 {
			cfg.PairCodeTTL = time.Duration(fc.Auth.PairCodeTTLSeconds) * time.Second
		}
	}
	setStr(&cfg.Bind, getenv("ZHILE_BIND"))
	setStr(&cfg.Bind, getenv("YIMENG_BIND"))
	if r := getenv("YIMENG_LIBRARY_ROOTS"); r != "" {
		cfg.LibraryRoots = nil
		for _, p := range strings.Split(r, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.LibraryRoots = append(cfg.LibraryRoots, p)
			}
		}
	}
	setStr(&cfg.DBPath, getenv("YIMENG_DB_PATH"))
	setStr(&cfg.VaultPath, getenv("YIMENG_VAULT_PATH"))
	setStr(&cfg.CacheDir, getenv("YIMENG_CACHE_DIR"))
	if v := getenv("YIMENG_CACHE_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("YIMENG_CACHE_MAX_BYTES: %w", err)
		}
		cfg.CacheMaxBytes = n
	}
	if v := getenv("YIMENG_SCAN_ON_START"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("YIMENG_SCAN_ON_START: %w", err)
		}
		cfg.ScanOnStart = b
	}
	setStr(&cfg.WatchMode, strings.ToLower(getenv("YIMENG_WATCH")))
	if err := setDur(&cfg.WatchDebounce, getenv("YIMENG_WATCH_DEBOUNCE"), "YIMENG_WATCH_DEBOUNCE"); err != nil {
		return cfg, err
	}
	if err := setDur(&cfg.RescanInterval, getenv("YIMENG_RESCAN_INTERVAL"), "YIMENG_RESCAN_INTERVAL"); err != nil {
		return cfg, err
	}
	if err := setDur(&cfg.PairCodeTTL, getenv("YIMENG_PAIR_CODE_TTL"), "YIMENG_PAIR_CODE_TTL"); err != nil {
		return cfg, err
	}
	if v := getenv("YIMENG_MB_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("YIMENG_MB_ENABLED: %w", err)
		}
		cfg.MusicBrainz.Enabled = b
	}
	setStr(&cfg.MusicBrainz.Contact, getenv("YIMENG_MB_CONTACT"))
	setStr(&cfg.MusicBrainz.BaseURL, getenv("YIMENG_MB_BASE_URL"))
	if cfg.MusicBrainz.Enabled && strings.TrimSpace(cfg.MusicBrainz.Contact) == "" {
		return cfg, fmt.Errorf("metadata.musicbrainz.contact (YIMENG_MB_CONTACT) is required when MusicBrainz is enabled")
	}
	switch cfg.WatchMode {
	case "auto", "fsnotify", "poll", "off":
	case "true", "on":
		cfg.WatchMode = "auto"
	case "false":
		cfg.WatchMode = "off"
	default:
		return cfg, fmt.Errorf("library.watch %q: want auto|fsnotify|poll|off", cfg.WatchMode)
	}
	if cfg.PairCodeTTL < 30*time.Second || cfg.PairCodeTTL > 15*time.Minute {
		return cfg, fmt.Errorf("pair code TTL %v out of range [30s,15m]", cfg.PairCodeTTL)
	}
	for i, r := range cfg.LibraryRoots {
		if !filepath.IsAbs(r) {
			return cfg, fmt.Errorf("library root %q must be an absolute path", r)
		}
		cfg.LibraryRoots[i] = filepath.Clean(r)
	}
	return cfg, nil
}

func setStr(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func setDur(dst *time.Duration, v, name string) error {
	if v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return fmt.Errorf("%s: invalid duration %q", name, v)
	}
	*dst = d
	return nil
}
