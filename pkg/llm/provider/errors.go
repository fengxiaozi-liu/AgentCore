package provider

import "errors"

var (
	ErrProviderNotConfigured = errors.New("provider is not configured")
	ErrModelNotConfigured    = errors.New("model is not configured")
)
