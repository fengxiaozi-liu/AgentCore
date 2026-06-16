package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentcore "ferryman-agent/pkg/agent"
	"ferryman-agent/pkg/llm/provider"
	"ferryman-agent/pkg/memory/message"
	"ferryman-agent/pkg/memory/session"
	toolcore "ferryman-agent/pkg/tools"
)

type agentToolParams struct {
	Prompt string `json:"prompt"`
}

type AgentTool struct {
	agentName        string
	agentDescription string
	sessions         session.Service
	messages         message.Service
	provider         *provider.ProviderClient
	modelProvider    provider.ModelProvider
	tools            []*toolcore.Tool
}

func NewAgentTool(opts ...SubAgentOption) (*toolcore.Tool, error) {
	options := SubAgentOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.Sessions == nil || options.Messages == nil {
		return nil, fmt.Errorf("memory session and messages services are required")
	}
	if options.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	agentName := strings.TrimSpace(options.AgentName)
	if agentName == "" {
		agentName = "agent_tool"
	}
	agentDescription := strings.TrimSpace(options.AgentDescription)
	if agentDescription == "" {
		agentDescription = "Launch a new subagent with the tools supplied by the host application."
	}

	at := &AgentTool{
		agentName:        agentName,
		agentDescription: agentDescription,
		sessions:         options.Sessions,
		messages:         options.Messages,
		provider:         options.Provider,
		modelProvider:    options.ModelProvider,
		tools:            append([]*toolcore.Tool(nil), options.Tools...),
	}

	return toolcore.NewTool(at.Info(), at.run, toolcore.WithKind(toolcore.ToolKindSubAgent), toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (a *AgentTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        a.agentName,
		Description: a.agentDescription,
		Parameters: map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "The task for the subagent to perform",
			},
		},
		Required: []string{"prompt"},
	}
}

func (a *AgentTool) run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var params agentToolParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	if params.Prompt == "" {
		return toolcore.NewTextErrorResponse("prompt is required"), nil
	}

	sessionID, messageID := toolcore.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return toolcore.ToolResponse{}, fmt.Errorf("session_id and message_id are required")
	}

	taskSession, err := a.sessions.CreateTaskSession(ctx, call.ID, sessionID, "New Agent Session")
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error creating session: %s", err)
	}

	response, err := a.runTask(ctx, taskSession.ID, params.Prompt)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error generating agent: %s", err)
	}
	if response.Role != message.Assistant {
		return toolcore.NewTextErrorResponse("no response"), nil
	}

	updatedSession, err := a.sessions.Get(ctx, taskSession.ID)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error getting session: %s", err)
	}
	parentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error getting parent session: %s", err)
	}

	parentSession.Cost += updatedSession.Cost
	if _, err = a.sessions.Save(ctx, parentSession); err != nil {
		return toolcore.ToolResponse{}, fmt.Errorf("error saving parent session: %s", err)
	}
	return toolcore.NewTextResponse(response.Content().String()), nil
}

func (a *AgentTool) runTask(ctx context.Context, sessionID string, content string) (message.MessageRecord, error) {
	runner, err := agentcore.NewAgent(
		agentcore.WithAgentName(a.agentName),
		agentcore.WithAgentDescription(a.agentDescription),
		agentcore.WithMemory(a.sessions, a.messages),
		agentcore.WithProvider(a.provider),
		agentcore.WithTools(a.tools...),
	)
	if err != nil {
		return message.MessageRecord{}, err
	}

	done, err := runner.Run(ctx, a.modelProvider, sessionID, content)
	if err != nil {
		return message.MessageRecord{}, err
	}
	result := <-done
	if result.Error != nil {
		return message.MessageRecord{}, result.Error
	}
	return result.Message, nil
}
