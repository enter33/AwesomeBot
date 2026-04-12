# 多 Agent 层级编排系统设计

## 1. 概述

本文档描述 AwesomeBot 多 Agent 层级编排系统的设计方案。该系统通过 PlanAgent、PlanReviewer、CodingAgent、CodeReviewer、TaskReviewer 五个专精 Agent 的协作，结合评分阈值和重试机制，实现带状态转移的流水线编排。

## 2. 整体架构

```
用户输入
    ↓
┌──────────────────────────────────────────────────────────────┐
│  Plan 阶段                                                    │
│  PlanAgent (制定计划)  ←→  PlanReviewer (质疑计划)           │
│       ↓ 通过                                                    │
├──────────────────────────────────────────────────────────────┤
│  Code 阶段                                                    │
│  CodingAgent (执行编码)  ←→  CodeReviewer (质疑结果)        │
│       ↓ 通过                                                    │
├──────────────────────────────────────────────────────────────┤
│  Final 阶段                                                   │
│  TaskReviewer (最终验收)                                      │
│       ↓                                                        │
└──────────────────────────────────────────────────────────────┘
```

### 2.1 核心机制

每个 Reviewer 持有独立评分维度 + 阈值：
- 评分 < 阈值 → 打回重做（携带审查意见）
- 评分 >= 阈值 → 进入下一阶段
- 超过重试上限 → 强制终止或升级处理

## 3. Agent 角色定义

| Agent | 职责 | 可用工具 | 工具权限说明 |
|-------|------|----------|--------------|
| **PlanAgent** | 分析需求，制定详细执行计划 | read, grep, glob, web_search | 只读 + 搜索，专注于分析和规划 |
| **PlanReviewer** | 质疑和挑战计划，发现漏洞 | read, grep, glob | 仅只读，评估计划质量 |
| **CodingAgent** | 根据计划执行代码任务 | read, write, edit, bash, grep, glob | 完整工具集，执行具体任务 |
| **CodeReviewer** | 质疑和挑战代码结果 | read, grep, glob | 仅只读，评估代码质量 |
| **TaskReviewer** | 最终验收，判断任务是否完成 | read, grep, glob | 仅只读，最终确认 |

## 4. 上下文传递规则

采用**增量上下文**模式，每个 Agent 只接收必要信息：

| Agent | 接收的上下文 |
|-------|-------------|
| PlanAgent | 用户原始需求 |
| PlanReviewer | 用户原始需求 + PlanAgent 的计划 |
| CodingAgent | 用户原始需求 + PlanAgent 的计划（通过审查后） |
| CodeReviewer | 用户原始需求 + CodingAgent 的输出 |
| TaskReviewer | 用户原始需求 + 最终代码结果 |

**不包含**：中间审查过程的完整历史、Agent 思考链细节。

## 5. 评分系统

### 5.1 评分机制

采用**多维评分 + 综合判断**：
- 每个维度 0-100 分
- 加权计算总分
- 总分 >= 阈值 → 通过

### 5.2 各 Reviewer 评分维度

**PlanReviewer**：

| 维度 | 权重 | 说明 |
|------|------|------|
| 目标清晰度 | 20% | 计划目标是否明确、无歧义 |
| 步骤可行性 | 30% | 执行步骤是否可落地、依赖是否明确 |
| 风险识别 | 25% | 是否识别潜在风险和边界情况 |
| 完整性 | 25% | 是否覆盖用户所有需求 |

**CodeReviewer**：

| 维度 | 权重 | 说明 |
|------|------|------|
| 逻辑正确性 | 30% | 代码逻辑是否符合计划要求 |
| 边界处理 | 20% | 边界情况和异常处理 |
| 安全性 | 25% | 是否有安全漏洞（注入、敏感信息等） |
| 可维护性 | 25% | 代码可读性、结构清晰度 |

**TaskReviewer**：

| 维度 | 权重 | 说明 |
|------|------|------|
| 需求覆盖度 | 35% | 是否满足用户原始需求 |
| 完成度 | 35% | 任务是否真正完成，非半成品 |
| 质量评估 | 30% | 整体代码质量评价 |

### 5.3 阈值配置

默认阈值：70 分（满分 100）
支持在 `~/.awesome/awesome.json` 中配置各阶段阈值。

## 6. 状态流转

### 6.1 状态定义

```
pending → planning → plan_reviewing → planning (打回重做)
                           ↓ 通过
                        coding → code_reviewing → coding (打回重做)
                                          ↓ 通过
                                     final_review → done
```

### 6.2 状态说明

| 状态 | 说明 |
|------|------|
| pending | 任务待处理 |
| planning | PlanAgent 正在制定计划 |
| plan_reviewing | PlanReviewer 正在审查计划 |
| coding | CodingAgent 正在执行编码 |
| code_reviewing | CodeReviewer 正在审查代码 |
| final_review | TaskReviewer 进行最终验收 |
| done | 任务完成 |
| failed | 任务失败（超过重试上限或不可恢复错误） |

**失败入口**：任何阶段超过重试上限（见 7.1）→ 进入 `failed` 状态

## 7. 保护机制

### 7.1 重试配置

