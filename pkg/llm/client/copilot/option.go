package client

import "ferryman-agent/pkg/data/logging"

type options struct {
	reasoningEffort string
	extraHeaders    map[string]string
	bearerToken     string
}

type Option func(*options)

func WithReasoningEffort(effort string) Option {
	return func(options *options) {
		defaultReasoningEffort := "medium"
		switch effort {
		case "low", "medium", "high":
			defaultReasoningEffort = effort
		default:
			logging.Warn("Invalid reasoning effort, using default: medium")
		}
		options.reasoningEffort = defaultReasoningEffort
	}
}

func WithExtraHeaders(headers map[string]string) Option {
	return func(options *options) {
		options.extraHeaders = headers
	}
}

func WithBearerToken(bearerToken string) Option {
	return func(options *options) {
		options.bearerToken = bearerToken
	}
}
