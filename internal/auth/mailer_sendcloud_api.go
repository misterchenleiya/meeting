package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultSendCloudAPIBaseURL = "https://api.sendcloud.net/apiv2"

type SendCloudAPIMailer struct {
	logger        *slog.Logger
	baseURL       string
	apiUser       string
	apiKey        string
	fromAddress   string
	fromName      string
	subjectPrefix string
	timeout       time.Duration
	httpClient    *http.Client
}

type sendCloudSendResponse struct {
	Result     bool   `json:"result"`
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
}

func NewSendCloudAPIMailer(logger *slog.Logger, config MailerConfig) (*SendCloudAPIMailer, error) {
	apiUser := strings.TrimSpace(config.SendCloudAPIUser)
	if apiUser == "" {
		return nil, fmt.Errorf("sendcloud api user is required")
	}

	apiKey := strings.TrimSpace(config.SendCloudAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("sendcloud api key is required")
	}

	fromAddress := strings.TrimSpace(config.SendCloudFromAddress)
	if fromAddress == "" {
		return nil, fmt.Errorf("sendcloud from address is required")
	}

	baseURL := strings.TrimSpace(config.SendCloudAPIBaseURL)
	if baseURL == "" {
		baseURL = defaultSendCloudAPIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}

	return &SendCloudAPIMailer{
		logger:        logger,
		baseURL:       baseURL,
		apiUser:       apiUser,
		apiKey:        apiKey,
		fromAddress:   fromAddress,
		fromName:      strings.TrimSpace(config.SendCloudFromName),
		subjectPrefix: strings.TrimSpace(config.SubjectPrefix),
		timeout:       timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (m *SendCloudAPIMailer) SendVerificationCode(ctx context.Context, message VerificationMessage) (MailDelivery, error) {
	form := url.Values{}
	form.Set("apiUser", m.apiUser)
	form.Set("apiKey", m.apiKey)
	form.Set("from", m.fromAddress)
	form.Set("to", message.Email)
	form.Set("subject", m.buildSubject(message.Purpose))
	form.Set("html", m.buildHTMLBody(message))
	form.Set("contentSummary", m.buildSummary(message.Purpose))
	if m.fromName != "" {
		form.Set("fromName", m.fromName)
	}

	if err := m.sendForm(ctx, form); err != nil {
		if m.logger != nil {
			m.logger.Error(
				"failed to call sendcloud api",
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
			"deliveryMode", MailerModeSendCloudAPI,
		)
	}

	return MailDelivery{Mode: MailerModeSendCloudAPI}, nil
}

func (m *SendCloudAPIMailer) SendEmail(ctx context.Context, message EmailMessage) (MailDelivery, error) {
	recipients := normalizeRecipients(message.To)
	if len(recipients) == 0 {
		return MailDelivery{}, fmt.Errorf("email recipient is required")
	}
	if strings.TrimSpace(message.Subject) == "" {
		return MailDelivery{}, fmt.Errorf("email subject is required")
	}

	form := url.Values{}
	form.Set("apiUser", m.apiUser)
	form.Set("apiKey", m.apiKey)
	form.Set("from", m.fromAddress)
	form.Set("to", strings.Join(recipients, ";"))
	form.Set("subject", message.Subject)
	form.Set("contentSummary", contentSummary(message))
	if m.fromName != "" {
		form.Set("fromName", m.fromName)
	}
	if strings.TrimSpace(message.HTMLBody) != "" {
		form.Set("html", message.HTMLBody)
	} else {
		form.Set("plain", message.TextBody)
	}

	var err error
	if len(message.Attachments) > 0 {
		err = m.sendMultipart(ctx, form, message.Attachments)
	} else {
		err = m.sendForm(ctx, form)
	}
	if err != nil {
		if m.logger != nil {
			m.logger.Error(
				"failed to send email via sendcloud api",
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
			"deliveryMode", MailerModeSendCloudAPI,
		)
	}

	return MailDelivery{Mode: MailerModeSendCloudAPI}, nil
}

func (m *SendCloudAPIMailer) sendForm(ctx context.Context, form url.Values) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		m.baseURL+"/mail/send",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create sendcloud request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return m.doSend(request)
}

func (m *SendCloudAPIMailer) sendMultipart(ctx context.Context, form url.Values, attachments []MailAttachment) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for key, values := range form {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return fmt.Errorf("write sendcloud field %s: %w", key, err)
			}
		}
	}

	for _, attachment := range attachments {
		filename := strings.TrimSpace(attachment.Filename)
		if filename == "" {
			filename = "attachment"
		}
		part, err := writer.CreateFormFile("attachments", filename)
		if err != nil {
			return fmt.Errorf("create sendcloud attachment %s: %w", filename, err)
		}
		if _, err := io.Copy(part, bytes.NewReader(attachment.Data)); err != nil {
			return fmt.Errorf("write sendcloud attachment %s: %w", filename, err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close sendcloud multipart payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/mail/send", &body)
	if err != nil {
		return fmt.Errorf("create sendcloud multipart request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	return m.doSend(request)
}

func (m *SendCloudAPIMailer) doSend(request *http.Request) error {
	response, err := m.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("sendcloud request failed: %w", err)
	}
	defer response.Body.Close()

	var payload sendCloudSendResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode sendcloud response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"sendcloud http status %d: %s",
			response.StatusCode,
			strings.TrimSpace(payload.Message),
		)
	}
	if !payload.Result || payload.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"sendcloud send failed: statusCode=%d message=%s",
			payload.StatusCode,
			strings.TrimSpace(payload.Message),
		)
	}

	return nil
}

func (m *SendCloudAPIMailer) buildSubject(purpose string) string {
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

func (m *SendCloudAPIMailer) buildSummary(purpose string) string {
	switch purpose {
	case registerPurpose:
		return "meeting 注册验证码"
	case loginPurpose:
		return "meeting 登录验证码"
	default:
		return "meeting 验证码"
	}
}

func contentSummary(message EmailMessage) string {
	if summary := strings.TrimSpace(message.ContentSummary); summary != "" {
		return summary
	}
	if text := strings.TrimSpace(message.TextBody); text != "" {
		if len([]rune(text)) <= 120 {
			return text
		}
		return string([]rune(text)[:120])
	}
	if htmlBody := strings.TrimSpace(message.HTMLBody); htmlBody != "" {
		if len([]rune(htmlBody)) <= 120 {
			return htmlBody
		}
		return string([]rune(htmlBody)[:120])
	}
	return message.Subject
}

func (m *SendCloudAPIMailer) buildHTMLBody(message VerificationMessage) string {
	expiryText := html.EscapeString(message.ExpiresAt.Local().Format("2006-01-02 15:04:05"))
	code := html.EscapeString(message.Code)
	nickname := html.EscapeString(messageNickname(message))

	switch message.Purpose {
	case registerPurpose:
		return fmt.Sprintf(
			"<p>您好，%s：</p><p>您的 <strong>meeting</strong> 注册验证码为：<strong>%s</strong></p><p>验证码将于 %s 失效。</p><p>如果这不是您的操作，请忽略本邮件。</p>",
			nickname,
			code,
			expiryText,
		)
	default:
		return fmt.Sprintf(
			"<p>您好：</p><p>您的 <strong>meeting</strong> 登录验证码为：<strong>%s</strong></p><p>验证码将于 %s 失效。</p><p>如果这不是您的操作，请忽略本邮件。</p>",
			code,
			expiryText,
		)
	}
}
