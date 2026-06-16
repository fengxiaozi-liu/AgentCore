package client

import "ferryman-agent/pkg/data/logging"

type options struct {
	baseURL         string
	disableCache    bool
	reasoningEffort string
	extraHeaders    map[string]string
}

type Option func(*options)

func WithBaseURL(baseURL string) Option {
	return func(options *options) {
		options.baseURL = baseURL
	}
}

func WithDefaultBaseURL(baseURL string) Option {
	return func(options *options) {
		if options.baseURL == "" {
			options.baseURL = baseURL
		}
	}
}

func WithExtraHeaders(headers map[string]string) Option {
	return func(options *options) {
		options.extraHeaders = headers
	}
}

func WithDisableCache() Option {
	return func(options *options) {
		options.disableCache = true
	}
}

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
