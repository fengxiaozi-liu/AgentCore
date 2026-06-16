package client

import (
	"context"
	"ferryman-agent/pkg/memory/message"
	toolcore "ferryman-agent/pkg/tools"
)

type ModelID string

type Provider string

const (
	ProviderAnthropic  Provider = "anthropic"
	ProviderAzure      Provider = "azure"
	ProviderBedrock    Provider = "bedrock"
	ProviderCopilot    Provider = "copilot"
	ProviderGemini     Provider = "gemini"
	ProviderGROQ       Provider = "groq"
	ProviderLocal      Provider = "local"
	ProviderOpenAI     Provider = "openai"
	ProviderOpenRouter Provider = "openrouter"
	ProviderVertexAI   Provider = "vertexai"
	ProviderXAI        Provider = "xai"
	ProviderMock       Provider = "__mock"
)

type Model struct {
	ID                  ModelID `json:"id"`
	Name                string  `json:"name"`
	APIModel            string  `json:"api_model"`
	CostPer1MIn         float64 `json:"cost_per_1m_in"`
	CostPer1MOut        float64 `json:"cost_per_1m_out"`
	CostPer1MInCached   float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached  float64 `json:"cost_per_1m_out_cached"`
	ContextWindow       int64   `json:"context_window"`
	MaxTokens           int64   `json:"max_tokens"`
	CanReason           bool    `json:"can_reason"`
	SupportsAttachments bool    `json:"supports_attachments"`
	ReasoningEffort     string  `json:"reasoning_effort,omitempty"`
}

type EventType string

const MaxRetries = 8

const (
	EventContentStart  EventType = "content_start"
	EventToolUseStart  EventType = "tool_use_start"
	EventToolUseDelta  EventType = "tool_use_delta"
	EventToolUseStop   EventType = "tool_use_stop"
	EventContentDelta  EventType = "content_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventContentStop   EventType = "content_stop"
	EventComplete      EventType = "complete"
	EventError         EventType = "error"
	EventWarning       EventType = "warning"
)

type TokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

type Response struct {
	Content      string
	ToolCalls    []message.ToolCall
	Usage        TokenUsage
	FinishReason message.FinishReason
}

type Event struct {
	Type     EventType
	Content  string
	Thinking string
	Response *Response
	ToolCall *message.ToolCall
	Error    error
}

type Request struct {
	Model         Model
	SystemMessage string
	Debug         bool
	Messages      []message.MessageRecord
	Tools         []*toolcore.ToolInfo
}

type Client interface {
	Send(ctx context.Context, request Request) (*Response, error)
	Stream(ctx context.Context, request Request) <-chan Event
}
