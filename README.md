# AwesomeBot

基于 Go 和 [Bubble Tea](https://charm.sh/) 的终端 AI 编程助手，支持流式响应、上下文管理、记忆系统、工具调用、MCP 集成和可扩展技能系统。

## 功能特性

- **流式响应**: LLM 流式输出，实时显示推理过程（reasoning content）
- **上下文管理**: 智能上下文策略（摘要、卸载、截断），自动维护上下文长度
- **两级记忆**: Global + Workspace 记忆系统，跨会话和当前目录持久化
- **内置工具**: bash（支持 Docker 沙箱）、read（分页/行号）、write、edit（智能匹配）、list（递归列表）、web_search、web_fetch、load_skill、load_storage、spawn（创建子代理）、get_subagent_result、send_message
- **子代理系统**: 支持创建后台运行的子代理，并行处理复杂任务
- **Web 工具**: web_search（网络搜索）、web_fetch（网页内容提取），支持多种 provider
- **MCP 集成**: 支持 Model Context Protocol，可连接多种 MCP 服务器，Schema 延迟加载减少初始 token 消耗
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
git clone https://github.com/enter33/AwesomeBot.git
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
  "api_key": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "timeout": 120
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `base_url` | string | 必需 | OpenAI 兼容 API 地址 |
| `model` | string | 必需 | 模型名称 |
| `api_key` | string | 必需 | API 密钥 |
| `timeout` | int | 120 | LLM 请求超时时间（秒） |

#### LLM 超时与重试

- **超时**: LLM 请求默认 120 秒超时，可在配置中修改 `timeout` 字段
- **重试**: 请求失败时自动重试，最多 3 次
- **退避策略**: 重试间隔采用指数退避（1s → 2s → 4s），并添加随机抖动避免多客户端同时重试

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

#### MCP Schema 延迟加载

MCP 工具采用延迟加载策略以减少初始 token 消耗：

- **初始**: 所有 MCP 工具只发送名称和描述（不含 schema）
- **按需获取**: LLM 需要使用某个 MCP 工具时，先调用 `get_mcp_tool_schema` 获取完整参数 schema
- **工具命名**: MCP 工具名称格式为 `awesomebot_mcp__<server>__<tool>`

示例流程：
1. LLM 调用 `get_mcp_tool_schema(server="filesystem", tool="read_file")` 获取参数 schema
2. LLM 根据 schema 组织参数，调用 `awesomebot_mcp__filesystem__read_file`

### Web 搜索配置

位于 `~/.awesome/web_search.json`：

```json
{
  "provider": "jina",
  "api_key": "your_jina_api_key",
  "max_results": 5
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `provider` | string | 无 | 搜索 provider：jina、duckduckgo、tavily、searxng、brave |
| `api_key` | string | 环境变量 | API Key |
| `base_url` | string | 无 | SearXNG 自托管地址 |
| `max_results` | int | 5 | 最大结果数（1-10） |

**支持的 provider：**

| Provider | API Key | 说明 |
|----------|---------|------|
| `jina` | 需要 | Jina AI 搜索，可从 https://jina.ai/ 获取 |
| `duckduckgo` | 不需要 | 免费搜索，**国内可能无法访问** |
| `tavily` | 需要 | Tavily 搜索 |
| `searxng` | 不需要 | 自建 SearXNG 服务，需配置 base_url |
| `brave` | 需要 | Brave 搜索 |

### Web 抓取配置

位于 `~/.awesome/web_fetch.json`：

```json
{
  "max_chars": 50000,
  "jina_api_key": "",
  "proxy": ""
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `max_chars` | int | 50000 | 最大字符数 |
| `jina_api_key` | string | 环境变量 | Jina Reader API Key（可选） |
| `proxy` | string | 无 | 代理地址 |

### 环境变量

程序会加载 `.env` 文件（项目根目录下），可设置环境变量覆盖配置：

```bash
OPENAI_API_KEY=sk-xxx
```

### 全局配置文件

位于 `~/.awesome/awesome.json`：

```json
{
  "use_memory": true,
  "memory_update_threshold": 5
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `use_memory` | bool | `true` | 是否启用记忆更新功能。关闭后不会调用 LLM 更新记忆，但已有的记忆内容仍会加载到上下文中 |
| `memory_update_threshold` | int | `5` | 记忆更新节流阈值。每 N 轮对话才执行一次记忆更新，减少 LLM 调用次数 |

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
| `Ctrl+S` | 显示/隐藏子代理面板 |

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
| `read` | 读取文件内容（分页/行号/图片检测） | 否 |
| `write` | 写入文件内容 | 是 |
| `edit` | 编辑文件（智能匹配/CRLF处理） | 是 |
| `list` | 列出目录（递归/忽略噪音目录） | 否 |
| `glob` | 模式匹配文件搜索 | 否 |
| `grep` | 正则搜索文件内容 | 否 |
| `todo` | 复杂多步骤任务 | 否 |
| `bash` | 执行 bash 命令 | 是 |
| `web_search` | 网络搜索 | 否 |
| `web_fetch` | 抓取网页内容 | 否 |
| `load_skill` | 加载技能指令 | 否 |
| `load_storage` | 加载存储内容 | 否 |
| `spawn` | 创建子代理（后台运行） | 否 |
| `get_subagent_result` | 获取子代理执行结果 | 否 |
| `send_message` | 向子代理发送消息 | 否 |

### Bash 工具模式

- **Docker 可用**: 使用 Docker 沙箱容器执行命令（隔离环境）
- **Docker 不可用**: 使用常规 bash 执行

### 文件系统工具

#### read

读取文件内容，支持分页和行号显示：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `path` | string | 必需 | 文件路径 |
| `offset` | int | 1 | 起始行号（1-indexed） |
| `limit` | int | 2000 | 最大行数 |

- 自动检测图片文件（MIME 类型）
- 超长内容自动截断（MAX_CHARS = 128000）
- 分页提示：`"(Showing lines X-Y of Z. Use offset=Y+1 to continue.)"`

#### write

写入内容到文件：

| 参数 | 类型 | 说明 |
|------|------|------|
| `path` | string | 文件路径 |
| `content` | string | 内容 |

- 自动创建父目录
- 支持 `~` 路径扩展

#### edit

编辑文件内容：

| 参数 | 类型 | 说明 |
|------|------|------|
| `path` | string | 文件路径 |
| `old_text` | string | 要替换的文本 |
| `new_text` | string | 替换后的文本 |
| `replace_all` | bool | 替换所有匹配（默认 false） |

- 精确匹配优先
- 失败时使用 trimmed sliding window 匹配（忽略首尾空白）
- CRLF 自动转换
- 相似度 >50% 时显示 unified diff 提示

#### list

列出目录内容：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `path` | string | 必需 | 目录路径 |
| `recursive` | bool | false | 递归列出 |
| `max_entries` | int | 200 | 最大条目数 |

- 自动忽略噪音目录：`.git`, `node_modules`, `__pycache__`, `.venv`, `venv`, `dist`, `build` 等
- 非递归：📁 `dirname/` / 📄 `filename`
- 递归：路径格式，目录带 `/` 后缀

## 上下文策略

程序内置三种上下文管理策略，按优先级依次执行：

### 1. Offload 策略

将长工具输出卸载到存储，减少 token 消耗。

- **触发条件**: 单个消息 token 超过上下文窗口的 40%
- **行为**: 将内容存储到文件系统，上下文仅保留引用
- **内存管理**: 被卸载的消息会记录其 storage key

### 2. Summary 策略

将旧消息汇总为摘要。

- **触发条件**: 消息数量超过 10 条，且 token 超过上下文窗口的 60%
- **行为**: 使用 LLM 生成摘要，替换原始消息
- **内存管理**: 被摘要的消息中如有 offload 内容，会自动释放

### 3. Truncate 策略

直接截断过时的对话历史。

- **触发条件**: token 超过上下文窗口的 85%
- **行为**: 保留最近的消息，丢弃旧消息
- **内存管理**: 被截断的消息中如有 offload 内容，会自动释放

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

采用节流策略，每 N 轮（可配置，默认 5 轮）对话才执行一次记忆更新：

- **消息累积**：达到阈值前，每轮新消息会累积到队列中，更新时传递完整的累积消息
- **节流减少开销**：避免每次对话都调用 LLM 更新记忆
- **完整上下文**：更新记忆时使用完整的对话历史（在上下文压缩之前）
- **两级记忆**：同时更新 global 和 workspace 记忆
- **持久化存储**：更新后的记忆自动持久化到文件系统

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

## 子代理系统

子代理允许创建独立的后台任务执行单元，实现并行处理复杂任务。

### 子代理类型

| 类型 | 说明 |
|------|------|
| `general-purpose` | 通用子代理，适合大多数任务 |
| `explore` | 探索型子代理，适合代码搜索和探索 |
| `plan` | 规划型子代理，适合任务分解和规划 |

### 子代理生命周期

1. **创建**: 使用 `spawn` 工具创建子代理，指定类型和任务
2. **运行**: 子代理在后台独立运行，不阻塞主对话
3. **查询**: 使用 `get_subagent_result` 获取执行结果
4. **通信**: 使用 `send_message` 向运行中的子代理发送消息

### 子代理状态

| 状态 | 说明 |
|------|------|
| `created` | 已创建，等待启动 |
| `running` | 正在运行 |
| `completed` | 已完成 |
| `failed` | 执行失败 |
| `stopped` | 已停止 |

### 子代理错误处理与状态通知

子代理系统内置完整的错误处理和状态通知机制：

**状态回调机制：**
- Instance 完成后自动触发回调，通知状态变化
- Manager 负责转发回调给所有注册的处理者
- TUI 通过监听 completionCh 获取完成通知

**错误传播：**
- subagent 执行失败时，错误信息会通过回调机制传播
- `get_subagent_result` 工具返回详细错误信息：`{"status": "failed", "error": "子代理执行失败"}`
- 创建失败时，spawn 工具会返回具体错误原因

**viewCh 阻塞保护：**
- viewCh 发送采用 non-blocking 模式（buffer=10）
- 当 viewCh 满时，消息会被丢弃并记录警告日志
- 防止 subagent 因 TUI 处理不及时而阻塞

**回调链路：**
```
Instance.Run() 完成
    └─> Instance.notifyCompletion()
            ├─> 发送 completionCh (non-blocking)
            └─> 触发 statusCallbacks
                    └─> Manager 转发
                            ├─> 发送 Manager.completionCh
                            └─> 触发 Manager.statusCallbacks
```

### TUI 子代理面板

按 `Ctrl+S` 可显示/隐藏子代理面板，实时查看所有子代理的状态：

```
--- Subagents ---
● [explore] search-agent (running)
✓ [general-purpose] file-reader (completed)
✗ [plan] task-planner (failed)
```

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
| `~/.awesome/awesome.json` | 全局配置（记忆功能开关等） |
| `~/.awesome/mcp.json` | MCP 服务器配置 |
| `~/.awesome/web_search.json` | Web 搜索配置 |
| `~/.awesome/web_fetch.json` | Web 抓取配置 |
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
│   ├── security/               # 安全模块
│   │   └── network.go          # SSRF 保护
│   ├── mcp/                     # MCP 客户端
│   │   ├── client.go           # MCP 客户端实现
│   │   └── schema_tool.go      # MCP Schema 延迟加载工具
│   ├── skill/                   # 技能系统
│   │   ├── skill.go            # 技能数据结构
│   │   └── load.go             # 技能加载逻辑
│   ├── subagent/                # 子代理系统
│   │   ├── subagent.go         # 子代理接口定义
│   │   ├── manager.go          # 子代理管理器
│   │   └── instance.go         # 子代理实例实现
│   ├── msgs/                    # 消息类型定义
│   │   └── types.go            # 消息 VO 类型
│   ├── storage/                 # 存储抽象
│   │   └── filesystem.go       # 文件系统存储
│   ├── tool/                    # 工具实现
│   │   ├── bash.go             # Bash 工具
│   │   ├── docker_bash.go      # Docker 沙箱 bash
│   │   ├── read.go             # Read 工具（分页/行号/图片检测）
│   │   ├── write.go            # Write 工具
│   │   ├── edit.go             # Edit 工具（智能匹配）
│   │   ├── list.go             # List 工具（递归列表）
│   │   ├── path.go             # 路径解析器
│   │   ├── web_search.go       # Web 搜索工具
│   │   ├── web_fetch.go        # Web 抓取工具
│   │   ├── web_helpers.go      # Web 工具辅助函数
│   │   ├── factory.go          # 工具工厂
│   │   ├── spawn.go            # 子代理创建工具
│   │   ├── get_result.go       # 获取子代理结果工具
│   │   └── send_message.go     # 发送消息到子代理工具
│   ├── tui/                     # TUI 界面
│   │   ├── tui.go              # TUI 主逻辑
│   │   ├── entry.go            # 日志条目渲染
│   │   └── subagent_panel.go   # 子代理面板渲染
│   └── logging/                 # 日志系统
│       └── logger.go           # 日志实现
├── pkg/
│   ├── config/                   # 配置管理
│   │   ├── config.go           # 主配置
│   │   ├── loader.go           # MCP 配置加载
│   │   └── web_config.go       # Web 工具配置
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
| github.com/PuerkitoBio/goquery | 1.10.1 | HTML DOM 解析 |
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

## 更新日志

### v1.3.3

**新功能：**

- **重构头部渲染，新增工作目录显示**：TUI 头部现在显示当前工作目录，提升使用体验

### v1.3.2

**Bug 修复：**

- **修复 Context 策略 bug**：修复 Offload/Summary/Truncate 策略执行后消息状态不一致的问题
- **修复 subagent tool not found 问题**：解决子代理完成后无法获取结果的问题
- **增强 MCP 连接管理**：改善 MCP 服务器连接生命周期管理

**新功能：**

- **子代理输出可折叠展示**：长输出自动折叠，按 Ctrl+O 展开/收起
- **真正的行内光标编辑功能**：支持在 TUI 中直接编辑输入内容

**改进：**

- 子代理折叠快捷键从 Enter 改为 Ctrl+O，避免误触
- 移除 token speed 显示，简化界面

### v1.3.1

**Bug 修复：**

- **修复 subagent 完成后主 agent context 被错误取消的问题**：删除了 TUI 中错误的 context 取消逻辑，确保主 agent 在处理 `get_subagent_result` 结果时不会被中断

**改进：**

- **优化 spawn/get_subagent_result 两步式调用设计**：
  - `spawn` 工具异步创建 subagent，立即返回 ID
  - `get_subagent_result` 工具使用 channel 阻塞等待结果
  - 添加 `resultCh` channel 通信机制，确保结果正确传递
- **精简调试日志**：删除了不必要的调试日志，保持代码简洁

### v1.3.0

**新功能：**

- **子代理系统 (Subagent)**: 支持创建后台运行的子代理，实现并行任务处理
  - 支持三种子代理类型：`general-purpose`、`explore`、`plan`
  - 新增工具：`spawn`（创建子代理）、`get_subagent_result`（获取结果）、`send_message`（发送消息）
  - TUI 子代理面板：按 `Ctrl+S` 显示/隐藏，实时查看子代理状态
  - 子代理生命周期管理：创建、运行、完成、失败、停止
  - **错误处理与状态通知**：状态回调机制、错误传播、viewCh 阻塞保护

**代码重构：**

- 消息类型定义迁移到 `internal/msgs` 包，实现更好的模块解耦

### v1.2.1

**提示词工程优化：**

- 系统提示词重构：添加 Tool Selection Strategy、Output Guidelines，重构 Error Handling 和 Clarification
- Error Handling 强化：强调"事前验证"规则，要求操作前先 list/glob 确认文件路径
- Todo 工具描述优化：改为结构化格式（Use when / Avoid when）
- Bash 工具描述：添加危险命令拦截列表
- Read 工具：添加目录操作限制说明
- 路径解析：Windows 中文路径兼容修复

### v1.2.0

**新功能：**
- MCP 工具 Schema 延迟加载：初始只发送工具名称和描述，按需获取完整 schema，减少 token 消耗

### v1.1.0

**Bug 修复：**
- 修复 Offload 内存在 Policy 执行后不释放的问题
- 修复 LLM 流式响应错误重试时未向 TUI 发送错误提示的问题

**改进：**
- 优化重试逻辑：达到最大重试次数时向 TUI 发送明确错误提示，避免任务静默中断

### v1.0.0

- 初始版本
- 流式响应、上下文管理、记忆系统、工具调用、MCP 集成

## 版权

MIT License
