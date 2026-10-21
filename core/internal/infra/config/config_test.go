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
