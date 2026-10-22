// Package sources holds StreamSource stubs for platforms without a safe public
// playback API in Core. They accept credentials into the Vault and advertise
// login methods so the admin UI / account API can be exercised; ResolveStream
// and SyncLibrary return ErrNotSupported until a licensed integration lands.
package sources

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

type Stub struct {
	id      string
	methods []vault.LoginMethod
	creds   vault.Scoped

	mu sync.Mutex
	qr map[string]time.Time
}

func NewStub(id string, methods []vault.LoginMethod, creds vault.Scoped) *Stub {
	return &Stub{id: id, methods: methods, creds: creds, qr: map[string]time.Time{}}
}

func (s *Stub) ID() string { return s.id }

func (s *Stub) Health(ctx context.Context) playback.SourceHealth {
	if s.creds == nil {
		return playback.SourceNeedsReauth
	}
	c, err := s.creds.Get(ctx)
	if err != nil {
		return playback.SourceNeedsReauth
	}
	if c.Expired(time.Now()) {
		return playback.SourceNeedsReauth
	}
	return playback.SourceOK
}

func (s *Stub) LoginMethods() []playback.LoginMethodInfo {
	out := make([]playback.LoginMethodInfo, len(s.methods))
	for i, m := range s.methods {
		out[i] = playback.LoginMethodInfo{Method: m}
	}
	return out
}

func (s *Stub) StartQRLogin(ctx context.Context) (playback.QRChallenge, error) {
	ok := false
	for _, m := range s.methods {
		if m == vault.LoginQRCode {
			ok = true
		}
	}
	if !ok {
		return playback.QRChallenge{}, playback.ErrNotSupported
	}
	id := s.id + "-qr-" + time.Now().Format("150405.000")
	exp := time.Now().Add(2 * time.Minute)
	s.mu.Lock()
	s.qr[id] = exp
	s.mu.Unlock()
	return playback.QRChallenge{
		ID: id, ImageURL: "data:text/plain," + id, ExpiresAt: exp,
	}, nil
}

func (s *Stub) PollQRLogin(ctx context.Context, challengeID string) (playback.QRStatus, error) {
	s.mu.Lock()
	exp, ok := s.qr[challengeID]
	s.mu.Unlock()
	if !ok || time.Now().After(exp) {
		return playback.QRExpired, nil
	}
	// Stub never confirms — real adapters would.
	return playback.QRPending, nil
}

func (s *Stub) Login(ctx context.Context, in playback.LoginInput) (playback.AccountInfo, error) {
	allowed := false
	for _, m := range s.methods {
		if m == in.Method {
			allowed = true
		}
	}
	if !allowed {
		return playback.AccountInfo{}, playback.ErrNotSupported
	}
	fields := map[string]string{}
	switch in.Method {
	case vault.LoginCookie:
		if in.Cookie == "" {
			return playback.AccountInfo{}, playback.ErrNeedsLogin
		}
		fields["cookie"] = in.Cookie
	case vault.LoginPassword:
		if in.Username == "" || in.Password == "" {
			return playback.AccountInfo{}, playback.ErrNeedsLogin
		}
		fields["username"] = in.Username
		fields["password"] = in.Password
	default:
		return playback.AccountInfo{}, playback.ErrNotSupported
	}
	if s.creds == nil {
		return playback.AccountInfo{}, playback.ErrSourceUnavailable
	}
	if err := s.creds.Put(ctx, vault.Credential{Method: in.Method, Fields: fields}); err != nil {
		return playback.AccountInfo{}, err
	}
	name := in.Username
	if name == "" {
		name = s.id + " account"
	}
	return playback.AccountInfo{DisplayName: name, Tier: "unknown"}, nil
}

func (s *Stub) Account(ctx context.Context) (playback.AccountInfo, error) {
	if s.creds == nil {
		return playback.AccountInfo{}, playback.ErrNeedsLogin
	}
	c, err := s.creds.Get(ctx)
	if err != nil {
		return playback.AccountInfo{}, playback.ErrNeedsLogin
	}
	name := c.Fields["username"]
	if name == "" {
		name = s.id + " account"
	}
	return playback.AccountInfo{DisplayName: name, Tier: "unknown"}, nil
}

func (s *Stub) RefreshCredentials(context.Context) error { return playback.ErrNotSupported }

func (s *Stub) Logout(ctx context.Context) error {
	if s.creds == nil {
		return nil
	}
	// Scoped has no Delete — clear via Put empty is wrong; use underlying vault through a side channel.
	// Adapters get Scoped only; logout is done by the accounts API deleting the vault entry.
	return nil
}

func (s *Stub) SyncLibrary(context.Context, string) (playback.SyncPage, error) {
	return playback.SyncPage{}, playback.ErrNotSupported
}
func (s *Stub) ResolveStream(context.Context, string) (playback.StreamLease, error) {
	return playback.StreamLease{}, playback.ErrNotSupported
}
func (s *Stub) OpenStream(context.Context, string, string) (io.ReadCloser, string, error) {
	return nil, "", playback.ErrNotSupported
}

var _ playback.StreamSource = (*Stub)(nil)
