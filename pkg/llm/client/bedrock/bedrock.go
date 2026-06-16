package client

import (
	"context"
	llmclient "ferryman-agent/pkg/llm/client"
	client3 "ferryman-agent/pkg/llm/client/anthropic"
	"fmt"
	"os"
	"strings"
)

type bedrockClient struct {
	options       options
	childProvider llmclient.Client
	regionPrefix  string
}

func NewClient(apiKey string, optionFns ...Option) llmclient.Client {
	bedrockOpts := options{}
	for _, o := range optionFns {
		o(&bedrockOpts)
	}

	// Get AWS region from environment
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}

	if region == "" {
		region = "us-east-1" // default region
	}
	if len(region) < 2 {
		return &bedrockClient{
			options:       bedrockOpts,
			childProvider: nil, // Will cause an error when used
		}
	}

	regionPrefix := region[:2]
	return &bedrockClient{
		options:       bedrockOpts,
		childProvider: client3.NewClient(apiKey, client3.WithBedrock(true), client3.WithDisableCache()),
		regionPrefix:  regionPrefix,
	}
}

func (b *bedrockClient) prepareRequest(request llmclient.Request) (llmclient.Request, error) {
	if b.childProvider == nil || !strings.Contains(request.Model.APIModel, "anthropic") {
		return llmclient.Request{}, ErrUnsupportedBedrockModel
	}
	request.Model.APIModel = fmt.Sprintf("%s.%s", b.regionPrefix, request.Model.APIModel)
	return request, nil
}

func (b *bedrockClient) Send(ctx context.Context, request llmclient.Request) (*llmclient.Response, error) {
	preparedRequest, err := b.prepareRequest(request)
	if err != nil {
		return nil, err
	}
	return b.childProvider.Send(ctx, preparedRequest)
}

func (b *bedrockClient) Stream(ctx context.Context, request llmclient.Request) <-chan llmclient.Event {
	eventChan := make(chan llmclient.Event)

	preparedRequest, err := b.prepareRequest(request)
	if err != nil {
		go func() {
			eventChan <- llmclient.Event{
				Type:  llmclient.EventError,
				Error: err,
			}
			close(eventChan)
		}()
		return eventChan
	}

	return b.childProvider.Stream(ctx, preparedRequest)
}
