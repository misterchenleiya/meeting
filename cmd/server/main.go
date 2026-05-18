package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/buildinfo"
	"github.com/misterchenleiya/meeting/internal/config"
	"github.com/misterchenleiya/meeting/internal/httpapi"
	"github.com/misterchenleiya/meeting/internal/logging"
	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/minutes"
	"github.com/misterchenleiya/meeting/internal/signaling"
	"github.com/misterchenleiya/meeting/internal/statistics"
	"github.com/misterchenleiya/meeting/internal/storage/sqlite"
	"github.com/misterchenleiya/meeting/internal/turnauth"
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
	signalContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		SMTPTLSMode:          cfg.SMTPTLSMode,
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

	var authOptions []auth.ServiceOption
	if cfg.WechatMiniProgramAppID != "" && cfg.WechatMiniProgramAppSecret != "" {
		wechatClient, wechatErr := auth.NewWechatMiniProgramClient(logger, auth.WechatMiniProgramClientConfig{
			AppID:      cfg.WechatMiniProgramAppID,
			AppSecret:  cfg.WechatMiniProgramAppSecret,
			APIBaseURL: cfg.WechatMiniProgramAPIBaseURL,
		})
		if wechatErr != nil {
			logger.Error("failed to initialize wechat mini program auth client", "error", wechatErr)
			os.Exit(1)
		}
		authOptions = append(authOptions, auth.WithWechatMiniProgramCodeExchanger(wechatClient))
	}

	authService := auth.NewService(store, mailer, authOptions...)
	meetingService := meeting.NewService(logger, store)
	signalingHub := signaling.NewHub(logger, meetingService)
	asrProvider, err := buildASRProvider(cfg)
	if err != nil {
		logger.Error("failed to initialize asr provider", "error", err)
		os.Exit(1)
	}
	llmProvider, err := buildLLMProvider(cfg)
	if err != nil {
		logger.Error("failed to initialize llm provider", "error", err)
		os.Exit(1)
	}
	minutesService, err := minutes.NewService(logger, minutes.Config{
		Enabled:                    cfg.TranscriptionEnabled,
		ASRProvider:                cfg.ASRProvider,
		ASRChunkMaxBytes:           cfg.ASRChunkMaxBytes,
		DefaultLanguage:            cfg.ASRLanguageDefault,
		MeetingLimitSeconds:        cfg.TranscriptionMeetingLimit,
		DailyLimitSeconds:          cfg.TranscriptionDailyLimit,
		ConcurrentParticipantLimit: cfg.TranscriptionConcurrentMax,
		MinutesJobTimeout:          time.Duration(cfg.MinutesJobTimeoutSeconds) * time.Second,
	}, minutes.Dependencies{
		Store:  store,
		Mailer: mailer,
		ASR:    asrProvider,
		LLM:    llmProvider,
		OnJobCompleted: func(job sqlite.MinutesJobRecord, record sqlite.MeetingMinutesRecord) {
			signalingHub.NotifyMinutesJobCompleted(record.MeetingID, job, record)
		},
	})
	if err != nil {
		logger.Error("failed to initialize minutes service", "error", err)
		os.Exit(1)
	}
	minutesService.Start(signalContext)
	turnService, err := turnauth.NewService(turnauth.Config{
		StunURLs:     turnauth.ParseURLList(cfg.MeetingSTUNURLs),
		TurnURLs:     turnauth.ParseURLList(cfg.MeetingTURNURLs),
		SharedSecret: cfg.MeetingTURNSharedSecret,
		TTL:          time.Duration(cfg.MeetingTURNTTLSeconds) * time.Second,
	})
	if err != nil {
		logger.Error("failed to initialize turn auth service", "error", err)
		os.Exit(1)
	}

	statsReporter, err := statistics.NewReporter(logger, store, mailer, statistics.Config{
		Recipients: cfg.StatsReportRecipients,
		SendAtUTC:  cfg.StatsReportSendAtUTC,
		BuildInfo:  buildinfo.Current(),
	})
	if err != nil {
		logger.Error("failed to initialize traffic statistics reporter", "error", err)
		os.Exit(1)
	}
	if statsReporter.Enabled() {
		go statsReporter.Run(signalContext)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewServer(logger, authService, meetingService, minutesService, store, signalingHub, turnService).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http server started", "addr", cfg.HTTPAddr)
		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Error("http server stopped unexpectedly", "error", serveErr)
			os.Exit(1)
		}
	}()

	<-signalContext.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		logger.Error("failed to shutdown http server", "error", shutdownErr)
		os.Exit(1)
	}

	logger.Info("http server stopped")
}

func buildASRProvider(cfg config.Config) (minutes.ASRProvider, error) {
	if !cfg.TranscriptionEnabled && cfg.TencentASRSecretID == "" && cfg.TencentASRSecretKey == "" && cfg.ASRProvider == minutes.ProviderTencent {
		return minutes.FakeASRProvider{}, nil
	}
	switch cfg.ASRProvider {
	case "", minutes.ProviderFake:
		return minutes.FakeASRProvider{}, nil
	case minutes.ProviderTencent:
		return minutes.NewTencentSentenceASRProvider(minutes.TencentSentenceASRConfig{
			Endpoint:        cfg.ASRAPIBaseURL,
			SecretID:        cfg.TencentASRSecretID,
			SecretKey:       cfg.TencentASRSecretKey,
			Region:          cfg.TencentASRRegion,
			EngineModelType: cfg.TencentASREngineModelType,
			VoiceFormat:     cfg.TencentASRVoiceFormat,
		})
	default:
		return nil, fmt.Errorf("unsupported ASR provider %q", cfg.ASRProvider)
	}
}

func buildLLMProvider(cfg config.Config) (minutes.LLMProvider, error) {
	if !cfg.TranscriptionEnabled && cfg.LLMAPIKey == "" && cfg.LLMProvider == minutes.ProviderDeepSeek {
		return minutes.NewFakeLLMProvider(cfg.LLMModel), nil
	}
	switch cfg.LLMProvider {
	case "", minutes.ProviderFake:
		return minutes.NewFakeLLMProvider(cfg.LLMModel), nil
	case minutes.ProviderDeepSeek:
		return minutes.NewDeepSeekProvider(minutes.DeepSeekConfig{
			BaseURL: cfg.LLMAPIBaseURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
		})
	default:
		return nil, fmt.Errorf("unsupported LLM provider %q", cfg.LLMProvider)
	}
}
