package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	llmclient "ferryman-agent/pkg/llm/client"
	clientfactory "ferryman-agent/pkg/llm/client/factory"
	"ferryman-agent/pkg/memory/message"
)

type ModelProvider struct {
	Provider llmclient.Provider `json:"provider"`
	APIKey   string             `json:"apiKey,omitempty"`
	BaseURL  string             `json:"baseURL,omitempty"`
	Model    llmclient.Model    `json:"model"`
}

type ProviderClient struct {
	mu      sync.RWMutex
	clients map[string]llmclient.Client
}

func NewProvider() *ProviderClient {
	return &ProviderClient{
		clients: map[string]llmclient.Client{},
	}
}

func (p *ProviderClient) SendMessages(ctx context.Context, modelProvider ModelProvider, request llmclient.Request) (*llmclient.Response, error) {
	client, err := p.Client(ctx, modelProvider)
	if err != nil {
		return nil, err
	}
	request = prepareRequest(modelProvider, request)
	return client.Send(ctx, request)
}

func (p *ProviderClient) StreamResponse(ctx context.Context, modelProvider ModelProvider, request llmclient.Request) <-chan llmclient.Event {
	client, err := p.Client(ctx, modelProvider)
	if err != nil {
		ch := make(chan llmclient.Event, 1)
		ch <- llmclient.Event{Type: llmclient.EventError, Error: err}
		close(ch)
		return ch
	}
	request = prepareRequest(modelProvider, request)
	return client.Stream(ctx, request)
}

func (p *ProviderClient) Client(ctx context.Context, modelProvider ModelProvider) (llmclient.Client, error) {
	_ = ctx

	if err := validateModelProvider(modelProvider); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("provider registry is required")
	}

	key := modelProvider.cacheKey()
	p.mu.RLock()
	client, ok := p.clients[key]
	p.mu.RUnlock()
	if ok {
		return client, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clients == nil {
		p.clients = map[string]llmclient.Client{}
	}
	if client, ok = p.clients[key]; ok {
		return client, nil
	}

	llmClient, err := clientfactory.NewClient(
		modelProvider.Provider,
		clientfactory.WithAPIKey(modelProvider.APIKey),
		clientfactory.WithBaseURL(modelProvider.BaseURL),
	)
	if err != nil {
		return nil, err
	}
	p.clients[key] = llmClient
	return llmClient, nil
}

func prepareRequest(modelProvider ModelProvider, request llmclient.Request) llmclient.Request {
	request.Messages = cleanMessages(request.Messages)
	request.Model = modelProvider.Model
	return request
}

func validateModelProvider(modelProvider ModelProvider) error {
	if modelProvider.Provider == "" {
		return ErrProviderNotConfigured
	}
	if modelProvider.Model.ID == "" {
		return ErrModelNotConfigured
	}
	return nil
}

func (m ModelProvider) cacheKey() string {
	hash := sha256.Sum256([]byte(string(m.Provider) + "\x00" + m.APIKey + "\x00" + m.BaseURL))
	return hex.EncodeToString(hash[:])
}

func cleanMessages(messages []message.MessageRecord) (cleaned []message.MessageRecord) {
	for _, msg := range messages {
		if len(msg.Parts) == 0 {
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return cleaned
}
