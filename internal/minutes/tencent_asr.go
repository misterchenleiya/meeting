package minutes

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tencentASRAction  = "SentenceRecognition"
	tencentASRVersion = "2019-06-14"
	tencentASRService = "asr"
)

type TencentSentenceASRConfig struct {
	Endpoint        string
	SecretID        string
	SecretKey       string
	Region          string
	EngineModelType string
	VoiceFormat     string
	HTTPClient      *http.Client
}

type TencentSentenceASRProvider struct {
	endpoint        string
	host            string
	secretID        string
	secretKey       string
	region          string
	engineModelType string
	voiceFormat     string
	client          *http.Client
}

func NewTencentSentenceASRProvider(config TencentSentenceASRConfig) (*TencentSentenceASRProvider, error) {
	if strings.TrimSpace(config.SecretID) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return nil, ErrProviderConfig
	}

	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		endpoint = "https://asr.tencentcloudapi.com"
	}
	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse tencent asr endpoint: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("%w: invalid tencent asr endpoint", ErrProviderConfig)
	}

	region := strings.TrimSpace(config.Region)
	if region == "" {
		region = "ap-shanghai"
	}
	engine := strings.TrimSpace(config.EngineModelType)
	if engine == "" {
		engine = "16k_zh"
	}
	voiceFormat := strings.TrimSpace(config.VoiceFormat)
	if voiceFormat == "" {
		voiceFormat = "wav"
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	return &TencentSentenceASRProvider{
		endpoint:        endpoint,
		host:            parsedURL.Host,
		secretID:        strings.TrimSpace(config.SecretID),
		secretKey:       strings.TrimSpace(config.SecretKey),
		region:          region,
		engineModelType: engine,
		voiceFormat:     voiceFormat,
		client:          client,
	}, nil
}

func (p *TencentSentenceASRProvider) Name() string {
	return ProviderTencent
}

func (p *TencentSentenceASRProvider) Transcribe(ctx context.Context, chunk AudioChunk) (ASRResult, error) {
	body := map[string]any{
		"SubServiceType": 2,
		"ProjectId":      0,
		"EngSerViceType": p.engineModelType,
		"VoiceFormat":    p.voiceFormat,
		"SourceType":     1,
		"Data":           base64.StdEncoding.EncodeToString(chunk.Data),
		"DataLen":        len(chunk.Data),
		"FilterPunc":     0,
		"ConvertNumMode": 1,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ASRResult{}, fmt.Errorf("marshal tencent asr request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ASRResult{}, fmt.Errorf("create tencent asr request: %w", err)
	}

	now := time.Now().UTC()
	p.signRequest(request, payload, now)

	response, err := p.client.Do(request)
	if err != nil {
		return ASRResult{}, fmt.Errorf("call tencent asr: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return ASRResult{}, fmt.Errorf("read tencent asr response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ASRResult{}, fmt.Errorf("tencent asr http %d: %s", response.StatusCode, string(responseBody))
	}

	var decoded struct {
		Response struct {
			RequestID     string `json:"RequestId"`
			Result        string `json:"Result"`
			AudioDuration int    `json:"AudioDuration"`
			Error         *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return ASRResult{}, fmt.Errorf("decode tencent asr response: %w", err)
	}
	if decoded.Response.Error != nil {
		return ASRResult{}, fmt.Errorf("tencent asr %s: %s", decoded.Response.Error.Code, decoded.Response.Error.Message)
	}

	return ASRResult{
		Text:        strings.TrimSpace(decoded.Response.Result),
		IsFinal:     true,
		DurationMS:  decoded.Response.AudioDuration,
		ProviderRef: decoded.Response.RequestID,
	}, nil
}

func (p *TencentSentenceASRProvider) signRequest(request *http.Request, payload []byte, now time.Time) {
	timestamp := now.Unix()
	date := now.Format("2006-01-02")
	payloadHash := sha256Hex(payload)
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\n", "application/json; charset=utf-8", p.host)
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		http.MethodPost,
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := date + "/" + tencentASRService + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+p.secretKey), []byte(date))
	secretService := hmacSHA256(secretDate, []byte(tencentASRService))
	secretSigning := hmacSHA256(secretService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(secretSigning, []byte(stringToSign)))
	authorization := fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.secretID,
		credentialScope,
		signedHeaders,
		signature,
	)

	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Host", p.host)
	request.Header.Set("X-TC-Action", tencentASRAction)
	request.Header.Set("X-TC-Region", p.region)
	request.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	request.Header.Set("X-TC-Version", tencentASRVersion)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(value)
	return mac.Sum(nil)
}
