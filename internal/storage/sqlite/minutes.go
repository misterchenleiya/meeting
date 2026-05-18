package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type TranscriptSegmentRecord struct {
	ID            string
	MeetingID     string
	MeetingNumber string
	ParticipantID string
	UserID        string
	Nickname      string
	Language      string
	Sequence      int64
	StartedAt     time.Time
	EndedAt       time.Time
	Text          string
	IsFinal       bool
	ASRProvider   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type MinutesJobRecord struct {
	ID                       string
	MeetingID                string
	MeetingNumber            string
	RequestedByUserID        string
	RequestedByParticipantID string
	Status                   string
	ErrorMessage             string
	EmailError               string
	StartedAt                *time.Time
	CompletedAt              *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type MeetingMinutesRecord struct {
	ID              string
	JobID           string
	MeetingID       string
	MeetingNumber   string
	HostUserID      string
	Title           string
	Summary         string
	MarkdownContent string
	OutlineJSON     string
	LLMProvider     string
	LLMModel        string
	GeneratedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type MeetingMinutesShareRecord struct {
	MinutesID        string
	SharedByUserID   string
	SharedWithUserID string
	CreatedAt        time.Time
}

type UserMeetingHistoryRecord struct {
	MeetingID       string
	MeetingNumber   string
	Title           string
	MeetingType     string
	HostUserID      string
	HostNickname    string
	UserRole        string
	JoinedAt        time.Time
	LeftAt          *time.Time
	CreatedAt       time.Time
	EndedAt         *time.Time
	MinutesID       string
	MinutesStatus   string
	MinutesShared   bool
	MinutesSharedAt *time.Time
	GeneratedAt     *time.Time
}

func (s *Store) InsertTranscriptSegment(ctx context.Context, record TranscriptSegmentRecord) error {
	const statement = `
INSERT INTO meeting_transcript_segments (
    id, meeting_id, meeting_number, participant_id, user_id, nickname, language,
    sequence, started_at, ended_at, text, is_final, asr_provider, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(meeting_id, participant_id, sequence) DO UPDATE SET
    user_id = excluded.user_id,
    nickname = excluded.nickname,
    language = excluded.language,
    started_at = excluded.started_at,
    ended_at = excluded.ended_at,
    text = excluded.text,
    is_final = excluded.is_final,
    asr_provider = excluded.asr_provider,
    updated_at = excluded.updated_at`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		record.ID,
		record.MeetingID,
		record.MeetingNumber,
		record.ParticipantID,
		record.UserID,
		record.Nickname,
		record.Language,
		record.Sequence,
		record.StartedAt.UTC().Format(time.RFC3339Nano),
		record.EndedAt.UTC().Format(time.RFC3339Nano),
		record.Text,
		boolToInteger(record.IsFinal),
		record.ASRProvider,
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
		record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert transcript segment: %w", err)
	}

	return nil
}

func (s *Store) ListTranscriptSegments(ctx context.Context, meetingID string) ([]TranscriptSegmentRecord, error) {
	const query = `
SELECT id, meeting_id, meeting_number, participant_id, user_id, nickname, language,
       sequence, started_at, ended_at, text, is_final, asr_provider, created_at, updated_at
FROM meeting_transcript_segments
WHERE meeting_id = ?
ORDER BY started_at ASC, sequence ASC`

	rows, err := s.db.QueryContext(ctx, query, meetingID)
	if err != nil {
		return nil, fmt.Errorf("list transcript segments: %w", err)
	}
	defer rows.Close()

	records, err := scanTranscriptSegmentRecords(rows)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func (s *Store) CreateMinutesJob(ctx context.Context, record MinutesJobRecord) (MinutesJobRecord, bool, error) {
	existing, found, err := s.GetActiveOrSucceededMinutesJob(ctx, record.MeetingID)
	if err != nil {
		return MinutesJobRecord{}, false, err
	}
	if found {
		return existing, false, nil
	}

	const statement = `
INSERT INTO meeting_minutes_jobs (
    id, meeting_id, meeting_number, requested_by_user_id, requested_by_participant_id,
    status, error_message, email_error, started_at, completed_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		record.ID,
		record.MeetingID,
		record.MeetingNumber,
		record.RequestedByUserID,
		record.RequestedByParticipantID,
		record.Status,
		record.ErrorMessage,
		record.EmailError,
		formatOptionalTime(record.StartedAt),
		formatOptionalTime(record.CompletedAt),
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
		record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return MinutesJobRecord{}, false, fmt.Errorf("create minutes job: %w", err)
	}

	return record, true, nil
}

func (s *Store) GetMinutesJob(ctx context.Context, jobID string) (MinutesJobRecord, bool, error) {
	const query = `
SELECT id, meeting_id, meeting_number, requested_by_user_id, requested_by_participant_id,
       status, error_message, email_error, started_at, completed_at, created_at, updated_at
FROM meeting_minutes_jobs
WHERE id = ?`

	rows, err := s.db.QueryContext(ctx, query, jobID)
	if err != nil {
		return MinutesJobRecord{}, false, fmt.Errorf("get minutes job: %w", err)
	}
	defer rows.Close()

	records, err := scanMinutesJobRecords(rows)
	if err != nil {
		return MinutesJobRecord{}, false, err
	}
	if len(records) == 0 {
		return MinutesJobRecord{}, false, nil
	}

	return records[0], true, nil
}

func (s *Store) GetActiveOrSucceededMinutesJob(ctx context.Context, meetingID string) (MinutesJobRecord, bool, error) {
	const query = `
SELECT id, meeting_id, meeting_number, requested_by_user_id, requested_by_participant_id,
       status, error_message, email_error, started_at, completed_at, created_at, updated_at
FROM meeting_minutes_jobs
WHERE meeting_id = ?
  AND status IN ('pending', 'waiting_transcript', 'running', 'succeeded')
ORDER BY created_at ASC
LIMIT 1`

	rows, err := s.db.QueryContext(ctx, query, meetingID)
	if err != nil {
		return MinutesJobRecord{}, false, fmt.Errorf("get active minutes job: %w", err)
	}
	defer rows.Close()

	records, err := scanMinutesJobRecords(rows)
	if err != nil {
		return MinutesJobRecord{}, false, err
	}
	if len(records) == 0 {
		return MinutesJobRecord{}, false, nil
	}

	return records[0], true, nil
}

func (s *Store) ListRunnableMinutesJobs(ctx context.Context) ([]MinutesJobRecord, error) {
	const query = `
SELECT id, meeting_id, meeting_number, requested_by_user_id, requested_by_participant_id,
       status, error_message, email_error, started_at, completed_at, created_at, updated_at
FROM meeting_minutes_jobs
WHERE status IN ('pending', 'waiting_transcript', 'running')
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list runnable minutes jobs: %w", err)
	}
	defer rows.Close()

	return scanMinutesJobRecords(rows)
}

func (s *Store) UpdateMinutesJobStatus(ctx context.Context, jobID string, status string, errorMessage string, emailError string, startedAt *time.Time, completedAt *time.Time, updatedAt time.Time) error {
	const statement = `
UPDATE meeting_minutes_jobs
SET status = ?,
    error_message = ?,
    email_error = ?,
    started_at = COALESCE(?, started_at),
    completed_at = COALESCE(?, completed_at),
    updated_at = ?
WHERE id = ?`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		status,
		errorMessage,
		emailError,
		formatOptionalTime(startedAt),
		formatOptionalTime(completedAt),
		updatedAt.UTC().Format(time.RFC3339Nano),
		jobID,
	); err != nil {
		return fmt.Errorf("update minutes job status: %w", err)
	}

	return nil
}

func (s *Store) InsertMeetingMinutes(ctx context.Context, record MeetingMinutesRecord) error {
	const statement = `
INSERT INTO meeting_minutes (
    id, job_id, meeting_id, meeting_number, host_user_id, title, summary,
    markdown_content, outline_json, llm_provider, llm_model, generated_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET
    title = excluded.title,
    summary = excluded.summary,
    markdown_content = excluded.markdown_content,
    outline_json = excluded.outline_json,
    llm_provider = excluded.llm_provider,
    llm_model = excluded.llm_model,
    generated_at = excluded.generated_at,
    updated_at = excluded.updated_at`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		record.ID,
		record.JobID,
		record.MeetingID,
		record.MeetingNumber,
		record.HostUserID,
		record.Title,
		record.Summary,
		record.MarkdownContent,
		defaultJSON(record.OutlineJSON),
		record.LLMProvider,
		record.LLMModel,
		record.GeneratedAt.UTC().Format(time.RFC3339Nano),
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
		record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert meeting minutes: %w", err)
	}

	return nil
}

