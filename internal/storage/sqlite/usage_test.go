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
		MeetingID:         "mtg_001",
		ParticipantID:     "p_001",
		Nickname:          "匿名参会者",
		IsAnonymous:       true,
		IPAddress:         "203.0.113.20",
		DeviceType:        "browser",
		ClientProfileJSON: `{"browser":"Chrome","deviceCategory":"desktop"}`,
		ParticipantRole:   "participant",
		JoinedAt:          joinedAt,
		UpdatedAt:         joinedAt,
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
	if participants[0].ClientProfileJSON != `{"browser":"Chrome","deviceCategory":"desktop"}` {
		t.Fatalf("participant client profile = %s", participants[0].ClientProfileJSON)
	}
}

func TestUsageWindowExcludesStaleOpenRecordsWithoutActivity(t *testing.T) {
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
	oldTime := start.Add(-48 * time.Hour)
	for _, meetingID := range []string{"mtg_stale", "mtg_active"} {
		if err := store.UpsertMeetingUsage(ctx, MeetingUsageRecord{
			ID:                meetingID,
			MeetingNumber:     "123456789",
			Title:             meetingID,
			MeetingType:       "quick",
			HostParticipantID: meetingID + "_host",
			HostNickname:      "主持人",
			HostIPAddress:     "203.0.113.10",
			CreatedAt:         oldTime,
			UpdatedAt:         oldTime,
		}); err != nil {
			t.Fatalf("UpsertMeetingUsage(%s) error = %v", meetingID, err)
		}
		if err := store.UpsertMeetingParticipantUsage(ctx, MeetingParticipantUsageRecord{
			MeetingID:       meetingID,
			ParticipantID:   meetingID + "_host",
			Nickname:        "主持人",
			IPAddress:       "203.0.113.10",
			ParticipantRole: "host",
			JoinedAt:        oldTime,
			UpdatedAt:       oldTime,
		}); err != nil {
			t.Fatalf("UpsertMeetingParticipantUsage(%s) error = %v", meetingID, err)
		}
	}
	if err := store.InsertAuditEvent(ctx, AuditEvent{
		MeetingID:       "mtg_active",
		ParticipantID:   "mtg_active_host",
		ParticipantRole: "host",
		EventType:       "media_report",
		DeviceType:      "browser",
		DetailsJSON:     "{}",
		CreatedAt:       start.Add(time.Hour),
	}); err != nil {
		t.Fatalf("InsertAuditEvent() error = %v", err)
	}

	meetings, err := store.ListMeetingUsageWindow(ctx, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ListMeetingUsageWindow() error = %v", err)
	}
	if len(meetings) != 1 || meetings[0].ID != "mtg_active" {
		t.Fatalf("meetings = %+v, want only active meeting", meetings)
	}

	participants, err := store.ListParticipantUsageWindow(ctx, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ListParticipantUsageWindow() error = %v", err)
	}
	if len(participants) != 1 || participants[0].ParticipantID != "mtg_active_host" {
		t.Fatalf("participants = %+v, want only active participant", participants)
	}
}

