package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type MeetingUsageRecord struct {
	ID                string
	MeetingNumber     string
	JoinCode          string
	Title             string
	MeetingType       string
	HostParticipantID string
	HostUserID        string
	HostEmail         string
	HostNickname      string
	HostIPAddress     string
	CreatedAt         time.Time
	EndedAt           *time.Time
	UpdatedAt         time.Time
}

type MeetingParticipantUsageRecord struct {
	MeetingID         string
	ParticipantID     string
	UserID            string
	Email             string
	Nickname          string
	IsAnonymous       bool
	IPAddress         string
	DeviceType        string
	ClientProfileJSON string
	ParticipantRole   string
	JoinedAt          time.Time
	LeftAt            *time.Time
	UpdatedAt         time.Time
}

func (s *Store) UpsertMeetingUsage(ctx context.Context, record MeetingUsageRecord) error {
	const statement = `
INSERT INTO meeting_usage_meetings (
    id, meeting_number, join_code, title, meeting_type, host_participant_id,
    host_user_id, host_email, host_nickname, host_ip_address, created_at, ended_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    meeting_number = excluded.meeting_number,
    join_code = excluded.join_code,
    title = excluded.title,
    meeting_type = excluded.meeting_type,
    host_participant_id = excluded.host_participant_id,
    host_user_id = excluded.host_user_id,
    host_email = excluded.host_email,
    host_nickname = excluded.host_nickname,
    host_ip_address = excluded.host_ip_address,
    created_at = excluded.created_at,
    ended_at = COALESCE(excluded.ended_at, meeting_usage_meetings.ended_at),
    updated_at = excluded.updated_at`

	var endedAt any
	if record.EndedAt != nil {
		endedAt = record.EndedAt.UTC().Format(time.RFC3339Nano)
	}

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		record.ID,
		record.MeetingNumber,
		record.JoinCode,
		record.Title,
		record.MeetingType,
		record.HostParticipantID,
		record.HostUserID,
		record.HostEmail,
		record.HostNickname,
		record.HostIPAddress,
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
		endedAt,
		record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("upsert meeting usage: %w", err)
	}

	return nil
}

func (s *Store) UpdateMeetingUsageEndedAt(ctx context.Context, meetingID string, endedAt time.Time, updatedAt time.Time) error {
	const statement = `
UPDATE meeting_usage_meetings
SET ended_at = ?, updated_at = ?
WHERE id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		endedAt.UTC().Format(time.RFC3339Nano),
		updatedAt.UTC().Format(time.RFC3339Nano),
		meetingID,
	); err != nil {
		return fmt.Errorf("update meeting usage ended_at: %w", err)
	}

	return nil
}

func (s *Store) UpsertMeetingParticipantUsage(ctx context.Context, record MeetingParticipantUsageRecord) error {
	const statement = `
INSERT INTO meeting_usage_participants (
    meeting_id, participant_id, user_id, email, nickname, is_anonymous, ip_address,
    device_type, client_profile_json, participant_role, joined_at, left_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(meeting_id, participant_id) DO UPDATE SET
    user_id = excluded.user_id,
    email = excluded.email,
    nickname = excluded.nickname,
    is_anonymous = excluded.is_anonymous,
    ip_address = excluded.ip_address,
    device_type = excluded.device_type,
    client_profile_json = excluded.client_profile_json,
    participant_role = excluded.participant_role,
    joined_at = excluded.joined_at,
    left_at = COALESCE(excluded.left_at, meeting_usage_participants.left_at),
    updated_at = excluded.updated_at`

	var leftAt any
	if record.LeftAt != nil {
		leftAt = record.LeftAt.UTC().Format(time.RFC3339Nano)
	}

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		record.MeetingID,
		record.ParticipantID,
		record.UserID,
		record.Email,
		record.Nickname,
		boolToInteger(record.IsAnonymous),
		record.IPAddress,
		record.DeviceType,
		defaultJSON(record.ClientProfileJSON),
		record.ParticipantRole,
		record.JoinedAt.UTC().Format(time.RFC3339Nano),
		leftAt,
		record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("upsert meeting participant usage: %w", err)
	}

	return nil
}

func (s *Store) UpdateMeetingParticipantUsageLeftAt(ctx context.Context, meetingID string, participantID string, leftAt time.Time, updatedAt time.Time) error {
	const statement = `
UPDATE meeting_usage_participants
SET left_at = ?, updated_at = ?
WHERE meeting_id = ? AND participant_id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		leftAt.UTC().Format(time.RFC3339Nano),
		updatedAt.UTC().Format(time.RFC3339Nano),
		meetingID,
		participantID,
	); err != nil {
		return fmt.Errorf("update meeting participant usage left_at: %w", err)
	}

	return nil
}

func (s *Store) UpdateMeetingParticipantUsageNickname(ctx context.Context, meetingID string, participantID string, nickname string, updatedAt time.Time) error {
	const statement = `
UPDATE meeting_usage_participants
SET nickname = ?, updated_at = ?
WHERE meeting_id = ? AND participant_id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		nickname,
		updatedAt.UTC().Format(time.RFC3339Nano),
		meetingID,
		participantID,
	); err != nil {
		return fmt.Errorf("update meeting participant usage nickname: %w", err)
	}

	return nil
}

func (s *Store) UpdateMeetingParticipantUsageRole(ctx context.Context, meetingID string, participantID string, role string, updatedAt time.Time) error {
	const statement = `
UPDATE meeting_usage_participants
SET participant_role = ?, updated_at = ?
WHERE meeting_id = ? AND participant_id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		role,
		updatedAt.UTC().Format(time.RFC3339Nano),
		meetingID,
		participantID,
	); err != nil {
		return fmt.Errorf("update meeting participant usage role: %w", err)
	}

	return nil
}

