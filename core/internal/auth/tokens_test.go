package auth

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAccessAndSignedURL(t *testing.T) {
	s := &Service{now: time.Now}
	s.SetSigningKey([]byte("k"))
	tok, _, err := s.IssueAccess("dev1")
	if err != nil || !strings.HasPrefix(tok, "yma_") {
		t.Fatal(tok, err)
	}
	if id, ok := s.verifyAccess(tok); !ok || id != "dev1" {
		t.Fatal("access verify", id, ok)
	}
	if _, ok := s.verifyAccess(tok[:len(tok)-2] + "xx"); ok {
		t.Fatal("tampered access accepted")
	}
	u := s.SignURL("/api/v1/covers/a1", "dev1", time.Hour)
	p, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := p.Query()
	q.Set("size", "300") // size/format are not signed
	if id, ok := s.VerifyURL("/api/covers/a1", q); !ok || id != "dev1" {
		t.Fatal("signed url verify", id, ok)
	}
	if _, ok := s.VerifyURL("/api/covers/a2", q); ok {
		t.Fatal("other path accepted")
	}
}
