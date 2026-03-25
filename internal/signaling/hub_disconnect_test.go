package signaling

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/07c2/projects/meeting/internal/meeting"
	"github.com/07c2/projects/meeting/internal/storage/sqlite"
	"github.com/gorilla/websocket"
)

type disconnectTestStore struct{}

func (disconnectTestStore) GetUserPreference(_ context.Context, _ string) (sqlite.UserPreference, bool, error) {
	return sqlite.UserPreference{}, false, nil
}

func (disconnectTestStore) UpsertUserPreference(_ context.Context, _ sqlite.UserPreference) error {
	return nil
}

func (disconnectTestStore) InsertAuditEvent(_ context.Context, _ sqlite.AuditEvent) error {
	return nil
}

func TestDisconnectCleanupRemovesParticipant(t *testing.T) {
	t.Parallel()

	previousGracePeriod := disconnectGracePeriod
	disconnectGracePeriod = 20 * time.Millisecond
	defer func() {
		disconnectGracePeriod = previousGracePeriod
	}()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	meetingService := meeting.NewService(logger, disconnectTestStore{})
	hub := NewHub(logger, meetingService)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meetingID := strings.TrimPrefix(r.URL.Path, "/ws/meetings/")
		if err := hub.ServeWS(w, r, meetingID, r.URL.Query().Get("participantId")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	meetingValue, host, err := meetingService.CreateMeeting(ctx, meeting.CreateMeetingInput{
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

	hostConn := dialDisconnectCleanupSocket(t, server.URL, meetingValue.ID, host.ID)
	defer func() { _ = hostConn.Close() }()
	participantConn := dialDisconnectCleanupSocket(t, server.URL, meetingValue.ID, participant.ID)

	readDisconnectCleanupEvent(t, hostConn, "session.welcome")
	readDisconnectCleanupEvent(t, participantConn, "session.welcome")
	readDisconnectCleanupEvent(t, hostConn, "participant.online")

	_ = participantConn.Close()

	readDisconnectCleanupEvent(t, hostConn, "participant.offline")
	readDisconnectCleanupEvent(t, hostConn, "participant.left")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		currentMeeting, found := meetingService.GetMeeting(meetingValue.ID)
		if !found {
			t.Fatal("meeting removed unexpectedly")
		}
		if _, ok := currentMeeting.Participants[participant.ID]; !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("participant %s should be removed after disconnect cleanup", participant.ID)
}

func dialDisconnectCleanupSocket(t *testing.T, serverURL string, meetingID string, participantID string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/meetings/" + meetingID + "?participantId=" + participantID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial(%s) error = %v", wsURL, err)
	}
	return conn
}

func readDisconnectCleanupEvent(t *testing.T, conn *websocket.Conn, expectedType string) map[string]any {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}

	var response map[string]any
	for {
		if err := conn.ReadJSON(&response); err != nil {
			t.Fatalf("ReadJSON() error = %v", err)
		}

		if response["type"] == expectedType {
			return response
		}
	}
}
