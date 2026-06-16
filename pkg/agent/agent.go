package agent

import (
	"context"
	"errors"
	"ferryman-agent/pkg/llm/client"
	"fmt"
	"strings"
	"sync"
	"time"

	"ferryman-agent/pkg/data/logging"
	"ferryman-agent/pkg/llm/provider"
	"ferryman-agent/pkg/memory/message"
	"ferryman-agent/pkg/memory/session"
	"ferryman-agent/pkg/permission"
	"ferryman-agent/pkg/pubsub"
	toolcore "ferryman-agent/pkg/tools"
)

type AgentEvent struct {
	Type      AgentEventType
	Message   message.MessageRecord
	Error     error
	SessionID string
	Progress  string
	Done      bool
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Run(ctx context.Context, modelProvider provider.ModelProvider, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	MapTools() toolcore.ToolMap
	RunTool(ctx context.Context, sessionID string, call toolcore.ToolCall) (toolcore.ToolResponse, error)
	RunToolByKind(ctx context.Context, sessionID string, kind toolcore.ToolKind, call toolcore.ToolCall) (toolcore.ToolResponse, error)
	Cancel(sessionID string)
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Summarize(ctx context.Context, modelProvider provider.ModelProvider, sessionID string) error
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	config         AgentConfig
	sessions       session.Service //
	messages       message.Service
	tools          toolcore.ToolMap
	agentName      string
	agentDesc      string
	provider       *provider.ProviderClient
	activeRequests sync.Map
}

func NewAgent(opts ...AgentOption) (Service, error) {
	cfg := AgentConfig{}
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	cfg.AgentName = strings.TrimSpace(cfg.AgentName)
	cfg.AgentDescription = strings.TrimSpace(cfg.AgentDescription)
	if cfg.Provider == nil {
		cfg.Provider = provider.NewProvider()
	}
	if cfg.Memory.Session == nil || cfg.Memory.Messages == nil {
		return nil, fmt.Errorf("memory session and messages services must be configured together")
	}
	toolMap, err := toolcore.BuildToolMap(cfg.Tools)
	if err != nil {
		return nil, err
	}
	return &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		config:         cfg,
		sessions:       cfg.Memory.Session,
		messages:       cfg.Memory.Messages,
		tools:          toolMap,
		agentName:      cfg.AgentName,
		agentDesc:      cfg.AgentDescription,
		provider:       cfg.Provider,
		activeRequests: sync.Map{},
	}, nil
}

func (a *agent) sendMessages(ctx context.Context, modelProvider provider.ModelProvider, agentDescription string, tools []*toolcore.ToolInfo, messages []message.MessageRecord) (*client.Response, error) {
	return a.provider.SendMessages(ctx, modelProvider, client.Request{
		SystemMessage: agentDescription,
		Debug:         a.config.Debug,
		Messages:      messages,
		Tools:         tools,
	})
}

func (a *agent) streamResponse(ctx context.Context, modelProvider provider.ModelProvider, agentDescription string, tools []*toolcore.ToolInfo, messages []message.MessageRecord) <-chan client.Event {
	return a.provider.StreamResponse(ctx, modelProvider, client.Request{
		SystemMessage: agentDescription,
		Debug:         a.config.Debug,
		Messages:      messages,
		Tools:         tools,
	})
}

func (a *agent) Cancel(sessionID string) {
	// Cancel regular requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Request cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Summarize cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}
}

func (a *agent) MapTools() toolcore.ToolMap {
	out := make(toolcore.ToolMap, len(a.tools))
	for kind, toolsByName := range a.tools {
		out[kind] = make(map[string]*toolcore.Tool, len(toolsByName))
		for name, tool := range toolsByName {
			out[kind][name] = tool
		}
	}
	return out
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	return busy
}

func (a *agent) generateTitle(ctx context.Context, modelProvider provider.ModelProvider, sessionID string, content string) error {
	if content == "" {
		return nil
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, toolcore.SessionIDContextKey, sessionID)
	parts := []message.ContentPart{message.TextContent{Text: content}}
	response, err := a.sendMessages(ctx, modelProvider, titlePrompt, nil, []message.MessageRecord{
		{
			Role:  message.User,
			Parts: parts,
		},
	})
	if err != nil {
		return err
	}

	title := strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
	if title == "" {
		return nil
	}

	session.Title = title
	_, err = a.sessions.Save(ctx, session)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) Run(ctx context.Context, modelProvider provider.ModelProvider, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	if !modelProvider.Model.SupportsAttachments && attachments != nil {
		attachments = nil
	}
	events := make(chan AgentEvent)
	if a.IsSessionBusy(sessionID) {
		return nil, ErrSessionBusy
	}

	genCtx, cancel := context.WithCancel(ctx)

	a.activeRequests.Store(sessionID, cancel)
	go func() {
		logging.Debug("Request started", "sessionID", sessionID)
		defer logging.RecoverPanic("agent.Run", func() {
			events <- a.err(fmt.Errorf("panic while running the agent"))
		})
		var attachmentParts []message.ContentPart
		for _, attachment := range attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
		}
		result := a.processGeneration(genCtx, modelProvider, sessionID, content, attachmentParts)
		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		logging.Debug("Request completed", "sessionID", sessionID)
		a.activeRequests.Delete(sessionID)
		cancel()
		a.Publish(pubsub.CreatedEvent, result)
		events <- result
		close(events)
	}()
	return events, nil
}

