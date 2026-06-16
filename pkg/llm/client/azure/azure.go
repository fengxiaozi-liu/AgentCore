package client

import (
	llmclient "ferryman-agent/pkg/llm/client"
	client3 "ferryman-agent/pkg/llm/client/openai"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/openai/openai-go/option"
)

type azureClient struct {
	llmclient.Client
}

func NewClient(apiKey string, optionFns ...client3.Option) llmclient.Client {

	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")      // ex: https://foo.openai.azure.com
	apiVersion := os.Getenv("AZURE_OPENAI_API_VERSION") // ex: 2025-04-01-preview

	if endpoint == "" || apiVersion == "" {
		return &azureClient{Client: client3.NewClient(apiKey, optionFns...)}
	}

	reqOpts := []option.RequestOption{
		azure.WithEndpoint(endpoint, apiVersion),
	}

	if apiKey != "" || os.Getenv("AZURE_OPENAI_API_KEY") != "" {
		key := apiKey
		if key == "" {
			key = os.Getenv("AZURE_OPENAI_API_KEY")
		}
		reqOpts = append(reqOpts, azure.WithAPIKey(key))
	} else if cred, err := azidentity.NewDefaultAzureCredential(nil); err == nil {
		reqOpts = append(reqOpts, azure.WithTokenCredential(cred))
	}

	base := client3.NewClientWithOpenAI(openai.NewClient(reqOpts...), optionFns...)

	return &azureClient{Client: base}
}
