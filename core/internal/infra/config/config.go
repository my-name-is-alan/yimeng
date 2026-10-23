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
	MasterKeyFile string   // vault.master_key_file (default <vault.path>/master.key)
	CacheDir      string   // cache.dir
	CacheMaxBytes int64    // cache.max_bytes (transcode cache LRU limit)

	FFmpeg               string // transcode.ffmpeg: path/name; "" = PATH lookup; "off" disables
	TranscodeConcurrency int    // transcode.concurrency; 0 = NumCPU/2

	ScanOnStart    bool          // library.scan_on_start
	WatchMode      string        // library.watch: auto | fsnotify | poll | off
	WatchDebounce  time.Duration // library.watch_debounce
	RescanInterval time.Duration // library.rescan_interval (poll / fallback); 0 = never

	PairCodeTTL time.Duration // auth.pair_code_ttl_seconds

	MusicBrainz MusicBrainzConfig // metadata.musicbrainz (opt-in)

	Backup  BackupConfig
	Log     LogConfig
	Server  ServerConfig
	Library LibraryExtra
	// Providers lists optional metadata/lyrics providers to enable (metadata.providers).
	Providers []string
	Subsonic  bool // subsonic.enabled (default true)
	MDNS      bool // server.mdns (default true)
}

type BackupConfig struct {
	Dir             string        `toml:"dir"`  // default <dir of db.path>/backups
	Interval        time.Duration `toml:"-"`    // backup.interval, default 24h; 0 = off
	Keep            int           `toml:"keep"` // default 7
	RestoreMaxBytes int64         `toml:"max_restore_bytes"`
}

type LogConfig struct {
	Format string `toml:"format"` // text | json
	Level  string `toml:"level"`  // debug | info | warn | error
}

type ServerConfig struct {
	TLSCert        string   `toml:"tls_cert"`
	TLSKey         string   `toml:"tls_key"`
	TrustedProxies []string `toml:"trusted_proxies"` // CIDRs whose X-Forwarded-For is honoured
	RateLimit      float64  `toml:"rate_limit"`      // requests/s per client (token or IP); 0 = off
	RateBurst      int      `toml:"rate_burst"`
	GlobalRate     float64  `toml:"global_rate_limit"` // requests/s for the whole server; 0 = off
}

type LibraryExtra struct {
	LoudnessAnalysis bool // library.loudness_analysis (ffmpeg ebur128 when tags are missing)
}

type MusicBrainzConfig struct {
	Enabled bool   `toml:"enabled"`
	Contact string `toml:"contact"`  // email or URL; required by the MB API policy
	BaseURL string `toml:"base_url"` // default https://musicbrainz.org (or a local mirror)
}

