package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type memVault map[string]Credential

func (m memVault) Get(_ context.Context, s string) (Credential, error) {
	c, ok := m[s]
	if !ok {
		return c, ErrNotFound
	}
	return c, nil
}
func (m memVault) Put(_ context.Context, s string, c Credential) error { m[s] = c; return nil }
func (m memVault) Delete(_ context.Context, s string) error            { delete(m, s); return nil }

func TestRedaction(t *testing.T) {
	c := Credential{Method: LoginCookie, Fields: map[string]string{"cookie": "SECRET123"}}
	for _, s := range []string{c.String(), fmt.Sprintf("%v %+v %#v", c, c, c)} {
		if strings.Contains(s, "SECRET123") {
			t.Fatalf("leaked: %s", s)
		}
	}
}

func TestScope(t *testing.T) {
	v := memVault{"qq": {Method: LoginQRCode}, "netease": {Method: LoginCookie}}
	s := Scope(v, "qq")
	c, err := s.Get(context.Background())
	if err != nil || c.Method != LoginQRCode || s.Source() != "qq" {
		t.Fatal(c, err)
	}
	if !(Credential{ExpiresAt: time.Unix(1, 0)}).Expired(time.Now()) {
		t.Fatal("expired")
	}
}
