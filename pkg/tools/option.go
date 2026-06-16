package tools

type ToolOption func(*toolOptions)

type toolOptions struct {
	kind        ToolKind
	middlewares []ToolMiddleware
}

func WithKind(kind ToolKind) ToolOption {
	return func(opts *toolOptions) {
		opts.kind = kind
	}
}

func WithMiddleware(middlewares ...ToolMiddleware) ToolOption {
	return func(opts *toolOptions) {
		opts.middlewares = append(opts.middlewares, middlewares...)
	}
}
