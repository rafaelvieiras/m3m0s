package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

func newAccounts(t *testing.T) (*Accounts, *time.Time) {
	t.Helper()
	db, err := sqlstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := NewAccounts(db)
	clock := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }
	return a, &clock
}

func TestPasswordHashRoundTripAndFormat(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$600000$") {
		t.Fatalf("unexpected hash format: %s", h)
	}
	if !VerifyPassword("correct horse battery", h) || VerifyPassword("wrong", h) {
		t.Fatal("verify mismatch")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("hashes of the same password must differ (random salt)")
	}
	for _, bad := range []string{"", "x", "pbkdf2-sha256$0$a$b", "bcrypt$1$a$b", "pbkdf2-sha256$1000$!!$!!"} {
		if VerifyPassword("x", bad) {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
}

func TestValidation(t *testing.T) {
	for _, u := range []string{"ab", "Has Space", "-lead", strings.Repeat("a", 33), "ünï"} {
		if ValidateUsername(u) == nil {
			t.Errorf("username %q accepted", u)
		}
	}
	for _, u := range []string{"abc", "rafael", "a.b-c_d9"} {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("username %q rejected: %v", u, err)
		}
	}
	if ValidatePassword("short", "u") == nil || ValidatePassword(strings.Repeat("x", 129), "u") == nil {
		t.Error("length policy not enforced")
	}
	if ValidatePassword("samename-user", "samename-user") == nil {
		t.Error("password equal to the username accepted")
	}
	if err := ValidatePassword("a-long-enough-pass", "u"); err != nil {
		t.Error(err)
	}
}

func TestOnboardOnlyOnceAndLogin(t *testing.T) {
	a, _ := newAccounts(t)
	ctx := context.Background()
	if has, _ := a.HasAccounts(ctx); has {
		t.Fatal("fresh instance must have no accounts")
	}
	if _, err := a.Onboard(ctx, "Admin", "a-long-enough-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Onboard(ctx, "other", "a-long-enough-pass"); !errors.Is(err, ErrUsersExist) {
		t.Fatalf("second onboarding: %v", err)
	}
	if _, _, err := a.Login(ctx, "admin", "wrong-password-x", "1.1.1.1", "ua"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := a.Login(ctx, "nobody", "a-long-enough-pass", "1.1.1.1", "ua"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user must look like a wrong password: %v", err)
	}
	u, token, err := a.Login(ctx, " ADMIN ", "a-long-enough-pass", "1.1.1.1", "ua")
	if err != nil || u.Username != "admin" || token == "" {
		t.Fatalf("login: %v %+v", err, u)
	}
	if got, ok := a.SessionUser(ctx, token); !ok || got.Username != "admin" {
		t.Fatal("session not valid after login")
	}
	a.Logout(ctx, token)
	if _, ok := a.SessionUser(ctx, token); ok {
		t.Fatal("session valid after logout")
	}
}

func TestSessionExpires(t *testing.T) {
	a, clock := newAccounts(t)
	ctx := context.Background()
	_, _ = a.Onboard(ctx, "admin", "a-long-enough-pass")
	_, token, _ := a.Login(ctx, "admin", "a-long-enough-pass", "", "")
	*clock = clock.Add(SessionTTL + time.Minute)
	if _, ok := a.SessionUser(ctx, token); ok {
		t.Fatal("expired session accepted")
	}
}

func TestLoginThrottleDoublesAndResetsOnSuccess(t *testing.T) {
	a, clock := newAccounts(t)
	ctx := context.Background()
	_, _ = a.Onboard(ctx, "admin", "a-long-enough-pass")
	for i := 0; i < freeAttempts; i++ {
		if _, _, err := a.Login(ctx, "admin", "bad-password-xx", "9.9.9.9", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// the next failure trips the lock
	_, _, _ = a.Login(ctx, "admin", "bad-password-xx", "9.9.9.9", "")
	var terr *ThrottledError
	if _, _, err := a.Login(ctx, "admin", "a-long-enough-pass", "9.9.9.9", ""); !errors.As(err, &terr) || terr.RetryAfter <= 0 {
		t.Fatalf("expected throttle even with the right password: %v", err)
	}
	// a different address and username is not blocked by this one's failures
	if _, _, err := a.Login(ctx, "admin", "a-long-enough-pass", "8.8.8.8", ""); !errors.As(err, &terr) {
		t.Fatalf("username lock must hold across addresses: %v", err)
	}
	*clock = clock.Add(maxLock + time.Second)
	if _, _, err := a.Login(ctx, "admin", "a-long-enough-pass", "9.9.9.9", ""); err != nil {
		t.Fatalf("after the lock expires: %v", err)
	}
}

func TestResetFlowSingleUseExpiryAndSessionRevocation(t *testing.T) {
	a, clock := newAccounts(t)
	ctx := context.Background()
	_, _ = a.Onboard(ctx, "admin", "a-long-enough-pass")
	_, session, _ := a.Login(ctx, "admin", "a-long-enough-pass", "", "")

	if _, _, _, err := a.StartReset(ctx, "ghost"); !errors.Is(err, ErrNoSuchUser) {
		t.Fatalf("reset of unknown user: %v", err)
	}
	_, first, _, err := a.StartReset(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, second, _, _ := a.StartReset(ctx, "admin")
	if _, _, err := a.ResetInfo(ctx, first); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("an older link must stop working when a newer one is issued")
	}
	if u, kind, err := a.ResetInfo(ctx, second); err != nil || u.Username != "admin" || kind != "reset" {
		t.Fatalf("ResetInfo: %v %+v %s", err, u, kind)
	}
	// a too-short password must not burn the link
	if _, err := a.CompleteReset(ctx, second, "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := a.CompleteReset(ctx, second, "a-brand-new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CompleteReset(ctx, second, "another-new-password"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("link reused: %v", err)
	}
	if _, ok := a.SessionUser(ctx, session); ok {
		t.Fatal("old session survived the password reset")
	}
	if _, _, err := a.Login(ctx, "admin", "a-long-enough-pass", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password still works")
	}
	if _, _, err := a.Login(ctx, "admin", "a-brand-new-password", "", ""); err != nil {
		t.Fatal(err)
	}

	_, tok, _, _ := a.StartReset(ctx, "admin")
	*clock = clock.Add(ResetTokenTTL + time.Second)
	if _, err := a.CompleteReset(ctx, tok, "yet-another-password"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired link accepted: %v", err)
	}
}

func TestInviteCreatesPasswordlessAccountThatCannotLogInUntilSet(t *testing.T) {
	a, _ := newAccounts(t)
	ctx := context.Background()
	u, token, _, err := a.Invite(ctx, "alice")
	if err != nil || u.HasPassword {
		t.Fatalf("invite: %v %+v", err, u)
	}
	if _, _, _, err := a.Invite(ctx, "alice"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate invite: %v", err)
	}
	if _, _, err := a.Login(ctx, "alice", "", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("passwordless account must not accept an empty password")
	}
	if _, kind, _ := a.ResetInfo(ctx, token); kind != "invite" {
		t.Fatalf("kind = %q", kind)
	}
	if _, err := a.CompleteReset(ctx, token, "alices-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Login(ctx, "alice", "alices-long-password", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, "alice"); !errors.Is(err, ErrNoSuchUser) {
		t.Fatalf("second delete: %v", err)
	}
}
