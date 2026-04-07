package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWechatMiniProgramClientExchangeCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sns/jscode2session" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/sns/jscode2session")
		}
		if got := r.URL.Query().Get("appid"); got != "test-appid" {
			t.Fatalf("appid = %q", got)
		}
		if got := r.URL.Query().Get("secret"); got != "test-secret" {
			t.Fatalf("secret = %q", got)
		}
		if got := r.URL.Query().Get("js_code"); got != "wx-code" {
			t.Fatalf("js_code = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openid":"openid-123","session_key":"session-key"}`))
	}))
	defer server.Close()

	client, err := NewWechatMiniProgramClient(nil, WechatMiniProgramClientConfig{
		AppID:      "test-appid",
		AppSecret:  "test-secret",
		APIBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewWechatMiniProgramClient() error = %v", err)
	}

	identity, err := client.ExchangeCode(context.Background(), "wx-code")
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if identity.OpenID != "openid-123" {
		t.Fatalf("ExchangeCode() openid = %q", identity.OpenID)
	}
}
