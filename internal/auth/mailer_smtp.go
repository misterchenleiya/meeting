package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

const defaultSMTPTimeout = 10 * time.Second

type SMTPMailer struct {
	logger        *slog.Logger
	host          string
	port          int
	username      string
	password      string
	fromAddress   string
	fromName      string
	requireTLS    bool
	subjectPrefix string
	timeout       time.Duration
}

func NewSMTPMailer(logger *slog.Logger, config MailerConfig) (*SMTPMailer, error) {
	host := strings.TrimSpace(config.SMTPHost)
	if host == "" {
		return nil, fmt.Errorf("smtp host is required")
	}
	if config.SMTPPort <= 0 {
		return nil, fmt.Errorf("smtp port must be greater than 0")
	}

	fromAddress := strings.TrimSpace(config.SMTPFromAddress)
	if fromAddress == "" {
		return nil, fmt.Errorf("smtp from address is required")
	}
	if _, err := mail.ParseAddress(fromAddress); err != nil {
		return nil, fmt.Errorf("invalid smtp from address: %w", err)
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}

	return &SMTPMailer{
		logger:        logger,
		host:          host,
		port:          config.SMTPPort,
		username:      strings.TrimSpace(config.SMTPUsername),
		password:      config.SMTPPassword,
		fromAddress:   fromAddress,
		fromName:      strings.TrimSpace(config.SMTPFromName),
		requireTLS:    config.SMTPRequireTLS,
		subjectPrefix: strings.TrimSpace(config.SubjectPrefix),
		timeout:       timeout,
	}, nil
}

func (m *SMTPMailer) SendVerificationCode(ctx context.Context, message VerificationMessage) (MailDelivery, error) {
	subject := m.buildSubject(message.Purpose)
	body := m.buildBody(message)
	payload := m.buildPayload(message.Email, subject, body)

	if err := m.send(ctx, message.Email, payload); err != nil {
		if m.logger != nil {
			m.logger.Error(
				"failed to send verification code email",
				"email", message.Email,
				"purpose", message.Purpose,
				"error", err,
			)
		}
		return MailDelivery{}, err
	}

	if m.logger != nil {
		m.logger.Info(
			"verification code email sent",
			"email", message.Email,
			"purpose", message.Purpose,
			"expiresAt", message.ExpiresAt,
			"deliveryMode", MailerModeSMTP,
		)
	}

	return MailDelivery{Mode: MailerModeSMTP}, nil
}

func (m *SMTPMailer) send(ctx context.Context, recipient string, payload []byte) error {
	address := net.JoinHostPort(m.host, fmt.Sprintf("%d", m.port))
	dialer := &net.Dialer{Timeout: m.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial smtp server: %w", err)
	}

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create smtp client: %w", err)
	}

	quit := false
	defer func() {
		if !quit {
			_ = client.Close()
		}
	}()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{
			ServerName: m.host,
			MinVersion: tls.VersionTLS12,
		}); err != nil {
			return fmt.Errorf("start tls: %w", err)
		}
	} else if m.requireTLS {
		return fmt.Errorf("smtp server does not support STARTTLS")
	}

	if m.username != "" {
		auth := smtp.PlainAuth("", m.username, m.password, m.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.fromAddress); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write smtp payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close smtp payload: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit smtp session: %w", err)
	}
	quit = true
	return nil
}

func (m *SMTPMailer) buildSubject(purpose string) string {
	base := "meeting 验证码"
	switch purpose {
	case registerPurpose:
		base = "meeting 注册验证码"
	case loginPurpose:
		base = "meeting 登录验证码"
	}

	if m.subjectPrefix == "" {
		return base
	}
	return strings.TrimSpace(m.subjectPrefix) + " " + base
}

func (m *SMTPMailer) buildBody(message VerificationMessage) string {
	expiryText := message.ExpiresAt.Local().Format("2006-01-02 15:04:05")
	switch message.Purpose {
	case registerPurpose:
		return fmt.Sprintf(
			"您好，%s：\n\n您的 meeting 注册验证码为：%s\n验证码将于 %s 失效。\n\n如果这不是您的操作，请忽略本邮件。\n",
			messageNickname(message),
			message.Code,
			expiryText,
		)
	default:
		return fmt.Sprintf(
			"您好：\n\n您的 meeting 登录验证码为：%s\n验证码将于 %s 失效。\n\n如果这不是您的操作，请忽略本邮件。\n",
			message.Code,
			expiryText,
		)
	}
}

func (m *SMTPMailer) buildPayload(recipient string, subject string, body string) []byte {
	from := m.fromAddress
	if m.fromName != "" {
		from = (&mail.Address{Name: m.fromName, Address: m.fromAddress}).String()
	}

	payload := strings.Join([]string{
		"From: " + from,
		"To: " + recipient,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	return []byte(payload)
}

func messageNickname(message VerificationMessage) string {
	nickname := strings.TrimSpace(message.Nickname)
	if nickname == "" {
		return "用户"
	}
	return nickname
}
