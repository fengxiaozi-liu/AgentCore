# AgentCore

AgentCore 是一个可嵌入宿主应用的 Go Agent SDK。它提供 Agent 运行编排、LLM Provider 适配、会话与消息持久化、工具协议、工作区工具、MCP 工具、Skill 工具、SubAgent 工具、权限控制、文件版本记录、日志以及 diff/patch 基础能力。

本仓库定位为 SDK 内核，不包含 CLI/TUI、IDE 插件、LSP 扩展或完整产品 UI。宿主应用负责配置加载、用户交互、权限审批界面、模型选择和运行入口。

## 核心能力

- Agent 编排：接收用户输入，构造上下文，流式调用模型，处理工具调用循环，并发布运行事件。
- 多模型接入：通过统一 `llm/client.Client` 接口适配 OpenAI、Anthropic、Gemini、Azure、Bedrock、Vertex AI、Copilot、OpenRouter、GROQ、XAI、Local 和 Mock Provider。
- 会话记忆：基于 GORM 持久化 session、message 和 summary，支持会话标题、摘要压缩、token 与成本记录。
- 工具运行时：提供工具 schema、工具调用、响应结构、工具分类、middleware 链和 ToolMap。
- 工作区能力：内置 `bash`、`edit`、`fetch`、`glob`、`grep`、`ls`、`patch`、`sourcegraph`、`view`、`write` 等工具。
- 权限控制：对敏感工具动作生成权限请求，支持一次性授权、持久授权、拒绝和 session 自动批准。
- 文件版本：记录 Agent 写入、编辑、patch 前后的文件内容快照，便于宿主做审计或回滚。
- MCP 扩展：可连接 stdio 或 SSE MCP Server，并把远端工具包装为统一 Agent 工具。
- 复合能力：支持 Skill 内容加载工具和 SubAgent 任务工具。

## 项目结构

```text
pkg/
  agent/                    Agent 服务、运行状态、事件与配置选项
  capability/
    workspace/              工作区边界与文件、shell、搜索、patch 等工具
    mcp/                    MCP Server 连接、工具发现与调用适配
    skill/                  Skill 内容加载工具
    subagent/               子 Agent 工具
  data/
    db/                     SQLite/MySQL GORM 客户端
    logging/                日志、事件订阅和调试文件输出
  llm/
    client/                 统一 LLM 请求、响应、事件和 Provider 枚举
    provider/               Provider 注册表与 client 缓存
  memory/
    session/                会话服务
    message/                消息服务与多类型内容片段
  permission/               工具权限请求服务
  pubsub/                   泛型发布订阅 Broker
  tools/                    工具协议、ToolMap、middleware
  utils/                    shell、fileutil、diff/patch 等工具函数
  version/                  文件版本服务
docs/
  architecture.md           更完整的架构说明
```

## 快速开始

项目使用 Go `1.24.2`。

```bash
go mod download
go test ./...
```

最小装配示例：

```go
package main

import (
	"context"
	"log"

	"ferryman-agent/pkg/agent"
	workspace "ferryman-agent/pkg/capability/workspace"
	workspacetools "ferryman-agent/pkg/capability/workspace/tools"
	datadb "ferryman-agent/pkg/data/db"
	llmclient "ferryman-agent/pkg/llm/client"
	"ferryman-agent/pkg/llm/provider"
	"ferryman-agent/pkg/memory/message"
	"ferryman-agent/pkg/memory/session"
	"ferryman-agent/pkg/version"
)

func main() {
	ctx := context.Background()

	db, err := datadb.NewClient(datadb.WithSQLite(".ferryer/agent.db"))
	if err != nil {
		log.Fatal(err)
	}

	sessions := session.NewSessionService(session.WithDbClient(db))
	messages := message.NewService(message.WithDbClient(db))
	files := version.NewFileVersionService(version.WithDbClient(db))

	viewTool, err := workspacetools.NewViewTool(workspace.WithRootDir("."), workspace.WithFileVersionService(files))
	if err != nil {
		log.Fatal(err)
	}
	grepTool, err := workspacetools.NewGrepTool(workspace.WithRootDir("."))
	if err != nil {
		log.Fatal(err)
	}

	agentSvc, err := agent.NewAgent(
		agent.WithMemory(sessions, messages),
		agent.WithAgentDescription("你是一个严谨的代码助手。"),
		agent.WithTools(viewTool, grepTool),
	)
	if err != nil {
		log.Fatal(err)
	}

	sess, err := sessions.Create(ctx, "新会话")
	if err != nil {
		log.Fatal(err)
	}

	modelProvider := provider.ModelProvider{
		Provider: llmclient.ProviderOpenAI,
		APIKey:   "YOUR_API_KEY",
		Model: llmclient.Model{
			ID:        "gpt-4.1",
			APIModel:  "gpt-4.1",
			MaxTokens: 8192,
		},
	}

	events, err := agentSvc.Run(ctx, modelProvider, sess.ID, "阅读这个项目并说明核心模块")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		if event.Error != nil {
			log.Fatal(event.Error)
		}
		if event.Done {
			log.Println(event.Message.Content().Text)
		}
	}
}
```

