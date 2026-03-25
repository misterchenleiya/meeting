package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	retentionWindow = 72 * time.Hour
	rotationWindow  = 24 * time.Hour
	cleanupInterval = time.Hour
	logFileName     = "meeting"
)

type rotatingFileWriter struct {
	mu sync.Mutex

	logDir string
	now    func() time.Time

	currentFile   *os.File
	openedAt      time.Time
	lastCleanupAt time.Time
}

func NewLogger(logDir string) (*slog.Logger, func() error, error) {
	fileWriter, err := newRotatingFileWriter(logDir, time.Now)
	if err != nil {
		return nil, nil, err
	}

	handler := newJSONHandler(io.MultiWriter(os.Stdout, fileWriter))
	return slog.New(handler), fileWriter.Close, nil
}

func NewBootstrapLogger(writer io.Writer) *slog.Logger {
	return slog.New(newJSONHandler(writer))
}

func newRotatingFileWriter(logDir string, now func() time.Time) (*rotatingFileWriter, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	writer := &rotatingFileWriter{
		logDir: logDir,
		now:    now,
	}

	if err := writer.ensureFileLocked(now().UTC()); err != nil {
		return nil, err
	}

	return writer, nil
}

func newJSONHandler(writer io.Writer) slog.Handler {
	return slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.MessageKey:
				attr.Key = "message"
			case slog.LevelKey:
				attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
			case slog.TimeKey:
				attr.Value = slog.StringValue(attr.Value.Time().UTC().Format(time.RFC3339Nano))
			}
			return attr
		},
	})
}

func (w *rotatingFileWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	currentTime := w.now().UTC()
	if err := w.ensureFileLocked(currentTime); err != nil {
		return 0, err
	}

	return w.currentFile.Write(payload)
}

func (w *rotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentFile == nil {
		return nil
	}

	closeErr := w.currentFile.Close()
	w.currentFile = nil
	return closeErr
}

func (w *rotatingFileWriter) ensureFileLocked(currentTime time.Time) error {
	needsRotation := w.currentFile == nil || currentTime.Before(w.openedAt) || currentTime.Sub(w.openedAt) >= rotationWindow
	if needsRotation {
		return w.rotateLocked(currentTime)
	}

	if w.lastCleanupAt.IsZero() || currentTime.Sub(w.lastCleanupAt) >= cleanupInterval {
		if err := cleanupExpiredLogs(w.logDir, currentTime.Add(-retentionWindow)); err != nil {
			return fmt.Errorf("cleanup old logs: %w", err)
		}
		w.lastCleanupAt = currentTime
	}

	return nil
}

func (w *rotatingFileWriter) rotateLocked(currentTime time.Time) error {
	if err := cleanupExpiredLogs(w.logDir, currentTime.Add(-retentionWindow)); err != nil {
		return fmt.Errorf("cleanup old logs: %w", err)
	}

	if w.currentFile != nil {
		if err := w.currentFile.Close(); err != nil {
			return fmt.Errorf("close log file: %w", err)
		}
	}

	fileName := currentTime.Format("2006-01-02_150405") + "_" + logFileName + ".log"
	logFilePath := filepath.Join(w.logDir, fileName)
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	w.currentFile = logFile
	w.openedAt = currentTime
	w.lastCleanupAt = currentTime
	return nil
}

func cleanupExpiredLogs(logDir string, cutoff time.Time) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}

		if info.ModTime().Before(cutoff) {
			if removeErr := os.Remove(filepath.Join(logDir, entry.Name())); removeErr != nil {
				return removeErr
			}
		}
	}

	return nil
}
