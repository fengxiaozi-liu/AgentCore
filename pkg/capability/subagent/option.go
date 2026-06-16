package subagent

import (
	"ferryman-agent/pkg/llm/provider"
	"ferryman-agent/pkg/memory/message"
	"ferryman-agent/pkg/memory/session"
	toolcore "ferryman-agent/pkg/tools"
)

type SubAgentOption func(*SubAgentOptions)

type SubAgentOptions struct {
	AgentName        string
	AgentDescription string
	Sessions         session.Service
	Messages         message.Service
	Provider         *provider.ProviderClient
	ModelProvider    provider.ModelProvider
	Tools            []*toolcore.Tool
	Middlewares      []toolcore.ToolMiddleware
}

func WithMemory(sessions session.Service, messages message.Service) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.Sessions = sessions
		opts.Messages = messages
	}
}

func WithProvider(provider *provider.ProviderClient) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.Provider = provider
	}
}

func WithModelProvider(modelProvider provider.ModelProvider) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.ModelProvider = modelProvider
	}
}

func WithAgentName(name string) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.AgentName = name
	}
}

func WithAgentDescription(description string) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.AgentDescription = description
	}
}

func WithTools(tools ...*toolcore.Tool) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.Tools = append(opts.Tools, tools...)
	}
}

func WithMiddleware(middlewares ...toolcore.ToolMiddleware) SubAgentOption {
	return func(opts *SubAgentOptions) {
		opts.Middlewares = append(opts.Middlewares, middlewares...)
	}
}
