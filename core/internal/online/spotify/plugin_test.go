package spotify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/online/spotify"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func TestSecretSettingsStayInVault(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := db.Migrate(ctx, d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.NewSQLStore(d, make([]byte, vault.KeySize))
	reg := online.NewRegistry(d, v, nil, http.DefaultClient)
	reg.Register(spotify.New())
	if err := reg.Load(ctx); err != nil {
		t.Fatal(err)
	}
	on := true
	st, err := reg.Configure(ctx, spotify.ID, &on, map[string]any{spotify.KeyClientID: "my-id", spotify.KeySecret: "my-secret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), "my-secret") || strings.Contains(string(raw), "my-id") {
		t.Fatalf("secret leaked in status: %s", raw)
	}
	if !st.Secrets[spotify.KeyClientID] || !st.Secrets[spotify.KeySecret] || st.LoggedIn {
		t.Fatalf("%+v", st)
	}
	var cfg string
	d.QueryRow(`SELECT config FROM plugin_settings WHERE id=?`, spotify.ID).Scan(&cfg)
	if strings.Contains(cfg, "my-") {
		t.Fatalf("secret in plugin_settings: %s", cfg)
	}
	if c, err := v.Get(ctx, "plugin."+spotify.ID); err != nil || c.Fields["secret.client_secret"] != "my-secret" {
		t.Fatalf("vault: %v", err)
	}
	if s, ok := reg.Secret(spotify.ID, spotify.KeySecret); !ok || s != "my-secret" {
		t.Fatal("Secret()")
	}
	for _, id := range reg.EnabledIDs() {
		if id == spotify.ID {
			t.Fatal("metadata-only plugin must not be a music source")
		}
	}
	// "" keeps the value; reload restores it; disabling hides it.
	reg.Configure(ctx, spotify.ID, nil, map[string]any{spotify.KeySecret: ""})
	reg2 := online.NewRegistry(d, v, nil, http.DefaultClient)
	reg2.Register(spotify.New())
	reg2.Load(ctx)
	if s, ok := reg2.Secret(spotify.ID, spotify.KeySecret); !ok || s != "my-secret" {
		t.Fatal("not restored")
	}
	if reg2.SeedSecret(ctx, spotify.ID, spotify.KeySecret, "other") {
		t.Fatal("seed must not overwrite")
	}
	off := false
	reg2.Configure(ctx, spotify.ID, &off, nil)
	if _, ok := reg2.Secret(spotify.ID, spotify.KeySecret); ok {
		t.Fatal("disabled plugin must not hand out secrets")
	}
	// null clears; both cleared removes the Vault entry
	reg2.Configure(ctx, spotify.ID, nil, map[string]any{spotify.KeyClientID: nil, spotify.KeySecret: nil})
	if _, err := v.Get(ctx, "plugin."+spotify.ID); err == nil {
		t.Fatal("vault entry should be gone")
	}
}
