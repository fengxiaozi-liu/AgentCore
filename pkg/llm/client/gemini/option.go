package client

type options struct {
	disableCache bool
}

type Option func(*options)

func WithDisableCache() Option {
	return func(options *options) {
		options.disableCache = true
	}
}
