package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaFS embed.FS

type Store struct {
	db *sql.DB
}

type UserPreference struct {
	UserID                   string
	DefaultCameraEnabled     bool
	DefaultMicrophoneEnabled bool
	UpdatedAt                time.Time
}

type AuditEvent struct {
	MeetingID        string
	ParticipantID    string
	UserID           string
	ParticipantRole  string
	EventType        string
	IPAddress        string
	DeviceType       string
	LatencyMS        int64
	PacketLossRate   float64
	AverageFPS       float64
	AverageBitrateKB int64
	DetailsJSON      string
	CreatedAt        time.Time
}

func Open(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) migrate(ctx context.Context) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) GetUserPreference(ctx context.Context, userID string) (UserPreference, bool, error) {
	const query = `
SELECT user_id, default_camera_enabled, default_microphone_enabled, updated_at
FROM user_preferences
WHERE user_id = ?`

	var (
		pref              UserPreference
		cameraEnabled     int
		microphoneEnabled int
		updatedAtRaw      string
	)

	if err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&pref.UserID,
		&cameraEnabled,
		&microphoneEnabled,
		&updatedAtRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return UserPreference{}, false, nil
		}
		return UserPreference{}, false, fmt.Errorf("get user preference: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return UserPreference{}, false, fmt.Errorf("parse user preference updated_at: %w", err)
	}

	pref.DefaultCameraEnabled = cameraEnabled == 1
	pref.DefaultMicrophoneEnabled = microphoneEnabled == 1
	pref.UpdatedAt = updatedAt

	return pref, true, nil
}

func (s *Store) UpsertUserPreference(ctx context.Context, pref UserPreference) error {
	const statement = `
INSERT INTO user_preferences (
    user_id, default_camera_enabled, default_microphone_enabled, updated_at
) VALUES (?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    default_camera_enabled = excluded.default_camera_enabled,
    default_microphone_enabled = excluded.default_microphone_enabled,
    updated_at = excluded.updated_at`

	_, err := s.db.ExecContext(
		ctx,
		statement,
		pref.UserID,
		boolToInteger(pref.DefaultCameraEnabled),
		boolToInteger(pref.DefaultMicrophoneEnabled),
		pref.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert user preference: %w", err)
	}

	return nil
}

func (s *Store) InsertAuditEvent(ctx context.Context, event AuditEvent) error {
	const statement = `
INSERT INTO audit_events (
    meeting_id, participant_id, user_id, participant_role, event_type, ip_address,
    device_type, latency_ms, packet_loss_rate, avg_fps, avg_bitrate_kbps, details_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(
		ctx,
		statement,
		event.MeetingID,
		event.ParticipantID,
		event.UserID,
		event.ParticipantRole,
		event.EventType,
		event.IPAddress,
		event.DeviceType,
		event.LatencyMS,
		event.PacketLossRate,
		event.AverageFPS,
		event.AverageBitrateKB,
		event.DetailsJSON,
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return nil
}

func boolToInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