func (a *agent) processGeneration(ctx context.Context, modelProvider provider.ModelProvider, sessionID, content string, attachmentParts []message.ContentPart) AgentEvent {
	debugEnabled := a.config.Debug
	// List visible conversation messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(context.Background(), modelProvider, sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	_, err = a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	msgHistory, err := a.buildModelContext(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to build model context: %w", err))
	}

	for {
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		agentMessage, toolResults, err := a.streamAndHandleEvents(ctx, modelProvider, sessionID, msgHistory)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				agentMessage.AddFinish(message.FinishReasonCanceled)
				a.messages.Update(context.Background(), agentMessage)
				return a.err(ErrRequestCancelled)
			}
			return a.err(fmt.Errorf("failed to process events: %w", err))
		}
		if debugEnabled {
			seqId := (len(msgHistory) + 1) / 2
			toolResultFilepath := logging.WriteToolResultsJson(sessionID, seqId, toolResults)
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", "{}", "filepath", toolResultFilepath)
		} else {
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", toolResults)
		}
		if (agentMessage.FinishReason() == message.FinishReasonToolUse) && toolResults != nil {
			// We are not done, we need to respond with the tool response
			msgHistory = append(msgHistory, agentMessage, *toolResults)
			continue
		}
		return AgentEvent{
			Type:    AgentEventTypeResponse,
			Message: agentMessage,
			Done:    true,
		}
	}
}

func (a *agent) buildModelContext(ctx context.Context, sessionID string) ([]message.MessageRecord, error) {
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.SummaryMessageID == "" {
		return a.messages.List(ctx, sessionID)
	}

	summaryMsg, err := a.messages.Get(ctx, session.SummaryMessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary message: %w", err)
	}
	if summaryMsg.SessionID != sessionID || summaryMsg.Kind != message.MessageKindSummary {
		return nil, fmt.Errorf("invalid summary message %s for session %s", session.SummaryMessageID, sessionID)
	}

	conversation, err := a.messages.List(ctx, sessionID, message.ListCondition{
		Kinds:          []message.MessageKind{message.MessageKindConversation},
		AfterMessageID: session.SummaryMessageID,
	})
	if err != nil {
		return nil, err
	}
	return append([]message.MessageRecord{summaryMsg}, conversation...), nil
}

func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.MessageRecord, error) {
	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

func (a *agent) streamAndHandleEvents(ctx context.Context, modelProvider provider.ModelProvider, sessionID string, msgHistory []message.MessageRecord) (message.MessageRecord, *message.MessageRecord, error) {
	ctx = context.WithValue(ctx, toolcore.SessionIDContextKey, sessionID)
	eventChan := a.streamResponse(ctx, modelProvider, a.agentDesc, toolcore.ToolInfos(a.tools), msgHistory)

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: string(modelProvider.Model.ID),
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, toolcore.MessageIDContextKey, assistantMsg.ID)

	// Process each event in the stream.
	for event := range eventChan {
		if processErr := a.processEvent(ctx, modelProvider.Model, sessionID, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, ctx.Err()
		}
	}

	toolResults := a.runToolCalls(ctx, &assistantMsg)
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

func (a *agent) runToolCalls(ctx context.Context, assistantMsg *message.MessageRecord) []message.ToolResult {
	toolCalls := assistantMsg.ToolCalls()
	toolResults := make([]message.ToolResult, len(toolCalls))

	for i, toolCall := range toolCalls {
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), assistantMsg, message.FinishReasonCanceled)
			fillCanceledToolResults(toolResults, toolCalls, i)
			return toolResults
		}

		toolResult, toolErr := a.RunTool(ctx, assistantMsg.SessionID, toolcore.ToolCall{
			ID:    toolCall.ID,
			Name:  toolCall.Name,
			Input: toolCall.Input,
		})
		if toolErr != nil && errors.Is(toolErr, ErrToolNotFound) {
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    fmt.Sprintf("Tool not found: %s", toolCall.Name),
				IsError:    true,
			}
			continue
		}
		if toolErr != nil && errors.Is(toolErr, ErrToolAmbiguous) {
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    fmt.Sprintf("Tool name is ambiguous: %s", toolCall.Name),
				IsError:    true,
			}
			continue
		}
		if toolErr != nil && errors.Is(toolErr, permission.ErrorPermissionDenied) {
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    "Permission denied",
				IsError:    true,
			}
			fillCanceledToolResults(toolResults, toolCalls, i+1)
			a.finishMessage(ctx, assistantMsg, message.FinishReasonPermissionDenied)
			return toolResults
		}

		toolResults[i] = message.ToolResult{
			ToolCallID: toolCall.ID,
			Content:    toolResult.Content,
			Metadata:   toolResult.Metadata,
			IsError:    toolResult.IsError,
		}
	}

	return toolResults
}

