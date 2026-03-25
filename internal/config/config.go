package config

import "os"

type Config struct {
	HTTPAddr   string
	SQLitePath string
	LogDir     string
}

func Load() Config {
	return Config{
		HTTPAddr:   envOrDefault("MEETING_HTTP_ADDR", ":5180"),
		SQLitePath: envOrDefault("MEETING_SQLITE_PATH", "./data/meeting.db"),
		LogDir:     envOrDefault("MEETING_LOG_DIR", "./logs"),
	}
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
