package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
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
	tlsMode       string
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

	tlsMode, err := normalizeSMTPTLSMode(config.SMTPTLSMode)
	if err != nil {
		return nil, err
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
		tlsMode:       tlsMode,
		subjectPrefix: strings.TrimSpace(config.SubjectPrefix),
		timeout:       timeout,
	}, nil
}

func (m *SMTPMailer) SendVerificationCode(ctx context.Context, message VerificationMessage) (MailDelivery, error) {
	subject := m.buildSubject(message.Purpose)
	body := m.buildBody(message)
	payload, err := m.buildPayload(EmailMessage{
		To:       []string{message.Email},
		Subject:  subject,
		TextBody: body,
	})
	if err != nil {
		return MailDelivery{}, err
	}

	if err := m.send(ctx, []string{message.Email}, payload); err != nil {
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

func (m *SMTPMailer) SendEmail(ctx context.Context, message EmailMessage) (MailDelivery, error) {
	recipients := normalizeRecipients(message.To)
	if len(recipients) == 0 {
		return MailDelivery{}, fmt.Errorf("email recipient is required")
	}
	if strings.TrimSpace(message.Subject) == "" {
		return MailDelivery{}, fmt.Errorf("email subject is required")
	}

	payload, err := m.buildPayload(message)
	if err != nil {
		return MailDelivery{}, err
	}
	if err := m.send(ctx, recipients, payload); err != nil {
		if m.logger != nil {
			m.logger.Error(
				"failed to send email",
				"to", strings.Join(recipients, ","),
				"subject", message.Subject,
				"error", err,
			)
		}
		return MailDelivery{}, err
	}

	if m.logger != nil {
		m.logger.Info(
			"email sent",
			"to", strings.Join(recipients, ","),
			"subject", message.Subject,
			"attachmentCount", len(message.Attachments),
			"deliveryMode", MailerModeSMTP,
		)
	}

	return MailDelivery{Mode: MailerModeSMTP}, nil
}

func (m *SMTPMailer) send(ctx context.Context, recipients []string, payload []byte) error {
	address := net.JoinHostPort(m.host, fmt.Sprintf("%d", m.port))
	dialer := &net.Dialer{Timeout: m.timeout}
	client, err := m.connect(ctx, dialer, address)
	if err != nil {
		return err
	}

	quit := false
	defer func() {
		if !quit {
			_ = client.Close()
		}
	}()

	if m.username != "" {
		auth := m.smtpAuth()
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.fromAddress); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp rcpt to %s: %w", recipient, err)
		}
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

func (m *SMTPMailer) smtpAuth() smtp.Auth {
	if m.effectiveTLSMode() == SMTPTLSModeImplicit {
		return &implicitTLSPlainAuth{
			username: m.username,
			password: m.password,
		}
	}
	return smtp.PlainAuth("", m.username, m.password, m.host)
}

type implicitTLSPlainAuth struct {
	username string
	password string
}

func (a *implicitTLSPlainAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	response := []byte("\x00" + a.username + "\x00" + a.password)
	return "PLAIN", response, nil
}

func (a *implicitTLSPlainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, fmt.Errorf("unexpected smtp plain auth challenge")
	}
	return nil, nil
}

func (m *SMTPMailer) connect(ctx context.Context, dialer *net.Dialer, address string) (*smtp.Client, error) {
	switch m.effectiveTLSMode() {
	case SMTPTLSModeImplicit:
		conn, err := dialImplicitTLS(ctx, dialer, address, smtpTLSConfig(m.host))
		if err != nil {
			return nil, fmt.Errorf("dial smtp server with implicit tls: %w", err)
		}
		client, err := smtp.NewClient(conn, m.host)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("create smtp client: %w", err)
		}
		return client, nil
	default:
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("dial smtp server: %w", err)
		}
		client, err := smtp.NewClient(conn, m.host)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("create smtp client: %w", err)
		}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(smtpTLSConfig(m.host)); err != nil {
				_ = client.Close()
				return nil, fmt.Errorf("start tls: %w", err)
			}
		} else if m.requireTLS {
			_ = client.Close()
			return nil, fmt.Errorf("smtp server does not support STARTTLS")
		}
		return client, nil
	}
}

