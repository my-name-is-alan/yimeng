package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KeySize is the AES-256 master key length.
const KeySize = 32

const blobVersion byte = 1

var ErrDecrypt = errors.New("vault: cannot decrypt entry (wrong master key or tampered data)")

// LoadOrCreateKey returns the master key. Precedence:
//  1. envKey (base64, e.g. from YIMENG_VAULT_KEY / a Docker secret) — never written to disk;
//  2. the key file at path (base64 text, must be 32 bytes);
//  3. otherwise a new random key is generated and written to path with 0600
//     (directory 0700), refusing to overwrite an existing file.
//
// A key file readable by group/others is tightened to 0600 and reported in warn.
func LoadOrCreateKey(path, envKey string) (key []byte, warn string, err error) {
	if envKey != "" {
		k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKey))
		if err != nil || len(k) != KeySize {
			return nil, "", fmt.Errorf("vault: env master key must be base64 of %d bytes", KeySize)
		}
		return k, "", nil
	}
	if b, err := os.ReadFile(path); err == nil {
		k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(k) != KeySize {
			return nil, "", fmt.Errorf("vault: %s is not a valid master key", path)
		}
		if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(path, 0o600); err == nil {
				warn = fmt.Sprintf("master key %s was accessible by other users; permissions reset to 0600", path)
			} else {
				warn = fmt.Sprintf("master key %s is accessible by other users (chmod failed: %v)", path, err)
			}
		}
		return k, warn, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("vault: read master key: %w", err)
	}
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("vault: create master key: %w", err)
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(k) + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, "", err
	}
	if err := f.Close(); err != nil {
		return nil, "", err
	}
	return k, "generated new vault master key at " + path + " (back it up; losing it makes stored credentials unreadable)", nil
}

// SQLStore keeps credentials in vault_entries, each sealed with AES-256-GCM.
// Blob layout: version(1) | nonce(12) | ciphertext+tag. The source name is
// bound as additional data, so a blob copied to another source fails to open.
type SQLStore struct {
	db   *sql.DB
	aead cipher.AEAD
}

var _ Vault = (*SQLStore)(nil)

func NewSQLStore(db *sql.DB, key []byte) (*SQLStore, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("vault: master key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SQLStore{db: db, aead: aead}, nil
}

func aad(source string) []byte { return []byte("yimeng-vault:v1:" + source) }

func validSource(s string) error {
	if s == "" || len(s) > 64 || strings.ContainsAny(s, " \x00/") {
		return fmt.Errorf("vault: invalid source %q", s)
	}
	return nil
}

func (s *SQLStore) seal(source string, c Credential) ([]byte, error) {
	plain, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{blobVersion}, nonce...)
	return s.aead.Seal(out, nonce, plain, aad(source)), nil
}

func (s *SQLStore) open(source string, blob []byte) (Credential, error) {
	var c Credential
	ns := s.aead.NonceSize()
	if len(blob) < 1+ns+s.aead.Overhead() || blob[0] != blobVersion {
		return c, ErrDecrypt
	}
	plain, err := s.aead.Open(nil, blob[1:1+ns], blob[1+ns:], aad(source))
	if err != nil {
		return c, ErrDecrypt
	}
	if err := json.Unmarshal(plain, &c); err != nil {
		return c, ErrDecrypt
	}
	return c, nil
}

func (s *SQLStore) Get(ctx context.Context, source string) (Credential, error) {
	if err := validSource(source); err != nil {
		return Credential{}, err
	}
	var blob []byte
	err := s.db.QueryRowContext(ctx, `SELECT blob FROM vault_entries WHERE source=?`, source).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, err
	}
	return s.open(source, blob)
}

func (s *SQLStore) Put(ctx context.Context, source string, c Credential) error {
	if err := validSource(source); err != nil {
		return err
	}
	blob, err := s.seal(source, c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO vault_entries(source, blob, updated_at) VALUES (?,?,?)
		ON CONFLICT(source) DO UPDATE SET blob=excluded.blob, updated_at=excluded.updated_at`, source, blob, time.Now().UnixMilli())
	return err
}

func (s *SQLStore) Delete(ctx context.Context, source string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM vault_entries WHERE source=?`, source)
	return err
}

// Sources lists sources with stored credentials (names only, never contents).
func (s *SQLStore) Sources(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source FROM vault_entries ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
