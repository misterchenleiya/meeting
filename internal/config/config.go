package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr              string
	SQLitePath            string
	LogDir                string
	MailerMode            string
	SMTPHost              string
	SMTPPort              int
	SMTPUsername          string
	SMTPPassword          string
	SMTPFromAddress       string
	SMTPFromName          string
	SMTPRequireTLS        bool
	SendCloudAPIBaseURL   string
	SendCloudAPIUser      string
	SendCloudAPIKey       string
	SendCloudFromAddress  string
	SendCloudFromName     string
	AuthCodeSubjectPrefix string
}

func Load() (Config, error) {
	smtpPort, err := envIntOrDefault("MEETING_SMTP_PORT", 587)
	if err != nil {
		return Config{}, err
	}

	smtpRequireTLS, err := envBoolOrDefault("MEETING_SMTP_REQUIRE_TLS", true)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:              envOrDefault("MEETING_HTTP_ADDR", ":5180"),
		SQLitePath:            envOrDefault("MEETING_SQLITE_PATH", "./data/meeting.db"),
		LogDir:                envOrDefault("MEETING_LOG_DIR", "./logs"),
		MailerMode:            strings.ToLower(envOrDefault("MEETING_MAILER_MODE", "debug")),
		SMTPHost:              envOrDefault("MEETING_SMTP_HOST", ""),
		SMTPPort:              smtpPort,
		SMTPUsername:          envOrDefault("MEETING_SMTP_USERNAME", ""),
		SMTPPassword:          envOrDefault("MEETING_SMTP_PASSWORD", ""),
		SMTPFromAddress:       envOrDefault("MEETING_SMTP_FROM_ADDRESS", ""),
		SMTPFromName:          envOrDefault("MEETING_SMTP_FROM_NAME", "meeting"),
		SMTPRequireTLS:        smtpRequireTLS,
		SendCloudAPIBaseURL:   envOrDefault("MEETING_SENDCLOUD_API_BASE_URL", "https://api.sendcloud.net/apiv2"),
		SendCloudAPIUser:      envOrDefault("MEETING_SENDCLOUD_API_USER", ""),
		SendCloudAPIKey:       envOrDefault("MEETING_SENDCLOUD_API_KEY", ""),
		SendCloudFromAddress:  envOrDefault("MEETING_SENDCLOUD_FROM_ADDRESS", "no-reply@mail.07c2.com.cn"),
		SendCloudFromName:     envOrDefault("MEETING_SENDCLOUD_FROM_NAME", "meeting"),
		AuthCodeSubjectPrefix: envOrDefault("MEETING_AUTH_CODE_SUBJECT_PREFIX", "[meeting]"),
	}, nil
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}

	return parsed, nil
}

func envBoolOrDefault(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}

	return parsed, nil
}
