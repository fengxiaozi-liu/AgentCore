package client

import (
	"context"
	"encoding/json"
	"errors"
	llmclient "ferryman-agent/pkg/llm/client"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ferryman-agent/pkg/data/logging"
	"ferryman-agent/pkg/memory/message"
	toolcore "ferryman-agent/pkg/tools"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

type copilotClient struct {
	options     options
	client      openai.Client
	httpClient  *http.Client
	githubToken string
}

// CopilotTokenResponse represents the response from GitHub's token exchange endpoint
type CopilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

func loadGitHubToken() (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	var configDir string
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		configDir = xdgConfig
	} else if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			configDir = localAppData
		} else {
			configDir = filepath.Join(os.Getenv("HOME"), "AppData", "Local")
		}
	} else {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}

	for _, filePath := range []string{
		filepath.Join(configDir, "github-copilot", "hosts.json"),
		filepath.Join(configDir, "github-copilot", "apps.json"),
	} {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var config map[string]map[string]interface{}
		if err := json.Unmarshal(data, &config); err != nil {
			continue
		}
		for key, value := range config {
			if strings.Contains(key, "github.com") {
				if oauthToken, ok := value["oauth_token"].(string); ok {
					return oauthToken, nil
				}
			}
		}
	}
	return "", fmt.Errorf("GitHub token not found in standard locations")
}

func (c *copilotClient) isAnthropicModel(model llmclient.Model) bool {
	modelID := string(model.ID)
	return strings.HasPrefix(modelID, "copilot.claude-") || strings.HasPrefix(modelID, "claude-")
}

// loadGitHubToken loads the GitHub OAuth token from the standard GitHub CLI/Copilot locations

// exchangeGitHubToken exchanges a GitHub token for a Copilot bearer token
func (c *copilotClient) exchangeGitHubToken(githubToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token exchange request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+githubToken)
	req.Header.Set("User-Agent", "Ferryer/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to exchange GitHub token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp CopilotTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	return tokenResp.Token, nil
}

func NewClient(apiKey string, optionFns ...Option) llmclient.Client {
	copilotOpts := options{
		reasoningEffort: "medium",
	}
	// Apply copilot-specific options
	for _, o := range optionFns {
		o(&copilotOpts)
	}

	// Create HTTP client for token exchange
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	var bearerToken string
	var githubToken string

	// If bearer token is already provided, use it
	if copilotOpts.bearerToken != "" {
		bearerToken = copilotOpts.bearerToken
	} else {
		// Try to get GitHub token from multiple sources
		// 1. Environment variable
		githubToken = os.Getenv("GITHUB_TOKEN")

		// 2. API key from options
		if githubToken == "" {
			githubToken = apiKey
		}

		// 3. Standard GitHub CLI/Copilot locations
		if githubToken == "" {
			var err error
			githubToken, err = loadGitHubToken()
			if err != nil {
				logging.Debug("Failed to load GitHub token from standard locations", "error", err)
			}
		}

		if githubToken == "" {
			logging.Error("GitHub token is required for Copilot provider. Set GITHUB_TOKEN environment variable, configure it in ferryer config, or ensure GitHub CLI/Copilot is properly authenticated.")
			return &copilotClient{
				options:     copilotOpts,
				httpClient:  httpClient,
				githubToken: githubToken,
			}
		}

		// Create a temporary client for token exchange
		tempClient := &copilotClient{
			options:     copilotOpts,
			httpClient:  httpClient,
			githubToken: githubToken,
		}

		// Exchange GitHub token for bearer token
		var err error
		bearerToken, err = tempClient.exchangeGitHubToken(githubToken)
		if err != nil {
			logging.Error("Failed to exchange GitHub token for Copilot bearer token", "error", err)
			return &copilotClient{
				options:     copilotOpts,
				httpClient:  httpClient,
				githubToken: githubToken,
			}
		}
	}

	copilotOpts.bearerToken = bearerToken

	// GitHub Copilot API base URL
	baseURL := "https://api.githubcopilot.com"

	openaiClientOptions := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithAPIKey(bearerToken), // Use bearer token as API key
	}

	// Add GitHub Copilot specific headers
	openaiClientOptions = append(openaiClientOptions,
		option.WithHeader("Editor-Version", "Ferryer/1.0"),
		option.WithHeader("Editor-Plugin-Version", "Ferryer/1.0"),
		option.WithHeader("Copilot-Integration-Id", "vscode-chat"),
	)

	// Add any extra headers
	if copilotOpts.extraHeaders != nil {
		for key, value := range copilotOpts.extraHeaders {
			openaiClientOptions = append(openaiClientOptions, option.WithHeader(key, value))
		}
	}

	client := openai.NewClient(openaiClientOptions...)
	// logging.Debug("Copilot client created", "opts", opts, "copilotOpts", copilotOpts, "model", opts.Model)
	return &copilotClient{
		options:     copilotOpts,
		client:      client,
		httpClient:  httpClient,
		githubToken: githubToken,
	}
}