| 阶段 | 最大重试次数 |
|------|-------------|
| Plan 阶段 | 3 次 |
| Code 阶段 | 5 次 |
| 全局 | 10 次（累计所有阶段） |

### 7.2 超时配置

| Agent | 超时时间 |
|-------|---------|
| PlanAgent | 60 秒 |
| CodingAgent | 300 秒 |
| PlanReviewer | 30 秒 |
| CodeReviewer | 30 秒 |
| TaskReviewer | 30 秒 |

### 7.3 失败处理

- 单阶段重试超限：终止任务，标记为 failed
- 全局重试超限：终止任务，输出当前状态
- Agent 执行异常：捕获错误，生成错误报告，进入重试或终止

## 8. 用户交互模式

### 8.1 复杂度判断

系统自动判断任务复杂度：

| 因素 | 加分 |
|------|------|
| 读取文件数 > 3 | +1 分 |
| 工具调用数 > 5 | +1 分 |
| 包含关键词（修改/重构/新增/创建） | +1 分 |

**判断规则**：总分 >= 2 → 复杂任务，否则 → 简单任务

### 8.2 交互模式

**简单模式**：
- 自动完成整个流程
- 用户只读最终结果
- 适合小改动、单一文件修改

**复杂模式**：
- 实时流式输出，每个 Agent 的思考和操作实时可见
- 用户可在任意时刻暂停/修改/终止任务
- 适合重构、多文件改动、新功能开发

### 8.3 用户干预点

复杂模式下用户可：
- 暂停执行，查看当前状态
- 修改上下文或指令
- 终止任务
- 强制进入下一阶段（跳过审查）

## 9. 提示词设计原则

每个 Agent 的提示词遵循专精原则：

### 9.1 PlanAgent

- 角色：战略规划师
- 关注：需求拆解、步骤排序、风险预判
- 禁止：直接写代码、给出具体实现细节
- 输出格式：分步骤计划，每个步骤有明确目标和验收标准

### 9.2 PlanReviewer

- 角色：怀疑论者、挑战者
- 关注：计划的漏洞、遗漏、风险
- 思考方式：假设计划失败，找原因；假设用户意图被误解，怎么办
- 输出格式：评分 + 具体质疑点 + 改进建议

### 9.3 CodingAgent

- 角色：执行者、工程师
- 关注：按计划实现、代码正确性、效率
- 约束：严格按计划执行，不要过度发挥
- 输出格式：代码变更 + 变更说明 + 遗留问题

### 9.4 CodeReviewer

- 角色：质量审计员
- 关注：代码缺陷、安全问题、可维护性
- 思考方式：这个代码上线后会出什么问题？
- 输出格式：评分 + 问题列表 + 严重程度

### 9.5 TaskReviewer

- 角色：最终验收者
- 关注：需求是否真正完成、用户体验
- 思考方式：用户拿到这个结果会满意吗？
- 输出格式：验收结论 + 改进建议（可选）

## 10. 配置项

在 `~/.awesome/awesome.json` 中新增配置：

```json
{
  "multi_agent": {
    "enabled": true,
    "complexity_threshold": 2,
    "timeouts": {
      "plan_agent": 60,
      "coding_agent": 300,
      "reviewer": 30
    },
    "retry_limits": {
      "plan_phase": 3,
      "code_phase": 5,
      "global": 10
    },
    "thresholds": {
      "plan_reviewer": 70,
      "code_reviewer": 70,
      "task_reviewer": 70
    },
    "interaction_mode": "auto"  // "auto"=简单模式(自动完成), "manual"=复杂模式(实时流式)
  }
}
```

## 11. 实现目录结构

```
internal/
  orchestrator/
    orchestrator.go      # 主编排器，管理状态流转
    state.go             # 状态定义和转换
    config.go            # 配置管理
    metrics.go           # 评分计算
  agent/
    factory.go          # Agent 工厂，创建专精 Agent
  planner/
    planner.go           # PlanAgent 实现
    planner_review.go    # PlanReviewer 实现
  coder/
    coder.go             # CodingAgent 实现
    coder_review.go      # CodeReviewer 实现
  reviewer/
    final_review.go      # TaskReviewer 实现
  prompt/
    planner.txt          # PlanAgent 提示词
    planner_reviewer.txt # PlanReviewer 提示词
    coder.txt            # CodingAgent 提示词
    coder_reviewer.txt   # CodeReviewer 提示词
    final_reviewer.txt   # TaskReviewer 提示词
```

## 12. 风险与限制

1. **Agent 能力依赖**：系统效果依赖各 Agent 的 prompt 质量和 LLM 能力
2. **无限循环风险**：虽有重试上限，但可能达到上限仍未收敛
3. **上下文丢失**：增量上下文可能丢失跨阶段的有用信息
4. **评分主观性**：多维评分存在主观性，可能影响判断一致性

## 13. 未来扩展

1. **动态 Agent 池**：根据任务类型动态选择 Agent 组合
2. **记忆共享**：支持跨 Agent 记忆传递
3. **并行化**：同阶段多 Agent 并行（如多个 CodeReviewer 从不同角度审查）
4. **持久化**：任务状态持久化，支持恢复和回放
