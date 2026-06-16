package tools

type (
	sessionIDContextKey string
	messageIDContextKey string
)

type toolResponseType string

const (
	ToolResponseTypeText  toolResponseType = "text"
	ToolResponseTypeImage toolResponseType = "image"

	SessionIDContextKey sessionIDContextKey = "session_id"
	MessageIDContextKey messageIDContextKey = "message_id"
)

type ToolKind string

const (
	ToolKindMCP      ToolKind = "mcp"
	ToolKindSkill    ToolKind = "skill"
	ToolKindSubAgent ToolKind = "sub_agent"
	ToolKindOther    ToolKind = "other"
)
