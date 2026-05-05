package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSendCloudAPIMailerSendsVerificationCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/mail/send" {
			t.Fatalf("path = %s, want /mail/send", r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("ParseQuery() error = %v", err)
		}

		if values.Get("apiUser") != "test_user" {
			t.Fatalf("apiUser = %q", values.Get("apiUser"))
		}
		if values.Get("apiKey") != "test_key" {
			t.Fatalf("apiKey = %q", values.Get("apiKey"))
		}
		if values.Get("from") != "no-reply@mail.07c2.com.cn" {
			t.Fatalf("from = %q", values.Get("from"))
		}
		if values.Get("fromName") != "meeting" {
			t.Fatalf("fromName = %q", values.Get("fromName"))
		}
		if values.Get("to") != "user@example.com" {
			t.Fatalf("to = %q", values.Get("to"))
		}
		if !strings.Contains(values.Get("subject"), "登录验证码") {
			t.Fatalf("subject = %q", values.Get("subject"))
		}
		if !strings.Contains(values.Get("html"), "654321") {
			t.Fatalf("html = %q", values.Get("html"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true,"statusCode":200,"message":"请求成功","info":{}}`))
	}))
	defer server.Close()

	mailer, err := NewSendCloudAPIMailer(nil, MailerConfig{
		SendCloudAPIBaseURL:  server.URL,
		SendCloudAPIUser:     "test_user",
		SendCloudAPIKey:      "test_key",
		SendCloudFromAddress: "no-reply@mail.07c2.com.cn",
		SendCloudFromName:    "meeting",
		SubjectPrefix:        "[meeting]",
		Timeout:              time.Second,
	})
	if err != nil {
		t.Fatalf("NewSendCloudAPIMailer() error = %v", err)
	}

	delivery, err := mailer.SendVerificationCode(context.Background(), VerificationMessage{
		Email:     "user@example.com",
		Purpose:   loginPurpose,
		Code:      "654321",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("SendVerificationCode() error = %v", err)
	}
	if delivery.Mode != MailerModeSendCloudAPI {
		t.Fatalf("delivery mode = %q, want %q", delivery.Mode, MailerModeSendCloudAPI)
	}
}

func TestSendCloudAPIMailerRejectsFailedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":false,"statusCode":40005,"message":"认证失败","info":{}}`))
	}))
	defer server.Close()

	mailer, err := NewSendCloudAPIMailer(nil, MailerConfig{
		SendCloudAPIBaseURL:  server.URL,
		SendCloudAPIUser:     "test_user",
		SendCloudAPIKey:      "bad_key",
		SendCloudFromAddress: "no-reply@mail.07c2.com.cn",
		SendCloudFromName:    "meeting",
	})
	if err != nil {
		t.Fatalf("NewSendCloudAPIMailer() error = %v", err)
	}

	_, err = mailer.SendVerificationCode(context.Background(), VerificationMessage{
		Email:     "user@example.com",
		Purpose:   loginPurpose,
		Code:      "654321",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err == nil {
		t.Fatal("SendVerificationCode() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("SendVerificationCode() error = %v, want auth failure message", err)
	}
}

func TestSendCloudAPIMailerSendsAttachments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("content type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}
		if got := r.FormValue("to"); got != "ops@example.com" {
			t.Fatalf("to = %q", got)
		}
		if got := r.FormValue("subject"); got != "meeting 流量统计报告" {
			t.Fatalf("subject = %q", got)
		}
		files := r.MultipartForm.File["attachments"]
		if len(files) != 1 {
			t.Fatalf("attachments count = %d, want 1", len(files))
		}
		if files[0].Filename != "summary.csv" {
			t.Fatalf("attachment filename = %q", files[0].Filename)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true,"statusCode":200,"message":"请求成功","info":{}}`))
	}))
	defer server.Close()

	mailer, err := NewSendCloudAPIMailer(nil, MailerConfig{
		SendCloudAPIBaseURL:  server.URL,
		SendCloudAPIUser:     "test_user",
		SendCloudAPIKey:      "test_key",
		SendCloudFromAddress: "no-reply@mail.07c2.com.cn",
		SendCloudFromName:    "meeting",
	})
	if err != nil {
		t.Fatalf("NewSendCloudAPIMailer() error = %v", err)
	}

	_, err = mailer.SendEmail(context.Background(), EmailMessage{
		To:       []string{"ops@example.com"},
		Subject:  "meeting 流量统计报告",
		TextBody: "统计报告",
		Attachments: []MailAttachment{{
			Filename:    "summary.csv",
			ContentType: "text/csv; charset=utf-8",
			Data:        []byte("指标,数值\n会议数量,1\n"),
		}},
	})
	if err != nil {
		t.Fatalf("SendEmail() error = %v", err)
	}
}
