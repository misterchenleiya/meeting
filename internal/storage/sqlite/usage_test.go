package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestUsageRecordsPersistAndQueryByWindow(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), t.TempDir()+"/meeting.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	ctx := context.Background()
	start := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertMeetingUsage(ctx, MeetingUsageRecord{
		ID:                "mtg_001",
		MeetingNumber:     "123456789",
		Title:             "daily sync",
		MeetingType:       "scheduled",
		HostParticipantID: "host_001",
		HostUserID:        "usr_host",
		HostEmail:         "host@example.com",
		HostNickname:      "主持人",
		HostIPAddress:     "203.0.113.10",
		CreatedAt:         start,
		UpdatedAt:         start,
	}); err != nil {
		t.Fatalf("UpsertMeetingUsage() error = %v", err)
	}

	joinedAt := start.Add(5 * time.Minute)
	if err := store.UpsertMeetingParticipantUsage(ctx, MeetingParticipantUsageRecord{
		MeetingID:       "mtg_001",
		ParticipantID:   "p_001",
		Nickname:        "匿名参会者",
		IsAnonymous:     true,
		IPAddress:       "203.0.113.20",
		DeviceType:      "browser",
		ParticipantRole: "participant",
		JoinedAt:        joinedAt,
		UpdatedAt:       joinedAt,
	}); err != nil {
		t.Fatalf("UpsertMeetingParticipantUsage() error = %v", err)
	}

	leftAt := joinedAt.Add(30 * time.Minute)
	if err := store.UpdateMeetingParticipantUsageLeftAt(ctx, "mtg_001", "p_001", leftAt, leftAt); err != nil {
		t.Fatalf("UpdateMeetingParticipantUsageLeftAt() error = %v", err)
	}
	endedAt := start.Add(time.Hour)
	if err := store.UpdateMeetingUsageEndedAt(ctx, "mtg_001", endedAt, endedAt); err != nil {
		t.Fatalf("UpdateMeetingUsageEndedAt() error = %v", err)
	}

	meetings, err := store.ListMeetingUsageWindow(ctx, start.Add(-time.Minute), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListMeetingUsageWindow() error = %v", err)
	}
	if len(meetings) != 1 {
		t.Fatalf("meeting count = %d, want 1", len(meetings))
	}
	if meetings[0].EndedAt == nil || !meetings[0].EndedAt.Equal(endedAt) {
		t.Fatalf("meeting endedAt = %v, want %s", meetings[0].EndedAt, endedAt)
	}

	participants, err := store.ListParticipantUsageWindow(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("ListParticipantUsageWindow() error = %v", err)
	}
	if len(participants) != 1 {
		t.Fatalf("participant count = %d, want 1", len(participants))
	}
	if participants[0].LeftAt == nil || !participants[0].LeftAt.Equal(leftAt) {
		t.Fatalf("participant leftAt = %v, want %s", participants[0].LeftAt, leftAt)
	}
}