func (s *Store) GetMeetingMinutes(ctx context.Context, minutesID string) (MeetingMinutesRecord, bool, error) {
	const query = `
SELECT id, job_id, meeting_id, meeting_number, host_user_id, title, summary,
       markdown_content, outline_json, llm_provider, llm_model, generated_at, created_at, updated_at
FROM meeting_minutes
WHERE id = ?`

	rows, err := s.db.QueryContext(ctx, query, minutesID)
	if err != nil {
		return MeetingMinutesRecord{}, false, fmt.Errorf("get meeting minutes: %w", err)
	}
	defer rows.Close()

	records, err := scanMeetingMinutesRecords(rows)
	if err != nil {
		return MeetingMinutesRecord{}, false, err
	}
	if len(records) == 0 {
		return MeetingMinutesRecord{}, false, nil
	}

	return records[0], true, nil
}

func (s *Store) GetMeetingMinutesByJob(ctx context.Context, jobID string) (MeetingMinutesRecord, bool, error) {
	const query = `
SELECT id, job_id, meeting_id, meeting_number, host_user_id, title, summary,
       markdown_content, outline_json, llm_provider, llm_model, generated_at, created_at, updated_at
FROM meeting_minutes
WHERE job_id = ?`

	rows, err := s.db.QueryContext(ctx, query, jobID)
	if err != nil {
		return MeetingMinutesRecord{}, false, fmt.Errorf("get meeting minutes by job: %w", err)
	}
	defer rows.Close()

	records, err := scanMeetingMinutesRecords(rows)
	if err != nil {
		return MeetingMinutesRecord{}, false, err
	}
	if len(records) == 0 {
		return MeetingMinutesRecord{}, false, nil
	}

	return records[0], true, nil
}

