package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	MailerModeDebug = "debug"
	MailerModeSMTP  = "smtp"
)

type Mailer interface {
	SendVerificationCode(ctx context.Context, message VerificationMessage) (MailDelivery, error)
}

type VerificationMessage struct {
	Email     string
	Purpose   string
	Code      string
	ExpiresAt time.Time
	SentAt    time.Time
	Nickname  string
}

type MailDelivery struct {
	Mode      string
	DebugCode string
}

type MailerConfig struct {
	Mode            string
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPassword    string
	SMTPFromAddress string
	SMTPFromName    string
	SMTPRequireTLS  bool
	SubjectPrefix   string
	Timeout         time.Duration
}

func NewMailer(logger *slog.Logger, config MailerConfig) (Mailer, error) {
	mode := strings.ToLower(strings.TrimSpace(config.Mode))
	if mode == "" {
		mode = MailerModeDebug
	}

	switch mode {
	case MailerModeDebug:
		return NewDebugMailer(logger), nil
	case MailerModeSMTP:
		return NewSMTPMailer(logger, config)
	default:
		return nil, fmt.Errorf("unsupported mailer mode %q", config.Mode)
	}
}
