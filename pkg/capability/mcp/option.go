package mcp

import toolcore "ferryman-agent/pkg/tools"

type McpOption func(*McpOptions)

type McpOptions struct {
	Name        string
	ServerType  MCPType
	Command     string
	Env         []string
	Args        []string
	URL         string
	Headers     map[string]string
	Middlewares []toolcore.ToolMiddleware
}

func WithName(name string) McpOption {
	return func(opts *McpOptions) {
		opts.Name = name
	}
}

func WithMcpServerType(serverType MCPType) McpOption {
	return func(opts *McpOptions) {
		opts.ServerType = serverType
	}
}

func WithMcpServerURL(url string) McpOption {
	return func(opts *McpOptions) {
		opts.ServerType = MCPSse
		opts.URL = url
	}
}

func WithMcpServerCommand(command string) McpOption {
	return func(opts *McpOptions) {
		opts.ServerType = MCPStdio
		opts.Command = command
	}
}

func WithMcpServerEnv(env ...string) McpOption {
	return func(opts *McpOptions) {
		opts.Env = append(opts.Env, env...)
	}
}

func WithMcpServerArgs(args ...string) McpOption {
	return func(opts *McpOptions) {
		opts.Args = append(opts.Args, args...)
	}
}

func WithMcpServerHeaders(headers map[string]string) McpOption {
	return func(opts *McpOptions) {
		opts.Headers = headers
	}
}

func WithMiddleware(middlewares ...toolcore.ToolMiddleware) McpOption {
	return func(opts *McpOptions) {
		opts.Middlewares = append(opts.Middlewares, middlewares...)
	}
}