func (s *Store) UpsertMeetingMinutesShare(ctx context.Context, record MeetingMinutesShareRecord) error {
	const statement = `
INSERT INTO meeting_minutes_shares (
    minutes_id, shared_by_user_id, shared_with_user_id, created_at
) VALUES (?, ?, ?, ?)
ON CONFLICT(minutes_id, shared_with_user_id) DO UPDATE SET
    shared_by_user_id = excluded.shared_by_user_id,
    created_at = excluded.created_at`

	if _, err := s.db.ExecContext(
		ctx,
		statement,
		record.MinutesID,
		record.SharedByUserID,
		record.SharedWithUserID,
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("upsert meeting minutes share: %w", err)
	}

	return nil
}

func (s *Store) DeleteMeetingMinutesShare(ctx context.Context, minutesID string, sharedWithUserID string) error {
	const statement = `
DELETE FROM meeting_minutes_shares
WHERE minutes_id = ? AND shared_with_user_id = ?`

	if _, err := s.db.ExecContext(ctx, statement, minutesID, sharedWithUserID); err != nil {
		return fmt.Errorf("delete meeting minutes share: %w", err)
	}

	return nil
}

func (s *Store) UserCanAccessMinutes(ctx context.Context, minutesID string, userID string) (bool, error) {
	const query = `
SELECT 1
FROM meeting_minutes mm
WHERE mm.id = ?
  AND (
      mm.host_user_id = ?
      OR EXISTS (
          SELECT 1
          FROM meeting_minutes_shares ms
          WHERE ms.minutes_id = mm.id
            AND ms.shared_with_user_id = ?
      )
  )
LIMIT 1`

	var marker int
	if err := s.db.QueryRowContext(ctx, query, minutesID, userID, userID).Scan(&marker); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check minutes access: %w", err)
	}

	return true, nil
}

func (s *Store) GetMeetingUsageByID(ctx context.Context, meetingID string) (MeetingUsageRecord, bool, error) {
	const query = `
SELECT id, meeting_number, join_code, title, meeting_type, host_participant_id,
       host_user_id, host_email, host_nickname, host_ip_address, created_at, ended_at, updated_at
FROM meeting_usage_meetings
WHERE id = ?`

	rows, err := s.db.QueryContext(ctx, query, meetingID)
	if err != nil {
		return MeetingUsageRecord{}, false, fmt.Errorf("get meeting usage by id: %w", err)
	}
	defer rows.Close()

	records, err := scanMeetingUsageRecords(rows)
	if err != nil {
		return MeetingUsageRecord{}, false, err
	}
	if len(records) == 0 {
		return MeetingUsageRecord{}, false, nil
	}

	return records[0], true, nil
}

func (s *Store) GetMeetingUsageByNumber(ctx context.Context, meetingNumber string) (MeetingUsageRecord, bool, error) {
	const query = `
SELECT id, meeting_number, join_code, title, meeting_type, host_participant_id,
       host_user_id, host_email, host_nickname, host_ip_address, created_at, ended_at, updated_at
FROM meeting_usage_meetings
WHERE meeting_number = ?`

	rows, err := s.db.QueryContext(ctx, query, meetingNumber)
	if err != nil {
		return MeetingUsageRecord{}, false, fmt.Errorf("get meeting usage by number: %w", err)
	}
	defer rows.Close()

	records, err := scanMeetingUsageRecords(rows)
	if err != nil {
		return MeetingUsageRecord{}, false, err
	}
	if len(records) == 0 {
		return MeetingUsageRecord{}, false, nil
	}

	return records[0], true, nil
}

