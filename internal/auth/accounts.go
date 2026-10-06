package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/idgen"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// Account limits and lifetimes (D35).
const (
	MinPasswordLength = 10
	MaxPasswordLength = 128

	SessionTTL     = 30 * 24 * time.Hour
	ResetTokenTTL  = 30 * time.Minute
	InviteTokenTTL = 72 * time.Hour

	// pbkdf2Iterations follows the OWASP guidance for PBKDF2-HMAC-SHA256.
	// PBKDF2 is in the standard library (no dependency) and, unlike a
	// memory-hard KDF, does not spike the memory of a small host on login.
	pbkdf2Iterations = 600_000
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

// Errors returned by Accounts; the HTTP layer maps them to status codes.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidToken       = errors.New("invalid or expired link")
	ErrUserExists         = sqlstore.ErrUserExists
	ErrUsersExist         = sqlstore.ErrUsersExist
	ErrNoSuchUser         = errors.New("no such account")
)

// ThrottledError means too many recent failed logins; RetryAfter says how long to wait.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("too many failed attempts, retry in %s", e.RetryAfter.Round(time.Second))
}

// ValidationError is a bad username or password; its message is safe to show.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// NormalizeUsername lowercases and trims a username.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidateUsername checks a normalized username.
func ValidateUsername(u string) error {
	if !usernamePattern.MatchString(u) {
		return &ValidationError{"username must be 3-32 characters: lowercase letters, digits, '.', '_' or '-', starting with a letter or digit"}
	}
	return nil
}

// ValidatePassword checks the password policy: length only (no composition
// rules), and not the username.
func ValidatePassword(password, username string) error {
	n := len([]rune(password))
	switch {
	case n < MinPasswordLength:
		return &ValidationError{fmt.Sprintf("password must be at least %d characters", MinPasswordLength)}
	case n > MaxPasswordLength:
		return &ValidationError{fmt.Sprintf("password must be at most %d characters", MaxPasswordLength)}
	case username != "" && strings.EqualFold(password, username):
		return &ValidationError{"password must not be the username"}
	}
	return nil
}

// HashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<hash>" (base64).
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return encodeHash(password, salt, pbkdf2Iterations)
}

