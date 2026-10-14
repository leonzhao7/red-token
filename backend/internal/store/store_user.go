package store

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"red-token/internal/domain"
)

const (
	// DefaultUsername and DefaultPassword seed the first account so a fresh
	// deployment is reachable. The password should be changed after first login.
	DefaultUsername = "admin"
	DefaultPassword = "admin"

	// SessionTTL bounds how long a login stays valid.
	SessionTTL = 7 * 24 * time.Hour

	pbkdf2Iterations = 210000
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
	pbkdf2Prefix     = "pbkdf2_sha256"
)

var ErrUsernameTaken = errors.New("username already exists")

// HashPassword derives a salted PBKDF2-SHA256 hash encoded as
// "pbkdf2_sha256$<iterations>$<salt>$<key>" with base64 raw-std segments.
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		pbkdf2Prefix,
		strconv.Itoa(pbkdf2Iterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	}, "$"), nil
}

// VerifyPassword reports whether password matches the stored hash. It always
// performs a constant-time comparison on well-formed hashes.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(strings.TrimSpace(encoded), "$")
	if len(parts) != 4 || parts[0] != pbkdf2Prefix {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// EnsureDefaultUser seeds admin/admin when no account exists yet.
func (s *Store) EnsureDefaultUser(ctx context.Context) error {
	total, err := s.CountUsers(ctx)
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	hash, err := HashPassword(DefaultPassword)
	if err != nil {
		return err
	}
	_, err = s.CreateUser(ctx, DefaultUsername, hash, true)
	return err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	return countRows(ctx, s.db, "users")
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, enabled, last_login_at, created_at, updated_at
		FROM users
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []domain.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id int64) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, enabled, last_login_at, created_at, updated_at
		FROM users
		WHERE id = ?
	`, id)
	return scanUser(row)
}

// FindUserByUsername returns nil when no account matches.
func (s *Store) FindUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, enabled, last_login_at, created_at, updated_at
		FROM users
		WHERE username = ?
	`, strings.TrimSpace(username))

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, enabled bool) (domain.User, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO users(username, password_hash, enabled, last_login_at, created_at, updated_at)
		VALUES(?, ?, ?, '', ?, ?)
	`, strings.TrimSpace(username), passwordHash, boolToInt(enabled), formatTime(now), formatTime(now))
	if err != nil {
		if isUniqueConstraintErr(err) {
			return domain.User{}, ErrUsernameTaken
		}
		return domain.User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.User{}, err
	}
	return s.GetUser(ctx, id)
}

// UpdateUser changes the username and enabled flag. Passwords are managed by
// SetUserPassword so a hash is never rewritten by accident.
func (s *Store) UpdateUser(ctx context.Context, id int64, username string, enabled bool) (domain.User, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET username = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, strings.TrimSpace(username), boolToInt(enabled), formatTime(now), id)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return domain.User{}, ErrUsernameTaken
		}
		return domain.User{}, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) SetUserPassword(ctx context.Context, id int64, passwordHash string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, updated_at = ?
		WHERE id = ?
	`, passwordHash, formatTime(now), id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) TouchUserLogin(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, formatTime(now), id)
	return err
}

// CreateSession issues an opaque session token. Only its hash is persisted, so
// a database leak cannot be replayed as a live session.
func (s *Store) CreateSession(ctx context.Context, userID int64) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC()
	expiresAt := now.Add(SessionTTL)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_sessions(token_hash, user_id, created_at, expires_at)
		VALUES(?, ?, ?, ?)
	`, HashToken(token), userID, formatTime(now), formatTime(expiresAt)); err != nil {
		return "", time.Time{}, err
	}
	// Opportunistically drop rows that can no longer authenticate anyone.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE expires_at <= ?`, formatTime(now))
	return token, expiresAt, nil
}

// FindUserBySession resolves a session token to its enabled owner. It returns
// nil when the token is unknown, expired, or the account is disabled.
func (s *Store) FindUserBySession(ctx context.Context, token string) (*domain.User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.enabled, u.last_login_at, u.created_at, u.updated_at
		FROM user_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.enabled = 1
	`, HashToken(token), formatTime(time.Now().UTC()))

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE token_hash = ?`, HashToken(strings.TrimSpace(token)))
	return err
}

// DeleteUserSessions revokes every session owned by a user, used after a
// password change or when an account is disabled.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE user_id = ?`, userID)
	return err
}

func scanUser(s scanner) (domain.User, error) {
	var (
		user                              domain.User
		enabled                           int
		lastLoginAt, createdAt, updatedAt string
	)
	err := s.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&enabled,
		&lastLoginAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return domain.User{}, err
	}
	user.Enabled = enabled == 1
	user.LastLoginAt = parseOptionalTime(lastLoginAt)
	user.CreatedAt = parseTime(createdAt)
	user.UpdatedAt = parseTime(updatedAt)
	return user, nil
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
