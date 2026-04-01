package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type UserRecord struct {
	ID              string
	Email           string
	PasswordHash    string
	WechatOpenID    string
	Nickname        string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type VerificationCodeRecord struct {
	ID           string
	Email        string
	Purpose      string
	Nickname     string
	ClientID     string
	IPAddress    string
	CodeHash     string
	AttemptCount int
	SentAt       time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SessionRecord struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	UserAgent string
	IPAddress string
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (UserRecord, bool, error) {
	const query = `
SELECT id, email, password_hash, wechat_openid, nickname, email_verified_at, created_at, updated_at
FROM users
WHERE id = ?`

	var (
		record          UserRecord
		emailVerifiedAt sql.NullString
		createdAtRaw    string
		updatedAtRaw    string
	)

	if err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&record.ID,
		&record.Email,
		&record.PasswordHash,
		&record.WechatOpenID,
		&record.Nickname,
		&emailVerifiedAt,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return UserRecord{}, false, nil
		}
		return UserRecord{}, false, fmt.Errorf("get user by id: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return UserRecord{}, false, fmt.Errorf("parse user created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return UserRecord{}, false, fmt.Errorf("parse user updated_at: %w", err)
	}

	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if emailVerifiedAt.Valid && emailVerifiedAt.String != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, emailVerifiedAt.String)
		if parseErr != nil {
			return UserRecord{}, false, fmt.Errorf("parse user email_verified_at: %w", parseErr)
		}
		record.EmailVerifiedAt = &parsed
	}

	return record, true, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (UserRecord, bool, error) {
	const query = `
SELECT id, email, password_hash, wechat_openid, nickname, email_verified_at, created_at, updated_at
FROM users
WHERE email = ?`

	var (
		record          UserRecord
		emailVerifiedAt sql.NullString
		createdAtRaw    string
		updatedAtRaw    string
	)

	if err := s.db.QueryRowContext(ctx, query, email).Scan(
		&record.ID,
		&record.Email,
		&record.PasswordHash,
		&record.WechatOpenID,
		&record.Nickname,
		&emailVerifiedAt,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return UserRecord{}, false, nil
		}
		return UserRecord{}, false, fmt.Errorf("get user by email: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return UserRecord{}, false, fmt.Errorf("parse user created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return UserRecord{}, false, fmt.Errorf("parse user updated_at: %w", err)
	}

	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if emailVerifiedAt.Valid && emailVerifiedAt.String != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, emailVerifiedAt.String)
		if parseErr != nil {
			return UserRecord{}, false, fmt.Errorf("parse user email_verified_at: %w", parseErr)
		}
		record.EmailVerifiedAt = &parsed
	}

	return record, true, nil
}

func (s *Store) GetUserByWechatOpenID(ctx context.Context, wechatOpenID string) (UserRecord, bool, error) {
	const query = `
SELECT id, email, password_hash, wechat_openid, nickname, email_verified_at, created_at, updated_at
FROM users
WHERE wechat_openid = ?`

	var (
		record          UserRecord
		emailVerifiedAt sql.NullString
		createdAtRaw    string
		updatedAtRaw    string
	)

	if err := s.db.QueryRowContext(ctx, query, wechatOpenID).Scan(
		&record.ID,
		&record.Email,
		&record.PasswordHash,
		&record.WechatOpenID,
		&record.Nickname,
		&emailVerifiedAt,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return UserRecord{}, false, nil
		}
		return UserRecord{}, false, fmt.Errorf("get user by wechat openid: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return UserRecord{}, false, fmt.Errorf("parse user created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return UserRecord{}, false, fmt.Errorf("parse user updated_at: %w", err)
	}

	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if emailVerifiedAt.Valid && emailVerifiedAt.String != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, emailVerifiedAt.String)
		if parseErr != nil {
			return UserRecord{}, false, fmt.Errorf("parse user email_verified_at: %w", parseErr)
		}
		record.EmailVerifiedAt = &parsed
	}

	return record, true, nil
}