func TestAuthReportQueries(t *testing.T) {
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
	createdAt := start.Add(time.Hour)
	registerSentAt := createdAt.Add(-10 * time.Minute)
	registerConsumedAt := createdAt.Add(-2 * time.Minute)
	if err := store.UpsertVerificationCode(ctx, VerificationCodeRecord{
		ID:        "verify_register_001",
		Email:     "new-user@example.com",
		Purpose:   "register",
		IPAddress: "203.0.113.55",
		CodeHash:  "hash",
		SentAt:    registerSentAt,
		ExpiresAt: registerSentAt.Add(10 * time.Minute),
		CreatedAt: registerSentAt,
		UpdatedAt: registerSentAt,
	}); err != nil {
		t.Fatalf("UpsertVerificationCode(register) error = %v", err)
	}
	if err := store.ConsumeVerificationCode(ctx, "verify_register_001", registerConsumedAt, registerConsumedAt); err != nil {
		t.Fatalf("ConsumeVerificationCode(register) error = %v", err)
	}
	if err := store.CreateUser(ctx, UserRecord{
		ID:        "usr_001",
		Email:     "new-user@example.com",
		Nickname:  "新用户",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	sentAt := start.Add(2 * time.Hour)
	loginAt := sentAt.Add(2 * time.Minute)
	if err := store.UpsertVerificationCode(ctx, VerificationCodeRecord{
		ID:        "verify_001",
		Email:     "new-user@example.com",
		Purpose:   "login",
		IPAddress: "203.0.113.10",
		CodeHash:  "hash",
		SentAt:    sentAt,
		ExpiresAt: sentAt.Add(10 * time.Minute),
		CreatedAt: sentAt,
		UpdatedAt: sentAt,
	}); err != nil {
		t.Fatalf("UpsertVerificationCode() error = %v", err)
	}
	if err := store.ConsumeVerificationCode(ctx, "verify_001", loginAt, loginAt); err != nil {
		t.Fatalf("ConsumeVerificationCode() error = %v", err)
	}
	unusedSentAt := start.Add(3 * time.Hour)
	if err := store.UpsertVerificationCode(ctx, VerificationCodeRecord{
		ID:        "verify_unused_001",
		Email:     "new-user@example.com",
		Purpose:   "login",
		IPAddress: "203.0.113.11",
		CodeHash:  "hash",
		SentAt:    unusedSentAt,
		ExpiresAt: unusedSentAt.Add(10 * time.Minute),
		CreatedAt: unusedSentAt,
		UpdatedAt: unusedSentAt,
	}); err != nil {
		t.Fatalf("UpsertVerificationCode(unused) error = %v", err)
	}

	users, err := store.ListUsersCreatedWindow(ctx, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ListUsersCreatedWindow() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("created user count = %d, want 1", len(users))
	}
	if users[0].Email != "new-user@example.com" || users[0].Nickname != "新用户" {
		t.Fatalf("created user = %+v", users[0])
	}
	if users[0].IPAddress != "203.0.113.55" {
		t.Fatalf("created user ip = %q, want 203.0.113.55", users[0].IPAddress)
	}

	codes, err := store.ListEmailCodeSendsWindow(ctx, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ListEmailCodeSendsWindow() error = %v", err)
	}
	if len(codes) != 3 {
		t.Fatalf("email code send count = %d, want 3", len(codes))
	}
	if codes[0].Purpose != "register" || !codes[0].SentAt.Equal(registerSentAt) {
		t.Fatalf("first email code send = %+v", codes[0])
	}
	if codes[1].Purpose != "login" || codes[1].IPAddress != "203.0.113.10" || codes[1].ConsumedAt == nil || !codes[1].ConsumedAt.Equal(loginAt) {
		t.Fatalf("second email code send = %+v", codes[1])
	}
	if codes[2].Purpose != "login" || codes[2].IPAddress != "203.0.113.11" || codes[2].ConsumedAt != nil {
		t.Fatalf("third email code send = %+v", codes[2])
	}
}

func TestAuditReportQuery(t *testing.T) {
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
	if err := store.InsertAuditEvent(ctx, AuditEvent{
		MeetingID:        "mtg_001",
		ParticipantID:    "p_001",
		ParticipantRole:  "participant",
		EventType:        "media_report",
		IPAddress:        "203.0.113.20",
		DeviceType:       "browser",
		LatencyMS:        120,
		PacketLossRate:   0.02,
		AverageFPS:       24,
		AverageBitrateKB: 1600,
		DetailsJSON:      "{}",
		CreatedAt:        start.Add(time.Hour),
	}); err != nil {
		t.Fatalf("InsertAuditEvent(media_report) error = %v", err)
	}
	if err := store.InsertAuditEvent(ctx, AuditEvent{
		MeetingID:     "mtg_001",
		ParticipantID: "p_001",
		EventType:     "chat_message",
		DetailsJSON:   "{}",
		CreatedAt:     start.Add(time.Hour),
	}); err != nil {
		t.Fatalf("InsertAuditEvent(chat_message) error = %v", err)
	}

	events, err := store.ListAuditEventsWindow(ctx, start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ListAuditEventsWindow() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit event count = %d, want 1", len(events))
	}
	if events[0].MeetingID != "mtg_001" || events[0].LatencyMS != 120 || events[0].AverageBitrateKB != 1600 {
		t.Fatalf("audit event = %+v", events[0])
	}
}
