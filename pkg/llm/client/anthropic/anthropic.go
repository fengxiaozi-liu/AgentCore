package client

import (
	"context"
	"encoding/json"
	"errors"
	llmclient "ferryman-agent/pkg/llm/client"
	"fmt"
	"io"
	"time"

	"ferryman-agent/pkg/data/logging"
	"ferryman-agent/pkg/memory/message"
	toolcore "ferryman-agent/pkg/tools"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type anthropicClient struct {
	options options
	client  anthropic.Client
}

func NewClient(apiKey string, optionFns ...Option) llmclient.Client {
	anthropicOpts := options{}
	for _, o := range optionFns {
		o(&anthropicOpts)
	}

	anthropicClientOptions := []option.RequestOption{}
	if apiKey != "" {
		anthropicClientOptions = append(anthropicClientOptions, option.WithAPIKey(apiKey))
	}
	if anthropicOpts.useBedrock {
		anthropicClientOptions = append(anthropicClientOptions, bedrock.WithLoadDefaultConfig(context.Background()))
	}

	client := anthropic.NewClient(anthropicClientOptions...)
	return &anthropicClient{
		options: anthropicOpts,
		client:  client,
	}
}

func (a *anthropicClient) convertMessages(messages []message.MessageRecord) (anthropicMessages []anthropic.MessageParam) {
	for i, msg := range messages {
		cache := false
		if i > len(messages)-3 {
			cache = true
		}
		switch msg.Role {
		case message.User:
			content := anthropic.NewTextBlock(msg.Content().String())
			if cache && !a.options.disableCache {
				content.OfText.CacheControl = anthropic.CacheControlEphemeralParam{
					Type: "ephemeral",
				}
			}
			var contentBlocks []anthropic.ContentBlockParamUnion
			contentBlocks = append(contentBlocks, content)
			for _, binaryContent := range msg.BinaryContent() {
				base64Image := binaryContent.String(string(llmclient.ProviderAnthropic))
				imageBlock := anthropic.NewImageBlockBase64(binaryContent.MIMEType, base64Image)
				contentBlocks = append(contentBlocks, imageBlock)
			}
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(contentBlocks...))

		case message.Assistant:
			blocks := []anthropic.ContentBlockParamUnion{}
			if msg.Content().String() != "" {
				content := anthropic.NewTextBlock(msg.Content().String())
				if cache && !a.options.disableCache {
					content.OfText.CacheControl = anthropic.CacheControlEphemeralParam{
						Type: "ephemeral",
					}
				}
				blocks = append(blocks, content)
			}

			for _, toolCall := range msg.ToolCalls() {
				var inputMap map[string]any
				err := json.Unmarshal([]byte(toolCall.Input), &inputMap)
				if err != nil {
					continue
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(toolCall.ID, inputMap, toolCall.Name))
			}

			if len(blocks) == 0 {
				logging.Warn("There is a message without content, investigate, this should not happen")
				continue
			}
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(blocks...))

		case message.Tool:
			results := make([]anthropic.ContentBlockParamUnion, len(msg.ToolResults()))
			for i, toolResult := range msg.ToolResults() {
				results[i] = anthropic.NewToolResultBlock(toolResult.ToolCallID, toolResult.Content, toolResult.IsError)
			}
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(results...))
		}
	}
	return
}

func (a *anthropicClient) convertTools(tools []*toolcore.ToolInfo) []anthropic.ToolUnionParam {
	anthropicTools := make([]anthropic.ToolUnionParam, len(tools))

	for i, info := range tools {
		toolParam := anthropic.ToolParam{
			Name:        info.Name,
			Description: anthropic.String(info.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: info.Parameters,
				// TODO: figure out how we can tell claude the required fields?
			},
		}

		if i == len(tools)-1 && !a.options.disableCache {
			toolParam.CacheControl = anthropic.CacheControlEphemeralParam{
				Type: "ephemeral",
			}
		}

		anthropicTools[i] = anthropic.ToolUnionParam{OfTool: &toolParam}
	}

	return anthropicTools
}

