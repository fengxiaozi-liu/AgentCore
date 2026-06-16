package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
}

type ToolHandler func(ctx context.Context, call ToolCall) (ToolResponse, error)

type ToolMiddleware func(next ToolHandler) ToolHandler

type Tool struct {
	Info    *ToolInfo
	Kind    ToolKind
	Handler ToolHandler
}

type ToolMap map[ToolKind]map[string]*Tool

type ToolResponse struct {
	Type     toolResponseType `json:"type"`
	Content  string           `json:"content"`
	Metadata string           `json:"metadata,omitempty"`
	Extra    map[string]any   `json:"extra,omitempty"`
	IsError  bool             `json:"is_error"`
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

func NewTool(info *ToolInfo, handler ToolHandler, opts ...ToolOption) *Tool {
	options := toolOptions{
		kind: ToolKindOther,
	}
	for _, opt := range opts {
		opt(&options)
	}

	return &Tool{
		Info:    info,
		Kind:    options.kind,
		Handler: Chain(options.middlewares...)(handler),
	}
}

func Chain(middlewares ...ToolMiddleware) ToolMiddleware {
	return func(handler ToolHandler) ToolHandler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			handler = middlewares[i](handler)
		}
		return handler
	}
}

func NewTextResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
	}
}

func NewTextErrorResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
		IsError: true,
	}
}

func WithResponseMetadata(response ToolResponse, metadata any) ToolResponse {
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return response
		}
		response.Metadata = string(metadataBytes)
	}
	return response
}

func WithResponseExtra(response ToolResponse, key string, value any) ToolResponse {
	if response.Extra == nil {
		response.Extra = make(map[string]any)
	}
	response.Extra[key] = value
	return response
}

func BuildToolMap(tools []*Tool) (ToolMap, error) {
	mapped := make(ToolMap)
	for _, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("tool is required")
		}
		if tool.Info == nil {
			return nil, fmt.Errorf("tool info is required")
		}
		kind := tool.Kind
		name := tool.Info.Name
		if name == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		if tool.Handler == nil {
			return nil, fmt.Errorf("tool %q handler is required", name)
		}
		if mapped[kind] == nil {
			mapped[kind] = make(map[string]*Tool)
		}
		if _, exists := mapped[kind][name]; exists {
			return nil, fmt.Errorf("duplicate tool name %q in kind %q", name, kind)
		}
		mapped[kind][name] = tool
	}
	return mapped, nil
}

func ToolInfos(toolMap ToolMap) []*ToolInfo {
	kinds := make([]string, 0, len(toolMap))
	for kind := range toolMap {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)

	infos := make([]*ToolInfo, 0)
	for _, kindValue := range kinds {
		toolsByName := toolMap[ToolKind(kindValue)]
		names := make([]string, 0, len(toolsByName))
		for name := range toolsByName {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			infos = append(infos, toolsByName[name].Info)
		}
	}
	return infos
}

func GetContextValues(ctx context.Context) (string, string) {
	sessionID := ctx.Value(SessionIDContextKey)
	messageID := ctx.Value(MessageIDContextKey)
	if sessionID == nil {
		return "", ""
	}
	if messageID == nil {
		return sessionID.(string), ""
	}
	return sessionID.(string), messageID.(string)
}
