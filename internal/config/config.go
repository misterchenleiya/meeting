package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr                    string
	SQLitePath                  string
	LogDir                      string
	MeetingSTUNURLs             string
	MeetingTURNURLs             string
	MeetingTURNSharedSecret     string
	MeetingTURNTTLSeconds       int
	MailerMode                  string
	SMTPHost                    string
	SMTPPort                    int
	SMTPUsername                string
	SMTPPassword                string
	SMTPFromAddress             string
	SMTPFromName                string
	SMTPRequireTLS              bool
	SMTPTLSMode                 string
	SendCloudAPIBaseURL         string
	SendCloudAPIUser            string
	SendCloudAPIKey             string
	SendCloudFromAddress        string
	SendCloudFromName           string
	StatsReportRecipients       []string
	StatsReportSendAtUTC        string
	TranscriptionEnabled        bool
	ASRProvider                 string
	ASRAPIBaseURL               string
	TencentASRAppID             string
	TencentASRSecretID          string
	TencentASRSecretKey         string
	TencentASRRegion            string
	TencentASREngineModelType   string
	TencentASRVoiceFormat       string
	ASRLanguageDefault          string
	ASRChunkMaxBytes            int
	TranscriptionMeetingLimit   int
	TranscriptionDailyLimit     int
	TranscriptionConcurrentMax  int
	LLMProvider                 string
	LLMAPIBaseURL               string
	LLMAPIKey                   string
	LLMModel                    string
	MinutesJobTimeoutSeconds    int
	MinutesEmailFrom            string
	WechatMiniProgramAppID      string
	WechatMiniProgramAppSecret  string
	WechatMiniProgramAPIBaseURL string
	AuthCodeSubjectPrefix       string
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

	turnTTLSeconds, err := envIntOrDefault("MEETING_TURN_TTL_SECONDS", 43200)
	if err != nil {
		return Config{}, err
	}

	transcriptionEnabled, err := envBoolOrDefault("MEETING_TRANSCRIPTION_ENABLED", false)
	if err != nil {
		return Config{}, err
	}

	asrChunkMaxBytes, err := envIntOrDefault("MEETING_ASR_CHUNK_MAX_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}

	transcriptionMeetingLimit, err := envIntOrDefault("MEETING_TRANSCRIPTION_MEETING_LIMIT_SECONDS", 3600)
	if err != nil {
		return Config{}, err
	}

	transcriptionDailyLimit, err := envIntOrDefault("MEETING_TRANSCRIPTION_DAILY_LIMIT_SECONDS", 7200)
	if err != nil {
		return Config{}, err
	}

	transcriptionConcurrentMax, err := envIntOrDefault("MEETING_TRANSCRIPTION_CONCURRENT_PARTICIPANTS", 3)
	if err != nil {
		return Config{}, err
	}

	minutesJobTimeoutSeconds, err := envIntOrDefault("MEETING_MINUTES_JOB_TIMEOUT_SECONDS", 600)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:                    envOrDefault("MEETING_HTTP_ADDR", ":5180"),
		SQLitePath:                  envOrDefault("MEETING_SQLITE_PATH", "./data/meeting.db"),
		LogDir:                      envOrDefault("MEETING_LOG_DIR", "./logs"),
		MeetingSTUNURLs:             envOrDefault("MEETING_STUN_URLS", "stun:stun.l.google.com:19302"),
		MeetingTURNURLs:             envOrDefault("MEETING_TURN_URLS", ""),
		MeetingTURNSharedSecret:     envOrDefault("MEETING_TURN_SHARED_SECRET", ""),
		MeetingTURNTTLSeconds:       turnTTLSeconds,
		MailerMode:                  strings.ToLower(envOrDefault("MEETING_MAILER_MODE", "debug")),
		SMTPHost:                    envOrDefault("MEETING_SMTP_HOST", ""),
		SMTPPort:                    smtpPort,
		SMTPUsername:                envOrDefault("MEETING_SMTP_USERNAME", ""),
		SMTPPassword:                envOrDefault("MEETING_SMTP_PASSWORD", ""),
		SMTPFromAddress:             envOrDefault("MEETING_SMTP_FROM_ADDRESS", ""),
		SMTPFromName:                envOrDefault("MEETING_SMTP_FROM_NAME", "meeting"),
		SMTPRequireTLS:              smtpRequireTLS,
		SMTPTLSMode:                 strings.ToLower(envOrDefault("MEETING_SMTP_TLS_MODE", "starttls")),
		SendCloudAPIBaseURL:         envOrDefault("MEETING_SENDCLOUD_API_BASE_URL", "https://api.sendcloud.net/apiv2"),
		SendCloudAPIUser:            envOrDefault("MEETING_SENDCLOUD_API_USER", ""),
		SendCloudAPIKey:             envOrDefault("MEETING_SENDCLOUD_API_KEY", ""),
		SendCloudFromAddress:        envOrDefault("MEETING_SENDCLOUD_FROM_ADDRESS", "no-reply@mail.07c2.com.cn"),
		SendCloudFromName:           envOrDefault("MEETING_SENDCLOUD_FROM_NAME", "meeting"),
		StatsReportRecipients:       envList("MEETING_STATS_REPORT_TO"),
		StatsReportSendAtUTC:        envOrDefault("MEETING_STATS_REPORT_SEND_AT_UTC", "12:00"),
		TranscriptionEnabled:        transcriptionEnabled,
		ASRProvider:                 strings.ToLower(envOrDefault("MEETING_ASR_PROVIDER", "tencent")),
		ASRAPIBaseURL:               envOrDefault("MEETING_ASR_API_BASE_URL", "https://asr.tencentcloudapi.com"),
		TencentASRAppID:             envOrDefault("MEETING_TENCENT_ASR_APP_ID", ""),
		TencentASRSecretID:          envOrDefault("MEETING_TENCENT_ASR_SECRET_ID", ""),
		TencentASRSecretKey:         envOrDefault("MEETING_TENCENT_ASR_SECRET_KEY", ""),
		TencentASRRegion:            envOrDefault("MEETING_TENCENT_ASR_REGION", "ap-shanghai"),
		TencentASREngineModelType:   envOrDefault("MEETING_TENCENT_ASR_ENGINE_MODEL_TYPE", "16k_zh"),
		TencentASRVoiceFormat:       envOrDefault("MEETING_TENCENT_ASR_VOICE_FORMAT", "wav"),
		ASRLanguageDefault:          envOrDefault("MEETING_ASR_LANGUAGE_DEFAULT", "zh-CN"),
		ASRChunkMaxBytes:            asrChunkMaxBytes,
		TranscriptionMeetingLimit:   transcriptionMeetingLimit,
		TranscriptionDailyLimit:     transcriptionDailyLimit,
		TranscriptionConcurrentMax:  transcriptionConcurrentMax,
		LLMProvider:                 strings.ToLower(envOrDefault("MEETING_LLM_PROVIDER", "deepseek")),
		LLMAPIBaseURL:               envOrDefault("MEETING_LLM_API_BASE_URL", "https://api.deepseek.com"),
		LLMAPIKey:                   envOrDefault("MEETING_LLM_API_KEY", ""),
		LLMModel:                    envOrDefault("MEETING_LLM_MODEL", "deepseek-v4-flash"),
		MinutesJobTimeoutSeconds:    minutesJobTimeoutSeconds,
		MinutesEmailFrom:            envOrDefault("MEETING_MINUTES_EMAIL_FROM", ""),
		WechatMiniProgramAppID:      envOrDefault("MEETING_WECHAT_MINIPROGRAM_APP_ID", ""),
		WechatMiniProgramAppSecret:  envOrDefault("MEETING_WECHAT_MINIPROGRAM_APP_SECRET", ""),
		WechatMiniProgramAPIBaseURL: envOrDefault("MEETING_WECHAT_MINIPROGRAM_API_BASE_URL", "https://api.weixin.qq.com"),
		AuthCodeSubjectPrefix:       envOrDefault("MEETING_AUTH_CODE_SUBJECT_PREFIX", "[meeting]"),
	}, nil
}

func envList(key string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	values := strings.Split(raw, ",")
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	return result
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
