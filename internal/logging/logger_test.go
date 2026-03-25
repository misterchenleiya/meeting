package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"log/slog"
)

func TestJSONHandlerUsesRequiredFields(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	logger := slog.New(newJSONHandler(&buffer))
	logger.Info("logger ready", "component", "test")

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if payload["message"] != "logger ready" {
		t.Fatalf("message = %v, want logger ready", payload["message"])
	}

	if payload["level"] != "info" {
		t.Fatalf("level = %v, want info", payload["level"])
	}

	if _, ok := payload["time"]; !ok {
		t.Fatalf("time field is missing")
	}

	if _, ok := payload["msg"]; ok {
		t.Fatalf("msg field should not exist")
	}
}

func TestRotatingFileWriterRotatesAndCleansExpiredFiles(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	oldPath := filepath.Join(logDir, "2026-03-20_010000_meeting.log")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldTime := time.Date(2026, 3, 20, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	currentTime := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	writer, err := newRotatingFileWriter(logDir, func() time.Time { return currentTime })
	if err != nil {
		t.Fatalf("newRotatingFileWriter() error = %v", err)
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil {
			t.Fatalf("Close() error = %v", closeErr)
		}
	}()

	if _, err := writer.Write([]byte("first log\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	currentTime = currentTime.Add(25 * time.Hour)
	if _, err := writer.Write([]byte("second log\n")); err != nil {
		t.Fatalf("Write() after rotation error = %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old log still exists, stat err = %v", err)
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	fileNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileNames = append(fileNames, entry.Name())
	}
	sort.Strings(fileNames)

	expected := []string{
		"2026-03-25_100000_meeting.log",
		"2026-03-26_110000_meeting.log",
	}
	if len(fileNames) != len(expected) {
		t.Fatalf("log file count = %d, want %d (%v)", len(fileNames), len(expected), fileNames)
	}

	for index, name := range expected {
		if fileNames[index] != name {
			t.Fatalf("log file %d = %s, want %s", index, fileNames[index], name)
		}
	}
}
