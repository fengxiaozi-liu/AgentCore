package agent

import (
	"ferryman-agent/pkg/llm/provider"
	"ferryman-agent/pkg/memory/message"
	"ferryman-agent/pkg/memory/session"
	toolcore "ferryman-agent/pkg/tools"
)

type AgentConfig struct {
	AgentName        string
	AgentDescription string
	Memory           MemoryConfig
	Provider         *provider.ProviderClient
	Tools            []*toolcore.Tool
	Debug            bool
	AutoCompact      bool
}

type MemoryConfig struct {
	Session  session.Service
	Messages message.Service
}
