package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/misterchenleiya/meeting/internal/logging"
)

func TestHandleClientLogsAccepted(t *testing.T) {
	var logBuffer bytes.Buffer
	server := NewServer(logging.NewBootstrapLogger(&logBuffer), nil, nil, nil)

	requestBody := `{"logs":[{"level":"warn","time":"2026-03-25T12:00:00.000Z","message":"meeting.end_request_failed","scope":"frontend.app","meetingId":"mtg-001","detail":"timeout"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/client-logs", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "meeting-test/1.0")
	request.RemoteAddr = "192.0.2.10:34567"

	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if response["accepted"] != float64(1) {
		t.Fatalf("accepted = %v, want 1", response["accepted"])
	}

	var payload map[string]any
	if err := json.Unmarshal(logBuffer.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal log payload: %v", err)
	}

	if payload["message"] != "meeting.end_request_failed" {
		t.Fatalf("message = %v, want meeting.end_request_failed", payload["message"])
	}

	if payload["source"] != "frontend" {
		t.Fatalf("source = %v, want frontend", payload["source"])
	}

	if payload["clientTime"] != "2026-03-25T12:00:00.000Z" {
		t.Fatalf("clientTime = %v, want client timestamp", payload["clientTime"])
	}

	if payload["clientIP"] != "192.0.2.10:34567" {
		t.Fatalf("clientIP = %v, want remote addr", payload["clientIP"])
	}

	if payload["scope"] != "frontend.app" {
		t.Fatalf("scope = %v, want frontend.app", payload["scope"])
	}
}

func TestHandleClientLogsRejectsInvalidLog(t *testing.T) {
	server := NewServer(logging.NewBootstrapLogger(&bytes.Buffer{}), nil, nil, nil)

	requestBody := `{"logs":[{"level":"info","message":"missing_time"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/client-logs", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if response["error"] == "" {
		t.Fatal("error response should not be empty")
	}
}
