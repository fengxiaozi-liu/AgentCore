package agent

type AgentEventType string

const (
	AgentEventTypeError     AgentEventType = "error"
	AgentEventTypeResponse  AgentEventType = "response"
	AgentEventTypeSummarize AgentEventType = "summarize"
)

const (
	titlePrompt      = "Generate a concise one-line title for the user's first message. Return only the title text."
	summarizerPrompt = "Summarize the conversation clearly and concisely, preserving decisions, changed files, open issues, and next steps."
)
