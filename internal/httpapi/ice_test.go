package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
	"github.com/misterchenleiya/meeting/internal/turnauth"
)

type turnHTTPStubStore struct{}

func (turnHTTPStubStore) GetUserByID(_ context.Context, _ string) (sqlite.UserRecord, bool, error) {
	return sqlite.UserRecord{}, false, nil
}

func (turnHTTPStubStore) GetUserPreference(_ context.Context, _ string) (sqlite.UserPreference, bool, error) {
	return sqlite.UserPreference{}, false, nil
}

func (turnHTTPStubStore) UpsertUserPreference(_ context.Context, _ sqlite.UserPreference) error {
	return nil
}

func (turnHTTPStubStore) InsertAuditEvent(_ context.Context, _ sqlite.AuditEvent) error {
	return nil
}

func (turnHTTPStubStore) UpsertMeetingUsage(_ context.Context, _ sqlite.MeetingUsageRecord) error {
	return nil
}

func (turnHTTPStubStore) UpdateMeetingUsageEndedAt(_ context.Context, _ string, _ time.Time, _ time.Time) error {
	return nil
}

func (turnHTTPStubStore) UpsertMeetingParticipantUsage(_ context.Context, _ sqlite.MeetingParticipantUsageRecord) error {
	return nil
}

func (turnHTTPStubStore) UpdateMeetingParticipantUsageLeftAt(_ context.Context, _ string, _ string, _ time.Time, _ time.Time) error {
	return nil
}

func (turnHTTPStubStore) UpdateMeetingParticipantUsageNickname(_ context.Context, _ string, _ string, _ string, _ time.Time) error {
	return nil
}

func (turnHTTPStubStore) UpdateMeetingParticipantUsageRole(_ context.Context, _ string, _ string, _ string, _ time.Time) error {
	return nil
}

func TestHandleGetICEServersForAnonymousParticipant(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	meetingService := meeting.NewService(logger, turnHTTPStubStore{})
	turnService, err := turnauth.NewService(turnauth.Config{
		StunURLs:     []string{"stun:stun.example.com:3478"},
		TurnURLs:     []string{"turn:turn.example.com:3478?transport=udp"},
		SharedSecret: "shared-secret",
		TTL:          2 * time.Hour,
		Now: func() time.Time {
			return time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	server := NewServer(logger, nil, meetingService, nil, nil, turnService)

	ctx := context.Background()
	meetingValue, _, err := meetingService.CreateMeeting(ctx, meeting.CreateMeetingInput{
		Title:        "demo",
		Password:     "secret",
		HostUserID:   "host-user",
		HostNickname: "主持人",
		DeviceType:   "desktop",
		IPAddress:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateMeeting() error = %v", err)
	}

	_, participant, err := meetingService.JoinMeeting(ctx, meeting.JoinMeetingInput{
		MeetingID:   meetingValue.ID,
		Password:    "secret",
		Nickname:    "参与者",
		IsAnonymous: true,
		DeviceType:  "desktop",
		IPAddress:   "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("JoinMeeting() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/meetings/"+meetingValue.ID+"/participants/"+participant.ID+"/ice-servers",
		nil,
	)
	recorder := httptest.NewRecorder()

	server.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response struct {
		IceServers []turnauth.IceServer `json:"iceServers"`
		ExpiresAt  string               `json:"expiresAt"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(response.IceServers) != 2 {
		t.Fatalf("len(response.IceServers) = %d, want 2", len(response.IceServers))
	}

	if response.IceServers[1].Username == "" {
		t.Fatal("turn username should not be empty")
	}

	if response.IceServers[1].Credential == "" {
		t.Fatal("turn credential should not be empty")
	}
}