type fileConfig struct {
	Server struct {
		Bind string `toml:"bind"`
		MDNS *bool  `toml:"mdns"`
	} `toml:"server"`
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
		Providers   []string          `toml:"providers"`
	} `toml:"metadata"`
	DB        struct{ Path string } `toml:"db"`
	Transcode struct {
		FFmpeg      string `toml:"ffmpeg"`
		Concurrency int    `toml:"concurrency"`
	} `toml:"transcode"`
	Vault struct {
		Path          string `toml:"path"`
		MasterKeyFile string `toml:"master_key_file"`
	} `toml:"vault"`
	Cache struct {
		Dir      string `toml:"dir"`
		MaxBytes int64  `toml:"max_bytes"`
	} `toml:"cache"`
	Backup struct {
		BackupConfig
		Interval string `toml:"interval"`
	} `toml:"backup"`
	Log      LogConfig    `toml:"log"`
	TLS      ServerConfig `toml:"http"`
	Subsonic struct {
		Enabled *bool `toml:"enabled"`
	} `toml:"subsonic"`
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

		Backup:    BackupConfig{Interval: 24 * time.Hour, Keep: 7, RestoreMaxBytes: 4 << 30},
		Log:       LogConfig{Format: "text", Level: "info"},
		Server:    ServerConfig{RateLimit: 50, RateBurst: 200},
		Library:   LibraryExtra{LoudnessAnalysis: true},
		Providers: []string{"lrclib"},
		Subsonic:  true,
		MDNS:      true,
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
//	YIMENG_VAULT_KEY_FILE  master key file (default <vault.path>/master.key, created 0600)
//	YIMENG_VAULT_KEY       base64 32-byte master key (overrides the file; read in main, never stored)
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
		if fc.Server.MDNS != nil {
			cfg.MDNS = *fc.Server.MDNS
		}
		if len(fc.Library.Roots) > 0 {
			cfg.LibraryRoots = fc.Library.Roots
		}
		setStr(&cfg.DBPath, fc.DB.Path)
		setStr(&cfg.VaultPath, fc.Vault.Path)
		setStr(&cfg.MasterKeyFile, fc.Vault.MasterKeyFile)
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
		if fc.Metadata.Providers != nil {
			cfg.Providers = fc.Metadata.Providers
		}
		setStr(&cfg.Backup.Dir, fc.Backup.Dir)
		if fc.Backup.Keep > 0 {
			cfg.Backup.Keep = fc.Backup.Keep
		}
		if fc.Backup.RestoreMaxBytes > 0 {
			cfg.Backup.RestoreMaxBytes = fc.Backup.RestoreMaxBytes
		}
		if err := setDur(&cfg.Backup.Interval, fc.Backup.Interval, "backup.interval"); err != nil {
			return cfg, err
		}
		setStr(&cfg.Log.Format, fc.Log.Format)
		setStr(&cfg.Log.Level, fc.Log.Level)
		setStr(&cfg.Server.TLSCert, fc.TLS.TLSCert)
		setStr(&cfg.Server.TLSKey, fc.TLS.TLSKey)
		if len(fc.TLS.TrustedProxies) > 0 {
			cfg.Server.TrustedProxies = fc.TLS.TrustedProxies
		}
		if fc.TLS.RateLimit > 0 {
			cfg.Server.RateLimit = fc.TLS.RateLimit
		}
		if fc.TLS.RateBurst > 0 {
			cfg.Server.RateBurst = fc.TLS.RateBurst
		}
		if fc.TLS.GlobalRate > 0 {
			cfg.Server.GlobalRate = fc.TLS.GlobalRate
		}
		if fc.Subsonic.Enabled != nil {
			cfg.Subsonic = *fc.Subsonic.Enabled
		}
		setStr(&cfg.FFmpeg, fc.Transcode.FFmpeg)
		if fc.Transcode.Concurrency > 0 {
			cfg.TranscodeConcurrency = fc.Transcode.Concurrency
		}
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
	setStr(&cfg.MasterKeyFile, getenv("YIMENG_VAULT_KEY_FILE"))
	if cfg.MasterKeyFile == "" {
		cfg.MasterKeyFile = filepath.Join(cfg.VaultPath, "master.key")
	}
	setStr(&cfg.CacheDir, getenv("YIMENG_CACHE_DIR"))
	if v := getenv("YIMENG_CACHE_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("YIMENG_CACHE_MAX_BYTES: %w", err)
		}
		cfg.CacheMaxBytes = n
	}
	setStr(&cfg.FFmpeg, getenv("YIMENG_FFMPEG"))
	if v := getenv("YIMENG_TRANSCODE_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return cfg, fmt.Errorf("YIMENG_TRANSCODE_CONCURRENCY: invalid %q", v)
		}
		cfg.TranscodeConcurrency = n
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
	if err := extraEnv(&cfg, getenv); err != nil {
		return cfg, err
	}
	if cfg.Backup.Dir == "" {
		cfg.Backup.Dir = filepath.Join(filepath.Dir(cfg.DBPath), "backups")
	}
	if (cfg.Server.TLSCert == "") != (cfg.Server.TLSKey == "") {
		return cfg, fmt.Errorf("TLS needs both a certificate and a key file")
	}
	switch cfg.Log.Format {
	case "text", "json":
	default:
		return cfg, fmt.Errorf("log.format %q: want text|json", cfg.Log.Format)
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

// extraEnv applies the environment overrides added after v0.5.
//
//	YIMENG_BACKUP_DIR, YIMENG_BACKUP_INTERVAL (24h; 0 = off), YIMENG_BACKUP_KEEP (7)
//	YIMENG_LOG_FORMAT text|json, YIMENG_LOG_LEVEL debug|info|warn|error
//	YIMENG_TLS_CERT, YIMENG_TLS_KEY, YIMENG_TRUSTED_PROXIES (comma-separated CIDRs)
//	YIMENG_RATE_LIMIT (req/s per client, default 50; 0 = off), YIMENG_RATE_BURST (200), YIMENG_GLOBAL_RATE_LIMIT
//	YIMENG_LOUDNESS_ANALYSIS true|false, YIMENG_SUBSONIC true|false, YIMENG_MDNS true|false
//	YIMENG_PROVIDERS comma-separated optional providers (e.g. lrclib)
func extraEnv(cfg *Config, getenv func(string) string) error {
	setStr(&cfg.Backup.Dir, getenv("YIMENG_BACKUP_DIR"))
	if err := setDur(&cfg.Backup.Interval, getenv("YIMENG_BACKUP_INTERVAL"), "YIMENG_BACKUP_INTERVAL"); err != nil {
		return err
	}
	setStr(&cfg.Log.Format, strings.ToLower(getenv("YIMENG_LOG_FORMAT")))
	setStr(&cfg.Log.Level, strings.ToLower(getenv("YIMENG_LOG_LEVEL")))
	setStr(&cfg.Server.TLSCert, getenv("YIMENG_TLS_CERT"))
	setStr(&cfg.Server.TLSKey, getenv("YIMENG_TLS_KEY"))
	if v := getenv("YIMENG_TRUSTED_PROXIES"); v != "" {
		cfg.Server.TrustedProxies = splitList(v)
	}
	if v := getenv("YIMENG_PROVIDERS"); v != "" {
		cfg.Providers = splitList(v)
	}
	ints := map[string]*int{"YIMENG_BACKUP_KEEP": &cfg.Backup.Keep, "YIMENG_RATE_BURST": &cfg.Server.RateBurst}
	for k, dst := range ints {
		if v := getenv(k); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fmt.Errorf("%s: invalid %q", k, v)
			}
			*dst = n
		}
	}
	floats := map[string]*float64{"YIMENG_RATE_LIMIT": &cfg.Server.RateLimit, "YIMENG_GLOBAL_RATE_LIMIT": &cfg.Server.GlobalRate}
	for k, dst := range floats {
		if v := getenv(k); v != "" {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil || n < 0 {
				return fmt.Errorf("%s: invalid %q", k, v)
			}
			*dst = n
		}
	}
	bools := map[string]*bool{"YIMENG_LOUDNESS_ANALYSIS": &cfg.Library.LoudnessAnalysis, "YIMENG_SUBSONIC": &cfg.Subsonic, "YIMENG_MDNS": &cfg.MDNS}
	for k, dst := range bools {
		if v := getenv(k); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			*dst = b
		}
	}
	return nil
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