func (c *copilotClient) convertMessages(systemMessage string, messages []message.MessageRecord) (copilotMessages []openai.ChatCompletionMessageParamUnion) {
	// Add system message first
	copilotMessages = append(copilotMessages, openai.SystemMessage(systemMessage))

	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			var content []openai.ChatCompletionContentPartUnionParam
			textBlock := openai.ChatCompletionContentPartTextParam{Text: msg.Content().String()}
			content = append(content, openai.ChatCompletionContentPartUnionParam{OfText: &textBlock})

			for _, binaryContent := range msg.BinaryContent() {
				imageURL := openai.ChatCompletionContentPartImageImageURLParam{URL: binaryContent.String(string(llmclient.ProviderCopilot))}
				imageBlock := openai.ChatCompletionContentPartImageParam{ImageURL: imageURL}
				content = append(content, openai.ChatCompletionContentPartUnionParam{OfImageURL: &imageBlock})
			}

			copilotMessages = append(copilotMessages, openai.UserMessage(content))

		case message.Assistant:
			assistantMsg := openai.ChatCompletionAssistantMessageParam{
				Role: "assistant",
			}

			if msg.Content().String() != "" {
				assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(msg.Content().String()),
				}
			}

			if len(msg.ToolCalls()) > 0 {
				assistantMsg.ToolCalls = make([]openai.ChatCompletionMessageToolCallParam, len(msg.ToolCalls()))
				for i, call := range msg.ToolCalls() {
					assistantMsg.ToolCalls[i] = openai.ChatCompletionMessageToolCallParam{
						ID:   call.ID,
						Type: "function",
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      call.Name,
							Arguments: call.Input,
						},
					}
				}
			}

			copilotMessages = append(copilotMessages, openai.ChatCompletionMessageParamUnion{
				OfAssistant: &assistantMsg,
			})

		case message.Tool:
			for _, result := range msg.ToolResults() {
				copilotMessages = append(copilotMessages,
					openai.ToolMessage(result.Content, result.ToolCallID),
				)
			}
		}
	}

	return
}

func (c *copilotClient) convertTools(tools []*toolcore.ToolInfo) []openai.ChatCompletionToolParam {
	copilotTools := make([]openai.ChatCompletionToolParam, len(tools))

	for i, info := range tools {
		copilotTools[i] = openai.ChatCompletionToolParam{
			Function: openai.FunctionDefinitionParam{
				Name:        info.Name,
				Description: openai.String(info.Description),
				Parameters: openai.FunctionParameters{
					"type":       "object",
					"properties": info.Parameters,
					"required":   info.Required,
				},
			},
		}
	}

	return copilotTools
}

func (c *copilotClient) finishReason(reason string) message.FinishReason {
	switch reason {
	case "stop":
		return message.FinishReasonEndTurn
	case "length":
		return message.FinishReasonMaxTokens
	case "tool_calls":
		return message.FinishReasonToolUse
	default:
		return message.FinishReasonUnknown
	}
}