func (a *agent) RunTool(ctx context.Context, sessionID string, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	var found *toolcore.Tool
	for _, toolsByName := range a.tools {
		tool, ok := toolsByName[call.Name]
		if !ok {
			continue
		}
		if found != nil {
			return toolcore.ToolResponse{}, fmt.Errorf("%w: %s", ErrToolAmbiguous, call.Name)
		}
		found = tool
	}
	if found == nil {
		return toolcore.ToolResponse{}, fmt.Errorf("%w: %s", ErrToolNotFound, call.Name)
	}
	return a.runTool(ctx, sessionID, found, call)
}

func (a *agent) RunToolByKind(ctx context.Context, sessionID string, kind toolcore.ToolKind, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	toolsByName := a.tools[kind]
	tool, ok := toolsByName[call.Name]
	if !ok {
		return toolcore.ToolResponse{}, fmt.Errorf("%w: %s/%s", ErrToolNotFound, kind, call.Name)
	}
	return a.runTool(ctx, sessionID, tool, call)
}

func (a *agent) runTool(ctx context.Context, sessionID string, tool *toolcore.Tool, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	if sessionID != "" {
		ctx = context.WithValue(ctx, toolcore.SessionIDContextKey, sessionID)
	}
	return tool.Handler(ctx, call)
}

func fillCanceledToolResults(toolResults []message.ToolResult, toolCalls []message.ToolCall, start int) {
	for i := start; i < len(toolCalls); i++ {
		toolResults[i] = message.ToolResult{
			ToolCallID: toolCalls[i].ID,
			Content:    "Tool execution canceled by user",
			IsError:    true,
		}
	}
}

func (a *agent) finishMessage(ctx context.Context, msg *message.MessageRecord, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

func (a *agent) processEvent(ctx context.Context, model client.Model, sessionID string, assistantMsg *message.MessageRecord, event client.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case client.EventThinkingDelta:
		assistantMsg.AppendReasoningContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case client.EventContentDelta:
		assistantMsg.AppendContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case client.EventToolUseStart:
		if event.ToolCall == nil {
			return nil
		}
		assistantMsg.AddToolCall(*event.ToolCall)
		return a.messages.Update(ctx, *assistantMsg)
	case client.EventToolUseDelta:
		if event.ToolCall == nil {
			return nil
		}
		tm := time.Unix(assistantMsg.UpdatedAt, 0)
		assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
		if time.Since(tm) > time.Second {
			err := a.messages.Update(ctx, *assistantMsg)
			assistantMsg.UpdatedAt = time.Now().Unix()
			return err
		}
	case client.EventToolUseStop:
		if event.ToolCall == nil {
			return nil
		}
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		return a.messages.Update(ctx, *assistantMsg)
	case client.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		return event.Error
	case client.EventComplete:
		if event.Response == nil {
			return nil
		}
		if len(event.Response.ToolCalls) > 0 {
			assistantMsg.SetToolCalls(event.Response.ToolCalls)
		}
		assistantMsg.AddFinish(event.Response.FinishReason)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		return a.TrackUsage(ctx, sessionID, model, event.Response.Usage)
	}

	return nil
}

func (a *agent) TrackUsage(ctx context.Context, sessionID string, model client.Model, usage client.TokenUsage) error {
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)

	sess.Cost += cost
	sess.CompletionTokens = usage.OutputTokens + usage.CacheReadTokens
	sess.PromptTokens = usage.InputTokens + usage.CacheCreationTokens

	_, err = a.sessions.Save(ctx, sess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (a *agent) Summarize(ctx context.Context, modelProvider provider.ModelProvider, sessionID string) error {
	// Check if session is busy
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		// Get the current summary plus conversation messages that happened after it.
		msgs, err := a.buildModelContext(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, toolcore.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		summarizePrompt := summarizerPrompt
		if strings.TrimSpace(summarizePrompt) == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("summarize prompt is empty"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		// Create a new message with the summarize prompt
		promptMsg := message.MessageRecord{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		response, err := a.sendMessages(summarizeCtx, modelProvider, summarizePrompt, nil, msgsWithPrompt)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Kind: message.MessageKindSummary,
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: string(modelProvider.Model.ID),
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		model := modelProvider.Model
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}
