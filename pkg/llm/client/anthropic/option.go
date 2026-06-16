package client

import "strings"

type options struct {
	useBedrock   bool
	disableCache bool
	shouldThink  func(userMessage string) bool
}

type Option func(*options)

func WithBedrock(useBedrock bool) Option {
	return func(options *options) {
		options.useBedrock = useBedrock
	}
}

func WithDisableCache() Option {
	return func(options *options) {
		options.disableCache = true
	}
}

func DefaultShouldThinkFn(s string) bool {
	return strings.Contains(strings.ToLower(s), "think")
}

func WithShouldThinkFn(fn func(string) bool) Option {
	return func(options *options) {
		options.shouldThink = fn
	}
}
