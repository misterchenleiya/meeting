package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) ListUsersCreatedWindow(ctx context.Context, start time.Time, end time.Time) ([]UserRegistrationRecord, error) {
	const query = `
SELECT u.email, u.nickname,
       COALESCE((
           SELECT vc.ip_address
           FROM auth_verification_codes vc
           WHERE vc.email = u.email
             AND vc.consumed_at IS NOT NULL
             AND vc.consumed_at <= u.created_at
             AND vc.purpose IN ('register', 'login')
           ORDER BY vc.consumed_at DESC
           LIMIT 1
       ), '') AS ip_address,
       u.created_at
FROM users u
WHERE u.email IS NOT NULL AND u.email <> '' AND u.created_at >= ? AND u.created_at < ?
ORDER BY u.created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("list users created window: %w", err)
	}
	defer rows.Close()

	records := []UserRegistrationRecord{}
	for rows.Next() {
		var (
			record       UserRegistrationRecord
			createdAtRaw string
		)
		if err := rows.Scan(&record.Email, &record.Nickname, &record.IPAddress, &createdAtRaw); err != nil {
			return nil, fmt.Errorf("scan created user: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse created user created_at: %w", err)
		}
		record.CreatedAt = createdAt
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate created users: %w", err)
	}

	return records, nil
}

func (s *Store) ListEmailCodeSendsWindow(ctx context.Context, start time.Time, end time.Time) ([]EmailCodeRecord, error) {
	const query = `
SELECT email, purpose, ip_address, sent_at, consumed_at
FROM auth_verification_codes
WHERE purpose IN ('login', 'register') AND sent_at >= ? AND sent_at < ?
ORDER BY sent_at ASC`

	rows, err := s.db.QueryContext(ctx, query, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("list email code sends window: %w", err)
	}
	defer rows.Close()

	records := []EmailCodeRecord{}
	for rows.Next() {
		var (
			record        EmailCodeRecord
			sentAtRaw     string
			consumedAtRaw sql.NullString
		)
		if err := rows.Scan(&record.Email, &record.Purpose, &record.IPAddress, &sentAtRaw, &consumedAtRaw); err != nil {
			return nil, fmt.Errorf("scan email code send: %w", err)
		}
		sentAt, err := time.Parse(time.RFC3339Nano, sentAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse email code send sent_at: %w", err)
		}
		record.SentAt = sentAt
		if consumedAtRaw.Valid && consumedAtRaw.String != "" {
			consumedAt, parseErr := time.Parse(time.RFC3339Nano, consumedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse email code send consumed_at: %w", parseErr)
			}
			record.ConsumedAt = &consumedAt
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate email code sends: %w", err)
	}

	return records, nil
}
