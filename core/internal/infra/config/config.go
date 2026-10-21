// Package config loads Core configuration: built-in defaults, then an optional
// TOML file (YIMENG_CONFIG), then environment variable overrides.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Bind          string   // server.bind
	LibraryRoots  []string // library.roots (absolute paths)
	DBPath        string   // db.path
	VaultPath     string   // vault.path
	CacheDir      string   // cache.dir
	CacheMaxBytes int64    // cache.max_bytes
}

type fileConfig struct {
	Server  struct{ Bind string }    `toml:"server"`
	Library struct{ Roots []string } `toml:"library"`
	DB      struct{ Path string }    `toml:"db"`
	Vault   struct{ Path string }    `toml:"vault"`
	Cache   struct {
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
	}
}

// Load reads configuration. Environment variables:
//
//	YIMENG_CONFIG          path to a TOML file (see configs/default.toml)
//	YIMENG_BIND            listen address (legacy alias: ZHILE_BIND)
//	YIMENG_LIBRARY_ROOTS   comma-separated music root directories
//	YIMENG_DB_PATH, YIMENG_VAULT_PATH, YIMENG_CACHE_DIR, YIMENG_CACHE_MAX_BYTES
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
