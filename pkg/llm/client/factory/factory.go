package factory

import (
	"fmt"
	"os"

	llmclient "ferryman-agent/pkg/llm/client"
	anthropicclient "ferryman-agent/pkg/llm/client/anthropic"
	azureclient "ferryman-agent/pkg/llm/client/azure"
	bedrockclient "ferryman-agent/pkg/llm/client/bedrock"
	copilotclient "ferryman-agent/pkg/llm/client/copilot"
	geminiclient "ferryman-agent/pkg/llm/client/gemini"
	mockclient "ferryman-agent/pkg/llm/client/mock"
	openaiclient "ferryman-agent/pkg/llm/client/openai"
	vertexaiclient "ferryman-agent/pkg/llm/client/vertexai"
)

func NewClient(provider llmclient.Provider, opts ...Option) (llmclient.Client, error) {
	clientOptions := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&clientOptions)
		}
	}

	switch provider {
	case llmclient.ProviderCopilot:
		return copilotclient.NewClient(clientOptions.APIKey, clientOptions.CopilotOptions...), nil
	case llmclient.ProviderAnthropic:
		clientOptions.AnthropicOptions = append(clientOptions.AnthropicOptions, anthropicclient.WithShouldThinkFn(anthropicclient.DefaultShouldThinkFn))
		return anthropicclient.NewClient(clientOptions.APIKey, clientOptions.AnthropicOptions...), nil
	case llmclient.ProviderOpenAI:
		if clientOptions.BaseURL != "" {
			clientOptions.OpenAIOptions = append(clientOptions.OpenAIOptions, openaiclient.WithBaseURL(clientOptions.BaseURL))
		}
		return openaiclient.NewClient(clientOptions.APIKey, clientOptions.OpenAIOptions...), nil
	case llmclient.ProviderGemini:
		return geminiclient.NewClient(clientOptions.APIKey, clientOptions.GeminiOptions...), nil
	case llmclient.ProviderBedrock:
		return bedrockclient.NewClient(clientOptions.APIKey, clientOptions.BedrockOptions...), nil
	case llmclient.ProviderGROQ:
		clientOptions.OpenAIOptions = append(clientOptions.OpenAIOptions, openaiclient.WithDefaultBaseURL("https://api.groq.com/openai/v1"))
		return openaiclient.NewClient(clientOptions.APIKey, clientOptions.OpenAIOptions...), nil
	case llmclient.ProviderAzure:
		return azureclient.NewClient(clientOptions.APIKey, clientOptions.OpenAIOptions...), nil
	case llmclient.ProviderVertexAI:
		return vertexaiclient.NewClient(clientOptions.APIKey, clientOptions.GeminiOptions...), nil
	case llmclient.ProviderOpenRouter:
		clientOptions.OpenAIOptions = append(clientOptions.OpenAIOptions,
			openaiclient.WithDefaultBaseURL("https://openrouter.ai/api/v1"),
			openaiclient.WithExtraHeaders(map[string]string{
				"HTTP-Referer": "ferryer.ai",
				"X-Title":      "Ferryer",
			}),
		)
		return openaiclient.NewClient(clientOptions.APIKey, clientOptions.OpenAIOptions...), nil
	case llmclient.ProviderXAI:
		clientOptions.OpenAIOptions = append(clientOptions.OpenAIOptions, openaiclient.WithDefaultBaseURL("https://api.x.ai/v1"))
		return openaiclient.NewClient(clientOptions.APIKey, clientOptions.OpenAIOptions...), nil
	case llmclient.ProviderLocal:
		baseURL := clientOptions.BaseURL
		if baseURL == "" {
			baseURL = os.Getenv("LOCAL_ENDPOINT")
		}
		clientOptions.OpenAIOptions = append(clientOptions.OpenAIOptions, openaiclient.WithDefaultBaseURL(baseURL))
		return openaiclient.NewClient(clientOptions.APIKey, clientOptions.OpenAIOptions...), nil
	case llmclient.ProviderMock:
		return mockclient.NewClient(), nil
	default:
		return nil, fmt.Errorf("provider not supported: %s", provider)
	}
}