func (c *copilotClient) preparedParams(model llmclient.Model, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(model.APIModel),
		Messages: messages,
		Tools:    tools,
	}

	if model.CanReason == true {
		params.MaxCompletionTokens = openai.Int(model.MaxTokens)
		reasoningEffort := model.ReasoningEffort
		if reasoningEffort == "" {
			reasoningEffort = c.options.reasoningEffort
		}
		switch reasoningEffort {
		case "low":
			params.ReasoningEffort = shared.ReasoningEffortLow
		case "medium":
			params.ReasoningEffort = shared.ReasoningEffortMedium
		case "high":
			params.ReasoningEffort = shared.ReasoningEffortHigh
		default:
			params.ReasoningEffort = shared.ReasoningEffortMedium
		}
	} else {
		params.MaxTokens = openai.Int(model.MaxTokens)
	}

	return params
}

func (c *copilotClient) Send(ctx context.Context, request llmclient.Request) (response *llmclient.Response, err error) {
	params := c.preparedParams(request.Model, c.convertMessages(request.SystemMessage, request.Messages), c.convertTools(request.Tools))
	var sessionId string
	requestSeqId := (len(request.Messages) + 1) / 2
	if request.Debug {
		// jsonData, _ := json.Marshal(params)
		// logging.Debug("Prepared messages", "messages", string(jsonData))
		if sid, ok := ctx.Value(toolcore.SessionIDContextKey).(string); ok {
			sessionId = sid
		}
		jsonData, _ := json.Marshal(params)
		if sessionId != "" {
			filepath := logging.WriteRequestMessageJson(sessionId, requestSeqId, params)
			logging.Debug("Prepared messages", "filepath", filepath)
		} else {
			logging.Debug("Prepared messages", "messages", string(jsonData))
		}
	}

	attempts := 0
	for {
		attempts++
		copilotResponse, err := c.client.Chat.Completions.New(
			ctx,
			params,
		)

		// If there is an error we are going to see if we can retry the call
		if err != nil {
			retry, after, retryErr := c.shouldRetry(attempts, err)
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, llmclient.MaxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			return nil, retryErr
		}

		content := ""
		if copilotResponse.Choices[0].Message.Content != "" {
			content = copilotResponse.Choices[0].Message.Content
		}

		toolCalls := c.toolCalls(*copilotResponse)
		finishReason := c.finishReason(string(copilotResponse.Choices[0].FinishReason))

		if len(toolCalls) > 0 {
			finishReason = message.FinishReasonToolUse
		}

		return &llmclient.Response{
			Content:      content,
			ToolCalls:    toolCalls,
			Usage:        c.usage(*copilotResponse),
			FinishReason: finishReason,
		}, nil
	}
}

