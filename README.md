# Agent SDK

Reusable Go Agent SDK for agent orchestration, LLM provider integration, session/message/history services, tool execution, MCP tools, permissions, file hooks, and diff/patch core.

The SDK intentionally does not include Skill tools, CLI/TUI UI, terminal themes, or IDE/LSP extensions. Those belong in host applications.

## Packages

- `pkg/agent`: runtime orchestration and agent events
- `pkg/config`: host-provided SDK configuration structs, defaults, validation, and injection
- `pkg/data/db`, `pkg/data/repo`: gorm-backed database connection/models and repository contracts
- `pkg/memory/session`, `pkg/memory/message`, `pkg/memory/history`: domain services over repos
- `pkg/llm/client`: model/provider metadata, client protocol, and vendor SDK adapters
- `pkg/llm/provider`: application-level ProviderService routing from model IDs to provider clients
- `pkg/prompt`: JSON/YAML system prompt resolver
- `pkg/tools`: tool protocol, file hook events, and hook result merging
- `pkg/capability/workspace`: SDK-safe workspace tools
- `pkg/capability/mcp`: MCP tool discovery and execution
- `pkg/utils/diff`: diff/patch core only
- `pkg/utils/fileutil`, `pkg/data/logging`: shared support utilities

## Prompts

Default prompts live in `pkg/prompt/prompts.json`. A host can provide a JSON or YAML prompt file and set `pkg/config.PromptConfigPath`; prompts are then resolved by key, for example `coder`, `title`, `task`, or `summarizer`.

## Model Config

Provider configuration is provider-scoped and each provider declares a `models` list. `ProviderConfig` and `ModelConfig` are defined in `pkg/agent/config.go`; database configuration is defined in `pkg/data/db`.

```json
{
  "providers": [
    {
      "provider": "openai",
      "apiKey": "...",
      "baseURL": "",
      "models": [
        {
          "model_id": "gpt-4.1",
          "api_model": "gpt-4.1",
          "maxTokens": 8192,
          "reasoning_effort": "medium",
          "weight": 1,
          "priority": 0
        }
      ]
    }
  ],
  "agent": {
    "model_id": "gpt-4.1",
    "provider": "openai"
  },
  "titleAgent": {
    "model_id": "gpt-4.1",
    "provider": "openai"
  },
  "summarizeAgent": {
    "model_id": "gpt-4.1",
    "provider": "openai"
  }
}
```

The old single `modelConfig` field is intentionally not compatible; migrate it to a one-item `models` list. Model metadata is represented by `client.Model`, and `max_tokens` is exposed at runtime as `client.Model.MaxTokens`.
