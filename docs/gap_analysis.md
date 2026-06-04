# Argus SE AI 能力差距分析

> 对比对象: Cursor / Claude Code / GitHub Copilot / Trae
> 分析日期: 2026-06-04
> 方法: 逐文件代码扫描（非文档推断）
> 状态: 代码实测定稿

---

## 一、工具集实测（14 个）

| # | 工具 | 代码位置 | 实现细节 | 状态 |
|---|------|---------|---------|------|
| 1 | write_file | [manager.go:3478](internal/chat/manager.go#L3478) | 受保护文件检查 + 路径安全检查 + isPathInDir | ✅ |
| 2 | edit_file | [manager.go:3510](internal/chat/manager.go#L3510) | old_str/new_str 精确替换，支持多行匹配 | ✅ |
| 3 | read_file | [se_prompt.go:370](internal/ai/se_prompt.go#L370) | offset/limit 行范围，默认100行，带行号输出 | ✅ |
| 4 | delete_file | [manager.go:3665](internal/chat/manager.go#L3665) | 工作目录范围检查 + 安全拒绝 + isPathInDir | ✅ |
| 5 | list_files | [se_prompt.go:398](internal/ai/se_prompt.go#L398) | 递归遍历 + glob 过滤 + ** 双星展开 + Walk 回退 | ✅ |
| 6 | glob | [se_prompt.go:438](internal/ai/se_prompt.go#L438) | glob 模式匹配 + ** 双星展开 + Walk 回退 | ✅ |
| 7 | search_files | [se_prompt.go:612](internal/ai/se_prompt.go#L612) | 正则 + 文件类型过滤 + 跳过 12 种忽略目录 + 上下文行 | ✅ |
| 8 | exec | [manager.go:3920](internal/chat/manager.go#L3920) | CMD独立进程，30s超时，sanitizeCommandPath 自动纠错 | ✅ |
| 9 | exec_session | [shell_session.go](internal/executor/shell_session.go#L1) (287行) | 持久化 cmd.exe，cd/env 状态保持跨命令，60s 超时，10MB buffer | 🆕✅ |
| 10 | run_tests | [manager.go:3760](internal/chat/manager.go#L3760) | go test，支持 pattern/coverage/verbose | ✅ |
| 11 | git_operation | [manager.go:3686](internal/chat/manager.go#L3686) | status/diff/commit/push/pull/log/branch/show 8 种操作 | ✅ |
| 12 | web_search | [se_prompt.go:563](internal/ai/se_prompt.go#L563) | DuckDuckGo HTML 解析，提取搜索结果摘要 | ⚠️ 单引擎 |
| 13 | semantic_search | [code_indexer.go](internal/ai/code_indexer.go#L1) (677行) | 倒排索引(函数/类型/注释/字段tag) + AI GenerateConcepts 双通道评分，跨 20+ 语言 | 🆕✅ |
| 14 | complete_task | [se_prompt.go:490](internal/ai/se_prompt.go#L490) | 文件清单 + 摘要 + 状态标记 + CheckSemanticComplete 验证 | ✅ |

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

### 2.7 语义搜索引擎

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

### 2.8 消息总线 + SSE 推送

- msgBus 历史记录 + SSE 实时推送
- 前端任意时刻重连 → 全量同步
- pendingQueue 忙时消息暂存（27处引用）
- 4 个 API 端点: send/history/pending/clear-pending

### 2.9 上下文压缩

[compressHistory()](internal/ai/se_prompt.go#L710)：
- history > 20 条时触发
- 保留最近 15 条活跃上下文
- 旧消息提取 tool name + result preview → 拼接为 `[上下文摘要]` system 消息插入头部

### 2.10 健康恢复机制

| 机制 | 触发条件 | 代码位置 | 动作 |
|------|----------|---------|------|
| PM 不健康检测 | 连续 API 失败 ≥ 3 次（60s 内） | manager.go:1227 | 清理会话 + 重置健康状态 |
| isProcessing 超时 | 锁持有 > 60s | manager.go:981 | 强制清理旧任务 + 复位锁 |
| 消息队列阻塞检测 | busy 时新消息到达 | manager.go:1005 | 暂存 pendingQueue，空闲时自动处理 |
| panic recover | SE/PM/AP 任意层 panic | 多处 `defer recover()` | 记录日志 + 优雅降级 |

---

## 三、竞品差距（代码实测）

### P0 — 影响核心编码能力

| # | 能力 | Cursor/Claude Code | Argus 实测 | 差距评估 |
|---|------|-------------------|-----------|---------|
| 1 | **LSP 代码理解** | go-to-definition / find-references / hover info / rename-symbol / diagnostics（gopls daemon） | ❌ 零 LSP 集成，纯文本搜索+正则 | **最大硬伤** — 无法精确理解类型关系、无法做 safe rename、无法获取实时编译诊断 |
| 2 | **多模态输入** | 截图→UI代码 / 设计稿→组件 / PDF→解析 | ❌ 无 image/vision/pdf 输入能力 | UI 开发场景完全不可用 |
| 3 | **撤销/回滚** | 每次 edit 可 undo，可回滚到任意 checkpoint | ❌ 无 undo 机制，无编辑前快照 | 改错代码只能靠 SE 自己修复，无安全网 |

### P1 — 影响效率体验

| # | 能力 | 竞品 | Argus 实测 | 差距评估 |
|---|------|------|-----------|---------|
| 4 | **Agent 智能终止** | LLM 自主判断"任务完成了"，精准停手 | ⚠️ CheckSemanticComplete() 存在但粗糙：仅关键词匹配("完成"/"done"/"finished")，无置信度评分，常误判或漏判 | 导致 continueSETask 无效空转或过早结束 |
| 5 | **上下文管理** | 分层摘要（tool结果压缩→决策保留→指令锁定）+ 滑动窗口 + token 计数 | ⚠️ compressHistory() 固定留15条+简单拼接摘要，无优先级分层，无 token 计数 | 长任务（50+轮）会丢失早期关键决策上下文 |
| 6 | **web_search 增强** | 多引擎聚合 + 网页全文抓取 + RAG 引用 | ⚠️ 仅 DuckDuckGo HTML 单引擎，无页面抓取，无引用来源 | 技术文档查询经常不够准确 |
| 7 | **文件变更追踪** | fsnotify 实时监听 + 编辑前自动 snapshot + 冲突检测 | ⚠️ DB schema 有 files_snapshot 字段设计，但 **无 watcher 实现** | 多 agent 并发编辑时可能冲突 |
| 8 | **SE→USR 直连** | SE 可以直接向用户汇报进度和提问 | ⚠️ argus.go 已全部改为 se_to_pm，但 manager.go 仍有 addSEToUserMsg() + Source:"se_to_user" 路径 | 路由策略不一致，部分场景 SE 可能绕过 PM |

### P2 — 锦上添花

| # | 能力 | 竞品 | Argus 实测 | 差距评估 |
|---|------|------|-----------|---------|
| 9 | **多 agent 协作** | 前端agent + 后端agent + DBA agent 同时工作 | ❌ 单 SE agent | 复杂全栈任务效率低 |
| 10 | **增量 diff 预览** | 编辑前显示精确 diff，用户确认后才写入 | ❌ 直接写入，无 diff 预审 | 用户对修改缺乏掌控感 |
| 11 | **交互式调试** | 断点设置 / 变量查看 / 单步执行 | ❌ 只能 exec 运行看输出 | 调试复杂 bug 困难 |
| 12 | **代码模板/片段库** | 常用模式一键生成（CRUD API / auth middleware 等） | ❌ 无 | 重复模式每次从头写 |

---

## 四、优先修复路线图

### Phase 1 — 补齐致命短板（P0）

| 顺序 | 项目 | 方案 | 预计工作量 | 影响 |
|------|------|------|-----------|------|
| **1.1** | **LSP 集成** | 启动 gopls daemon 作为子进程；实现 GoToDef/FindRefs/Hover/Diagnostics/Rename 5 个基础能力；将 diagnostics 注入 Tool Result Feedback | 大（2-3天） | **质变** — 从"文本搜索"升级到"语义理解"，safe rename 成为可能 |
| **1.2** | **撤销/回滚** | 每次 write/edit 前 snapshot 目标文件内容；维护 undo stack（最近20步）；实现 undo_tool 或自动 rollback-on-failure | 中（1天） | **安全感** — 敢大胆改，改错了能回 |
| **1.3** | **多模态基础** | 接入 vision-capable LLM（如 gpt-4o）；实现 screenshot→code 和 image-description→code 两个工具 | 中（1-2天） | UI 场景可用 |

### Phase 2 — 效率提升（P1）

| 顺序 | 项目 | 方案 | 预计工作量 | 影响 |
|------|------|------|-----------|------|
| **2.1** | **Agent 智能终止** | CheckSemanticComplete 升级为：LLM confidence score + 最近N轮 action 有效性评分 + 工具调用收敛检测（连续 read/search 无 write/exec = 准备收尾） | 小（半天） | 减少 30-50% 无效空转 |
| **2.2** | **上下文管理升级** | compressHistory 改为分层：① 保留 user/system 指令完整 ② 压缩 tool result 为 1 行摘要 ③ 保留最后 3 轮完整对话 ④ 加 token 计数器控制总长度 | 中（1天） | 长任务不再丢失关键信息 |
| **2.3** | **统一 SE 路由** | 清理 manager.go 中残留的 se_to_user 路径；统一为全部走 se_to_pm → PM 审核后转发 USR | 小（半天） | 消除路由不一致 bug |
| **2.4** | **web_search 增强** | 加入 Google/Bing fallback + 简单网页抓取（正文提取）+ 来源 URL 引用 | 小（半天） | 文档查询准确度提升 |

### Phase 3 — 体验打磨（P2）

| 顺序 | 项目 | 方案 | 预计工作量 |
|------|------|------|-----------|
| 3.1 | 文件变更 watcher | fsnotify 监听 workDir；编辑前 auto-snapshot；冲突检测 | 中 |
| 3.2 | Diff 预览 | edit/write 前计算 unified diff；通过 SSE 推送前端展示；用户确认才执行 | 中 |
| 3.3 | 代码片段库 | JSON/YAML 模板文件 + semantic_search 匹配 + SE prompt 引用 | 小 |

---

## 五、架构优势总结（不应丢失）

以下能力经代码验证确实领先于大多数同类产品，后续迭代应保持并强化：

1. **Tool Result Feedback Loop** — 不是"AI猜结果"，而是"AI看到真实执行结果再决定"。这是与普通 chatbot 的本质区别。
2. **Auto-Fix 三级降级** — SE自修 → SE继续尝试 → PM介入，每级有次数上限和超时保护。
3. **7层防死循环** — 从空actions到isProcessing超时，覆盖了所有已知死循环模式。
4. **9类错误分析 + 3类重试分类** — 不是简单"报错了"，而是精确到 File:Line:Column + SuggestedFix + ExampleFix。
5. **并行只读批处理** — 5个 read/search 操作同时跑，速度提升 2-5x。
6. **双通道语义搜索** — 关键词倒排 + AI概念融合，不需要 embedding API 就能实现语义级代码查找。
7. **持久化 Shell Session** — 多步构建（cd → make → ./test）不再是噩梦。

---

## 六、数据速查

| 指标 | 数值 |
|------|------|
| 工具总数 | 14 |
| AddResult 反馈点 | 30+ |
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
| ShellSession buffer | 10 MB |
| ShellSession 超时 | 60 s |
| exec 超时 | 30 s |
| CodeIndexer 行数 | 677 |
| ShellSession 行数 | 287 |
| manager.go 总行数 | ~6500 |
