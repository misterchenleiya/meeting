package signaling_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/misterchenleiya/meeting/internal/httpapi"
	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/signaling"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

type stubStore struct{}

func (stubStore) GetUserPreference(_ context.Context, _ string) (sqlite.UserPreference, bool, error) {
	return sqlite.UserPreference{}, false, nil
}

func (stubStore) UpsertUserPreference(_ context.Context, _ sqlite.UserPreference) error {
	return nil
}

func (stubStore) InsertAuditEvent(_ context.Context, _ sqlite.AuditEvent) error {
	return nil
}

func TestSignalForwardingAndCapabilityGrant(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	meetingService := meeting.NewService(logger, stubStore{})
	hub := signaling.NewHub(logger, meetingService)

	server := httptest.NewServer(httpapi.NewServer(logger, nil, meetingService, nil, hub).Routes())
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

	hostConn := dialWebSocket(t, server.URL, meetingValue.ID, host.ID)
	defer func() { _ = hostConn.Close() }()
	participantConn := dialWebSocket(t, server.URL, meetingValue.ID, participant.ID)
	defer func() { _ = participantConn.Close() }()

	readEvent(t, hostConn, "session.welcome")
	readEvent(t, participantConn, "session.welcome")

	writeEvent(t, participantConn, map[string]any{
		"type": "capability.request",
		"payload": map[string]any{
			"capability": "camera",
		},
	})

	requested := readEvent(t, hostConn, "capability.requested")
	payload := requested["payload"].(map[string]any)
	if payload["fromParticipantId"] != participant.ID {
		t.Fatalf("fromParticipantId = %v, want %s", payload["fromParticipantId"], participant.ID)
	}

	writeEvent(t, hostConn, map[string]any{
		"type": "capability.grant",
		"payload": map[string]any{
			"targetParticipantId": participant.ID,
			"capability":          "camera",
		},
	})

	grantedOnHost := readEvent(t, hostConn, "capability.granted")
	if grantedOnHost["type"] != "capability.granted" {
		t.Fatalf("host event type = %v", grantedOnHost["type"])
	}

	grantedOnParticipant := readEvent(t, participantConn, "capability.granted")
	grantedPayload := grantedOnParticipant["payload"].(map[string]any)
	if grantedPayload["targetParticipantId"] != participant.ID {
		t.Fatalf("targetParticipantId = %v, want %s", grantedPayload["targetParticipantId"], participant.ID)
	}

	writeEvent(t, participantConn, map[string]any{
		"type": "signal.offer",
		"payload": map[string]any{
			"targetParticipantId": host.ID,
			"data": map[string]any{
				"sdp":  "offer-sdp",
				"type": "offer",
			},
		},
	})

	signalOffer := readEvent(t, hostConn, "signal.offer")
	signalPayload := signalOffer["payload"].(map[string]any)
	if signalPayload["fromParticipantId"] != participant.ID {
		t.Fatalf("signal fromParticipantId = %v, want %s", signalPayload["fromParticipantId"], participant.ID)
	}
}

func dialWebSocket(t *testing.T, serverURL string, meetingID string, participantID string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/meetings/" + meetingID + "?participantId=" + participantID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial(%s) error = %v", wsURL, err)
	}
	return conn
}

func writeEvent(t *testing.T, conn *websocket.Conn, payload map[string]any) {
	t.Helper()

	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
}

func readEvent(t *testing.T, conn *websocket.Conn, expectedType string) map[string]any {
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
