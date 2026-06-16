package skill

import (
	"context"

	toolcore "ferryman-agent/pkg/tools"
)

type SkillOption func(*skillOptions)

type skillOptions struct {
	Name        string
	Description string
	LoadContent func(ctx context.Context) (string, error)
	Metadata    map[string]string
	BaseUIR     string
	Middlewares []toolcore.ToolMiddleware
}

func WithName(name string) SkillOption {
	return func(opts *skillOptions) {
		opts.Name = name
	}
}

func WithDescription(description string) SkillOption {
	return func(opts *skillOptions) {
		opts.Description = description
	}
}

func WithLoadContent(loadContent func(ctx context.Context) (string, error)) SkillOption {
	return func(opts *skillOptions) {
		opts.LoadContent = loadContent
	}
}

func WithMetadata(metadata map[string]string) SkillOption {
	return func(opts *skillOptions) {
		opts.Metadata = metadata
	}
}

func WithBaseUIR(baseUIR string) SkillOption {
	return func(opts *skillOptions) {
		opts.BaseUIR = baseUIR
	}
}

func WithMiddleware(middlewares ...toolcore.ToolMiddleware) SkillOption {
	return func(opts *skillOptions) {
		opts.Middlewares = append(opts.Middlewares, middlewares...)
	}
}