func (c *copilotClient) Stream(ctx context.Context, request llmclient.Request) <-chan llmclient.Event {
	params := c.preparedParams(request.Model, c.convertMessages(request.SystemMessage, request.Messages), c.convertTools(request.Tools))
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}

	var sessionId string
	requestSeqId := (len(request.Messages) + 1) / 2
	if request.Debug {
		if sid, ok := ctx.Value(toolcore.SessionIDContextKey).(string); ok {
			sessionId = sid
		}
		jsonData, _ := json.Marshal(params)
		if sessionId != "" {
			filepath := logging.WriteRequestMessageJson(sessionId, requestSeqId, params)
			logging.Debug("Prepared messages", "filepath", filepath)
		} else {
			logging.Debug("Prepared messages", "messages", string(jsonData))
		}

	}

	attempts := 0
	eventChan := make(chan llmclient.Event)

	go func() {
		for {
			attempts++
			copilotStream := c.client.Chat.Completions.NewStreaming(
				ctx,
				params,
			)

			acc := openai.ChatCompletionAccumulator{}
			currentContent := ""
			toolCalls := make([]message.ToolCall, 0)

			var currentToolCallId string
			var currentToolCall openai.ChatCompletionMessageToolCall
			var msgToolCalls []openai.ChatCompletionMessageToolCall
			for copilotStream.Next() {
				chunk := copilotStream.Current()
				acc.AddChunk(chunk)

				if request.Debug {
					logging.AppendToStreamSessionLogJson(sessionId, requestSeqId, chunk)
				}

				for _, choice := range chunk.Choices {
					if choice.Delta.Content != "" {
						eventChan <- llmclient.Event{
							Type:    llmclient.EventContentDelta,
							Content: choice.Delta.Content,
						}
						currentContent += choice.Delta.Content
					}
				}

				if c.isAnthropicModel(request.Model) {
					// Monkeypatch adapter for Sonnet-4 multi-tool use
					for _, choice := range chunk.Choices {
						if choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0 {
							toolCall := choice.Delta.ToolCalls[0]
							// Detect tool use start
							if currentToolCallId == "" {
								if toolCall.ID != "" {
									currentToolCallId = toolCall.ID
									currentToolCall = openai.ChatCompletionMessageToolCall{
										ID:   toolCall.ID,
										Type: "function",
										Function: openai.ChatCompletionMessageToolCallFunction{
											Name:      toolCall.Function.Name,
											Arguments: toolCall.Function.Arguments,
										},
									}
								}
							} else {
								// Delta tool use
								if toolCall.ID == "" {
									currentToolCall.Function.Arguments += toolCall.Function.Arguments
								} else {
									// Detect new tool use
									if toolCall.ID != currentToolCallId {
										msgToolCalls = append(msgToolCalls, currentToolCall)
										currentToolCallId = toolCall.ID
										currentToolCall = openai.ChatCompletionMessageToolCall{
											ID:   toolCall.ID,
											Type: "function",
											Function: openai.ChatCompletionMessageToolCallFunction{
												Name:      toolCall.Function.Name,
												Arguments: toolCall.Function.Arguments,
											},
										}
									}
								}
							}
						}
						if choice.FinishReason == "tool_calls" {
							msgToolCalls = append(msgToolCalls, currentToolCall)
							acc.ChatCompletion.Choices[0].Message.ToolCalls = msgToolCalls
						}
					}
				}
			}

			err := copilotStream.Err()
			if err == nil || errors.Is(err, io.EOF) {
				if request.Debug {
					respFilepath := logging.WriteChatResponseJson(sessionId, requestSeqId, acc.ChatCompletion)
					logging.Debug("Chat completion response", "filepath", respFilepath)
				}
				// Stream completed successfully
				finishReason := c.finishReason(string(acc.ChatCompletion.Choices[0].FinishReason))
				if len(acc.ChatCompletion.Choices[0].Message.ToolCalls) > 0 {
					toolCalls = append(toolCalls, c.toolCalls(acc.ChatCompletion)...)
				}
				if len(toolCalls) > 0 {
					finishReason = message.FinishReasonToolUse
				}

				eventChan <- llmclient.Event{
					Type: llmclient.EventComplete,
					Response: &llmclient.Response{
						Content:      currentContent,
						ToolCalls:    toolCalls,
						Usage:        c.usage(acc.ChatCompletion),
						FinishReason: finishReason,
					},
				}
				close(eventChan)
				return
			}

			// If there is an error we are going to see if we can retry the call
			retry, after, retryErr := c.shouldRetry(attempts, err)
			if retryErr != nil {
				eventChan <- llmclient.Event{Type: llmclient.EventError, Error: retryErr}
				close(eventChan)
				return
			}
			// shouldRetry is not catching the max retries...
			// TODO: Figure out why
			if attempts > llmclient.MaxRetries {
				logging.Warn("Maximum retry attempts reached for rate limit", "attempts", attempts, "max_retries", llmclient.MaxRetries)
				retry = false
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d (paused for %d ms)", attempts, llmclient.MaxRetries, after), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				select {
				case <-ctx.Done():
					// context cancelled
					if ctx.Err() == nil {
						eventChan <- llmclient.Event{Type: llmclient.EventError, Error: ctx.Err()}
					}
					close(eventChan)
					return
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			eventChan <- llmclient.Event{Type: llmclient.EventError, Error: retryErr}
			close(eventChan)
			return
		}
	}()

	return eventChan
}

