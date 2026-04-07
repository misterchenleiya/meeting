package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WechatMiniProgramIdentity struct {
	OpenID string
}

type WechatMiniProgramCodeExchanger interface {
	ExchangeCode(ctx context.Context, code string) (WechatMiniProgramIdentity, error)
}

type WechatMiniProgramClientConfig struct {
	AppID      string
	AppSecret  string
	APIBaseURL string
	Timeout    time.Duration
}

type wechatMiniProgramClient struct {
	logger     *slog.Logger
	httpClient *http.Client
	appID      string
	appSecret  string
	apiBaseURL string
}

func NewWechatMiniProgramClient(logger *slog.Logger, config WechatMiniProgramClientConfig) (WechatMiniProgramCodeExchanger, error) {
	appID := strings.TrimSpace(config.AppID)
	appSecret := strings.TrimSpace(config.AppSecret)
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("wechat mini program app id and secret are required")
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(config.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.weixin.qq.com"
	}

	return &wechatMiniProgramClient{
		logger: logger,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		appID:      appID,
		appSecret:  appSecret,
		apiBaseURL: apiBaseURL,
	}, nil
}

func (c *wechatMiniProgramClient) ExchangeCode(ctx context.Context, code string) (WechatMiniProgramIdentity, error) {
	query := url.Values{}
	query.Set("appid", c.appID)
	query.Set("secret", c.appSecret)
	query.Set("js_code", strings.TrimSpace(code))
	query.Set("grant_type", "authorization_code")

	endpoint := c.apiBaseURL + "/sns/jscode2session?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return WechatMiniProgramIdentity{}, fmt.Errorf("build wechat jscode2session request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return WechatMiniProgramIdentity{}, fmt.Errorf("request wechat jscode2session: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<10))
	if err != nil {
		return WechatMiniProgramIdentity{}, fmt.Errorf("read wechat jscode2session response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return WechatMiniProgramIdentity{}, fmt.Errorf("wechat jscode2session status %d", response.StatusCode)
	}

	var payload struct {
		OpenID     string `json:"openid"`
		SessionKey string `json:"session_key"`
		UnionID    string `json:"unionid"`
		ErrCode    int    `json:"errcode"`
		ErrMsg     string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return WechatMiniProgramIdentity{}, fmt.Errorf("decode wechat jscode2session response: %w", err)
	}
	if payload.ErrCode != 0 {
		if c.logger != nil {
			c.logger.Warn("wechat mini program login exchange failed",
				"errcode", payload.ErrCode,
				"errmsg", payload.ErrMsg)
		}
		return WechatMiniProgramIdentity{}, fmt.Errorf("wechat jscode2session error %d: %s", payload.ErrCode, payload.ErrMsg)
	}
	if strings.TrimSpace(payload.OpenID) == "" {
		return WechatMiniProgramIdentity{}, fmt.Errorf("wechat jscode2session returned empty openid")
	}

	return WechatMiniProgramIdentity{
		OpenID: strings.TrimSpace(payload.OpenID),
	}, nil
}