func dialImplicitTLS(ctx context.Context, dialer *net.Dialer, address string, config *tls.Config) (*tls.Conn, error) {
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(conn, config)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (m *SMTPMailer) effectiveTLSMode() string {
	if m.tlsMode != SMTPTLSModeAuto {
		return m.tlsMode
	}
	if m.port == 465 {
		return SMTPTLSModeImplicit
	}
	return SMTPTLSModeStartTLS
}

func normalizeSMTPTLSMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return SMTPTLSModeStartTLS, nil
	}

	switch normalized {
	case SMTPTLSModeStartTLS, SMTPTLSModeImplicit, SMTPTLSModeAuto:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported smtp tls mode %q", mode)
	}
}

func smtpTLSConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
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

func (m *SMTPMailer) buildPayload(message EmailMessage) ([]byte, error) {
	from := m.fromAddress
	if m.fromName != "" {
		from = (&mail.Address{Name: m.fromName, Address: m.fromAddress}).String()
	}

	headers := []string{
		"From: " + from,
		"To: " + strings.Join(normalizeRecipients(message.To), ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", message.Subject),
		"MIME-Version: 1.0",
	}
	if len(message.Attachments) == 0 {
		body, contentType := messageBody(message)
		headers = append(headers, "Content-Type: "+contentType+"; charset=UTF-8")
		payload := strings.Join(append(headers, "", body), "\r\n")
		return []byte(payload), nil
	}

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	headers = append(headers, "Content-Type: multipart/mixed; boundary="+writer.Boundary(), "")
	for _, header := range headers {
		if _, err := buffer.WriteString(header + "\r\n"); err != nil {
			return nil, fmt.Errorf("write smtp header: %w", err)
		}
	}

	body, contentType := messageBody(message)
	bodyHeader := textproto.MIMEHeader{}
	bodyHeader.Set("Content-Type", contentType+"; charset=UTF-8")
	bodyHeader.Set("Content-Transfer-Encoding", "8bit")
	bodyPart, err := writer.CreatePart(bodyHeader)
	if err != nil {
		return nil, fmt.Errorf("create smtp body part: %w", err)
	}
	if _, err := bodyPart.Write([]byte(body)); err != nil {
		return nil, fmt.Errorf("write smtp body part: %w", err)
	}

	for _, attachment := range message.Attachments {
		partHeader := textproto.MIMEHeader{}
		contentType := strings.TrimSpace(attachment.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		filename := strings.TrimSpace(attachment.Filename)
		if filename == "" {
			filename = "attachment"
		}
		partHeader.Set("Content-Type", mime.FormatMediaType(contentType, map[string]string{"name": filename}))
		partHeader.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
		partHeader.Set("Content-Transfer-Encoding", "base64")
		part, err := writer.CreatePart(partHeader)
		if err != nil {
			return nil, fmt.Errorf("create smtp attachment part: %w", err)
		}
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(attachment.Data)))
		base64.StdEncoding.Encode(encoded, attachment.Data)
		if err := writeWrappedBase64(part, encoded); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close smtp multipart payload: %w", err)
	}
	payload := buffer.String()

	return []byte(payload), nil
}

func messageBody(message EmailMessage) (string, string) {
	if strings.TrimSpace(message.HTMLBody) != "" {
		return message.HTMLBody, "text/html"
	}
	return message.TextBody, "text/plain"
}

func writeWrappedBase64(part interface{ Write([]byte) (int, error) }, encoded []byte) error {
	for len(encoded) > 0 {
		lineLength := 76
		if len(encoded) < lineLength {
			lineLength = len(encoded)
		}
		if _, err := part.Write(encoded[:lineLength]); err != nil {
			return fmt.Errorf("write smtp attachment body: %w", err)
		}
		if _, err := part.Write([]byte("\r\n")); err != nil {
			return fmt.Errorf("write smtp attachment newline: %w", err)
		}
		encoded = encoded[lineLength:]
	}
	return nil
}

func normalizeRecipients(recipients []string) []string {
	normalized := make([]string, 0, len(recipients))
	seen := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		trimmed := strings.TrimSpace(recipient)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func messageNickname(message VerificationMessage) string {
	nickname := strings.TrimSpace(message.Nickname)
	if nickname == "" {
		return "用户"
	}
	return nickname
}