func (a *anthropicClient) finishReason(reason string) message.FinishReason {
	switch reason {
	case "end_turn":
		return message.FinishReasonEndTurn
	case "max_tokens":
		return message.FinishReasonMaxTokens
	case "tool_use":
		return message.FinishReasonToolUse
	case "stop_sequence":
		return message.FinishReasonEndTurn
	default:
		return message.FinishReasonUnknown
	}
}

func (a *anthropicClient) preparedMessages(model llmclient.Model, systemMessage string, messages []anthropic.MessageParam, tools []anthropic.ToolUnionParam) anthropic.MessageNewParams {
	var thinkingParam anthropic.ThinkingConfigParamUnion
	lastMessage := messages[len(messages)-1]
	isUser := lastMessage.Role == anthropic.MessageParamRoleUser
	messageContent := ""
	temperature := anthropic.Float(0)
	if isUser {
		for _, m := range lastMessage.Content {
			if m.OfText != nil && m.OfText.Text != "" {
				messageContent = m.OfText.Text
			}
		}
		if messageContent != "" && a.options.shouldThink != nil && a.options.shouldThink(messageContent) {
			thinkingParam = anthropic.ThinkingConfigParamOfEnabled(int64(float64(model.MaxTokens) * 0.8))
			temperature = anthropic.Float(1)
		}
	}

	return anthropic.MessageNewParams{
		Model:       anthropic.Model(model.APIModel),
		MaxTokens:   model.MaxTokens,
		Temperature: temperature,
		Messages:    messages,
		Tools:       tools,
		Thinking:    thinkingParam,
		System: []anthropic.TextBlockParam{
			{
				Text: systemMessage,
				CacheControl: anthropic.CacheControlEphemeralParam{
					Type: "ephemeral",
				},
			},
		},
	}
}

