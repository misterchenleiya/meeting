package auth

import (
	"context"
	"log/slog"
)

type DebugMailer struct {
	logger *slog.Logger
}

func NewDebugMailer(logger *slog.Logger) *DebugMailer {
	return &DebugMailer{
		logger: logger,
	}
}

func (m *DebugMailer) SendVerificationCode(_ context.Context, message VerificationMessage) (MailDelivery, error) {
	if m.logger != nil {
		m.logger.Info(
			"verification code generated in debug mode",
			"email", message.Email,
			"purpose", message.Purpose,
			"expiresAt", message.ExpiresAt,
		)
	}

	return MailDelivery{
		Mode:      MailerModeDebug,
		DebugCode: message.Code,
	}, nil
}
