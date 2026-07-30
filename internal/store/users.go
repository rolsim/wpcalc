package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/rolsim/wpcalc/internal/domain"
)

// ErrDuplicateUsername is returned when a username is already taken.
var ErrDuplicateUsername = errors.New("username already exists")

// CreateUser adds an account, hashing the password with bcrypt.
func (db *DB) CreateUser(ctx context.Context, username, password, role string) (int64, error) {
	return db.CreateUserWeak(ctx, username, password, role, false)
}

// CreateUserWeak is CreateUser with the option to skip the password length
// requirement.
//
// The exception exists only so a local development database can be primed with
// throwaway credentials. It is a separate, explicitly named entry point rather
// than a lower global minimum, so that every caller that waives the rule is
// greppable and no ordinary call site can waive it by accident.
func (db *DB) CreateUserWeak(ctx context.Context, username, password, role string, allowWeak bool) (int64, error) {
	username = strings.TrimSpace(username)
	if err := domain.ValidUsername(username); err != nil {
		return 0, err
	}
	if err := domain.ValidRole(role); err != nil {
		return 0, err
	}
	if password == "" {
		return 0, fmt.Errorf("%w: password is required", domain.ErrInvalidUser)
	}
	if !allowWeak {
		if err := domain.ValidPassword(password); err != nil {
			return 0, err
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("store: hash password: %w", err)
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
		username, string(hash), role)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("store: create user %q: %w", username, ErrDuplicateUsername)
		}
		return 0, fmt.Errorf("store: create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: create user: %w", err)
	}
	return id, nil
}

// SetPassword replaces an account's password.
func (db *DB) SetPassword(ctx context.Context, username, password string) error {
	return db.SetPasswordWeak(ctx, username, password, false)
}

// SetPasswordWeak is SetPassword with the same explicit length waiver as
// CreateUserWeak.
func (db *DB) SetPasswordWeak(ctx context.Context, username, password string, allowWeak bool) error {
	if password == "" {
		return fmt.Errorf("%w: password is required", domain.ErrInvalidUser)
	}
	if !allowWeak {
		if err := domain.ValidPassword(password); err != nil {
			return err
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("store: hash password: %w", err)
	}

	res, err := db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = datetime('now') WHERE username = ?`,
		string(hash), strings.TrimSpace(username))
	if err != nil {
		return fmt.Errorf("store: set password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set password: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: set password for %q: %w", username, ErrNotFound)
	}

	// Changing a password revokes every existing session for that account.
	// Otherwise a password change in response to a suspected compromise would
	// leave the attacker's session working.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM sessions
		  WHERE user_id IN (SELECT id FROM users WHERE username = ?)`,
		strings.TrimSpace(username)); err != nil {
		return fmt.Errorf("store: revoke sessions: %w", err)
	}
	return nil
}

// SetUserLanguage stores an interface language preference, or clears it back
// to "follow the browser" when lang is domain.LanguageAuto.
//
// The value is not checked against the shipped catalogs: that belongs to the
// caller, which knows which are loaded, and the read path falls back anyway.
// What is checked is the shape, so a stray path or a whole HTTP header cannot
// end up in the column.
func (db *DB) SetUserLanguage(ctx context.Context, userID int64, lang string) error {
	lang = strings.TrimSpace(lang)
	if len(lang) > 35 { // BCP 47 tags are well under this
		return fmt.Errorf("%w: language tag is too long", domain.ErrInvalidUser)
	}
	if strings.ContainsAny(lang, " \t\n\r/\\") {
		return fmt.Errorf("%w: %q is not a language tag", domain.ErrInvalidUser, lang)
	}

	res, err := db.ExecContext(ctx,
		`UPDATE users SET language = ?, updated_at = datetime('now') WHERE id = ?`,
		lang, userID)
	if err != nil {
		return fmt.Errorf("store: set language: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set language: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: set language for user %d: %w", userID, ErrNotFound)
	}
	return nil
}

// Authenticate verifies a username and password.
//
// It always runs a bcrypt comparison, even when the user does not exist, so
// that a missing account and a wrong password take the same time. Returning
// early on an unknown username turns the login form into a user enumerator.
func (db *DB) Authenticate(ctx context.Context, username, password string) (domain.User, error) {
	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// A real hash of a dummy password, to burn the same time.
			_ = bcrypt.CompareHashAndPassword(
				[]byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"),
				[]byte(password))
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return domain.User{}, ErrNotFound
	}
	return u, nil
}

// UserByUsername looks up an account. Usernames compare case-insensitively.
func (db *DB) UserByUsername(ctx context.Context, username string) (domain.User, error) {
	var u domain.User
	err := db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, language
		   FROM users WHERE username = ? COLLATE NOCASE`,
		strings.TrimSpace(username)).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Language)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("store: user %q: %w", username, ErrNotFound)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("store: user %q: %w", username, err)
	}
	return u, nil
}

// Users lists every account, without password hashes.
func (db *DB) Users(ctx context.Context) ([]domain.User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, role, language FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Language); err != nil {
			return nil, fmt.Errorf("store: list users: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	return out, nil
}

// HasUsers reports whether any account exists, so the server can tell the
// operator to create the first one rather than silently refusing every login.
func (db *DB) HasUsers(ctx context.Context) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, fmt.Errorf("store: count users: %w", err)
	}
	return n > 0, nil
}

// CreateSession records a session token.
func (db *DB) CreateSession(ctx context.Context, token string, userID int64, expires time.Time) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expires.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// SessionUser resolves a session token to its account, rejecting expired ones.
func (db *DB) SessionUser(ctx context.Context, token string) (domain.User, error) {
	var (
		u       domain.User
		expires string
	)
	err := db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.role, u.language, s.expires_at
		   FROM sessions s
		   JOIN users u ON u.id = s.user_id
		  WHERE s.token = ?`, token).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Language, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("store: session lookup: %w", err)
	}

	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return domain.User{}, fmt.Errorf("store: session expiry: %w", err)
	}
	if time.Now().After(exp) {
		// Clean up on the way past rather than needing a sweeper.
		_ = db.DeleteSession(ctx, token)
		return domain.User{}, ErrNotFound
	}
	return u, nil
}

// DeleteSession revokes one session.
func (db *DB) DeleteSession(ctx context.Context, token string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return nil
}

// PurgeExpiredSessions removes stale rows.
func (db *DB) PurgeExpiredSessions(ctx context.Context) error {
	_, err := db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: purge sessions: %w", err)
	}
	return nil
}

// isUniqueViolation recognises SQLite's uniqueness error without depending on
// the driver's concrete error type.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT FAILED")
}
