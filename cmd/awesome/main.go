package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/joho/godotenv"

	"github.com/enter33/AwesomeBot/internal/agent"
	ctxengine "github.com/enter33/AwesomeBot/internal/context"
	"github.com/enter33/AwesomeBot/internal/logging"
	"github.com/enter33/AwesomeBot/internal/mcp"
	"github.com/enter33/AwesomeBot/internal/memory"
	"github.com/enter33/AwesomeBot/internal/storage"
	"github.com/enter33/AwesomeBot/internal/tool"
	"github.com/enter33/AwesomeBot/internal/tui"
	"github.com/enter33/AwesomeBot/pkg/config"
	"github.com/enter33/AwesomeBot/pkg/llm"
	"github.com/enter33/AwesomeBot/pkg/prompt"
)

const version = "1.0.0"

func main() {
	// 初始化日志系统
	if err := logging.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志系统失败: %v\n", err)
	}
	defer logging.Close()

	// 加载 .env 文件
	_ = godotenv.Load()

	// 确保配置目录和文件存在
	configPath := config.GetConfigPath()
	if err := config.EnsureConfigFile(configPath); err != nil {
		log.Fatalf("创建配置文件失败: %v", err)
	}

	// 加载配置
	llmConfig, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 检查配置是否有效
	if !llmConfig.IsValid() {
		fmt.Println("未检测到有效的 LLM 配置，正在启动初始化向导...")
		fmt.Println()
		llmConfig = runInitWizard()
		if !llmConfig.IsValid() {
			fmt.Println("初始化取消，程序退出。")
			return
		}
	}

	ctx := context.Background()

	// 加载 MCP 服务器配置
	mcpConfigPath := config.GetMcpConfigPath()
	if err := config.EnsureMcpConfigFile(mcpConfigPath); err != nil {
		log.Printf("创建 MCP 配置文件失败: %v", err)
	}
	mcpServerMap, err := config.LoadMcpServerConfig(mcpConfigPath)
	if err != nil {
		log.Printf("加载 MCP 服务器配置失败: %v", err)
	}

	// 初始化 MCP 客户端
	mcpClients := make([]*mcp.Client, 0)
	for k, v := range mcpServerMap {
		mcpClient := mcp.NewClient(k, v)
		if err := mcpClient.RefreshTools(ctx); err != nil {
			log.Printf("刷新 MCP 服务器工具失败 %s: %v", k, err)
			continue
		}
		mcpClients = append(mcpClients, mcpClient)
	}

	// 确保 awesome.json 存在并加载配置
	awesomeConfigPath := config.GetAwesomeConfigPath()
	if err := config.EnsureAwesomeConfigFile(awesomeConfigPath); err != nil {
		log.Printf("创建 awesome 配置文件失败: %v", err)
	}
	awesomeConfig, _ := config.LoadAwesomeConfig(awesomeConfigPath)

	// 获取上下文窗口大小，默认 128K
	contextWindow := awesomeConfig.ContextWindow
	if contextWindow <= 0 {
		contextWindow = config.DefaultContextWindow
	}

	// 生成唯一实例 ID: YYYYMMDD_HHMMSS_ffffff
	instanceID := time.Now().Format("20060102_150405") + "_" + fmt.Sprintf("%06d", rand.Intn(1000000))

	// 创建上下文引擎和 policy
	offloadStorage := storage.NewFileSystemStorage(filepath.Join(config.GetAwesomeDir(), "offload", instanceID))
	summarizer := ctxengine.NewLLMSummarizer(llmConfig, 200, contextWindow)

	policies := []ctxengine.Policy{
		ctxengine.NewOffloadPolicy(offloadStorage, 0.4, 0, 100, instanceID),
		ctxengine.NewSummaryPolicy(summarizer, 10, 20, 0.6),
		ctxengine.NewTruncatePolicy(0, 0.85),
	}

	homeStorage := storage.NewFileSystemStorage(config.GetAwesomeDir())
	workspaceStorage := storage.NewFileSystemStorage(config.GetWorkspaceDir())

	memoryUpdater := memory.NewLLMMemoryUpdater(llmConfig)
	conditionalUpdater := memory.NewConditionalMemoryUpdater(memoryUpdater, awesomeConfig.UseMemory)
	throttledUpdater := memory.NewThrottledMemoryUpdater(conditionalUpdater, awesomeConfig.MemoryUpdateThreshold)
	multiLevelMemory := memory.NewMultiLevelMemory(homeStorage, workspaceStorage, throttledUpdater)

	contextEngine := ctxengine.NewContextEngine(multiLevelMemory, policies, contextWindow, offloadStorage)

	// 配置需要确认的工具
	confirmConfig := agent.ToolConfirmConfig{
		RequireConfirmTools: map[tool.AgentTool]bool{
			tool.AgentToolBash: true,
			tool.AgentToolWrite: true,
		},
	}

	// 创建 PathResolver
	workspaceDir := config.GetWorkspaceDir()
	pathResolver := tool.NewPathResolver(workspaceDir, workspaceDir)

	// 构建工具列表
	tools := []tool.Tool{
		tool.NewReadToolWithResolver(pathResolver),
		tool.NewWriteToolWithResolver(pathResolver),
		tool.NewEditToolWithResolver(pathResolver),
		tool.NewListDirToolWithResolver(pathResolver),
		tool.CreateBashTool(workspaceDir),
		tool.NewLoadStorageTool(offloadStorage),
		tool.NewLoadSkillTool(),
	}

	// 添加 Web 工具（如果配置存在）
	webSearchConfigPath := config.GetWebSearchConfigPath()
	if err := config.EnsureWebSearchConfigFile(webSearchConfigPath); err != nil {
		log.Printf("创建 web_search 配置文件失败: %v", err)
	} else {
		webSearchCfg, _ := config.LoadWebSearchConfig(webSearchConfigPath)
		tools = append(tools, tool.NewWebSearchTool(webSearchCfg))
	}

	webFetchConfigPath := config.GetWebFetchConfigPath()
	if err := config.EnsureWebFetchConfigFile(webFetchConfigPath); err != nil {
		log.Printf("创建 web_fetch 配置文件失败: %v", err)
	} else {
		webFetchCfg, _ := config.LoadWebFetchConfig(webFetchConfigPath)
		tools = append(tools, tool.NewWebFetchTool(webFetchCfg))
	}

	// 创建 LLM 客户端
	llmClient := llm.NewOpenAIClient(llmConfig)

	// 创建 Agent
	codingAgent := agent.NewAgent(
		llmConfig,
		prompt.CodingAgentSystemPrompt,
		confirmConfig,
		tools,
		mcpClients,
		contextEngine,
		llmClient,
		contextWindow,
	)

	// 丢弃标准库的日志输出
	log.SetOutput(io.Discard)

	// 进程退出时清理实例的 offload 目录
	defer func() {
		instanceOffloadDir := filepath.Join(config.GetAwesomeDir(), "offload", instanceID)
		os.RemoveAll(instanceOffloadDir)
	}()

	// 创建 TUI
	tuiModel := tui.NewModel(codingAgent, llmConfig.Model, version)

	// 运行 TUI
	p := tea.NewProgram(tuiModel)
	if _, err := p.Run(); err != nil {
		os.Exit(1)
	}
}

func runInitWizard() config.Config {
	fmt.Println("=== AwesomeBot LLM 配置向导 ===")
	fmt.Println()

	var cfg config.Config

	fmt.Print("请输入 OpenAI Base URL (例如: https://api.openai.com/v1): ")
	fmt.Scanln(&cfg.BaseURL)
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	fmt.Print("请输入模型名称 (例如: gpt-4o-mini): ")
	fmt.Scanln(&cfg.Model)
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}

	fmt.Print("请输入 API Key: ")
	fmt.Scanln(&cfg.ApiKey)

	if cfg.ApiKey == "" {
		fmt.Println("API Key 不能为空，初始化取消。")
		return cfg
	}

	fmt.Printf("请输入超时时间（秒，默认 %d）: ", config.DefaultLLMTimeout)
	fmt.Scanln(&cfg.Timeout)
	if cfg.Timeout <= 0 {
		cfg.Timeout = config.DefaultLLMTimeout
	}

	// 保存配置
	configPath := config.GetConfigPath()
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("保存配置失败: %v\n", err)
		return cfg
	}

	fmt.Printf("配置已保存到: %s\n", configPath)
	return cfg
}