func (a *anthropicClient) Send(ctx context.Context, request llmclient.Request) (resposne *llmclient.Response, err error) {
	preparedMessages := a.preparedMessages(request.Model, request.SystemMessage, a.convertMessages(request.Messages), a.convertTools(request.Tools))
	if request.Debug {
		jsonData, _ := json.Marshal(preparedMessages)
		logging.Debug("Prepared messages", "messages", string(jsonData))
	}

	attempts := 0
	for {
		attempts++
		anthropicResponse, err := a.client.Messages.New(
			ctx,
			preparedMessages,
		)
		// If there is an error we are going to see if we can retry the call
		if err != nil {
			logging.Error("Error in Anthropic API call", "error", err)
			retry, after, retryErr := a.shouldRetry(attempts, err)
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
		for _, block := range anthropicResponse.Content {
			if text, ok := block.AsAny().(anthropic.TextBlock); ok {
				content += text.Text
			}
		}

		return &llmclient.Response{
			Content:   content,
			ToolCalls: a.toolCalls(*anthropicResponse),
			Usage:     a.usage(*anthropicResponse),
		}, nil
	}
}

func (a *anthropicClient) Stream(ctx context.Context, request llmclient.Request) <-chan llmclient.Event {
	preparedMessages := a.preparedMessages(request.Model, request.SystemMessage, a.convertMessages(request.Messages), a.convertTools(request.Tools))

	var sessionId string
	requestSeqId := (len(request.Messages) + 1) / 2
	if request.Debug {
		if sid, ok := ctx.Value(toolcore.SessionIDContextKey).(string); ok {
			sessionId = sid
		}
		jsonData, _ := json.Marshal(preparedMessages)
		if sessionId != "" {
			filepath := logging.WriteRequestMessageJson(sessionId, requestSeqId, preparedMessages)
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
			anthropicStream := a.client.Messages.NewStreaming(
				ctx,
				preparedMessages,
			)
			accumulatedMessage := anthropic.Message{}

			currentToolCallID := ""
			for anthropicStream.Next() {
				event := anthropicStream.Current()
				err := accumulatedMessage.Accumulate(event)
				if err != nil {
					logging.Warn("Error accumulating message", "error", err)
					continue
				}

				switch event := event.AsAny().(type) {
				case anthropic.ContentBlockStartEvent:
					if event.ContentBlock.Type == "text" {
						eventChan <- llmclient.Event{Type: llmclient.EventContentStart}
					} else if event.ContentBlock.Type == "tool_use" {
						currentToolCallID = event.ContentBlock.ID
						eventChan <- llmclient.Event{
							Type: llmclient.EventToolUseStart,
							ToolCall: &message.ToolCall{
								ID:       event.ContentBlock.ID,
								Name:     event.ContentBlock.Name,
								Finished: false,
							},
						}
					}

				case anthropic.ContentBlockDeltaEvent:
					if event.Delta.Type == "thinking_delta" && event.Delta.Thinking != "" {
						eventChan <- llmclient.Event{
							Type:     llmclient.EventThinkingDelta,
							Thinking: event.Delta.Thinking,
						}
					} else if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
						eventChan <- llmclient.Event{
							Type:    llmclient.EventContentDelta,
							Content: event.Delta.Text,
						}
					} else if event.Delta.Type == "input_json_delta" {
						if currentToolCallID != "" {
							eventChan <- llmclient.Event{
								Type: llmclient.EventToolUseDelta,
								ToolCall: &message.ToolCall{
									ID:       currentToolCallID,
									Finished: false,
									Input:    event.Delta.JSON.PartialJSON.Raw(),
								},
							}
						}
					}
				case anthropic.ContentBlockStopEvent:
					if currentToolCallID != "" {
						eventChan <- llmclient.Event{
							Type: llmclient.EventToolUseStop,
							ToolCall: &message.ToolCall{
								ID: currentToolCallID,
							},
						}
						currentToolCallID = ""
					} else {
						eventChan <- llmclient.Event{Type: llmclient.EventContentStop}
					}

				case anthropic.MessageStopEvent:
					content := ""
					for _, block := range accumulatedMessage.Content {
						if text, ok := block.AsAny().(anthropic.TextBlock); ok {
							content += text.Text
						}
					}

					eventChan <- llmclient.Event{
						Type: llmclient.EventComplete,
						Response: &llmclient.Response{
							Content:      content,
							ToolCalls:    a.toolCalls(accumulatedMessage),
							Usage:        a.usage(accumulatedMessage),
							FinishReason: a.finishReason(string(accumulatedMessage.StopReason)),
						},
					}
				}
			}

			err := anthropicStream.Err()
			if err == nil || errors.Is(err, io.EOF) {
				close(eventChan)
				return
			}
			// If there is an error we are going to see if we can retry the call
			retry, after, retryErr := a.shouldRetry(attempts, err)
			if retryErr != nil {
				eventChan <- llmclient.Event{Type: llmclient.EventError, Error: retryErr}
				close(eventChan)
				return
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, llmclient.MaxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				select {
				case <-ctx.Done():
					// context cancelled
					if ctx.Err() != nil {
						eventChan <- llmclient.Event{Type: llmclient.EventError, Error: ctx.Err()}
					}
					close(eventChan)
					return
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			if ctx.Err() != nil {
				eventChan <- llmclient.Event{Type: llmclient.EventError, Error: ctx.Err()}
			}

			close(eventChan)
			return
		}
	}()
	return eventChan
}

func (a *anthropicClient) shouldRetry(attempts int, err error) (bool, int64, error) {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) {
		return false, 0, err
	}

	if apierr.StatusCode != 429 && apierr.StatusCode != 529 {
		return false, 0, err
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

func (a *anthropicClient) toolCalls(msg anthropic.Message) []message.ToolCall {
	var toolCalls []message.ToolCall

	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.ToolUseBlock:
			toolCall := message.ToolCall{
				ID:       variant.ID,
				Name:     variant.Name,
				Input:    string(variant.Input),
				Type:     string(variant.Type),
				Finished: true,
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	return toolCalls
}

func (a *anthropicClient) usage(msg anthropic.Message) llmclient.TokenUsage {
	return llmclient.TokenUsage{
		InputTokens:         msg.Usage.InputTokens,
		OutputTokens:        msg.Usage.OutputTokens,
		CacheCreationTokens: msg.Usage.CacheCreationInputTokens,
		CacheReadTokens:     msg.Usage.CacheReadInputTokens,
	}
}