func (s *Store) CreateUser(ctx context.Context, user UserRecord) error {
	const statement = `
INSERT INTO users (
    id, email, password_hash, wechat_openid, nickname, email_verified_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	var emailVerifiedAt any
	if user.EmailVerifiedAt != nil {
		emailVerifiedAt = user.EmailVerifiedAt.UTC().Format(time.RFC3339Nano)
	}

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.WechatOpenID,
		user.Nickname,
		emailVerifiedAt,
		user.CreatedAt.UTC().Format(time.RFC3339Nano),
		user.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

func (s *Store) UpdateUserEmailVerification(ctx context.Context, userID string, emailVerifiedAt time.Time, nickname string, updatedAt time.Time) error {
	const statement = `
UPDATE users
SET email_verified_at = ?, nickname = COALESCE(NULLIF(?, ''), nickname), updated_at = ?
WHERE id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		emailVerifiedAt.UTC().Format(time.RFC3339Nano),
		nickname,
		updatedAt.UTC().Format(time.RFC3339Nano),
		userID,
	); err != nil {
		return fmt.Errorf("update user email verification: %w", err)
	}

	return nil
}

func (s *Store) UpsertVerificationCode(ctx context.Context, code VerificationCodeRecord) error {
	const statement = `
INSERT INTO auth_verification_codes (
    id, email, purpose, nickname, client_id, ip_address, code_hash, attempt_count, sent_at, expires_at, consumed_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var consumedAt any
	if code.ConsumedAt != nil {
		consumedAt = code.ConsumedAt.UTC().Format(time.RFC3339Nano)
	}

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		code.ID,
		code.Email,
		code.Purpose,
		code.Nickname,
		code.ClientID,
		code.IPAddress,
		code.CodeHash,
		code.AttemptCount,
		code.SentAt.UTC().Format(time.RFC3339Nano),
		code.ExpiresAt.UTC().Format(time.RFC3339Nano),
		consumedAt,
		code.CreatedAt.UTC().Format(time.RFC3339Nano),
		code.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert verification code: %w", err)
	}

	return nil
}

func (s *Store) GetLatestVerificationCode(ctx context.Context, email string, purpose string) (VerificationCodeRecord, bool, error) {
	const query = `
SELECT id, email, purpose, nickname, client_id, ip_address, code_hash, attempt_count, sent_at, expires_at, consumed_at, created_at, updated_at
FROM auth_verification_codes
WHERE email = ? AND purpose = ?
ORDER BY created_at DESC
LIMIT 1`

	var (
		record        VerificationCodeRecord
		sentAtRaw     string
		expiresAtRaw  string
		consumedAtRaw sql.NullString
		createdAtRaw  string
		updatedAtRaw  string
	)

	if err := s.db.QueryRowContext(ctx, query, email, purpose).Scan(
		&record.ID,
		&record.Email,
		&record.Purpose,
		&record.Nickname,
		&record.ClientID,
		&record.IPAddress,
		&record.CodeHash,
		&record.AttemptCount,
		&sentAtRaw,
		&expiresAtRaw,
		&consumedAtRaw,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return VerificationCodeRecord{}, false, nil
		}
		return VerificationCodeRecord{}, false, fmt.Errorf("get verification code: %w", err)
	}

	sentAt, err := time.Parse(time.RFC3339Nano, sentAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code sent_at: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code expires_at: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code updated_at: %w", err)
	}

	record.SentAt = sentAt
	record.ExpiresAt = expiresAt
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if consumedAtRaw.Valid && consumedAtRaw.String != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, consumedAtRaw.String)
		if parseErr != nil {
			return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code consumed_at: %w", parseErr)
		}
		record.ConsumedAt = &parsed
	}

	return record, true, nil
}

func (s *Store) GetLatestVerificationCodeByClientID(ctx context.Context, clientID string) (VerificationCodeRecord, bool, error) {
	const query = `
SELECT id, email, purpose, nickname, client_id, ip_address, code_hash, attempt_count, sent_at, expires_at, consumed_at, created_at, updated_at
FROM auth_verification_codes
WHERE client_id = ?
ORDER BY created_at DESC
LIMIT 1`

	var (
		record        VerificationCodeRecord
		sentAtRaw     string
		expiresAtRaw  string
		consumedAtRaw sql.NullString
		createdAtRaw  string
		updatedAtRaw  string
	)

	if err := s.db.QueryRowContext(ctx, query, clientID).Scan(
		&record.ID,
		&record.Email,
		&record.Purpose,
		&record.Nickname,
		&record.ClientID,
		&record.IPAddress,
		&record.CodeHash,
		&record.AttemptCount,
		&sentAtRaw,
		&expiresAtRaw,
		&consumedAtRaw,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return VerificationCodeRecord{}, false, nil
		}
		return VerificationCodeRecord{}, false, fmt.Errorf("get verification code by client id: %w", err)
	}

	sentAt, err := time.Parse(time.RFC3339Nano, sentAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code sent_at: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code expires_at: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code updated_at: %w", err)
	}

	record.SentAt = sentAt
	record.ExpiresAt = expiresAt
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if consumedAtRaw.Valid && consumedAtRaw.String != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, consumedAtRaw.String)
		if parseErr != nil {
			return VerificationCodeRecord{}, false, fmt.Errorf("parse verification code consumed_at: %w", parseErr)
		}
		record.ConsumedAt = &parsed
	}

	return record, true, nil
}

func (s *Store) CountVerificationCodesByIPAddressSince(ctx context.Context, ipAddress string, since time.Time) (int, error) {
	const query = `
SELECT COUNT(*)
FROM auth_verification_codes
WHERE ip_address = ? AND created_at >= ?`

	var count int
	if err := s.db.QueryRowContext(ctx, query, ipAddress, since.UTC().Format(time.RFC3339Nano)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count verification codes by ip address: %w", err)
	}

	return count, nil
}

func (s *Store) IncrementVerificationCodeAttempt(ctx context.Context, codeID string, updatedAt time.Time) error {
	const statement = `
UPDATE auth_verification_codes
SET attempt_count = attempt_count + 1, updated_at = ?
WHERE id = ?`

	if _, err := s.db.ExecContext(ctx, statement, updatedAt.UTC().Format(time.RFC3339Nano), codeID); err != nil {
		return fmt.Errorf("increment verification code attempts: %w", err)
	}

	return nil
}

func (s *Store) ConsumeVerificationCode(ctx context.Context, codeID string, consumedAt time.Time, updatedAt time.Time) error {
	const statement = `
UPDATE auth_verification_codes
SET consumed_at = ?, updated_at = ?
WHERE id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		consumedAt.UTC().Format(time.RFC3339Nano),
		updatedAt.UTC().Format(time.RFC3339Nano),
		codeID,
	); err != nil {
		return fmt.Errorf("consume verification code: %w", err)
	}

	return nil
}

