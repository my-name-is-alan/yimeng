package sources

import (
	"context"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

type memVault struct{ m map[string]vault.Credential }

func (v *memVault) Get(_ context.Context, s string) (vault.Credential, error) {
	c, ok := v.m[s]
	if !ok {
		return vault.Credential{}, vault.ErrNotFound
	}
	return c, nil
}
func (v *memVault) Put(_ context.Context, s string, c vault.Credential) error {
	v.m[s] = c
	return nil
}
func (v *memVault) Delete(_ context.Context, s string) error {
	delete(v.m, s)
	return nil
}

func TestStubLogin(t *testing.T) {
	v := &memVault{m: map[string]vault.Credential{}}
	s := NewStub("netease", []vault.LoginMethod{vault.LoginCookie, vault.LoginQRCode}, vault.Scope(v, "netease"))
	if s.Health(context.Background()) != playback.SourceNeedsReauth {
		t.Fatal(s.Health(context.Background()))
	}
	info, err := s.Login(context.Background(), playback.LoginInput{Method: vault.LoginCookie, Cookie: "MUSIC_U=x"})
	if err != nil || info.DisplayName == "" {
		t.Fatal(info, err)
	}
	if s.Health(context.Background()) != playback.SourceOK {
		t.Fatal(s.Health(context.Background()))
	}
	ch, err := s.StartQRLogin(context.Background())
	if err != nil || ch.ID == "" {
		t.Fatal(ch, err)
	}
	st, _ := s.PollQRLogin(context.Background(), ch.ID)
	if st != playback.QRPending {
		t.Fatal(st)
	}
	if _, err := s.ResolveStream(context.Background(), "x"); err != playback.ErrNotSupported {
		t.Fatal(err)
	}
}
