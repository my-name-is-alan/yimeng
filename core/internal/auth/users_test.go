package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const good = "Tr0ub4dor&3x"

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword(good)
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal(h, err)
	}
	if !CheckPassword(h, good) || CheckPassword(h, good+"x") || CheckPassword("garbage", good) {
		t.Fatal("check")
	}
	h2, _ := HashPassword(good)
	if h == h2 {
		t.Fatal("salt must differ")
	}
}

func TestStrength(t *testing.T) {
	for pw, ok := range map[string]bool{
		"short1A!": false, "password": false, "aaaaaaaaaaaaaaaaaaaa": false, "alllowercase": false,
		"admin-Secret-99": false, // contains username
		good:              true, "correct horse battery staple": true, "密码安全测试Ab12": true,
	} {
		if err := CheckStrength("admin", pw); (err == nil) != ok {
			t.Errorf("%q: %v", pw, err)
		}
	}
}

func TestSetupLoginLockout(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if n, _ := s.UserCount(ctx); n != 0 {
		t.Fatal(n)
	}
	if _, err := s.CreateUser(ctx, "edy", "weak", RoleAdmin, true); !errors.Is(err, ErrWeakPassword) {
		t.Fatal(err)
	}
	u, err := s.CreateUser(ctx, "edy", good, RoleUser, true)
	if err != nil || u.Role != RoleAdmin {
		t.Fatal(u, err)
	}
	if _, err := s.CreateUser(ctx, "other", good, RoleAdmin, true); !errors.Is(err, ErrSetupDone) {
		t.Fatal("second setup must fail", err)
	}
	dev, _, tok, err := s.Login(ctx, "1.1.1.1", "EDY", good, "Chrome", "")
	if err != nil || !dev.Admin || dev.UserID != u.ID {
		t.Fatal(dev, err)
	}
	got, err := s.Authenticate(ctx, tok)
	if err != nil || got.Username != "edy" || !got.Admin {
		t.Fatal(got, err)
	}
	// 5 failures for one username lock it, even from different IPs
	for i := 0; i < 5; i++ {
		if _, _, _, err := s.Login(ctx, "2.2.2."+string(rune('0'+i)), "edy", "wrong-Password1", "", ""); !errors.Is(err, ErrBadCredentials) {
			t.Fatal(i, err)
		}
	}
	if _, _, _, err := s.Login(ctx, "3.3.3.3", "edy", good, "", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected lockout", err)
	}
}

func TestUsersAndPasswords(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	admin, _ := s.CreateUser(ctx, "root", good, RoleAdmin, true)
	d1, _, tok1, _ := s.Login(ctx, "1.1.1.1", "root", good, "", "")
	_, _, tok2, _ := s.Login(ctx, "1.1.1.1", "root", good, "", "")
	if err := s.ChangePassword(ctx, admin.ID, "nope", "N3w-passw0rd!", d1.ID); !errors.Is(err, ErrBadCredentials) {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, admin.ID, good, "N3w-passw0rd!", d1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok1); err != nil {
		t.Fatal("current session must survive")
	}
	if _, err := s.Authenticate(ctx, tok2); err == nil {
		t.Fatal("other sessions must be revoked")
	}
	if err := s.SetRole(ctx, admin.ID, RoleUser); !errors.Is(err, ErrLastAdmin) {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "bob", "B0b-likes-music", RoleUser, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "BOB", good, RoleUser, false); !errors.Is(err, ErrUserExists) {
		t.Fatal(err)
	}
	bd, _, btok, _ := s.Login(ctx, "1.1.1.1", "bob", "B0b-likes-music", "", "")
	if bd.Admin {
		t.Fatal("user role must not be admin")
	}
	if err := s.SetRole(ctx, bob.ID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Authenticate(ctx, btok); !d.Admin {
		t.Fatal("role change must apply to sessions")
	}
	if err := s.DeleteUser(ctx, bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, btok); err == nil {
		t.Fatal("deleted user's session must die")
	}
	// CLI reset: existing user → new password, sessions revoked
	if created, err := s.ResetPassword(ctx, "root", "Reset-Passw0rd"); err != nil || created {
		t.Fatal(created, err)
	}
	if _, err := s.Authenticate(ctx, tok1); err == nil {
		t.Fatal("reset must revoke sessions")
	}
	if _, _, _, err := s.Login(ctx, "9.9.9.9", "root", "Reset-Passw0rd", "", ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.RemoveAllUsers(ctx); n != 1 {
		t.Fatal(n)
	}
	if c, _ := s.UserCount(ctx); c != 0 {
		t.Fatal("setup should be available again")
	}
}
