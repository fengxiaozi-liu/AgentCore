package factory

import (
	anthropicclient "ferryman-agent/pkg/llm/client/anthropic"
	bedrockclient "ferryman-agent/pkg/llm/client/bedrock"
	copilotclient "ferryman-agent/pkg/llm/client/copilot"
	geminiclient "ferryman-agent/pkg/llm/client/gemini"
	openaiclient "ferryman-agent/pkg/llm/client/openai"
)

type Option func(*options)

type options struct {
	APIKey  string
	BaseURL string

	OpenAIOptions    []openaiclient.Option
	AnthropicOptions []anthropicclient.Option
	GeminiOptions    []geminiclient.Option
	BedrockOptions   []bedrockclient.Option
	CopilotOptions   []copilotclient.Option
}

func WithAPIKey(apiKey string) Option {
	return func(options *options) {
		options.APIKey = apiKey
	}
}

func WithBaseURL(baseURL string) Option {
	return func(options *options) {
		options.BaseURL = baseURL
	}
}

func WithOpenAIOptions(openaiOptions ...openaiclient.Option) Option {
	return func(options *options) {
		options.OpenAIOptions = append(options.OpenAIOptions, openaiOptions...)
	}
}

func WithAnthropicOptions(anthropicOptions ...anthropicclient.Option) Option {
	return func(options *options) {
		options.AnthropicOptions = append(options.AnthropicOptions, anthropicOptions...)
	}
}

func WithGeminiOptions(geminiOptions ...geminiclient.Option) Option {
	return func(options *options) {
		options.GeminiOptions = append(options.GeminiOptions, geminiOptions...)
	}
}

func WithBedrockOptions(bedrockOptions ...bedrockclient.Option) Option {
	return func(options *options) {
		options.BedrockOptions = append(options.BedrockOptions, bedrockOptions...)
	}
}

func WithCopilotOptions(copilotOptions ...copilotclient.Option) Option {
	return func(options *options) {
		options.CopilotOptions = append(options.CopilotOptions, copilotOptions...)
	}
}
