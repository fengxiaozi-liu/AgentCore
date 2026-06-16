package version

import datadb "ferryman-agent/pkg/data/db"

type Option func(*optionsConfig)

type optionsConfig struct {
	client *datadb.DbClient
}

func WithDbClient(client *datadb.DbClient) Option {
	return func(opts *optionsConfig) {
		opts.client = client
	}
}