func (s *Store) ListRegisteredParticipantsForMeeting(ctx context.Context, meetingID string) ([]MeetingParticipantUsageRecord, error) {
	const query = `
SELECT meeting_id, participant_id, user_id, email, nickname, is_anonymous, ip_address,
       device_type, client_profile_json, participant_role, joined_at, left_at, updated_at
FROM meeting_usage_participants
WHERE meeting_id = ?
  AND user_id <> ''
ORDER BY joined_at ASC`

	rows, err := s.db.QueryContext(ctx, query, meetingID)
	if err != nil {
		return nil, fmt.Errorf("list registered participants for meeting: %w", err)
	}
	defer rows.Close()

	return scanParticipantUsageRecords(rows)
}

func (s *Store) ListUserMeetingHistory(ctx context.Context, userID string) ([]UserMeetingHistoryRecord, error) {
	const query = `
SELECT
    m.id,
    m.meeting_number,
    m.title,
    m.meeting_type,
    m.host_user_id,
    m.host_nickname,
    CASE WHEN m.host_user_id = ? THEN 'host' ELSE 'participant' END AS user_role,
    COALESCE(p.joined_at, m.created_at) AS joined_at,
    p.left_at,
    m.created_at,
    m.ended_at,
    COALESCE(mm.id, shared_mm.id, '') AS minutes_id,
    COALESCE(j.status, shared_j.status, '') AS minutes_status,
    CASE WHEN shared_mm.id IS NULL THEN 0 ELSE 1 END AS minutes_shared,
    ms.created_at AS minutes_shared_at,
    COALESCE(mm.generated_at, shared_mm.generated_at) AS generated_at
FROM meeting_usage_meetings m
LEFT JOIN meeting_usage_participants p
  ON p.meeting_id = m.id
 AND p.user_id = ?
LEFT JOIN meeting_minutes mm
  ON mm.meeting_id = m.id
 AND mm.host_user_id = ?
LEFT JOIN meeting_minutes_jobs j
  ON j.id = mm.job_id
LEFT JOIN meeting_minutes_shares ms
  ON ms.shared_with_user_id = ?
LEFT JOIN meeting_minutes shared_mm
  ON shared_mm.id = ms.minutes_id
 AND shared_mm.meeting_id = m.id
LEFT JOIN meeting_minutes_jobs shared_j
  ON shared_j.id = shared_mm.job_id
WHERE m.host_user_id = ?
   OR p.user_id = ?
   OR shared_mm.id IS NOT NULL
GROUP BY m.id
ORDER BY COALESCE(m.ended_at, m.created_at) DESC`

	rows, err := s.db.QueryContext(ctx, query, userID, userID, userID, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("list user meeting history: %w", err)
	}
	defer rows.Close()

	var records []UserMeetingHistoryRecord
	for rows.Next() {
		var (
			record             UserMeetingHistoryRecord
			joinedAtRaw        string
			leftAtRaw          sql.NullString
			createdAtRaw       string
			endedAtRaw         sql.NullString
			minutesShared      int
			minutesSharedAtRaw sql.NullString
			generatedAtRaw     sql.NullString
		)
		if err := rows.Scan(
			&record.MeetingID,
			&record.MeetingNumber,
			&record.Title,
			&record.MeetingType,
			&record.HostUserID,
			&record.HostNickname,
			&record.UserRole,
			&joinedAtRaw,
			&leftAtRaw,
			&createdAtRaw,
			&endedAtRaw,
			&record.MinutesID,
			&record.MinutesStatus,
			&minutesShared,
			&minutesSharedAtRaw,
			&generatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan user meeting history: %w", err)
		}

		joinedAt, err := time.Parse(time.RFC3339Nano, joinedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse history joined_at: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse history created_at: %w", err)
		}
		record.JoinedAt = joinedAt
		record.CreatedAt = createdAt
		record.MinutesShared = minutesShared == 1
		if leftAtRaw.Valid && leftAtRaw.String != "" {
			leftAt, parseErr := time.Parse(time.RFC3339Nano, leftAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse history left_at: %w", parseErr)
			}
			record.LeftAt = &leftAt
		}
		if endedAtRaw.Valid && endedAtRaw.String != "" {
			endedAt, parseErr := time.Parse(time.RFC3339Nano, endedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse history ended_at: %w", parseErr)
			}
			record.EndedAt = &endedAt
		}
		if minutesSharedAtRaw.Valid && minutesSharedAtRaw.String != "" {
			sharedAt, parseErr := time.Parse(time.RFC3339Nano, minutesSharedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse history minutes_shared_at: %w", parseErr)
			}
			record.MinutesSharedAt = &sharedAt
		}
		if generatedAtRaw.Valid && generatedAtRaw.String != "" {
			generatedAt, parseErr := time.Parse(time.RFC3339Nano, generatedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse history generated_at: %w", parseErr)
			}
			record.GeneratedAt = &generatedAt
		}

		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan user meeting history rows: %w", err)
	}

	return records, nil
}

