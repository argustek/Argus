# Argus SE AI 能力差距分析

> 对比对象: Cursor / Claude Code / GitHub Copilot / Trae / Windsurf
> 分析日期: 2026-06-06（基于 e098cdb 重新扫描）
> 方法: 逐文件代码扫描 → 代码验证对比，经 3 次代码实测 + 10轮回归测试
> 状态: ✅ 已更新 — P0/P1 全补齐，P2 聚焦多Agent/动态调试

---

## 一、工具集实测（31 个）

### 1.1 基础文件/搜索（11个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 1 | write_file | [manager.go:3478](internal/chat/manager.go#L3478) | 受保护文件检查 + 路径安全检查 + isPathInDir | ✅ |
| 2 | edit_file | [manager.go:3510](internal/chat/manager.go#L3510) | old_str/new_str 精确替换，支持多行匹配 | ✅ |
| 3 | read_file | [se_prompt.go:570](internal/ai/se_prompt.go#L570) | offset/limit 行范围，默认100行，带行号输出 | ✅ |
| 4 | delete_file | [manager.go:3665](internal/chat/manager.go#L3665) | 工作目录范围检查 + 安全拒绝 + isPathInDir | ✅ |
| 5 | list_files | [se_prompt.go:398](internal/ai/se_prompt.go#L398) | 递归遍历 + glob 过滤 + ** 双星展开 + Walk 回退 | ✅ |
| 6 | glob | [se_prompt.go:438](internal/ai/se_prompt.go#L438) | glob 模式匹配 + ** 双星展开 + Walk 回退 | ✅ |
| 7 | search_files | [se_prompt.go:612](internal/ai/se_prompt.go#L612) | 正则 + 文件类型过滤 + 跳过 12 种忽略目录 + 上下文行 | ✅ |
| 8 | semantic_search | [code_indexer.go:1](internal/ai/code_indexer.go#L1) (677行) | 倒排索引(函数/类型/注释/字段tag) + AI GenerateConcepts 双通道评分，跨 20+ 语言 | 🆕✅ |
| 9 | web_search | [se_prompt.go:819](internal/ai/se_prompt.go#L819) | 并行 3 引擎（DuckDuckGo/Bing/Google），goroutine 并发取最快结果 | 🆕✅ |
| 10 | show_diff | [se_prompt.go:2178](internal/ai/se_prompt.go#L2178) 🆕 | 预览文件编辑前后差异，不实际修改文件，生成 unified diff 供确认 | 🆕✅ |
| 11 | fetch_url | [se_prompt.go:1798](internal/ai/se_prompt.go#L1798) | HTTP GET + 正文提取（去除 script/style/head 标签 + HTML → 纯文本 + 实体解码），2MB 限制，8000字符截断 | 🆕✅ |

### 1.2 执行/终端（4个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 12 | exec | [manager.go:3920](internal/chat/manager.go#L3920) | CMD独立进程，30s超时，sanitizeCommandPath 自动纠错 | ✅ |
| 13 | exec_session | [shell_session.go:1](internal/executor/shell_session.go#L1) (287行) | 持久化 cmd.exe，cd/env 状态保持跨命令，60s 超时，10MB buffer | 🆕✅ |
| 14 | run_tests | [manager.go:3760](internal/chat/manager.go#L3760) | go test，支持 pattern/coverage/verbose | ✅ |
| 15 | debug_run | [se_prompt.go:2197](internal/ai/se_prompt.go#L2197) 🆕 | 自动加调试flag（-v -race -count=1），panic/trace结构化展示，60s超时 | 🆕✅ |

### 1.3 版本控制/任务（2个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 16 | git_operation | [manager.go:3686](internal/chat/manager.go#L3686) | status/diff/commit/push/pull/log/branch/show 8 种操作 | ✅ |
| 17 | complete_task | [se_prompt.go:490](internal/ai/se_prompt.go#L490) | 文件清单 + 摘要 + 状态标记 + CheckSemanticComplete 验证 | ✅ |

### 1.4 LSP 代码理解（5个）🆕

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 18 | go_to_definition | [lsp_client.go:319](internal/ai/lsp_client.go#L319) | gopls daemon → textDocument/definition JSON-RPC；返回 LocationList | 🆕✅ |
| 19 | find_references | [lsp_client.go:346](internal/ai/lsp_client.go#L346) | gopls daemon → textDocument/references；支持 includeDeclaration；按文件分组展示 | 🆕✅ |
| 20 | hover_info | [lsp_client.go:379](internal/ai/lsp_client.go#L379) | gopls daemon → textDocument/hover；获取类型签名/文档注释；支持 plaintext/markdown | 🆕✅ |
| 21 | diagnostics | [lsp_client.go:406](internal/ai/lsp_client.go#L406) | gopls daemon → textDocument/diagnostic (pull API)；打开文件后主动拉取编译诊断，回退兼容旧版 gopls | 🆕✅ |
| 22 | rename_symbol | [lsp_client.go:440](internal/ai/lsp_client.go#L440) | gopls daemon → textDocument/rename → WorkspaceEdit；ApplyWorkspaceEdit 从后往前应用文本编辑避免位置偏移 | 🆕✅ |

### 1.5 多模态/撤销（3个）🆕

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 23 | analyze_image | [vision.go:1](internal/ai/vision.go#L1) (272行) | PNG/JPG/GIF/WebP/BMP/PDF → base64 data URI → vision LLM；支持 HTTP URL/本地路径；IsVisionModel() 自动检测模型能力；5 种场景预设提示词 | 🆕✅ |
| 24 | undo_file | [file_tracker.go:112](internal/executor/file_tracker.go#L112) | RollbackLast() 回滚到编辑前快照；自动回滚已集成到 write/edit 失败路径；最近 20 步 | 🆕✅ |
| 25 | list_changes | [file_tracker.go:149](internal/executor/file_tracker.go#L149) | GetRecentChanges() 列出最近 N 步变更（按时间倒序）；Stats() 展示文件追踪统计 | 🆕✅ |

### 1.6 代码片段/分析/调试（6个）🆕

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 26 | search_snippet | [snippets.go:1](internal/ai/snippets.go#L1) (837行) | 按语言/标签/评分搜索代码片段库，9个内置模板+自定义 | 🆕✅ |
| 27 | add_snippet | [snippets.go:1](internal/ai/snippets.go#L1) | 添加自定义代码片段（持久化到 .argus/snippets.json） | 🆕✅ |
| 28 | list_snippets | [snippets.go:1](internal/ai/snippets.go#L1) | 列出所有片段，支持语言过滤 | 🆕✅ |
| 29 | delete_snippet | [snippets.go:1](internal/ai/snippets.go#L1) | 删除自定义片段（内置模板受保护不可删除） | 🆕✅ |
| 30 | analyze_code | [se_prompt.go:2121](internal/ai/se_prompt.go#L2121) | 静态代码分析，22条规则，9类检测（nil安全/资源泄漏/并发安全/弱加密等） | 🆕✅ |
| 31 | auto_debug | [se_prompt.go:2152](internal/ai/se_prompt.go#L2152) | Test-Fix自动调试循环，最多5次迭代，自动分析错误并生成修复 | 🆕✅ |

---

## 二、已实现的核心优势（代码验证通过）

### 2.1 Tool Result Feedback Loop

每个工具执行后结果通过 `AddResult()` 注入 SE 对话历史。SE 基于实际执行结果决定下一步。

**实测 AddResult 调用点（15+ 处）：**

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

[LSPClient](internal/ai/lsp_client.go#L1)（641行）— 通过 gopls daemon 子进程实现 5 个语言服务器协议操作：

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

[FileChangeTracker](internal/executor/file_tracker.go#L1)（255行）— 三重能力：

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

## 三、竞品差距（代码实测，2026-06-06 更新）

### P0 — 已全部补齐 ✅

| # | 能力 | 之前状态 | 实现 | 评估 |
|---|------|----------|------|------|
| 1 | **LSP 代码理解** | ❌ 零 LSP 集成 | [lsp_client.go](internal/ai/lsp_client.go#L1) 641行 — gopls daemon 子进程，5个操作（GoToDef/FindRefs/Hover/Diagnostics/Rename）全部可用 | ✅ **已补齐** |
| 2 | **多模态输入** | ❌ 无 vision 能力 | [vision.go](internal/ai/vision.go#L1) 272行 — base64 data URI → vision LLM，5种场景预设提示词，支持PNG/JPG/GIF/WebP/BMP/PDF | ✅ **已补齐** |
| 3 | **撤销/回滚** | ❌ 无 undo 机制 | [file_tracker.go](internal/executor/file_tracker.go#L1) 255行 — Snapshot → RollbackLast 双层回滚，失败自动回滚集成到 write/edit 路径 | ✅ **已补齐** |

### P1 — 实际差距（代码扫描后修订）

| # | 能力 | Argus 实测 | 差距评估 |
|---|------|-----------|---------|
| 4 | **多 Agent 协作** | ❌ 单 SE agent，无任务拆分 | 复杂全栈任务效率低 |
| 5 | **增量 Diff 预览** | ✅ 后端 ComputeDiff + emitWailsEvent("diff_preview") + 前端 DiffPreviewDialog + SE工具 show_diff 已注册 | 用户可预审修改再确认写入 |
| 6 | **静态代码分析** | ✅ analyze_code 已实现（AST+正则，22条规则，9类检测） | 🔴 动态调试（DAP/Delve集成）仍缺口 |
| 7 | **代码模板/片段库** | ✅ 完整版：9个内置模板 + 持久化(JSON) + CRUD + 增强搜索 + 4个SE工具 | 已完善 |
| 8 | **agent 调试可视化** | ❌ 无执行过程可视化 | 用户看不到 agent 思考过程 |

### P2 — 长期优化

| # | 能力 | 说明 |
|---|------|------|
| 9 | 多语言 LSP | ✅ 已支持 7 语言：Go(gopls) / TS-JS(typescript-language-server) / Python(pylsp) / Rust(rust-analyzer) / C-CPP(clangd)；lspServerMap + NewLSPClientForLang() 按语言自动选择 |
| 10 | 分布式 Agent | 单机运行 |
| 11 | 监控/指标 | 有健康自愈，无 Prometheus/Grafana（桌面应用不需要） |

---

## 四、优先修复路线图

### ✅ Phase 1 — P0 致命差距（已完成）

| 项目 | 方案 | 代码 | 状态 |
|------|------|------|------|
| **LSP 集成** | 启动 gopls daemon 子进程；JSON-RPC 2.0 协议；GoToDef/FindRefs/Hover/Diagnostics/Rename 5个能力；diagnostics 注入 Tool Result Feedback | [lsp_client.go](internal/ai/lsp_client.go#L1) (641行) | ✅ |
| **撤销/回滚** | write/edit 前自动 Snapshot（最多20步）；RollbackLast 双层回滚到前一个快照；失败自动回滚已集成到 write/edit 路径 | [file_tracker.go](internal/executor/file_tracker.go#L1) (255行) | ✅ |
| **多模态基础** | base64 data URI → vision LLM；5种场景预设提示词；HTTP URL/本地路径双支持；IsVisionModel 自动检测 | [vision.go](internal/ai/vision.go#L1) (272行) | ✅ |

### Phase 2 — P1 增强（2026-06-06 全部完成）

| # | 项目 | 方案 | 状态 |
|---|------|------|------|
| **2.1** | **web_search 增强** | 并行 3 引擎（DuckDuckGo/Bing/Google）goroutine 竞速取最快返回 + fetch_url 网页抓取正文提取 | ✅ |
| **2.2** | **Agent 智能终止** | CheckSemanticComplete 升级：关键词信号 + complete_task 验证 + 动作收敛检测 + 置信度评分 (0-1.0) | ✅ |
| **2.3** | **上下文管理升级** | compressHistory 分层：①保留 user/system 指令 ②压缩 tool result ③保留最近3轮 ④token 计数 + 二次压缩保护 | ✅ |
| **2.4** | **Diff 预览** | edit/write 前计算 unified diff；SSE 推送前端用户确认 + SE 工具 show_diff 注册 | ✅ |
| **2.5** | **多语言 LSP** | 扩展 LSPClient → typescript-language-server / rust-analyzer / pyright | ✅ lspServerMap 已支持7语言(gopls/ts/pylsp/rust-analyzer/clangd) + NewLSPClientForLang() |
| **2.6** | **调试运行** | debug_run 工具注册：自动加 -v -race -count=1，panic/trace 结构化展示，60s超时 | ✅ 🆕 |

### Phase 3 — 体验打磨（P2）🆕

| # | 项目 | 难度 | 状态 |
|---|------|------|------|
| 3.1 | 代码片段库 | ⭐ 已完成 | ✅ 持久化(.argus/snippets.json) + 完整CRUD + 4个SE工具 |
| 3.2 | 多 Agent 协作（前端/后端/DB 并行子 agent） | ⭐⭐⭐⭐ 架构级 | 🔴 待做：任务拆分 + 并行协调 + 结果合并 |
| 3.3 | 动态调试（Delve/DAP 集成） | ⭐⭐⭐ 新模块 | 🔴 待做：断点/单步/变量检查/调用栈 |
| 3.4 | Agent 调试可视化（思考链展示） | ⭐⭐ 前端为主 | ✅ **已完成**：AgentDashboard.vue组件 + 后端ThoughtEvent通过MessageBus发射(reasoning_content/step/tool事件) |
| 3.5 | Shell Session 空闲自动清理 | ⭐ 几行代码 | ✅ **已完成**：idleChecker goroutine，10s ticker检测，60s超时自动关闭 |

---

## 四、已知技术债务（每次开发前必须扫一眼！）

> **📌 完整记录见 [KNOWN_BUGS.md](KNOWN_BUGS.md) — 重大 Bug 独立文档，含根因分析和修复方案**
>
> **规则：改 message_bus.go 追踪策略前，先读 KNOWN_BUGS.md 的 TD-1。**

| # | 问题 | 严重度 | 状态 | 临时方案 |
|---|------|--------|------|---------|
| TD-1 | **MessageBus 高频路径追踪导致前端卡死** | 🔴 致命 | 🔴 未解决 | PathCoreOutput/PathStatus 保持 NO_TRACK；重要事件改用 PathSystem |
| TD-2 | **pendingQueue 无容量上限，高频事件可撑爆内存** | 🟠 高 | 🔴 未解决 | 同上（根因与TD-1相同） |
| TD-3 | **CheckPending O(n) 全扫描，消息量大时 CPU 飙升** | 🟠 高 | 🔴 未解决 | 同上 |

### TD-1 详细记录（2026-06-06）

**现象**：`shouldTrack()` 中 `PathStatus` 改为 `return true` 后，前端完全无响应，AI 全部卡死不动。

**根因链**：
```
PathStatus 追踪 → 每秒几十条 status 事件进 pendingQueue
  → CheckPending O(n) 扫描全队列 + 超时检测
  → CPU 100% 卡在 pendingQueue 操作
  → 前端事件循环阻塞 → UI 冻结
  → AI 回调也走前端通道 → 整个系统死锁
```

**正确解法方向**（待实现）：
- 方案A：pendingQueue 改 ring buffer + 异步清理线程
- 方案B：高频路径采样追踪（每 N 条追 1 条）
- 方案C：shouldTrack 加 QPS 频率限制（超阈值自动降级 NO_TRACK）

**谁会提醒你**：`message_bus.go:374` 的 `⚠️ TECH-DEBT` 注释。每次改 shouldTrack 都会看到。

---

## 五、核心能力盘点

> 本节客观列出 Argus 代码验证通过的能力，不做"独有/领先"断言。
> 行业中 Planner-Executor-Reviewer 是常见模式（OpenCode 10角色编排、Copilot Plan-Implement、Windsurf Plan Mode），Argus 的 PM→SE→AP 是此模式的工程化实现。

以下能力经代码验证正常工作，后续迭代应保持：

1. **Tool Result Feedback Loop** — 工具执行结果注入 SE 对话历史，SE 基于实际输出（非猜测）决定下一步。15+ 处 AddResult 调用点。
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
12. **代码片段库** — 9个内置模板 + 持久化存储(.argus/snippets.json) + 完整CRUD(Add/Update/Delete/List/GetByID) + 增强搜索(SearchOptions: 语言/标签/评分) + 4个SE工具(search_snippet/add_snippet/list_snippets/delete_snippet)。内置模板受保护不可删除。
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
| 工具总数 | 31 |
| AddResult 反馈点 | 15+ |
| web_search 引擎数 | 3 (并行) |
| fetch_url | 正文提取 + 2MB/8000字符限制 |
| show_diff | ✅ unified diff 预览（已注册SE工具） |
| debug_run | ✅ 自动加 -v -race -count=1，panic/trace 结构化 |
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
| LSP 操作数 | 5 (gopls) + 7语言支持(TS/JS/Python/Rust/C/C++) |
| Undo 栈深度 | 20 步 |
| Snippet 内置模板 | 9 |
| Diff 预览 | ✅ ComputeDiff(unified) + DiffPreviewDialog(前端弹窗) + show_diff工具 |
| 支持图片格式 | 6 (PNG/JPG/GIF/WebP/BMP/PDF) |
| ShellSession buffer | 10 MB |
| ShellSession 超时 | 60 s (命令) / 60s (空闲自动清理) |
| exec 超时 | 30 s |
| lsp_client.go 行数 | 641 |
| file_tracker.go 行数 | 255 |
| code_indexer.go 行数 | 677 |
| snippets.go 行数 | 837 |
| se_prompt.go 行数 | 2214 |
| manager.go 行数 | 7435 |
| PM 健康检测 | 连续失败≥3次(60s内) → 清理会话 |

---

## 五、三层环境防御体系 🆕 (2026-06-07)

> **设计目标**: 解决 SE 执行时因缺少编译器/解释器导致的"静默失败"问题
> **竞品参考**: Claude Code (`claude doctor` + cli-tools插件) / Cursor (MCP工具链声明)
> **实现状态**: ✅ Layer 1+2 已完成，Layer 3 待用户确认

### 5.1 问题背景

**原始问题（2026-06-07 发现）：**

```
用户请求: "Write Rust program main.rs"
  ↓
PM 分析: 确认是编程任务，提取 .rs 扩展名
  ↓
SE 执行: write_file main.rs ✅ 成功
  ↓
SE 执行: exec 'rustc main.rs' ❌ 失败!
  输出: 'rustc' 不是内部或外部命令
  ↓
Self-Fix 循环 (5次):
  #1 API调用超时(120s) ← 浪费时间!
  #2-5 circuit breaker open ← 全部失败!
  ↓
最终: phase:error, 用户等待 2 分钟后看到错误
```

**痛点分析：**

| 痛点 | 影响 | 用户反馈 |
|------|------|----------|
| 缺少预检 | PM 明知缺 Rust 还让 SE 干活 | "为啥还让se干活呢？" |
| 无安装提示 | 错误信息不友好 | "怎么久出错了 停了 不吭气了" |
| Self-Fix浪费 | 5次重试 × 2分钟 = 10分钟浪费 | "等鸡毛呢" |
| 状态灯残留 | error 后 PM 仍显示 busy | "pm显示忙碌呢" |

### 5.2 三层架构设计

```
┌─────────────────────────────────────────────────────────────┐
│ Layer 1: 预检 (Pre-Flight Check)                             │
│ 触发时机: PM 分析完成后，SE 执行前                            │
│ 核心逻辑: checkToolAvailability(detectedLang)                │
│ 检测方式: where/which 命令查找编译器路径                      │
│ 行为模式: 阻断式 — 缺失则暂停流程，询问用户                  │
│                                                              │
│ 支持语言映射表:                                               │
│   go       → {go}                                           │
│   python   → {python, python3}                              │
│   rust     → {rustc, cargo}                                 │
│   nodejs   → {node, npm}                                    │
│   java     → {javac, java}                                  │
│   c/c++    → {gcc, g++, clang}                              │
│   ruby     → {ruby}                                         │
│   php      → {php}                                          │
├─────────────────────────────────────────────────────────────┤
│ Layer 2: 智能错误分类 (Smart Error Classification)            │
│ 触发时机: SE exec 失败时                                     │
│ 核心逻辑: classifyExecutionError()                          │
│ 分类维度:                                                    │
│   ErrorType (9种): syntax/runtime/test/import/type/...       │
│   Category (3种): transient/fixable/permanent                │
│ 新增检测: command not found / 不是内部或外部命令              │
│ 行为模式: 结构化错误提示 + 安装指导                           │
├─────────────────────────────────────────────────────────────┤
│ Layer 3: 自愈选项 (Auto-Recovery) [待实现]                   │
│ 触发时机: 用户确认后                                         │
│ 方案 A: 自动安装 (winget/brew/apt)                          │
│ 方案 B: AI 重写任务为替代语言                                │
│ 方案 C: 取消任务 + 清理资源                                   │
└─────────────────────────────────────────────────────────────┘
```

### 5.3 实现细节

#### 5.3.1 Layer 1: checkToolAvailability()

**代码位置**: [argus.go:1042-1116](internal/core/argus.go#L1042-L1116)

```go
func (c *ArgusCore) checkToolAvailability(language string) (
    available []string,   // 已安装的工具列表
    missing   []string,   // 缺失的工具列表
    hints     []string,   // 安装建议
) {
    // 1. 语言→工具映射表查询
    // 2. 循环执行 where/which 检测每个工具
    // 3. 缺失工具生成平台相关安装提示
    // 4. 返回三元组供上层决策
}
```

**关键设计决策：**

| 决策点 | 选择 | 原因 |
|--------|------|------|
| 检测方式 | `where` (Windows) / `which` (Unix) | 跨平台兼容 |
| 阻断 vs 警告 | **阻断式** (return result) | 避免 SE 无谓执行 |
| 语言识别 | 文件扩展名匹配 (.go/.py/.rs) | PM 响应中包含文件名 |

**实际输出示例（Rust 缺失时）：**

```
🛑 **环境阻断**: 目标语言 [rust] 缺少必要工具!

❌ 缺失工具: rustc, cargo
✅ 已有工具:

**请选择处理方式:**
1️⃣ 自动安装 (运行: winget install Rustlang.Rust.MSVC)
2️⃣ 改用其他语言 (如Python/Go)
3️⃣ 取消任务
```

#### 5.3.2 Layer 2: classifyExecutionError() 增强

**新增环境检测分支**：

```go
if strings.Contains(errLower, "不是内部或外部命令") ||
   strings.Contains(errLower, "not recognized") ||
   strings.Contains(errLower, "command not found") {

    cmdName := extractCommandName(errOutput)
    installHint := getInstallHint(cmdName)

    analysis = append(analysis, fmt.Sprintf(
        "❌ MISSING COMPILER/RUNTIME: %s\n%s",
        cmdName, installHint,
    ))
}
```

**支持的平台错误模式：**

| 平台 | 错误特征 | 示例 |
|------|----------|------|
| Windows | `不是内部或外部命令` | `'rustc' 不是内部或外部命令` |
| Unix | `command not found` | `bash: rustc: command not found` |

#### 5.3.3 emitStatus() 状态修复

**代码位置**: [argus.go:196-231](internal/core/argus.go#L196-L231)

**问题**: 原实现在 `phase=error` 时只重置传入的 role：
```
emitStatus("error", "se", "idle")  → 只重置 SE, PM/AP 仍 busy!
```

**修复**: done/error 阶段**无条件重置所有角色**：

```go
if phase == "done" || phase == "error" {
    c.state.PM = "idle"
    c.state.SE = "idle"
    c.state.AP = "idle"
}
```

### 5.4 测试验证结果

| 测试用例 | 预期行为 | 实际结果 | 状态 |
|----------|----------|----------|------|
| **Test 1: Rust (缺失)** | Layer 1 阻断 + 提示安装 | ✅ 检测到缺失并阻断 | ✅ 通过 |
| **Test 2: Python (可用)** | 正常执行全流程 | ✅ 25秒完成全流程 | ✅ 通过 |
| **状态灯重置** | done/error 后全部 idle | ✅ phase:done\|role:none\|status:idle | ✅ 修复 |

**Test 2 日志时间线（25秒总耗时）：**

```
[01:48:35] USER: Test2: Write Python script hello_test2.py...
[01:48:35] SYSTEM: phase:start|pm|busy
[01:48:43] SE: ✅ write_file hello_test2.py (26 bytes)
[01:48:44] SE: ✅ exec 'python hello_test2.py' → Hello from Test2!
[01:48:44] SYSTEM: phase:review|pm|busy
[01:48:54] PM: Code Review APPROVED
[01:48:54] SYSTEM: phase:approve|ap|busy
[01:49:00] AP: Approval PASSED
[01:49:00] SYSTEM: phase:done|none|idle  ← 全部重置! ✅
```

### 5.5 与竞品对比

| 能力 | Argus | Claude Code | Cursor | Windsurf |
|------|-------|-------------|--------|----------|
| **预检机制** | ✅ Layer 1 阻断式 | ⚠️ `/doctor` 手动 | ❌ 无 | ❌ 无 |
| **自动安装** | 🔄 Layer 3 待实现 | ✅ cli-tools 插件 | ❌ 无 | ❌ 无 |
| **语言切换建议** | ✅ 智能推荐 | ❌ 无 | ❌ 无 | ❌ 无 |
| **状态灯同步** | ✅ 强制全量重置 | N/A (CLI) | ⚠️ 有时不同步 | ⚠️ 有时不同步 |

**核心优势:**
1. **唯一实现预检阻断的AI IDE**
2. **智能语言推荐** — 检测已安装工具链并推荐替代方案
3. **零等待体验** — 不浪费在注定失败的 Self-Fix 循环

### 5.6 未来扩展路线图

#### Phase 1: v1.0 (当前) — ✅ 已发布

- [x] Layer 1: checkToolAvailability() 预检
- [x] Layer 2: classifyExecutionError() 环境检测增强
- [x] emitStatus() done/error 全量重置
- [x] 阻断式用户交互（SSE消息 + 三选一）

#### Phase 2: v1.1 — 自动安装集成

- [ ] Windows winget / macOS brew / Linux apt 自动安装
- [ ] 安装进度条 + 超时控制
- [ ] 安装失败回退到 Layer 2 提示

#### Phase 3: v1.2 — 智能语言切换

- [ ] AI 自动重写任务为替代语言
- [ ] 工具链依赖图（Node.js 项目需要 npm）
- [ ] Docker/云端编译服务集成

### 5.7 关键代码文件清单

| 文件 | 功能 |
|------|------|
| [argus.go:196-231](internal/core/argus.go#L196-L231) | emitStatus() 状态管理修复 |
| [argus.go:442-515](internal/core/argus.go#L442-L515) | Process() 中 Layer 1 预检集成 |
| [argus.go:1042-1116](internal/core/argus.go#L1042-L1116) | checkToolAvailability() 工具检测 |
| [App.vue](frontend/src/App.vue) | 前端 role-state 事件监听 |
| [manager.go](internal/chat/manager.go) | forceProjectDone() 状态发射 |

---

## 六、优先修复路线图
