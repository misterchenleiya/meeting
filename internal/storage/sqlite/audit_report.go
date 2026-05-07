package sqlite

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) ListAuditEventsWindow(ctx context.Context, start time.Time, end time.Time) ([]AuditEvent, error) {
	const query = `
SELECT meeting_id, participant_id, user_id, participant_role, event_type, ip_address,
       device_type, latency_ms, packet_loss_rate, avg_fps, avg_bitrate_kbps, details_json, created_at
FROM audit_events
WHERE event_type = 'media_report' AND created_at >= ? AND created_at < ?
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("list audit events window: %w", err)
	}
	defer rows.Close()

	records := []AuditEvent{}
	for rows.Next() {
		var (
			record       AuditEvent
			createdAtRaw string
		)
		if err := rows.Scan(
			&record.MeetingID,
			&record.ParticipantID,
			&record.UserID,
			&record.ParticipantRole,
			&record.EventType,
			&record.IPAddress,
			&record.DeviceType,
			&record.LatencyMS,
			&record.PacketLossRate,
			&record.AverageFPS,
			&record.AverageBitrateKB,
			&record.DetailsJSON,
			&createdAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse audit event created_at: %w", err)
		}
		record.CreatedAt = createdAt
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}

	return records, nil
}