func scanTranscriptSegmentRecords(rows *sql.Rows) ([]TranscriptSegmentRecord, error) {
	var records []TranscriptSegmentRecord
	for rows.Next() {
		var (
			record       TranscriptSegmentRecord
			startedAtRaw string
			endedAtRaw   string
			createdAtRaw string
			updatedAtRaw string
			isFinal      int
		)
		if err := rows.Scan(
			&record.ID,
			&record.MeetingID,
			&record.MeetingNumber,
			&record.ParticipantID,
			&record.UserID,
			&record.Nickname,
			&record.Language,
			&record.Sequence,
			&startedAtRaw,
			&endedAtRaw,
			&record.Text,
			&isFinal,
			&record.ASRProvider,
			&createdAtRaw,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan transcript segment: %w", err)
		}

		startedAt, err := time.Parse(time.RFC3339Nano, startedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse transcript started_at: %w", err)
		}
		endedAt, err := time.Parse(time.RFC3339Nano, endedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse transcript ended_at: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse transcript created_at: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse transcript updated_at: %w", err)
		}
		record.StartedAt = startedAt
		record.EndedAt = endedAt
		record.CreatedAt = createdAt
		record.UpdatedAt = updatedAt
		record.IsFinal = isFinal == 1
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript rows: %w", err)
	}

	return records, nil
}

func scanMinutesJobRecords(rows *sql.Rows) ([]MinutesJobRecord, error) {
	var records []MinutesJobRecord
	for rows.Next() {
		var (
			record         MinutesJobRecord
			startedAtRaw   sql.NullString
			completedAtRaw sql.NullString
			createdAtRaw   string
			updatedAtRaw   string
		)
		if err := rows.Scan(
			&record.ID,
			&record.MeetingID,
			&record.MeetingNumber,
			&record.RequestedByUserID,
			&record.RequestedByParticipantID,
			&record.Status,
			&record.ErrorMessage,
			&record.EmailError,
			&startedAtRaw,
			&completedAtRaw,
			&createdAtRaw,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan minutes job: %w", err)
		}

		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse minutes job created_at: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse minutes job updated_at: %w", err)
		}
		record.CreatedAt = createdAt
		record.UpdatedAt = updatedAt
		if startedAtRaw.Valid && startedAtRaw.String != "" {
			startedAt, parseErr := time.Parse(time.RFC3339Nano, startedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse minutes job started_at: %w", parseErr)
			}
			record.StartedAt = &startedAt
		}
		if completedAtRaw.Valid && completedAtRaw.String != "" {
			completedAt, parseErr := time.Parse(time.RFC3339Nano, completedAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse minutes job completed_at: %w", parseErr)
			}
			record.CompletedAt = &completedAt
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan minutes job rows: %w", err)
	}

	return records, nil
}

func scanMeetingMinutesRecords(rows *sql.Rows) ([]MeetingMinutesRecord, error) {
	var records []MeetingMinutesRecord
	for rows.Next() {
		var (
			record         MeetingMinutesRecord
			generatedAtRaw string
			createdAtRaw   string
			updatedAtRaw   string
		)
		if err := rows.Scan(
			&record.ID,
			&record.JobID,
			&record.MeetingID,
			&record.MeetingNumber,
			&record.HostUserID,
			&record.Title,
			&record.Summary,
			&record.MarkdownContent,
			&record.OutlineJSON,
			&record.LLMProvider,
			&record.LLMModel,
			&generatedAtRaw,
			&createdAtRaw,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan meeting minutes: %w", err)
		}

		generatedAt, err := time.Parse(time.RFC3339Nano, generatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse minutes generated_at: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse minutes created_at: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse minutes updated_at: %w", err)
		}
		record.GeneratedAt = generatedAt
		record.CreatedAt = createdAt
		record.UpdatedAt = updatedAt
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan meeting minutes rows: %w", err)
	}

	return records, nil
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func normalizeStatus(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