func encodeHash(password string, salt []byte, iterations int) (string, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, pbkdf2KeyLength)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword checks password against a stored hash in constant time.
func VerifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// randomToken returns 32 random bytes, base64url-encoded.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// User is an account as exposed to callers (never the hash).
type User struct {
	ID          string
	Username    string
	HasPassword bool
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

func toUser(r sqlstore.UserRow) User {
	return User{ID: r.ID, Username: r.Username, HasPassword: r.PasswordHash != "", CreatedAt: r.CreatedAt, LastLoginAt: r.LastLoginAt}
}

// Accounts is the account service: onboarding, login, sessions and
// reset/invite links.
type Accounts struct {
	db  *sqlstore.DB
	now func() time.Time

	// dummyHash is verified when the username does not exist, so an unknown
	// user and a wrong password take about the same time (computed lazily:
	// hashing costs ~100 ms).
	dummyOnce sync.Once
	dummyHash string

	mu       sync.Mutex
	failures map[string]*failure
}

// NewAccounts builds the service on db.
func NewAccounts(db *sqlstore.DB) *Accounts {
	return &Accounts{db: db, now: time.Now, failures: map[string]*failure{}}
}

// HasAccounts reports whether at least one account exists.
func (a *Accounts) HasAccounts(ctx context.Context) (bool, error) {
	n, err := a.db.CountUsers(ctx)
	return n > 0, err
}

// Onboard creates the first account. It fails with ErrUsersExist when an
// account already exists. Callers must authenticate the request with an API
// key first.
func (a *Accounts) Onboard(ctx context.Context, username, password string) (User, error) {
	username = NormalizeUsername(username)
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password, username); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	now := a.now()
	row := sqlstore.UserRow{ID: idgen.New("usr"), Username: username, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := a.db.CreateFirstUser(ctx, row); err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// Invite creates an account without a password and returns the single-use
// token the owner opens to set one.
func (a *Accounts) Invite(ctx context.Context, username string) (User, string, time.Time, error) {
	username = NormalizeUsername(username)
	if err := ValidateUsername(username); err != nil {
		return User{}, "", time.Time{}, err
	}
	now := a.now()
	row := sqlstore.UserRow{ID: idgen.New("usr"), Username: username, CreatedAt: now, UpdatedAt: now}
	if err := a.db.CreateUser(ctx, row); err != nil {
		return User{}, "", time.Time{}, err
	}
	token, exp, err := a.issueToken(ctx, row.ID, sqlstore.TokenKindInvite, InviteTokenTTL)
	return toUser(row), token, exp, err
}

// StartReset issues a single-use password reset token for the account. Only
// the newest token of an account works.
func (a *Accounts) StartReset(ctx context.Context, username string) (User, string, time.Time, error) {
	u, err := a.db.UserByName(ctx, NormalizeUsername(username))
	if errors.Is(err, sqlstore.ErrNotFound) {
		return User{}, "", time.Time{}, ErrNoSuchUser
	}
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	token, exp, err := a.issueToken(ctx, u.ID, sqlstore.TokenKindReset, ResetTokenTTL)
	return toUser(u), token, exp, err
}

func (a *Accounts) issueToken(ctx context.Context, userID, kind string, ttl time.Duration) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := a.now()
	exp := now.Add(ttl)
	if err := a.db.ReplaceResetToken(ctx, userID, hashToken(token), kind, now, exp); err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// ResetInfo says whom a token is for, without consuming it (the reset page
// shows it before asking for the password).
func (a *Accounts) ResetInfo(ctx context.Context, token string) (User, string, error) {
	u, kind, err := a.db.PeekResetToken(ctx, hashToken(token), a.now())
	if errors.Is(err, sqlstore.ErrNotFound) {
		return User{}, "", ErrInvalidToken
	}
	return toUser(u), kind, err
}

// CompleteReset consumes the token, sets the new password and revokes every
// session of the account.
func (a *Accounts) CompleteReset(ctx context.Context, token, password string) (User, error) {
	peeked, _, err := a.db.PeekResetToken(ctx, hashToken(token), a.now())
	if errors.Is(err, sqlstore.ErrNotFound) {
		return User{}, ErrInvalidToken
	}
	if err != nil {
		return User{}, err
	}
	// Validate before consuming, so a too-short password does not burn the link.
	if err := ValidatePassword(password, peeked.Username); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u, _, err := a.db.ConsumeResetToken(ctx, hashToken(token), a.now())
	if errors.Is(err, sqlstore.ErrNotFound) {
		return User{}, ErrInvalidToken
	}
	if err != nil {
		return User{}, err
	}
	if err := a.db.SetPassword(ctx, u.ID, hash, a.now()); err != nil {
		return User{}, err
	}
	return toUser(u), nil
}

// Login verifies the credentials and opens a session, returning the raw
// session token (shown once, to be set as a cookie). ip and userAgent are
// used for throttling and display.
func (a *Accounts) Login(ctx context.Context, username, password, ip, userAgent string) (User, string, error) {
	username = NormalizeUsername(username)
	if wait := a.lockedFor(username, ip); wait > 0 {
		return User{}, "", &ThrottledError{RetryAfter: wait}
	}
	row, err := a.db.UserByName(ctx, username)
	hash := row.PasswordHash
	if err != nil || hash == "" {
		a.dummyOnce.Do(func() { a.dummyHash, _ = HashPassword("dummy-password-for-timing") })
		hash = a.dummyHash
	}
	ok := VerifyPassword(password, hash) && err == nil && row.PasswordHash != ""
	if !ok {
		a.recordFailure(username, ip)
		return User{}, "", ErrInvalidCredentials
	}
	a.clearFailures(username, ip)

	token, err := randomToken()
	if err != nil {
		return User{}, "", err
	}
	now := a.now()
	if err := a.db.InsertSession(ctx, sqlstore.SessionRow{
		TokenHash: hashToken(token), UserID: row.ID, CreatedAt: now, ExpiresAt: now.Add(SessionTTL),
		LastUsedAt: now, UserAgent: truncate(userAgent, 200),
	}); err != nil {
		return User{}, "", err
	}
	_ = a.db.TouchLogin(ctx, row.ID, now)
	_ = a.db.PurgeExpired(ctx, now)
	return toUser(row), token, nil
}

// SessionUser returns the account of a valid session token.
func (a *Accounts) SessionUser(ctx context.Context, token string) (User, bool) {
	if token == "" {
		return User{}, false
	}
	u, err := a.db.SessionUser(ctx, hashToken(token), a.now())
	if err != nil {
		return User{}, false
	}
	return toUser(u), true
}

// Logout revokes one session.
func (a *Accounts) Logout(ctx context.Context, token string) {
	if token != "" {
		_ = a.db.DeleteSession(ctx, hashToken(token))
	}
}

// List returns every account.
func (a *Accounts) List(ctx context.Context) ([]User, error) {
	rows, err := a.db.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, len(rows))
	for i, r := range rows {
		out[i] = toUser(r)
	}
	return out, nil
}

// Delete removes an account and its sessions.
func (a *Accounts) Delete(ctx context.Context, username string) error {
	u, err := a.db.UserByName(ctx, NormalizeUsername(username))
	if errors.Is(err, sqlstore.ErrNotFound) {
		return ErrNoSuchUser
	}
	if err != nil {
		return err
	}
	return a.db.DeleteUser(ctx, u.ID)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ── failed-login throttling ──
//
// After a few consecutive failures a username (and, separately, an address)
// is locked for a doubling delay. Unknown usernames are counted too, so the
// lock does not reveal which accounts exist. The table is bounded.

type failure struct {
	count int
	until time.Time
	last  time.Time
}

const (
	freeAttempts    = 5
	maxLock         = 15 * time.Minute
	maxFailureCache = 10_000
	failureMemory   = time.Hour
)

func failureKeys(username, ip string) []string {
	keys := []string{"u:" + username}
	if ip != "" {
		keys = append(keys, "i:"+ip)
	}
	return keys
}

func (a *Accounts) lockedFor(username, ip string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	var wait time.Duration
	for _, k := range failureKeys(username, ip) {
		if f, ok := a.failures[k]; ok && f.until.After(now) {
			if d := f.until.Sub(now); d > wait {
				wait = d
			}
		}
	}
	return wait
}

func (a *Accounts) recordFailure(username, ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if len(a.failures) >= maxFailureCache {
		for k, f := range a.failures {
			if now.Sub(f.last) > failureMemory {
				delete(a.failures, k)
			}
		}
		if len(a.failures) >= maxFailureCache {
			a.failures = map[string]*failure{}
		}
	}
	for _, k := range failureKeys(username, ip) {
		f := a.failures[k]
		if f == nil || now.Sub(f.last) > failureMemory {
			f = &failure{}
			a.failures[k] = f
		}
		f.count++
		f.last = now
		if f.count > freeAttempts {
			lock := time.Second << min(f.count-freeAttempts, 10)
			f.until = now.Add(min(lock, maxLock))
		}
	}
}

func (a *Accounts) clearFailures(username, ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range failureKeys(username, ip) {
		delete(a.failures, k)
	}
}
