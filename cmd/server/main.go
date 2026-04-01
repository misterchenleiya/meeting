package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/config"
	"github.com/misterchenleiya/meeting/internal/httpapi"
	"github.com/misterchenleiya/meeting/internal/logging"
	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/signaling"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
)

func main() {
	bootstrapLogger := logging.NewBootstrapLogger(os.Stderr)
	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger, closeLogger, err := logging.NewLogger(cfg.LogDir)
	if err != nil {
		bootstrapLogger.Error("failed to initialize logger", "error", err, "logDir", cfg.LogDir)
		os.Exit(1)
	}
	defer closeLogger()

	ctx := context.Background()

	store, err := sqlite.Open(ctx, cfg.SQLitePath)
	if err != nil {
		logger.Error("failed to open sqlite store", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			logger.Error("failed to close sqlite store", "error", closeErr)
		}
	}()

	mailer, err := auth.NewMailer(logger, auth.MailerConfig{
		Mode:                 cfg.MailerMode,
		SMTPHost:             cfg.SMTPHost,
		SMTPPort:             cfg.SMTPPort,
		SMTPUsername:         cfg.SMTPUsername,
		SMTPPassword:         cfg.SMTPPassword,
		SMTPFromAddress:      cfg.SMTPFromAddress,
		SMTPFromName:         cfg.SMTPFromName,
		SMTPRequireTLS:       cfg.SMTPRequireTLS,
		SendCloudAPIBaseURL:  cfg.SendCloudAPIBaseURL,
		SendCloudAPIUser:     cfg.SendCloudAPIUser,
		SendCloudAPIKey:      cfg.SendCloudAPIKey,
		SendCloudFromAddress: cfg.SendCloudFromAddress,
		SendCloudFromName:    cfg.SendCloudFromName,
		SubjectPrefix:        cfg.AuthCodeSubjectPrefix,
	})
	if err != nil {
		logger.Error("failed to initialize auth mailer", "error", err)
		os.Exit(1)
	}

	authService := auth.NewService(store, mailer)
	meetingService := meeting.NewService(logger, store)
	signalingHub := signaling.NewHub(logger, meetingService)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewServer(logger, authService, meetingService, store, signalingHub).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http server started", "addr", cfg.HTTPAddr)
		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Error("http server stopped unexpectedly", "error", serveErr)
			os.Exit(1)
		}
	}()

	signalContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-signalContext.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		logger.Error("failed to shutdown http server", "error", shutdownErr)
		os.Exit(1)
	}

	logger.Info("http server stopped")
}