func (c *copilotClient) shouldRetry(attempts int, err error) (bool, int64, error) {
	var apierr *openai.Error
	if !errors.As(err, &apierr) {
		return false, 0, err
	}

	// Check for token expiration (401 Unauthorized)
	if apierr.StatusCode == 401 {
		// Try to refresh the bearer token
		var githubToken string

		// 1. Environment variable
		githubToken = os.Getenv("GITHUB_TOKEN")

		// 2. API key from options
		if githubToken == "" {
			githubToken = c.githubToken
		}

		// 3. Standard GitHub CLI/Copilot locations
		if githubToken == "" {
			var err error
			githubToken, err = loadGitHubToken()
			if err != nil {
				logging.Debug("Failed to load GitHub token from standard locations during retry", "error", err)
			}
		}

		if githubToken != "" {
			newBearerToken, tokenErr := c.exchangeGitHubToken(githubToken)
			if tokenErr == nil {
				c.options.bearerToken = newBearerToken
				// Update the client with the new token
				// Note: This is a simplified approach. In a production system,
				// you might want to recreate the entire client with the new token
				logging.Info("Refreshed Copilot bearer token")
				return true, 1000, nil // Retry immediately with new token
			}
			logging.Error("Failed to refresh Copilot bearer token", "error", tokenErr)
		}
		return false, 0, fmt.Errorf("authentication failed: %w", err)
	}
	logging.Debug("Copilot API Error", "status", apierr.StatusCode, "headers", apierr.Response.Header, "body", apierr.RawJSON())

	if apierr.StatusCode != 429 && apierr.StatusCode != 500 {
		return false, 0, err
	}

	if apierr.StatusCode == 500 {
		logging.Warn("Copilot API returned 500 error, retrying", "error", err)
	}

	if attempts > llmclient.MaxRetries {
		return false, 0, fmt.Errorf("maximum retry attempts reached for rate limit: %d retries", llmclient.MaxRetries)
	}

	retryMs := 0
	retryAfterValues := apierr.Response.Header.Values("Retry-After")

	backoffMs := 2000 * (1 << (attempts - 1))
	jitterMs := int(float64(backoffMs) * 0.2)
	retryMs = backoffMs + jitterMs
	if len(retryAfterValues) > 0 {
		if _, err := fmt.Sscanf(retryAfterValues[0], "%d", &retryMs); err == nil {
			retryMs = retryMs * 1000
		}
	}
	return true, int64(retryMs), nil
}

func (c *copilotClient) toolCalls(completion openai.ChatCompletion) []message.ToolCall {
	var toolCalls []message.ToolCall

	if len(completion.Choices) > 0 && len(completion.Choices[0].Message.ToolCalls) > 0 {
		for _, call := range completion.Choices[0].Message.ToolCalls {
			toolCall := message.ToolCall{
				ID:       call.ID,
				Name:     call.Function.Name,
				Input:    call.Function.Arguments,
				Type:     "function",
				Finished: true,
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	return toolCalls
}

func (c *copilotClient) usage(completion openai.ChatCompletion) llmclient.TokenUsage {
	cachedTokens := completion.Usage.PromptTokensDetails.CachedTokens
	inputTokens := completion.Usage.PromptTokens - cachedTokens

	return llmclient.TokenUsage{
		InputTokens:         inputTokens,
		OutputTokens:        completion.Usage.CompletionTokens,
		CacheCreationTokens: 0, // GitHub Copilot doesn't provide this directly
		CacheReadTokens:     cachedTokens,
	}
}
