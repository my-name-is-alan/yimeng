package vault

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func newStore(t *testing.T, key []byte) *SQLStore {
	d, err := db.Open(filepath.Join(t.TempDir(), "v.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	db.Migrate(context.Background(), d, migrations.FS)
	s, err := NewSQLStore(d, key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreRoundTripAndEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{7}, KeySize)
	s := newStore(t, key)
	c := Credential{Method: LoginCookie, Fields: map[string]string{"cookie": "MUSIC_U=supersecret"}, ExpiresAt: time.Unix(2e9, 0).UTC()}
	if err := s.Put(ctx, "netease", c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "netease")
	if err != nil || got.Fields["cookie"] != "MUSIC_U=supersecret" || !got.ExpiresAt.Equal(c.ExpiresAt) {
		t.Fatalf("%v %v", got, err)
	}
	var blob []byte
	s.db.QueryRow(`SELECT blob FROM vault_entries WHERE source='netease'`).Scan(&blob)
	if bytes.Contains(blob, []byte("supersecret")) || bytes.Contains(blob, []byte("cookie")) {
		t.Fatal("plaintext at rest")
	}
	// same plaintext twice -> different blobs (random nonce)
	s.Put(ctx, "netease", c)
	var blob2 []byte
	s.db.QueryRow(`SELECT blob FROM vault_entries WHERE source='netease'`).Scan(&blob2)
	if bytes.Equal(blob, blob2) {
		t.Fatal("nonce reuse")
	}
	// tamper
	blob2[len(blob2)-1] ^= 1
	s.db.Exec(`UPDATE vault_entries SET blob=? WHERE source='netease'`, blob2)
	if _, err := s.Get(ctx, "netease"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("tamper undetected")
	}
	// blob moved to another source fails (AAD binding)
	s.Put(ctx, "qq", c)
	s.db.Exec(`INSERT OR REPLACE INTO vault_entries(source, blob, updated_at) SELECT 'apple', blob, 1 FROM vault_entries WHERE source='qq'`)
	if _, err := s.Get(ctx, "apple"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("cross-source blob accepted")
	}
	// wrong key
	other, _ := NewSQLStore(s.db, bytes.Repeat([]byte{8}, KeySize))
	if _, err := other.Get(ctx, "qq"); !errors.Is(err, ErrDecrypt) {
		t.Fatal("wrong key accepted")
	}
	if _, err := s.Get(ctx, "spotify"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "../x", c); err == nil {
		t.Fatal("bad source accepted")
	}
	srcs, _ := s.Sources(ctx)
	if len(srcs) != 3 {
		t.Fatal(srcs)
	}
	s.Delete(ctx, "qq")
	if _, err := s.Get(ctx, "qq"); !errors.Is(err, ErrNotFound) {
		t.Fatal("delete")
	}
	if _, err := NewSQLStore(s.db, []byte("short")); err == nil {
		t.Fatal("short key")
	}
}

func TestLoadOrCreateKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vault", "master.key")
	k1, warn, err := LoadOrCreateKey(p, "")
	if err != nil || len(k1) != KeySize || warn == "" {
		t.Fatal(err)
	}
	assertKeyPermissions(t, p)
	k2, warn, err := LoadOrCreateKey(p, "")
	if err != nil || !bytes.Equal(k1, k2) || warn != "" {
		t.Fatal("reload differs")
	}
	loosenKeyPermissions(t, p)
	if _, warn, err := LoadOrCreateKey(p, ""); err != nil || warn == "" {
		t.Fatalf("loose permissions not repaired: %q %v", warn, err)
	}
	assertKeyPermissions(t, p)
	envKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, KeySize))
	envPath := filepath.Join(t.TempDir(), "never")
	k3, _, err := LoadOrCreateKey(envPath, envKey)
	if err != nil || k3[0] != 1 {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("env key written to disk")
	}
	if _, _, err := LoadOrCreateKey(p, "bm90LWEta2V5"); err == nil {
		t.Fatal("short env key accepted")
	}
	os.WriteFile(p, []byte("garbage"), 0o600)
	if _, _, err := LoadOrCreateKey(p, ""); err == nil {
		t.Fatal("corrupt key file accepted")
	}
}
