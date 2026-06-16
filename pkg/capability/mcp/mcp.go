package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	toolcore "ferryman-agent/pkg/tools"
	"github.com/mark3labs/mcp-go/client"

	"github.com/mark3labs/mcp-go/mcp"
)

type MCPServer struct {
	Command string            `json:"command,omitempty"`
	Env     []string          `json:"env,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Type    MCPType           `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type MCPClient interface {
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

type McpTool struct {
	mcpName string
	tool    mcp.Tool
	server  MCPServer
}

func NewMcpTool(ctx context.Context, opts ...McpOption) ([]*toolcore.Tool, error) {
	options := McpOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return nil, fmt.Errorf("mcp name is required")
	}
	server := MCPServer{
		Command: options.Command,
		Env:     options.Env,
		Args:    options.Args,
		Type:    options.ServerType,
		URL:     options.URL,
		Headers: options.Headers,
	}
	switch server.Type {
	case MCPStdio:
		if strings.TrimSpace(server.Command) == "" {
			return nil, fmt.Errorf("mcp server command is required")
		}
	case MCPSse:
		if strings.TrimSpace(server.URL) == "" {
			return nil, fmt.Errorf("mcp server url is required")
		}
	default:
		return nil, fmt.Errorf("invalid mcp type: %s", server.Type)
	}

	c, err := newMCPClient(server)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    MCPClientName,
		Version: MCPClientVersion,
	}
	if _, err := c.Initialize(ctx, initRequest); err != nil {
		return nil, err
	}
	availableTools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}

	tools := make([]*toolcore.Tool, 0, len(availableTools.Tools))
	for _, remoteTool := range availableTools.Tools {
		tools = append(tools, newRemoteMcpTool(name, remoteTool, server, options.Middlewares...))
	}
	return tools, nil
}

func newRemoteMcpTool(name string, tool mcp.Tool, server MCPServer, middlewares ...toolcore.ToolMiddleware) *toolcore.Tool {
	mt := &McpTool{
		mcpName: name,
		tool:    tool,
		server:  server,
	}
	return toolcore.NewTool(mt.Info(), mt.Run, toolcore.WithKind(toolcore.ToolKindMCP), toolcore.WithMiddleware(middlewares...))
}

func (b *McpTool) Info() *toolcore.ToolInfo {
	required := b.tool.InputSchema.Required
	if required == nil {
		required = make([]string, 0)
	}
	return &toolcore.ToolInfo{
		Name:        fmt.Sprintf("%s_%s", b.mcpName, b.tool.Name),
		Description: b.tool.Description,
		Parameters:  b.tool.InputSchema.Properties,
		Required:    required,
	}
}

func (b *McpTool) Run(ctx context.Context, params toolcore.ToolCall) (toolcore.ToolResponse, error) {
	c, err := newMCPClient(b.server)
	if err != nil {
		return toolcore.NewTextErrorResponse(err.Error()), nil
	}
	defer c.Close()
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    MCPClientName,
		Version: MCPClientVersion,
	}

	_, err = c.Initialize(ctx, initRequest)
	if err != nil {
		return toolcore.NewTextErrorResponse(err.Error()), nil
	}

	toolRequest := mcp.CallToolRequest{}
	toolRequest.Params.Name = b.tool.Name
	var args map[string]any
	if err = json.Unmarshal([]byte(params.Input), &args); err != nil {
		return toolcore.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	toolRequest.Params.Arguments = args
	result, err := c.CallTool(ctx, toolRequest)
	if err != nil {
		return toolcore.NewTextErrorResponse(err.Error()), nil
	}

	output := ""
	for _, v := range result.Content {
		if v, ok := v.(mcp.TextContent); ok {
			output = v.Text
		} else {
			output = fmt.Sprintf("%v", v)
		}
	}

	return toolcore.NewTextResponse(output), nil
}

func newMCPClient(server MCPServer) (MCPClient, error) {
	switch server.Type {
	case MCPStdio:
		return client.NewStdioMCPClient(server.Command, server.Env, server.Args...)
	case MCPSse:
		return client.NewSSEMCPClient(server.URL, client.WithHeaders(server.Headers))
	default:
		return nil, fmt.Errorf("invalid mcp type: %s", server.Type)
	}
}
