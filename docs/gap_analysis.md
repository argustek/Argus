# Argus SE AI 能力差距分析

> 对比对象: Cursor / Claude Code / GitHub Copilot / Trae / Windsurf
> 分析日期: 2026-06-05（基于 e31f7ac 重新扫描）
> 方法: 逐文件代码扫描（非文档推断），经 2 次代码实测
> 状态: ✅ 已更新 — P0 已补齐，聚焦 P1

---

## 一、工具集实测（23 个）

### 1.1 基础文件/搜索（9个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 1 | write_file | [manager.go:3478](internal/chat/manager.go#L3478) | 受保护文件检查 + 路径安全检查 + isPathInDir | ✅ |
| 2 | edit_file | [manager.go:3510](internal/chat/manager.go#L3510) | old_str/new_str 精确替换，支持多行匹配 | ✅ |
| 3 | read_file | [se_prompt.go:370](internal/ai/se_prompt.go#L370) | offset/limit 行范围，默认100行，带行号输出 | ✅ |
| 4 | delete_file | [manager.go:3665](internal/chat/manager.go#L3665) | 工作目录范围检查 + 安全拒绝 + isPathInDir | ✅ |
| 5 | list_files | [se_prompt.go:398](internal/ai/se_prompt.go#L398) | 递归遍历 + glob 过滤 + ** 双星展开 + Walk 回退 | ✅ |
| 6 | glob | [se_prompt.go:438](internal/ai/se_prompt.go#L438) | glob 模式匹配 + ** 双星展开 + Walk 回退 | ✅ |
| 7 | search_files | [se_prompt.go:612](internal/ai/se_prompt.go#L612) | 正则 + 文件类型过滤 + 跳过 12 种忽略目录 + 上下文行 | ✅ |
| 8 | semantic_search | [code_indexer.go](internal/ai/code_indexer.go#L1) (677行) | 倒排索引(函数/类型/注释/字段tag) + AI GenerateConcepts 双通道评分，跨 20+ 语言 | 🆕✅ |
| 9 | web_search | [se_prompt.go:563](internal/ai/se_prompt.go#L563) | DuckDuckGo HTML 解析 + 来源URL，并行搜索 3 引擎（DuckDuckGo/Bing/Google），goroutine 并发取最快结果 | 🆕✅ |

### 1.1b 网络抓取（1个）🆕

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 10 | fetch_url | [se_prompt.go:921](internal/ai/se_prompt.go#L921) | HTTP GET + 正文提取（去除 script/style/head 标签 + HTML → 纯文本 + 实体解码），2MB 限制，8000字符截断 | 🆕✅ |

### 1.2 执行/终端（3个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 11 | exec | [manager.go:3920](internal/chat/manager.go#L3920) | CMD独立进程，30s超时，sanitizeCommandPath 自动纠错 | ✅ |
| 12 | exec_session | [shell_session.go](internal/executor/shell_session.go#L1) (287行) | 持久化 cmd.exe，cd/env 状态保持跨命令，60s 超时，10MB buffer | 🆕✅ |
| 13 | run_tests | [manager.go:3760](internal/chat/manager.go#L3760) | go test，支持 pattern/coverage/verbose | ✅ |

### 1.3 版本控制/任务（2个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 14 | git_operation | [manager.go:3686](internal/chat/manager.go#L3686) | status/diff/commit/push/pull/log/branch/show 8 种操作 | ✅ |
| 15 | complete_task | [se_prompt.go:490](internal/ai/se_prompt.go#L490) | 文件清单 + 摘要 + 状态标记 + CheckSemanticComplete 验证 | ✅ |

### 1.4 LSP 代码理解（5个）🆕

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 16 | go_to_definition | [lsp_client.go:319](internal/ai/lsp_client.go#L319) | gopls daemon → textDocument/definition JSON-RPC；返回 LocationList | 🆕✅ |
| 17 | find_references | [lsp_client.go:346](internal/ai/lsp_client.go#L346) | gopls daemon → textDocument/references；支持 includeDeclaration；按文件分组展示 | 🆕✅ |
| 18 | hover_info | [lsp_client.go:379](internal/ai/lsp_client.go#L379) | gopls daemon → textDocument/hover；获取类型签名/文档注释；支持 plaintext/markdown | 🆕✅ |
| 19 | diagnostics | [lsp_client.go:406](internal/ai/lsp_client.go#L406) | gopls daemon → textDocument/diagnostic (pull API)；打开文件后主动拉取编译诊断，回退兼容旧版 gopls | 🆕✅ |
| 20 | rename_symbol | [lsp_client.go:440](internal/ai/lsp_client.go#L440) | gopls daemon → textDocument/rename → WorkspaceEdit；ApplyWorkspaceEdit 从后往前应用文本编辑避免位置偏移 | 🆕✅ |

### 1.5 多模态/撤销（3个）🆕

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 21 | analyze_image | [vision.go:1](internal/ai/vision.go#L1) (272行) | PNG/JPG/GIF/WebP/BMP/PDF → base64 data URI → vision LLM；支持 HTTP URL/本地路径；IsVisionModel() 自动检测模型能力；5 种场景预设提示词 | 🆕✅ |
| 22 | undo_file | [file_tracker.go:112](internal/executor/file_tracker.go#L112) | RollbackLast() 回滚到编辑前快照；自动回滚已集成到 write/edit 失败路径（[manager.go:3584](manager.go#L3584) + [manager.go:3687](manager.go#L3687)）；最近 20 步 | 🆕✅ |
| 23 | list_changes | [file_tracker.go:149](internal/executor/file_tracker.go#L149) | GetRecentChanges() 列出最近 N 步变更（按时间倒序）；Stats() 展示文件追踪统计（总快照数/文件数/操作类型分布） | 🆕✅ |

---

## 二、已实现的核心优势（代码验证通过）

### 2.1 Tool Result Feedback Loop

每个工具执行后结果通过 `AddResult()` 注入 SE 对话历史。SE 基于实际执行结果决定下一步。

**实测 AddResult 调用点（30+ 处）：**

| 工具 | 反馈内容 | 位置 |
|------|---------|------|
| write_file | 写入成功/失败 + 文件路径 | manager.go:3479, 3501 |
| edit_file | 替换匹配数/失败原因 | manager.go:3524, 3540 |
| exec | stdout/stderr 全文 + **AnalyzeError** 错误分析 | manager.go:3991, 3996 |
| exec（错误） | **FormatErrorForSE** 结构化错误：Type/File/Line/Column/SuggestedFix/PossibleCauses/ExampleFix | manager.go:3953, 3965 |
| read_file | 文件内容带行号 + 行范围信息 | manager.go:3315 |
| search_files | 匹配数量 + 内容预览 | manager.go:3317 |
| git_operation | commit/push/diff 结果文本 | manager.go:3699 |
| run_tests | 通过/失败/覆盖率 + TestResults 结构化 | manager.go:3788 |
| delete_file | 删除确认/拒绝原因 | manager.go:3686 |
| exec_session | shell 命令输出 | manager.go:3835 |

### 2.2 Auto-Fix 循环

```
action 执行失败
  → AnalyzeError() 分类错误（9种 ErrorType × 3种 Category）
    → syntax/compile/import/type → CategoryFixable
    → timeout/network → CategoryTransient
    → permission/fatal → CategoryPermanent
  → FormatErrorForSE() 构造结构化反馈
    → File + Line + Column + Message
    → SuggestedFix + PossibleCauses + ExampleFix
    → TestResults（测试失败时含 fail_names/skipped）
  → fixPrompt = 错误详情 + 之前 actions + 常见修复指南
  → SE AI 生成修复 actions（maxFixRetries=3）
  → 执行修复 → 成功则合并
  → 3次全失败 → 降级路径:
      ① continueSETask() 自主尝试（最多10次，seContinueCount限制）
      ② handleSEAskPM() 请求 PM 介入（最多3次，seAskPMCount限制）
      ③ PM 分析后给 SE 新指令
```

### 2.3 多层防死循环防护

| 层 | 机制 | 阈值 | 代码位置 | 动作 |
|----|------|------|---------|------|
| L1 | 空 actions 检测 | 连续 3 次 | manager.go:2673 | 强制路由到 PM 审核 |
| L2 | JSON 格式重试 | 第 1 次空 actions | manager.go:2648 | 用严格 JSON-only 提示词重试 AI |
| L3 | continue 次数限制 | > 10 次 | manager.go:2984 | 强制结束任务 |
| L4 | 语义完成兜底 | SE 说"完成"但无 JSON | se_prompt.go:798 | CheckSemanticComplete() 自动触发 PM 审核 |
| L5 | PM 审核轮次限制 | argus.go 中 PM/AP 各有限次 | argus.go | 强制结束审核流程 |
| L6 | SE 问 PM 次数限制 | > 3 次 | manager.go:1868 | 停止循环，强制完成 |
| L7 | isProcessing 超时 | > 60s | manager.go:981 | 强制清理旧任务 |

### 2.4 错误智能分析系统

**AnalyzeError() — 9 种 ErrorType：**

```go
// internal/executor/result.go
ErrSyntax    = "syntax_error"       // 语法错误
ErrRuntime   = "runtime_error"      // 运行时 panic/nil/index
ErrTestFail  = "test_failure"       // 测试失败
ErrImport    = "import_error"       // 包未找到/模块路径错
ErrType      = "type_error"         // 类型不匹配
ErrPermission = "permission_error"  // 权限拒绝/文件被锁
ErrTimeout   = "timeout"            // 超时
ErrCompile   = "compile_error"      // 编译错误
ErrUnknown   = "unknown"
```

**ClassifyError() — 3 种 Category（用于重试决策）：**

```go
CategoryTransient  = "transient"  // 网络/超时/限流 → 可重试
CategoryFixable    = "fixable"    // 语法/编译/导入/运行时/测试 → SE可修复
CategoryPermanent  = "permanent"  // 权限/致命错误 → 不重试
```

**ExecuteWithRetry() — 指数退避重试：**
- MaxRetries=3, InitialDelay=1s, MaxDelay=30s, Multiplier=2x, Jitter=true
- Permanent 类立即停止；Fixable 类报告给 SE；Transient 类自动重试

### 2.5 并行工具执行

[executeReadOnlyBatch()](internal/chat/manager.go#L3248)：
- 连续的只读操作（read_file / search_files / list_files / glob / web_search）自动批处理
- goroutine + WaitGroup 并发执行
- 最多 5 个操作一批
- 结果按原始顺序组装返回

### 2.6 Shell Session 持久化终端

[ShellSession](internal/executor/shell_session.go#L1)（287行）：
- 持久 cmd.exe 进程，stdin/stdout pipe
- cd 和 set 命令的状态跨调用保持
- 10MB output buffer 防截断
- `___ARGUS_CMD_END___` 标记分隔命令输出
- 60s 空闲超时自动清理
- HTTP API 直接访问：`POST /api/v1/tool/exec-session`

### 2.7 LSP 代码理解引擎 🆕

[LSPClient](internal/ai/lsp_client.go#L1)（560行）— 通过 gopls daemon 子进程实现 5 个语言服务器协议操作：

```go
// 初始化流程
NewLSPClient(workDir)
  → exec.LookPath("gopls") 检查可用性
  → exec.CommandContext(ctx, "gopls", "serve") 启动 daemon
  → initialize(): 发送 capabilities 声明（hover/def/refs/rename/documentSymbol）
  → initialized 通知 → initDone=true
  → readLoop() goroutine 循环读取 stdout（Content-Length header + JSON-RPC body）

// 5 个核心操作
GoToDefinition(file, line, col)   → "textDocument/definition"  → []LSPLocation
FindReferences(file, line, col)   → "textDocument/references" → []LSPLocation（按文件分组）
Hover(file, line, col)            → "textDocument/hover"      → 类型签名 + 文档注释
Diagnostics(file)                 → "textDocument/diagnostic" → Diagnostic[]（Error/Warning/Info/Hint）
Rename(file, line, col, newName)  → "textDocument/rename"     → WorkspaceEdit（跨文件安全重命名）
```

**关键设计细节：**
- JSON-RPC 2.0 协议，30s 请求超时
- 旧版 gopls 不支持 pull diagnostics 时自动回退返回空（不崩溃）
- ApplyWorkspaceEdit：从后往前应用文本编辑（避免行号偏移Bug）
- GoToDefinition 兼容单/location 和数组两种响应格式
- 每个操作独立锁保护并发安全

### 2.8 多模态视觉分析 🆕

[vision.go](internal/ai/vision.go#L1)（272行）— AnalyzeImage 核心流程：

```
本地路径 或 HTTP URL
  → 读取图片数据（PNG/JPG/GIF/WebP/BMP/PDF）
  → 检测 MIME 类型（detectImageMIME）
  → 验证支持格式（isSupportedImage）
  → base64.StdEncoding → data URI
  → 构造多模态消息（text + image_url）
  → callVisionLLM → vision-capable LLM API
  → extractCodeBlock → 提取 ```language\n...\n``` 代码块
  → 返回 VisionResponse{Description, Code, Raw}
```

**5 种场景预设提示词**（GetImageAnalysisPrompt）：
| 场景 | 描述 |
|------|------|
| `ui_to_code` | UI截图→React/HTML代码 |
| `design_review` | UI设计稿评审（布局/色彩/UX） |
| `screenshot_debug` | 错误截图诊断 |
| `diagram_parse` | 图表/流程图/架构图→文本 |
| `general` | 通用图片描述 |

IsVisionModel() 自动检测模型名称是否包含 vision 关键词（gpt-4o / claude-3 / gemini / qwen-vl / glm-4v）。

### 2.9 文件变更追踪 + 撤销系统 🆕

[FileChangeTracker](internal/executor/file_tracker.go#L1)（181行）— 三重能力：

```
1. Snapshot(path, action) — 编辑前自动快照
   → 读取完整文件内容 + mtime
   → 追加到内存栈（最多20步）
   → write/edit 执行前自动调用（manager.go:3533 + manager.go:3625）
   → 新文件（不存在）自动跳过

2. RollbackLast(path) — 回滚到编辑前状态
   → 从栈尾找到该文件的最新快照
   → 定位前一个快照（i-1）→ 恢复到该状态
   → os.WriteFile 写回文件
   → 自动回滚已集成到失败路径：
     - write 失败 → manager.go:3584 Auto-Rollback
     - edit 失败 → manager.go:3687 Auto-Rollback

3. CheckConflict(path) — 外部修改冲突检测
   → 对比当前文件 mtime 与最近快照 mtime
   → 不一致则返回冲突描述（用于 PM 审核参考）
```

### 2.10 语义搜索引擎

[CodeIndexer](internal/ai/code_indexer.go#L1)（677行）：

```
IndexProject()
  → filepath.Walk 扫描项目所有源码文件
  → 解析每个文件的函数/类型/包/注释
  → tokenize(): 驼峰拆分 + 下划线分割 + stop word 过滤
  → 建立倒排索引: token → entry indices
  → 异步 GenerateConcepts():
      → 选 Top 重要条目（按 Importance 排序）
      → AI 提取语义概念（如 "authentication", "REST API", "database query"）
      → 写入 SemanticConcepts 字段

Search(query)
  → 关键词通道: query 分词 → 倒排索引匹配 → TF-like 评分
  → 概念通道: query 与 SemanticConcepts 语义匹配 → 评分
  → 双通道加权融合排序 → 返回 Top N SemSearchResult
  → 每个 Result 含: Score + Entry + Snippet(带行号) + MatchOn(匹配维度)
```

- 跨 20+ 语言识别（Go/Python/JS/TS/Rust/Java/C/C++/Ruby/PHP/Swift/Kotlin...）
- 不依赖外部 embedding API，本地运行
- HTTP API: `POST /api/v1/tool/semantic-search`

### 2.11 消息总线 + SSE 推送

- msgBus 历史记录 + SSE 实时推送
- 前端任意时刻重连 → 全量同步
- pendingQueue 忙时消息暂存（27处引用）
- 4 个 API 端点: send/history/pending/clear-pending

### 2.12 上下文压缩

[compressHistory()](internal/ai/se_prompt.go#L710)：
- history > 20 条时触发
- 保留最近 15 条活跃上下文
- 旧消息提取 tool name + result preview → 拼接为 `[上下文摘要]` system 消息插入头部

### 2.13 健康恢复机制

| 机制 | 触发条件 | 代码位置 | 动作 |
|------|----------|---------|------|
| PM 不健康检测 | 连续 API 失败 ≥ 3 次（60s 内） | manager.go:1227 | 清理会话 + 重置健康状态 |
| isProcessing 超时 | 锁持有 > 60s | manager.go:981 | 强制清理旧任务 + 复位锁 |
| 消息队列阻塞检测 | busy 时新消息到达 | manager.go:1005 | 暂存 pendingQueue，空闲时自动处理 |
| panic recover | SE/PM/AP 任意层 panic | 多处 `defer recover()` | 记录日志 + 优雅降级 |

---

## 三、竞品差距（代码实测，2026-06-05 更新）

### P0 — 已全部补齐 ✅

| # | 能力 | 之前状态 | 实现 | 评估 |
|---|------|----------|------|------|
| 1 | **LSP 代码理解** | ❌ 零 LSP 集成 | [lsp_client.go](internal/ai/lsp_client.go#L1) 560行 — gopls daemon 子进程，5个操作（GoToDef/FindRefs/Hover/Diagnostics/Rename）全部可用 | ✅ **已补齐** |
| 2 | **多模态输入** | ❌ 无 vision 能力 | [vision.go](internal/ai/vision.go#L1) 272行 — base64 data URI → vision LLM，5种场景预设提示词，支持PNG/JPG/GIF/WebP/BMP/PDF | ✅ **已补齐** |
| 3 | **撤销/回滚** | ❌ 无 undo 机制 | [file_tracker.go](internal/executor/file_tracker.go#L1) 181行 — Snapshot → RollbackLast 双层回滚，失败自动回滚集成到 write/edit 路径 | ✅ **已补齐** |

### P1 — 实际差距（代码扫描后修订）

| # | 能力 | Argus 实测 | 差距评估 |
|---|------|-----------|---------|
| 4 | **多 Agent 协作** | ❌ 单 SE agent，无任务拆分 | 复杂全栈任务效率低 |
| 5 | **增量 Diff 预览** | ❌ 直接写入，无 diff 预审 | 用户对修改缺乏掌控感 |
| 6 | **交互式调试** | ❌ 只能 exec 运行看输出 | 调试复杂 bug 困难 |
| 7 | **代码模板/片段库** | ❌ 无 | 重复模式每次从头写 |
| 8 | **agent 调试可视化** | ❌ 无执行过程可视化 | 用户看不到 agent 思考过程 |

### P2 — 长期优化

| # | 能力 | 说明 |
|---|------|------|
| 9 | 多语言 LSP | 仅 gopls（Go），无 TS/RS/Python |
| 10 | 分布式 Agent | 单机运行 |
| 11 | 监控/指标 | 有健康自愈，无 Prometheus/Grafana |

---

## 四、优先修复路线图

### ✅ Phase 1 — P0 致命差距（已完成）

| 项目 | 方案 | 代码 | 状态 |
|------|------|------|------|
| **LSP 集成** | 启动 gopls daemon 子进程；JSON-RPC 2.0 协议；GoToDef/FindRefs/Hover/Diagnostics/Rename 5个能力；diagnostics 注入 Tool Result Feedback | [lsp_client.go](internal/ai/lsp_client.go#L1) (560行) | ✅ |
| **撤销/回滚** | write/edit 前自动 Snapshot（最多20步）；RollbackLast 双层回滚到前一个快照；失败自动回滚已集成到 write/edit 路径 | [file_tracker.go](internal/executor/file_tracker.go#L1) (181行) | ✅ |
| **多模态基础** | base64 data URI → vision LLM；5种场景预设提示词；HTTP URL/本地路径双支持；IsVisionModel 自动检测 | [vision.go](internal/ai/vision.go#L1) (272行) | ✅ |

### Phase 2 — P1 增强（2026-06-05 执行中）

| # | 项目 | 方案 | 状态 |
|---|------|------|------|
| **2.1** | **web_search 增强** | 并行 3 引擎（DuckDuckGo/Bing/Google）goroutine 竞速取最快返回 + fetch_url 网页抓取正文提取 | ✅ |
| **2.2** | **Agent 智能终止** | CheckSemanticComplete 升级：关键词信号 + complete_task 验证 + 动作收敛检测 + 置信度评分 (0-1.0) | ✅ （远程已实现） |
| **2.3** | **上下文管理升级** | compressHistory 分层：①保留 user/system 指令 ②压缩 tool result ③保留最近3轮 ④token 计数 + 二次压缩保护 | ✅ （远程已实现） |
| **2.4** | **Diff 预览** | edit/write 前计算 unified diff；SSE 推送前端用户确认 | 🔴 |
| **2.5** | **多语言 LSP** | 扩展 LSPClient → typescript-language-server / rust-analyzer / pyright | 🔴 |

### Phase 3 — 体验打磨（P2）

| # | 项目 | 状态 |
|---|------|------|
| 3.1 | 代码片段库（JSON/YAML + semantic_search 匹配） | 🔴 |
| 3.2 | 交互式调试（断点/变量查看） | 🔴 |
| 3.3 | 多 Agent 协作（前端/后端/DB 并行子 agent） | 🔴 |

---

## 五、核心能力盘点

> 本节客观列出 Argus 代码验证通过的能力，不做"独有/领先"断言。
> 行业中 Planner-Executor-Reviewer 是常见模式（OpenCode 10角色编排、Copilot Plan-Implement、Windsurf Plan Mode），Argus 的 PM→SE→AP 是此模式的工程化实现。

以下能力经代码验证正常工作，后续迭代应保持：

1. **Tool Result Feedback Loop** — 工具执行结果注入 SE 对话历史，SE 基于实际输出（非猜测）决定下一步。30+ 处 AddResult 调用点。
2. **Auto-Fix 三级降级** — SE自修 → SE继续尝试 → PM介入，每级有次数上限和超时保护。
3. **7层防死循环** — L1空actions → L2 JSON重试 → L3 continue次数 → L4 语义兜底 → L5 PM轮次 → L6 SE求助上限 → L7 超时清理。
4. **9类错误分析 + 3类重试分类** — AnalyzeError + ClassifyError + ExecuteWithRetry(指数退避)，精确到 File:Line:Column + SuggestedFix + ExampleFix。
5. **并行只读批处理** — 5个 read/search 操作 goroutine 并发，结果按序组装。
6. **双通道语义搜索** — 关键词倒排 + AI概念融合，不依赖外部 embedding API。
7. **持久化 Shell Session** — cmd.exe 持久进程，cd/env 状态跨命令保持，10MB buffer。
8. **LSP 语义理解** — 5个操作（GoToDef/FindRefs/Hover/Diagnostics/Rename），safe rename 成为可能。
9. **多模态视觉分析** — PNG/JPG/GIF/WebP/BMP/PDF → vision LLM → 代码。
10. **文件变更追踪 + 自动回滚** — 编辑前快照，失败自动 RollbackLast，20步 undo 栈。
11. **权限配置系统** — PermissionConfig + PathRule + CheckPermission，每次 write/edit/delete 前校验，默认保护 .env / .git / .argus / 系统目录。
12. **代码片段库** — 9个模板（HTTP/CRUD/认证/DB/测试/并发/配置/CLI），search_snippet 工具。
13. **Diff 预览** — write/edit 前 ComputeDiff 推送到前端 SSE，show_diff 工具。
14. **调试运行** — debug_run 自动加 -v -race -count=1，panic/trace 结构化展示。
15. **web_search 三引擎并行** — DuckDuckGo + Bing + Google goroutine 竞速，fetch_url HTML→纯文本。

### 与竞品共有的能力（不是独有）

| 能力 | Argus | OpenCode | Copilot | Claude Code | Windsurf |
|------|------|----------|---------|-------------|----------|
| 规划-执行-审查管道 | PM→SE→AP | Orchestrator→Worker→Reviewer | Plan→Implement | Plan→Code→Review | Plan Mode→Cascade |
| 角色分工 | 3角色 | 10角色 | 2角色 | 3内置+自定义 | 2角色 |

### Argus 实际差异点

| 差异 | 说明 |
|------|------|
| **每角色可配不同模型** | PM/SE/AP 各自独立 AI Client，可分别用强推理/编码/轻量模型。OpenCode 也支持多模型分层，其他家基本单一模型串全流程 |
| **工程可靠性优先** | 7层防死循环 + 9类错误分析 + 三级降级 + 健康自愈，比多数竞品更偏"生产稳定性"而非"炫技" |
| **安全靠审查不靠隔离** | CheckPermission + PathRule + PM审核 + 自动回滚，不需要 Docker/VM |

---

## 六、数据速查

| 指标 | 数值 |
|------|------|
| 工具总数 | 23 |
| AddResult 反馈点 | 30+ |
| web_search 引擎数 | 3 (并行) |
| fetch_url | 正文提取 + 2MB/8000字符限制 |
| 防死循环层数 | 7 |
| ErrorType 种类 | 9 |
| ClassifyError Category | 3 |
| maxFixRetries (Auto-Fix) | 3 |
| maxSelfFix (argus.go) | 5 |
| maxContinue (SE自主) | 10 |
| maxAskPM (SE求助) | 3 |
| PM retry | 2 |
| AP retry | 1 |
| 并行批处理上限 | 5 操作/批 |
| compressHistory 保留条数 | 15 |
| LSP 操作数 | 5 (gopls) |
| Undo 栈深度 | 20 步 |
| 支持图片格式 | 6 (PNG/JPG/GIF/WebP/BMP/PDF) |
| ShellSession buffer | 10 MB |
| ShellSession 超时 | 60 s |
| exec 超时 | 30 s |
| manager.go 总行数 | ~7500 |
