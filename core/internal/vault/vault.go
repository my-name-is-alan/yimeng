// Package vault is the only place platform credentials live. Credentials are
// encrypted at rest (vault_entries.blob) and are never returned to clients:
// the Gateway has no export endpoint, and adapters receive a Scoped view that
// can only read their own source's entry.
//
// P0 defines the abstraction only; the encrypted store is implemented later.
package vault

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound    = errors.New("vault: no credential for source")
	ErrNeedsReauth = errors.New("vault: source needs re-authentication")
)

// LoginMethod is how the user authenticated with a platform.
type LoginMethod string

const (
	LoginQRCode   LoginMethod = "qr"
	LoginCookie   LoginMethod = "cookie"
	LoginPassword LoginMethod = "password"
	LoginOAuth    LoginMethod = "oauth"   // e.g. MusicKit user token / Spotify OAuth
	LoginAPIKey   LoginMethod = "api_key" // e.g. developer token for metadata APIs
)

// Credential is an opaque bag of secret fields for one source. Its String and
// GoString methods redact values so it is safe to log by accident.
type Credential struct {
	Method    LoginMethod
	Fields    map[string]string // e.g. "cookie", "access_token", "refresh_token"
	ExpiresAt time.Time         // zero = unknown / non-expiring
}

func (c Credential) String() string {
	keys := make([]string, 0, len(c.Fields))
	for k := range c.Fields {
		keys = append(keys, k)
	}
	return fmt.Sprintf("Credential{method=%s fields=%v values=[REDACTED] expires=%s}", c.Method, keys, c.ExpiresAt.Format(time.RFC3339))
}
func (c Credential) GoString() string { return c.String() }

// Expired reports whether the credential has a known expiry in the past.
func (c Credential) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// Vault stores credentials per source, encrypted at rest.
type Vault interface {
	Get(ctx context.Context, source string) (Credential, error)
	Put(ctx context.Context, source string, c Credential) error
	Delete(ctx context.Context, source string) error
}

// Scoped is the view handed to one adapter/provider: it can only touch its own source.
type Scoped interface {
	Source() string
	Get(ctx context.Context) (Credential, error)
	Put(ctx context.Context, c Credential) error // used after a token refresh
}

// Scope returns a Scoped view of v restricted to source.
func Scope(v Vault, source string) Scoped { return scoped{v: v, source: source} }

type scoped struct {
	v      Vault
	source string
}

func (s scoped) Source() string                              { return s.source }
func (s scoped) Get(ctx context.Context) (Credential, error) { return s.v.Get(ctx, s.source) }
func (s scoped) Put(ctx context.Context, c Credential) error { return s.v.Put(ctx, s.source, c) }

// Secrets adapts a Vault to a simple key/value secret store (one field).
type Secrets struct{ V Vault }

func (s Secrets) PutSecret(ctx context.Context, key, value string) error {
	return s.V.Put(ctx, key, Credential{Method: LoginPassword, Fields: map[string]string{"secret": value}})
}

func (s Secrets) GetSecret(ctx context.Context, key string) (string, error) {
	c, err := s.V.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return c.Fields["secret"], nil
}

func (s Secrets) DeleteSecret(ctx context.Context, key string) error { return s.V.Delete(ctx, key) }