func (s *Store) ListMeetingUsageWindow(ctx context.Context, start time.Time, end time.Time) ([]MeetingUsageRecord, error) {
	const query = `
SELECT id, meeting_number, join_code, title, meeting_type, host_participant_id,
       host_user_id, host_email, host_nickname, host_ip_address, created_at, ended_at, updated_at
FROM meeting_usage_meetings
WHERE created_at < ? AND (ended_at IS NULL OR ended_at >= ?)
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, end.UTC().Format(time.RFC3339Nano), start.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("list meeting usage window: %w", err)
	}
	defer rows.Close()

	records, err := scanMeetingUsageRecords(rows)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func (s *Store) ListParticipantUsageWindow(ctx context.Context, start time.Time, end time.Time) ([]MeetingParticipantUsageRecord, error) {
	const query = `
SELECT meeting_id, participant_id, user_id, email, nickname, is_anonymous, ip_address,
       device_type, client_profile_json, participant_role, joined_at, left_at, updated_at
FROM meeting_usage_participants
WHERE joined_at < ? AND (left_at IS NULL OR left_at >= ?)
ORDER BY joined_at ASC`

	rows, err := s.db.QueryContext(ctx, query, end.UTC().Format(time.RFC3339Nano), start.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("list participant usage window: %w", err)
	}
	defer rows.Close()

	records, err := scanParticipantUsageRecords(rows)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func (s *Store) ListParticipantsForMeetings(ctx context.Context, meetingIDs []string) ([]MeetingParticipantUsageRecord, error) {
	if len(meetingIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(meetingIDs))
	args := make([]any, len(meetingIDs))
	for index, meetingID := range meetingIDs {
		placeholders[index] = "?"
		args[index] = meetingID
	}

	query := fmt.Sprintf(`
SELECT meeting_id, participant_id, user_id, email, nickname, is_anonymous, ip_address,
       device_type, client_profile_json, participant_role, joined_at, left_at, updated_at
FROM meeting_usage_participants
WHERE meeting_id IN (%s)
ORDER BY meeting_id ASC, joined_at ASC`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list participants for meetings: %w", err)
	}
	defer rows.Close()

	records, err := scanParticipantUsageRecords(rows)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func scanMeetingUsageRecords(rows *sql.Rows) ([]MeetingUsageRecord, error) {
	var records []MeetingUsageRecord
	for rows.Next() {
		var (
			record       MeetingUsageRecord
			createdAtRaw string
			endedAtRaw   sql.NullString
			updatedAtRaw string
		)
		if err := rows.Scan(
			&record.ID,
			&record.MeetingNumber,
			&record.JoinCode,
			&record.Title,
			&record.MeetingType,
			&record.HostParticipantID,
			&record.HostUserID,
			&record.HostEmail,
			&record.HostNickname,
			&record.HostIPAddress,
			&createdAtRaw,
			&endedAtRaw,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan meeting usage: %w", err)
		}

		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse meeting usage created_at: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse meeting usage updated_at: %w", err)
		}
		record.CreatedAt = createdAt
		record.UpdatedAt = updatedAt
		if endedAtRaw.Valid && endedAtRaw.String != "" {
			endedAt, parseErr := time.Parse(time.RFC3339Nano, endedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse meeting usage ended_at: %w", parseErr)
			}
			record.EndedAt = &endedAt
		}

		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan meeting usage rows: %w", err)
	}

	return records, nil
}

func scanParticipantUsageRecords(rows *sql.Rows) ([]MeetingParticipantUsageRecord, error) {
	var records []MeetingParticipantUsageRecord
	for rows.Next() {
		var (
			record       MeetingParticipantUsageRecord
			isAnonymous  int
			joinedAtRaw  string
			leftAtRaw    sql.NullString
			updatedAtRaw string
		)
		if err := rows.Scan(
			&record.MeetingID,
			&record.ParticipantID,
			&record.UserID,
			&record.Email,
			&record.Nickname,
			&isAnonymous,
			&record.IPAddress,
			&record.DeviceType,
			&record.ClientProfileJSON,
			&record.ParticipantRole,
			&joinedAtRaw,
			&leftAtRaw,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan participant usage: %w", err)
		}

		joinedAt, err := time.Parse(time.RFC3339Nano, joinedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse participant usage joined_at: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse participant usage updated_at: %w", err)
		}
		record.JoinedAt = joinedAt
		record.UpdatedAt = updatedAt
		record.IsAnonymous = isAnonymous == 1
		if leftAtRaw.Valid && leftAtRaw.String != "" {
			leftAt, parseErr := time.Parse(time.RFC3339Nano, leftAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse participant usage left_at: %w", parseErr)
			}
			record.LeftAt = &leftAt
		}

		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan participant usage rows: %w", err)
	}

	return records, nil
}

func defaultJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	return value
}
