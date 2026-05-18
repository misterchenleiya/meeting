package minutes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DeepSeekConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type DeepSeekProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewDeepSeekProvider(config DeepSeekConfig) (*DeepSeekProvider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, ErrProviderConfig
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse deepseek base url: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("%w: invalid deepseek base url", ErrProviderConfig)
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = "deepseek-v4-flash"
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}

	return &DeepSeekProvider{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(config.APIKey),
		model:   model,
		client:  client,
	}, nil
}

func (p *DeepSeekProvider) Name() string {
	return ProviderDeepSeek
}

func (p *DeepSeekProvider) Model() string {
	return p.model
}

func (p *DeepSeekProvider) GenerateMinutes(ctx context.Context, input GenerateMinutesInput) (GenerateMinutesResult, error) {
	prompt := buildMinutesPrompt(input)
	requestBody := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "你是会议纪要整理助手。只输出 Markdown 纯文本，不输出 HTML，不编造事实。必须严格按用户给定的四个章节结构整理。",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.2,
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return GenerateMinutesResult{}, fmt.Errorf("marshal deepseek request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return GenerateMinutesResult{}, fmt.Errorf("create deepseek request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return GenerateMinutesResult{}, fmt.Errorf("call deepseek: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return GenerateMinutesResult{}, fmt.Errorf("read deepseek response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return GenerateMinutesResult{}, fmt.Errorf("deepseek http %d: %s", response.StatusCode, string(responseBody))
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return GenerateMinutesResult{}, fmt.Errorf("decode deepseek response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return GenerateMinutesResult{}, fmt.Errorf("deepseek response has no choices")
	}

	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return GenerateMinutesResult{}, fmt.Errorf("deepseek returned empty minutes")
	}

	title := input.Meeting.Title
	if strings.TrimSpace(title) == "" {
		title = input.Meeting.MeetingNumber
	}
	return GenerateMinutesResult{
		Title:           "会议纪要：" + title,
		Summary:         firstMarkdownParagraph(content),
		MarkdownContent: content,
		OutlineJSON:     "{}",
	}, nil
}

func buildMinutesPrompt(input GenerateMinutesInput) string {
	var builder strings.Builder
	title := input.Meeting.Title
	if strings.TrimSpace(title) == "" {
		title = input.Meeting.MeetingNumber
	}

	builder.WriteString("请根据以下会议转写内容生成 Markdown 纯文本会议纪要。\n\n")
	builder.WriteString("硬性格式要求：\n")
	builder.WriteString("1. 必须包含目录。\n")
	builder.WriteString("2. 必须包含四个一级章节：一、开场白；二、会议主要议题；三、各议题的讨论；四、会议流程。\n")
	builder.WriteString("3. 会议主要议题只写议题结构和基本介绍，不写讨论内容。\n")
	builder.WriteString("4. 各议题的讨论按议题归并对话；无法确认或无关内容放到本章末尾的“其他”小节。\n")
	builder.WriteString("5. 会议流程按时间升序记录会议开始、入会、离会、各章节开始和结束、会议结束。\n")
	builder.WriteString("6. 不要逐字复述完整转写，不要编造转写中不存在的信息。\n\n")

	builder.WriteString("会议信息：\n")
	builder.WriteString(fmt.Sprintf("- 标题：%s\n", title))
	builder.WriteString(fmt.Sprintf("- 会议号：%s\n", input.Meeting.MeetingNumber))
	builder.WriteString(fmt.Sprintf("- 开始时间：%s\n", input.Meeting.CreatedAt.Format(time.RFC3339)))
	if input.Meeting.EndedAt != nil {
		builder.WriteString(fmt.Sprintf("- 结束时间：%s\n", input.Meeting.EndedAt.Format(time.RFC3339)))
	}
	builder.WriteString("\n参会人员：\n")
	for _, participant := range input.Participants {
		leftAt := ""
		if participant.LeftAt != nil {
			leftAt = "，离会：" + participant.LeftAt.Format(time.RFC3339)
		}
		builder.WriteString(fmt.Sprintf("- %s（%s），入会：%s%s\n", participant.Nickname, participant.ParticipantRole, participant.JoinedAt.Format(time.RFC3339), leftAt))
	}

	builder.WriteString("\n转写内容：\n")
	for _, segment := range input.Segments {
		builder.WriteString(fmt.Sprintf("[%s-%s] %s：%s\n",
			segment.StartedAt.Format("15:04:05"),
			segment.EndedAt.Format("15:04:05"),
			segment.Nickname,
			segment.Text,
		))
	}

	return builder.String()
}

func firstMarkdownParagraph(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		if len([]rune(trimmed)) > 120 {
			return string([]rune(trimmed)[:120])
		}
		return trimmed
	}
	return "会议纪要已生成。"
}
