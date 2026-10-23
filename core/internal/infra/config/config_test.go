package config

import (
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := load(env(nil))
	if err != nil || c.Bind != "0.0.0.0:8741" || c.LibraryRoots[0] != "/music" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestFileAndEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[server]\nbind=\"127.0.0.1:1\"\n[library]\nroots=[\"/a\",\"/b/\"]\n[db]\npath=\"/x.db\"\n"), 0o600)
	c, err := load(env(map[string]string{"YIMENG_CONFIG": p}))
	if err != nil || c.Bind != "127.0.0.1:1" || len(c.LibraryRoots) != 2 || c.LibraryRoots[1] != "/b" || c.DBPath != "/x.db" {
		t.Fatalf("%+v %v", c, err)
	}
	c, err = load(env(map[string]string{"YIMENG_CONFIG": p, "YIMENG_LIBRARY_ROOTS": "/m1, /m2", "YIMENG_BIND": ":9"}))
	if err != nil || c.Bind != ":9" || len(c.LibraryRoots) != 2 || c.LibraryRoots[0] != "/m1" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := load(env(map[string]string{"YIMENG_LIBRARY_ROOTS": "relative/dir"})); err == nil {
		t.Fatal("want error for relative root")
	}
}

func TestShippedDefaultToml(t *testing.T) {
	c, err := load(env(map[string]string{"YIMENG_CONFIG": "../../../configs/default.toml"}))
	if err != nil || c.Bind != "0.0.0.0:8741" || c.LibraryRoots[0] != "/music" || c.CacheMaxBytes != 2147483648 {
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