func (s *Store) CreateSession(ctx context.Context, session SessionRecord) error {
	const statement = `
INSERT INTO auth_sessions (
    token_hash, user_id, created_at, expires_at, revoked_at, user_agent, ip_address
) VALUES (?, ?, ?, ?, ?, ?, ?)`

	var revokedAt any
	if session.RevokedAt != nil {
		revokedAt = session.RevokedAt.UTC().Format(time.RFC3339Nano)
	}

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		session.TokenHash,
		session.UserID,
		session.CreatedAt.UTC().Format(time.RFC3339Nano),
		session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		revokedAt,
		session.UserAgent,
		session.IPAddress,
	); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (SessionRecord, bool, error) {
	const query = `
SELECT token_hash, user_id, created_at, expires_at, revoked_at, user_agent, ip_address
FROM auth_sessions
WHERE token_hash = ?`

	var (
		record       SessionRecord
		createdAtRaw string
		expiresAtRaw string
		revokedAtRaw sql.NullString
	)

	if err := s.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&record.TokenHash,
		&record.UserID,
		&createdAtRaw,
		&expiresAtRaw,
		&revokedAtRaw,
		&record.UserAgent,
		&record.IPAddress,
	); err != nil {
		if err == sql.ErrNoRows {
			return SessionRecord{}, false, nil
		}
		return SessionRecord{}, false, fmt.Errorf("get session: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return SessionRecord{}, false, fmt.Errorf("parse session created_at: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil {
		return SessionRecord{}, false, fmt.Errorf("parse session expires_at: %w", err)
	}

	record.CreatedAt = createdAt
	record.ExpiresAt = expiresAt
	if revokedAtRaw.Valid && revokedAtRaw.String != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, revokedAtRaw.String)
		if parseErr != nil {
			return SessionRecord{}, false, fmt.Errorf("parse session revoked_at: %w", parseErr)
		}
		record.RevokedAt = &parsed
	}

	return record, true, nil
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash string, revokedAt time.Time) error {
	const statement = `
UPDATE auth_sessions
SET revoked_at = ?
WHERE token_hash = ?`

	if _, err := s.db.ExecContext(ctx, statement, revokedAt.UTC().Format(time.RFC3339Nano), tokenHash); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	return nil
}
