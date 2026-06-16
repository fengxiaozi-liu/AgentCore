package workspace

import (
	toolcore "ferryman-agent/pkg/tools"
	"ferryman-agent/pkg/version"
)

type WorkspaceOption func(*WorkspaceOptions)

type WorkspaceOptions struct {
	RootDir     string
	Middlewares []toolcore.ToolMiddleware
	FileVersion version.FileVersionService
}

func WithRootDir(rootDir string) WorkspaceOption {
	return func(opts *WorkspaceOptions) {
		opts.RootDir = rootDir
	}
}

func WithMiddleware(middlewares ...toolcore.ToolMiddleware) WorkspaceOption {
	return func(opts *WorkspaceOptions) {
		opts.Middlewares = append(opts.Middlewares, middlewares...)
	}
}

func WithFileVersionService(files version.FileVersionService) WorkspaceOption {
	return func(opts *WorkspaceOptions) {
		opts.FileVersion = files
	}
}

func ApplyOptions(opts []WorkspaceOption) WorkspaceOptions {
	options := WorkspaceOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
