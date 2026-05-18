package minutes

import (
	"context"
	"fmt"
	"strings"
)

type FakeASRProvider struct{}

func (p FakeASRProvider) Name() string {
	return ProviderFake
}

func (p FakeASRProvider) Transcribe(_ context.Context, chunk AudioChunk) (ASRResult, error) {
	text := strings.TrimSpace(string(chunk.Data))
	if text == "" || !strings.HasPrefix(text, "text:") {
		text = fmt.Sprintf("模拟转写：收到 %s 的第 %d 段音频。", chunk.Nickname, chunk.Sequence)
	} else {
		text = strings.TrimSpace(strings.TrimPrefix(text, "text:"))
	}

	return ASRResult{
		Text:       text,
		IsFinal:    true,
		DurationMS: int(chunk.EndedAt.Sub(chunk.StartedAt).Milliseconds()),
	}, nil
}

type FakeLLMProvider struct {
	model string
}

func NewFakeLLMProvider(model string) FakeLLMProvider {
	if strings.TrimSpace(model) == "" {
		model = "fake-minutes"
	}
	return FakeLLMProvider{model: model}
}

func (p FakeLLMProvider) Name() string {
	return ProviderFake
}

func (p FakeLLMProvider) Model() string {
	return p.model
}

func (p FakeLLMProvider) GenerateMinutes(_ context.Context, input GenerateMinutesInput) (GenerateMinutesResult, error) {
	title := input.Meeting.Title
	if strings.TrimSpace(title) == "" {
		title = input.Meeting.MeetingNumber
	}

	lines := make([]string, 0, len(input.Segments))
	for _, segment := range input.Segments {
		lines = append(lines, fmt.Sprintf("- %s %s：%s", segment.StartedAt.Format("15:04"), segment.Nickname, segment.Text))
	}
	if len(lines) == 0 {
		lines = append(lines, "- 暂无有效转写内容。")
	}

	markdown := fmt.Sprintf(`# 会议纪要：%s

## 目录

- [一、开场白](#一开场白)
- [二、会议主要议题](#二会议主要议题)
- [三、各议题的讨论](#三各议题的讨论)
- [四、会议流程](#四会议流程)

## 一、开场白

本纪要由本地 fake LLM provider 生成，用于开发和测试完整流程。

## 二、会议主要议题

### 议题 1：会议实时记录与纪要整理

围绕 AI 助理实时记录、主持人主动触发纪要整理、邮件通知和纪要分享进行讨论。

## 三、各议题的讨论

### 议题 1：会议实时记录与纪要整理

%s

### 其他

暂无无法归类的内容。

## 四、会议流程

- %s：会议开始。
- %s：根据当前转写内容生成会议纪要。
`, title, strings.Join(lines, "\n"), input.Meeting.CreatedAt.Format("15:04"), lastSegmentTime(input))

	return GenerateMinutesResult{
		Title:           "会议纪要：" + title,
		Summary:         "已根据实时转写内容生成会议纪要。",
		MarkdownContent: markdown,
		OutlineJSON:     "{}",
	}, nil
}

func lastSegmentTime(input GenerateMinutesInput) string {
	if len(input.Segments) == 0 {
		return input.Meeting.CreatedAt.Format("15:04")
	}
	return input.Segments[len(input.Segments)-1].EndedAt.Format("15:04")
}
