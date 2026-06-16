package skill

import (
	"context"
	"fmt"
	"sort"
	"strings"

	toolcore "ferryman-agent/pkg/tools"
)

type SkillTool struct {
	Name        string
	Description string
	LoadContent func(ctx context.Context) (string, error)
	Metadata    map[string]string
	BaseUIR     string
}

func NewSkillTool(opts ...SkillOption) (*toolcore.Tool, error) {
	options := skillOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	skill := &SkillTool{
		Name:        options.Name,
		Description: options.Description,
		LoadContent: options.LoadContent,
		Metadata:    options.Metadata,
		BaseUIR:     options.BaseUIR,
	}
	if skill.LoadContent == nil {
		return nil, errSkillContentLoaderRequired(skill.Name)
	}
	return toolcore.NewTool(skill.Info(), skill.Run, toolcore.WithKind(toolcore.ToolKindSkill), toolcore.WithMiddleware(options.Middlewares...)), nil
}

func (s *SkillTool) Info() *toolcore.ToolInfo {
	return &toolcore.ToolInfo{
		Name:        s.Name,
		Description: skillDescription(s.Description, s.Metadata),
		Parameters:  map[string]any{},
		Required:    []string{},
	}
}

func (s *SkillTool) Run(ctx context.Context, call toolcore.ToolCall) (toolcore.ToolResponse, error) {
	if s.LoadContent == nil {
		return toolcore.ToolResponse{}, errSkillContentLoaderRequired(s.Name)
	}
	content, err := s.LoadContent(ctx)
	if err != nil {
		return toolcore.ToolResponse{}, err
	}
	if strings.TrimSpace(s.BaseUIR) != "" {
		content = fmt.Sprintf(rootPathContentFormat, s.BaseUIR, content)
	}
	return toolcore.NewTextResponse(content), nil
}

func skillDescription(description string, metadata map[string]string) string {
	if len(metadata) == 0 {
		return description
	}

	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString(description)
	builder.WriteString("\n\n")
	builder.WriteString(metadataOpenTag)
	for _, key := range keys {
		builder.WriteString("\n")
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(metadata[key])
	}
	builder.WriteString("\n")
	builder.WriteString(metadataCloseTag)
	return builder.String()
}
