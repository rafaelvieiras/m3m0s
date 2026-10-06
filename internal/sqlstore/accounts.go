package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUserExists is returned when the username is already taken.
var ErrUserExists = errors.New("username already exists")

// ErrUsersExist is returned by CreateFirstUser when an account already exists.
var ErrUsersExist = errors.New("an account already exists")

// ErrNotFound is returned when a user, session or token does not exist (or,
// for tokens, is expired or already used).
var ErrNotFound = errors.New("not found")

// UserRow is one account. PasswordHash is empty until the owner sets a password.
type UserRow struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

// SessionRow is a server-side browser session (the raw token is never stored).
type SessionRow struct {
	TokenHash  string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	UserAgent  string
}

// Reset token kinds.
const (
	TokenKindReset  = "reset"
	TokenKindInvite = "invite"
)

func ts(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTS(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CountUsers returns how many accounts exist.
func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateUser inserts an account.
func (db *DB) CreateUser(ctx context.Context, u UserRow) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, NULLIF(?, ''), ?, ?)`,
		u.ID, u.Username, u.PasswordHash, ts(u.CreatedAt), ts(u.UpdatedAt))
	if isUnique(err) {
		return ErrUserExists
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// CreateFirstUser inserts the account only if the table is empty, in one
// statement, so two simultaneous onboardings cannot both win.
func (db *DB) CreateFirstUser(ctx context.Context, u UserRow) error {
	res, err := db.sql.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, created_at, updated_at)
		SELECT ?, ?, NULLIF(?, ''), ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		u.ID, u.Username, u.PasswordHash, ts(u.CreatedAt), ts(u.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create first user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUsersExist
	}
	return nil
}

const userCols = `id, username, COALESCE(password_hash, ''), created_at, updated_at, last_login_at`

func scanUser(scan func(...any) error) (UserRow, error) {
	var u UserRow
	var created, updated string
	var last sql.NullString
	if err := scan(&u.ID, &u.Username, &u.PasswordHash, &created, &updated, &last); err != nil {
		return UserRow{}, err
	}
	var err error
	if u.CreatedAt, err = parseTS(created); err != nil {
		return UserRow{}, err
	}
	if u.UpdatedAt, err = parseTS(updated); err != nil {
		return UserRow{}, err
	}
	if last.Valid {
		t, err := parseTS(last.String)
		if err != nil {
			return UserRow{}, err
		}
		u.LastLoginAt = &t
	}
	return u, nil
}

// UserByName returns the account with that (already normalized) username.
func (db *DB) UserByName(ctx context.Context, username string) (UserRow, error) {
	u, err := scanUser(db.sql.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, username).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return UserRow{}, ErrNotFound
	}
	return u, err
}

// UserByID returns the account with that id.
func (db *DB) UserByID(ctx context.Context, id string) (UserRow, error) {
	u, err := scanUser(db.sql.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return UserRow{}, ErrNotFound
	}
	return u, err
}

// ListUsers returns every account, oldest first.
func (db *DB) ListUsers(ctx context.Context) ([]UserRow, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at, username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UserRow
	for rows.Next() {
		u, err := scanUser(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser removes an account; its sessions and tokens go with it.
func (db *DB) DeleteUser(ctx context.Context, id string) error {
	res, err := db.sql.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPassword stores a new password hash and revokes every session of the
// account (a password change must sign the account out everywhere).
func (db *DB) SetPassword(ctx context.Context, userID, hash string, now time.Time) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, ts(now), userID)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return tx.Commit()
}

// TouchLogin records the last successful login.
func (db *DB) TouchLogin(ctx context.Context, userID string, now time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, ts(now), userID)
	return err
}

// InsertSession stores a session.
func (db *DB) InsertSession(ctx context.Context, s SessionRow) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_used_at, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		s.TokenHash, s.UserID, ts(s.CreatedAt), ts(s.ExpiresAt), ts(s.LastUsedAt), s.UserAgent)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// SessionUser returns the unexpired session with that token hash and its
// account, refreshing last_used_at.
func (db *DB) SessionUser(ctx context.Context, tokenHash string, now time.Time) (UserRow, error) {
	var userID string
	err := db.sql.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, ts(now)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return UserRow{}, ErrNotFound
	}
	if err != nil {
		return UserRow{}, err
	}
	_, _ = db.sql.ExecContext(ctx, `UPDATE sessions SET last_used_at = ? WHERE token_hash = ?`, ts(now), tokenHash)
	return db.UserByID(ctx, userID)
}

// DeleteSession revokes one session.
func (db *DB) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// PurgeExpired removes expired sessions and expired or used tokens.
func (db *DB) PurgeExpired(ctx context.Context, now time.Time) error {
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, ts(now)); err != nil {
		return err
	}
	_, err := db.sql.ExecContext(ctx, `DELETE FROM reset_tokens WHERE expires_at <= ? OR used_at IS NOT NULL`, ts(now))
	return err
}

// ReplaceResetToken stores a token for the user, dropping that user's other
// unused tokens first so only the newest link works.
func (db *DB) ReplaceResetToken(ctx context.Context, userID, tokenHash, kind string, now, expires time.Time) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM reset_tokens WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reset_tokens (token_hash, user_id, kind, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, kind, ts(now), ts(expires)); err != nil {
		return fmt.Errorf("insert reset token: %w", err)
	}
	return tx.Commit()
}

// PeekResetToken returns the account and kind of a valid (unused, unexpired)
// token without consuming it.
func (db *DB) PeekResetToken(ctx context.Context, tokenHash string, now time.Time) (UserRow, string, error) {
	var userID, kind string
	err := db.sql.QueryRowContext(ctx, `SELECT user_id, kind FROM reset_tokens WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`, tokenHash, ts(now)).Scan(&userID, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return UserRow{}, "", ErrNotFound
	}
	if err != nil {
		return UserRow{}, "", err
	}
	u, err := db.UserByID(ctx, userID)
	return u, kind, err
}

// ConsumeResetToken marks the token used and returns its account, atomically:
// the second of two simultaneous redemptions gets ErrNotFound.
func (db *DB) ConsumeResetToken(ctx context.Context, tokenHash string, now time.Time) (UserRow, string, error) {
	res, err := db.sql.ExecContext(ctx, `UPDATE reset_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`, ts(now), tokenHash, ts(now))
	if err != nil {
		return UserRow{}, "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return UserRow{}, "", ErrNotFound
	}
	var userID, kind string
	if err := db.sql.QueryRowContext(ctx, `SELECT user_id, kind FROM reset_tokens WHERE token_hash = ?`, tokenHash).Scan(&userID, &kind); err != nil {
		return UserRow{}, "", err
	}
	u, err := db.UserByID(ctx, userID)
	return u, kind, err
}
