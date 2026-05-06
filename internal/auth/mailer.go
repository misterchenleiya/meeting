package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	MailerModeDebug        = "debug"
	MailerModeSMTP         = "smtp"
	MailerModeSendCloudAPI = "sendcloud_api"

	SMTPTLSModeStartTLS = "starttls"
	SMTPTLSModeImplicit = "implicit"
	SMTPTLSModeAuto     = "auto"
)

type Mailer interface {
	SendVerificationCode(ctx context.Context, message VerificationMessage) (MailDelivery, error)
	SendEmail(ctx context.Context, message EmailMessage) (MailDelivery, error)
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

type EmailMessage struct {
	To             []string
	Subject        string
	TextBody       string
	HTMLBody       string
	ContentSummary string
	Attachments    []MailAttachment
}

type MailAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

type MailerConfig struct {
	Mode                 string
	SMTPHost             string
	SMTPPort             int
	SMTPUsername         string
	SMTPPassword         string
	SMTPFromAddress      string
	SMTPFromName         string
	SMTPRequireTLS       bool
	SMTPTLSMode          string
	SendCloudAPIBaseURL  string
	SendCloudAPIUser     string
	SendCloudAPIKey      string
	SendCloudFromAddress string
	SendCloudFromName    string
	SubjectPrefix        string
	Timeout              time.Duration
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
	case MailerModeSendCloudAPI:
		return NewSendCloudAPIMailer(logger, config)
	default:
		return nil, fmt.Errorf("unsupported mailer mode %q", config.Mode)
	}
}