## Agent 运行流程

1. 宿主创建数据库、session/message 服务、Provider 注册表和工具集合。
2. `agent.NewAgent` 构建 Agent 服务，并将工具整理成 `ToolMap`。
3. 宿主创建 session 后调用 `Run`。
4. Agent 写入 user message，并读取历史消息或 summary 构造模型上下文。
5. Provider 注册表根据 `ModelProvider` 创建或复用具体 LLM client。
6. Agent 消费模型流式事件，持续更新 assistant message。
7. 如果模型请求工具，Agent 执行工具并写入 tool message，然后继续下一轮模型请求。
8. 如果模型结束或发生错误，Agent 发布 `AgentEvent`，并更新 token、成本和 finish reason。

同一个 session 同一时间只能有一个活跃请求；重复调用会返回 `ErrSessionBusy`。可以通过 `Cancel(sessionID)` 取消正在运行的请求或摘要任务。

## 模型配置

运行时通过 `provider.ModelProvider` 指定 Provider、API Key、Base URL 和模型元数据：

```go
modelProvider := provider.ModelProvider{
	Provider: llmclient.ProviderOpenAI,
	APIKey:   "YOUR_API_KEY",
	BaseURL:  "",
	Model: llmclient.Model{
		ID:                  "gpt-4.1",
		Name:                "GPT-4.1",
		APIModel:            "gpt-4.1",
		MaxTokens:           8192,
		ContextWindow:       128000,
		SupportsAttachments: true,
		ReasoningEffort:     "medium",
	},
}
```

Provider client 会按 `provider + apiKey + baseURL` 缓存，不按 model 缓存；同一凭据下切换不同模型会复用底层 client。

## 数据库

`pkg/data/db` 默认使用 SQLite，默认路径为 `.ferryer/agent.db`。也可以切换到 MySQL：

```go
db, err := datadb.NewClient(
	datadb.WithMySQL("user:password@tcp(127.0.0.1:3306)/agent?charset=utf8mb4&parseTime=true&loc=Local"),
)
```

`session`、`message` 和 `version` 服务在构造时会执行对应模型的 auto migrate。

## 工具与权限

工具由 `pkg/tools` 定义统一协议：

- `ToolInfo` 描述模型可见的工具名称、描述和 JSON 参数 schema。
- `ToolCall` 表示模型发起的工具调用。
- `ToolResponse` 表示工具返回内容、metadata、extra 和错误状态。
- `ToolMiddleware` 可用于注入权限、审计、限流等横切逻辑。

工作区工具会通过 `workspace.Workspace.Resolve` 将路径限制在 root 内。写入类工具还可以结合 `version.FileVersionService` 记录文件变更。

权限服务位于 `pkg/permission`。宿主可以订阅权限事件，在 UI 或策略层决定调用：

- `Grant`：批准一次。
- `GrantPersistant`：批准并保存为同 session/tool/action/resource 可复用授权。
- `Deny`：拒绝。
- `AutoApproveSession`：自动批准某个 session 后续权限请求。

## MCP、Skill 与 SubAgent

- MCP：`capability/mcp.NewMcpTool` 连接 MCP Server，发现远端工具并包装为 Agent 工具。
- Skill：`capability/skill.NewSkillTool` 将宿主提供的内容加载函数包装为工具。
- SubAgent：`capability/subagent.NewAgentTool` 创建子任务工具，可在工具调用中启动新的 Agent session。

这些能力都最终暴露为 `*tools.Tool`，因此可以和普通工具一起传入 `agent.WithTools(...)`。

## 文档

更详细的组件关系、状态机、接口契约和设计规则见：

- [docs/architecture.md](docs/architecture.md)

## 当前边界

- 当前仓库不提供独立可执行入口。
- 当前仓库不包含 CLI/TUI、IDE/LSP、宿主配置中心或完整 Skill 文件发现系统。
- `AgentConfig.AutoCompact` 已定义，但当前运行流程未自动触发摘要压缩；宿主可显式调用 `Summarize`。
- 权限请求会阻塞等待宿主 grant/deny；生产宿主应提供明确的审批、超时或取消策略。
