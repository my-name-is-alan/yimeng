package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := load(env(nil))
	if err != nil || c.Bind != "0.0.0.0:8741" || len(c.LibraryRoots) != 1 || !filepath.IsAbs(c.LibraryRoots[0]) {
		t.Fatalf("%+v %v", c, err)
	}
	if runtime.GOOS != "windows" && c.LibraryRoots[0] != "/music" {
		t.Fatal("changed NAS defaults", c.LibraryRoots)
	}
	if runtime.GOOS == "windows" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		if c.LibraryRoots[0] != filepath.Join(home, "Music") {
			t.Fatal(c.LibraryRoots)
		}
	}
	for _, path := range []string{c.DBPath, c.VaultPath, c.MasterKeyFile, c.CacheDir, c.Backup.Dir} {
		if !filepath.IsAbs(path) {
			t.Errorf("default path is not absolute: %q", path)
		}
	}
}

func TestFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	a, b, dbPath := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "x.db")
	if err := os.WriteFile(p, []byte(fmt.Sprintf("[server]\nbind=\"127.0.0.1:1\"\n[library]\nroots=[%q,%q]\n[db]\npath=%q\n", a, b+string(filepath.Separator), dbPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := load(env(map[string]string{"YIMENG_CONFIG": p}))
	if err != nil || c.Bind != "127.0.0.1:1" || len(c.LibraryRoots) != 2 || c.LibraryRoots[0] != a || c.LibraryRoots[1] != b || c.DBPath != dbPath {
		t.Fatalf("%+v %v", c, err)
	}
	m1, m2 := filepath.Join(dir, "m1"), filepath.Join(dir, "m2")
	c, err = load(env(map[string]string{"YIMENG_CONFIG": p, "YIMENG_LIBRARY_ROOTS": m1 + ", " + m2, "YIMENG_BIND": ":9"}))
	if err != nil || c.Bind != ":9" || len(c.LibraryRoots) != 2 || c.LibraryRoots[0] != m1 || c.LibraryRoots[1] != m2 {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := load(env(map[string]string{"YIMENG_LIBRARY_ROOTS": "relative/dir"})); err == nil {
		t.Fatal("want error for relative root")
	}
}

func TestShippedDefaultToml(t *testing.T) {
	vars := map[string]string{"YIMENG_CONFIG": "../../../configs/default.toml"}
	wantRoot := "/music"
	if runtime.GOOS == "windows" {
		if _, err := load(env(vars)); err == nil {
			t.Fatal("NAS config's drive-relative root accepted on Windows")
		}
		def := Defaults()
		wantRoot = def.LibraryRoots[0]
		vars["YIMENG_LIBRARY_ROOTS"], vars["YIMENG_DB_PATH"], vars["YIMENG_VAULT_PATH"], vars["YIMENG_CACHE_DIR"] = wantRoot, def.DBPath, def.VaultPath, def.CacheDir
	}
	c, err := load(env(vars))
	if err != nil || c.Bind != "0.0.0.0:8741" || c.LibraryRoots[0] != wantRoot || c.CacheMaxBytes != 2147483648 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestWatchAndAuthEnv(t *testing.T) {
	c, err := load(env(map[string]string{"YIMENG_SCAN_ON_START": "false", "YIMENG_WATCH": "poll",
		"YIMENG_RESCAN_INTERVAL": "10m", "YIMENG_WATCH_DEBOUNCE": "2s", "YIMENG_PAIR_CODE_TTL": "2m"}))
	if err != nil || c.ScanOnStart || c.WatchMode != "poll" || c.RescanInterval.Minutes() != 10 ||
		c.WatchDebounce.Seconds() != 2 || c.PairCodeTTL.Minutes() != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	for _, bad := range []map[string]string{{"YIMENG_WATCH": "maybe"}, {"YIMENG_PAIR_CODE_TTL": "1h"}, {"YIMENG_RESCAN_INTERVAL": "x"}} {
		if _, err := load(env(bad)); err == nil {
			t.Errorf("want error for %v", bad)
		}
	}
}

func TestMusicBrainzConfig(t *testing.T) {
	c, err := load(env(nil))
	if err != nil || c.MusicBrainz.Enabled {
		t.Fatal("MB must be opt-in")
	}
	if _, err := load(env(map[string]string{"YIMENG_MB_ENABLED": "true"})); err == nil {
		t.Fatal("contact required")
	}
	c, err = load(env(map[string]string{"YIMENG_MB_ENABLED": "1", "YIMENG_MB_CONTACT": "me@example.com"}))
	if err != nil || !c.MusicBrainz.Enabled || c.MusicBrainz.Contact != "me@example.com" {
		t.Fatal(c.MusicBrainz, err)
	}
}
