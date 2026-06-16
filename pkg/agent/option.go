package agent

import (
	"ferryman-agent/pkg/llm/provider"
	"ferryman-agent/pkg/memory/message"
	"ferryman-agent/pkg/memory/session"
	toolcore "ferryman-agent/pkg/tools"
)

type AgentOption func(*AgentConfig) error

func WithMemory(sessions session.Service, messages message.Service) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.Memory.Session = sessions
		cfg.Memory.Messages = messages
		return nil
	}
}

func WithProvider(providerRegistry *provider.ProviderClient) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.Provider = providerRegistry
		return nil
	}
}

func WithTools(tools ...*toolcore.Tool) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.Tools = append(cfg.Tools, tools...)
		return nil
	}
}

func WithAgentName(name string) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.AgentName = name
		return nil
	}
}

func WithAgentDescription(description string) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.AgentDescription = description
		return nil
	}
}

func WithDebug(enabled bool) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.Debug = enabled
		return nil
	}
}

func WithAutoCompact(enabled bool) AgentOption {
	return func(cfg *AgentConfig) error {
		cfg.AutoCompact = enabled
		return nil
	}
}
