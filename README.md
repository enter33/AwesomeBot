# AwesomeBot

基于 Go 和 [Bubble Tea](https://charm.sh/) 的终端 AI 编程助手，支持流式响应、上下文管理、记忆系统、工具调用、MCP 集成和可扩展技能系统。

## 功能特性

- **流式响应**: LLM 流式输出，实时显示推理过程（reasoning content）
- **上下文管理**: 智能上下文策略（摘要、卸载、截断），自动维护上下文长度
- **两级记忆**: Global + Workspace 记忆系统，跨会话和当前目录持久化
- **内置工具**: bash（支持 Docker 沙箱）、read、write、load_skill、load_storage
- **MCP 集成**: 支持 Model Context Protocol，可连接多种 MCP 服务器
- **技能系统**: 可扩展的技能加载机制，通过 SKILL.md 定义复杂技能
- **工具确认**: 危险操作（bash/write）需要用户确认，可选择"始终允许"
- **日志系统**: 完整运行日志，支持日志轮转，调试友好
- **TUI 界面**: 基于 Bubble Tea 的交互式终端界面，支持键盘导航

## 系统要求

- Go 1.25+
- Docker（可选，用于 bash 工具沙箱隔离）
- 终端支持 ANSI 颜色

## 安装

### 方式一：源码编译

```bash
git clone https://github.com/awesome/awesomebot.git
cd awesomebot
go build -o awesome ./cmd/awesome/
```

### 方式二：直接运行

```bash
go run ./cmd/awesome/
```

## 首次使用

首次启动时，如果配置文件不存在或无效，程序会自动引导你创建配置：

```bash
./awesome
```

按提示输入：
- **Base URL**: OpenAI 兼容 API 地址（默认: `https://api.openai.com/v1`）
- **模型名称**: 例如 `gpt-4o-mini`、`claude-3-5-sonnet`、`deepseek-chat`
- **API Key**: 你的 API 密钥

配置会自动保存到 `~/.awesome/config.json`。

## 配置

### 主配置文件

位于 `~/.awesome/config.json`：

```json
{
  "base_url": "https://api.openai.com/v1",
  "model": "gpt-4o-mini",
  "api_key": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
}
```

### MCP 服务器配置

位于 `~/.awesome/mcp.json`，首次运行会自动创建：

```json
{
  "filesystem": {
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/directory"]
  }
}
```

#### MCP 配置字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `command` | string | 启动命令（如 `npx`、`uvx`、`python`） |
| `args` | string[] | 命令参数 |
| `env` | map | 环境变量 |
| `url` | string | HTTP MCP 服务器地址（替代 command/args） |

### 环境变量

程序会加载 `.env` 文件（项目根目录下），可设置环境变量覆盖配置：

```bash
OPENAI_API_KEY=sk-xxx
```

## 使用

### 启动

```bash
./awesome
```

### 交互界面

```
┌─────────────────────────────────────────────────────┐
│ AwesomeBot TUI (Bubble Tea)                        │
├─────────────────────────────────────────────────────┤
│ 模型: gpt-4o-mini | 版本: 1.0.0                     │
│                                                     │
│ [对话历史区域]                                       │
│                                                     │
│ >>> 请输入问题后回车                                │
│ 快捷键: Ctrl+C 退出，Esc 取消当前流式              │
│ 命令: /clear 清空会话                               │
└─────────────────────────────────────────────────────┘
```

### 快捷键

| 快捷键 | 功能 |
|--------|------|
| `Enter` | 发送消息 / 确认工具调用 |
| `↑ / ↓` | 选择工具确认选项 / 滚动历史 |
| `PgUp / PgDn` | 向上/下翻页 |
| `Home / End` | 跳转到顶部/底部 |
| `Esc` | 取消当前流式响应 / 拒绝工具调用 |
| `Ctrl+C` | 退出程序 |
| `/clear` | 清空会话（保留 system prompt） |

### 工具确认

某些危险操作需要用户确认：

- **允许**: 仅本次允许执行
- **拒绝**: 拒绝本次调用
- **始终允许**: 以后自动允许该工具

### 输出类型

TUI 会实时显示以下类型的消息：

| 类型 | 说明 |
|------|------|
| `reasoning` | 推理过程（模型思考） |
| `content` | 最终回答内容 |
| `tool_call` | 工具调用记录 |
| `error` | 错误信息 |
| `policy` | 上下文策略执行状态 |
| `memory` | 记忆更新状态 |
| `token_usage` | Token 用量统计 |

## 工具系统

### 内置工具

| 工具 | 说明 | 需要确认 |
|------|------|----------|
| `read` | 读取文件内容 | 否 |
| `write` | 写入文件内容 | 是 |
| `bash` | 执行 bash 命令 | 是 |
| `load_skill` | 加载技能指令 | 否 |
| `load_storage` | 加载存储内容 | 否 |

### Bash 工具模式

- **Docker 可用**: 使用 Docker 沙箱容器执行命令（隔离环境）
- **Docker 不可用**: 使用常规 bash 执行

## 上下文策略

程序内置三种上下文管理策略，按优先级依次执行：

### 1. Offload 策略

将长工具输出卸载到存储，减少 token 消耗。

- **触发条件**: 单个消息 token 超过上下文窗口的 40%
- **行为**: 将内容存储到文件系统，上下文仅保留引用

### 2. Summary 策略

将旧消息汇总为摘要。

- **触发条件**: 消息数量超过 10 条，且 token 超过上下文窗口的 60%
- **行为**: 使用 LLM 生成摘要，替换原始消息

### 3. Truncate 策略

直接截断过时的对话历史。

- **触发条件**: token 超过上下文窗口的 85%
- **行为**: 保留最近的消息，丢弃旧消息

## 记忆系统

采用两级记忆架构：

### Global Memory

- 位置: `~/.awesome/memory/MEMORY.md`
- 内容: 跨所有会话的持久化记忆
- 用途: 用户偏好、跨项目知识

### Workspace Memory

- 位置: `{工作目录}/.memory/MEMORY.md`
- 内容: 当前工作空间的记忆
- 用途: 项目特定信息、当前目录上下文

### 记忆更新机制

每次对话轮次结束后，LLM 会分析新消息并更新记忆：
- 提取用户偏好和关键信息
- 更新 global 和 workspace 记忆
- 持久化到文件系统

## 技能系统

技能允许你定义复杂的、可复用的指令模板。

### 技能目录结构

```
~/.awesome/skills/<skill-id>/
├── SKILL.md          # 技能定义（必需）
├── scripts/          # 脚本目录（可选）
│   └── ...
└── references/       # 参考文档目录（可选）
    └── ...
```

### SKILL.md 格式

```markdown
---
name: code-review
description: 执行代码审查，检查潜在问题和改进建议
---

## 代码审查技能

你是一个专业的代码审查助手。请：

1. 检查代码质量和风格
2. 识别潜在的 bug 和安全问题
3. 提出改进建议
4. 评估测试覆盖率

执行审查时，请使用以下工具：
- read: 读取源文件
- bash: 运行测试和 linter
```

### 加载技能

在对话中，Agent 会根据用户请求自动判断是否需要加载技能。技能加载后会提供完整的指令和上下文。

### 技能元数据

技能通过 YAML front matter 定义元数据：

| 字段 | 说明 |
|------|------|
| `name` | 技能显示名称 |
| `description` | 技能描述，用于 Agent 判断何时加载 |

## 日志系统

### 日志位置

- 主日志: `~/.awesome/logs/awesomebot.log`
- 轮转日志: `~/.awesome/logs/awesomebot.log.1`, `.log.2`, ...

### 日志级别

| 级别 | 说明 |
|------|------|
| DEBUG | 详细调试信息（消息内容、LLM 调用参数） |
| INFO | 一般信息（启动、工具调用） |
| WARN | 警告信息 |
| ERROR | 错误信息 |

### 日志内容

程序会记录：
- 用户输入
- 发送给 LLM 的完整消息历史
- LLM 返回内容
- 工具调用和执行结果
- Token 用量统计
- 策略执行过程

## 目录结构

| 路径 | 说明 |
|------|------|
| `~/.awesome/config.json` | LLM 配置文件 |
| `~/.awesome/mcp.json` | MCP 服务器配置 |
| `~/.awesome/memory/MEMORY.md` | 全局记忆 |
| `~/.awesome/logs/` | 日志文件目录 |
| `~/.awesome/skills/` | 技能目录 |
| `{工作目录}/.memory/MEMORY.md` | 工作空间记忆 |
| `{工作目录}/.awesome/skills/` | 本地技能目录 |

## 项目结构

```
awesomebot/
├── cmd/awesome/main.go          # 程序入口
├── internal/
│   ├── agent/                   # Agent 核心逻辑
│   │   ├── agent.go             # Agent 实现
│   │   └── types.go             # 类型定义
│   ├── context/                 # 上下文管理
│   │   ├── engine.go            # 上下文引擎
│   │   ├── policy.go            # 策略接口
│   │   ├── policy_offload.go    # Offload 策略
│   │   ├── policy_summary.go    # Summary 策略
│   │   └── policy_truncate.go   # Truncate 策略
│   ├── memory/                  # 记忆系统
│   │   ├── memory.go           # 记忆接口和实现
│   │   └── update.go           # 记忆更新逻辑
│   ├── mcp/                     # MCP 客户端
│   │   └── client.go           # MCP 客户端实现
│   ├── skill/                   # 技能系统
│   │   ├── skill.go            # 技能数据结构
│   │   └── load.go             # 技能加载逻辑
│   ├── storage/                 # 存储抽象
│   │   └── filesystem.go       # 文件系统存储
│   ├── tool/                    # 工具实现
│   │   ├── bash.go             # Bash 工具
│   │   ├── docker_bash.go      # Docker 沙箱 bash
│   │   ├── read.go             # Read 工具
│   │   ├── write.go            # Write 工具
│   │   ├── factory.go          # 工具工厂
│   │   └── ...
│   ├── tui/                     # TUI 界面
│   │   ├── tui.go              # TUI 主逻辑
│   │   └── entry.go            # 日志条目渲染
│   └── logging/                 # 日志系统
│       └── logger.go           # 日志实现
├── pkg/
│   ├── config/                   # 配置管理
│   │   ├── config.go           # 主配置
│   │   └── loader.go           # MCP 配置加载
│   ├── llm/                     # LLM 客户端
│   │   ├── client.go          # 客户端接口
│   │   └── openai.go          # OpenAI 兼容客户端
│   └── prompt/                  # 系统提示词
│       └── prompt.go          # 提示词模板
├── go.mod                       # Go 模块定义
└── README.md                    # 本文档
```

## 依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| charm.land/bubbletea/v2 | 2.0.0 | TUI 框架 |
| charm.land/lipgloss/v2 | 2.0.0 | 终端样式 |
| github.com/openai/openai-go/v3 | 3.24.0 | OpenAI API 客户端 |
| github.com/modelcontextprotocol/go-sdk | 1.4.0 | MCP 协议支持 |
| github.com/tiktoken-go/tokenizer | 0.7.0 | Token 计数 |
| gopkg.in/yaml.v3 | 3.0.1 | YAML 解析 |

## 常见问题

### Q: Docker 不可用怎么办？

不影响基本使用，程序会自动切换到常规 bash 工具。Docker 仅用于隔离危险操作。

### Q: 如何查看详细日志？

程序日志默认写入 `~/.awesome/logs/awesomebot.log`。DEBUG 日志包含完整的消息历史和 LLM 调用参数。

### Q: 记忆系统如何工作？

每次对话轮次结束后，程序会用 LLM 分析新消息，提取有价值的信息存入两级记忆（global + workspace）。记忆会持久化到文件系统。

### Q: 如何扩展工具？

1. 实现 `tool.Tool` 接口
2. 在 `factory.go` 中注册工具
3. 重新编译

### Q: 支持哪些 MCP 服务器？

理论上支持所有实现 MCP 协议的服务器。常用示例：
- `@modelcontextprotocol/server-filesystem` - 文件系统访问
- `@modelcontextprotocol/server-memory` - 内存服务器
- `@modelcontextprotocol/server-github` - GitHub API

## 版权

MIT License
