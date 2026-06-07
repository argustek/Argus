# Argus 编码调试能力差距分析报告 v0.7.0

> **版本**: v0.7.0
> **日期**: 2026-06-08
> **对比对象**: Cursor, Windsurf, Cline, Trae IDE, OpenCode

---

## 1. 当前能力盘点 (v0.7.0)

### 已有能力

| 能力 | 状态 | 说明 |
|------|------|------|
| SE 工具集 | ✅ 31 个工具 | read/write/edit/exec/debug_run/auto_debug/analyze_code 等 |
| PM-SE-AP 三角色协作 | ✅ 核心优势 | 任务分解→执行→审核，业界独有 |
| LSP 集成 | ✅ 基础 | Go/TS/JS/Python/Rust/C/C++ 的 hover/completion/diagnostic |
| AutoDebug | ✅ Go 专用 | 跑测试→AI分析错误→生成修复→重跑循环（最多3轮） |
| CodeAnalyzer | ✅ Go AST | nil panic/越界/资源泄漏/并发安全/弱加密等 9 类静态分析 |
| 文档处理引擎 | ✅ 完整 | PDF读写(OCR)/Word读写/文档比较 + 工具自举 |
| Post-Execution Summary | ✅ 新增 | SE 执行后自动生成自然语言总结 |
| 语义搜索 + 代码索引 | ✅ | 按功能意图查找代码 |
| 并行搜索引擎 | ✅ | 3 引擎竞速获取最新信息 |
| Per-Role Model Config | ✅ | PM/SE/AP 可配不同 AI 模型 |

---

## 2. 核心差距分析 (P0 — 严重限制编码调试能力)

### 2.1 无结构化错误分类

**现状**:
```
SE exec("go build .") → 收到原始 stderr 字符串
"./main.go:45:12: syntax error: unexpected newline, expecting comma or }"
SE 只能自己猜：哦，可能是语法错误？在第45行？
```

**竞品做法**:
```json
{
  "error_type": "SYNTAX_ERROR",
  "file": "main.go",
  "line": 45,
  "column": 12,
  "code_context": "func login(user User) {\n    if user != nil\n",
  "suggested_fix": "在第 45 行添加 && user.Token != ''"
}
```

**影响**: SE 修复效率低 3-5x，经常误判错误类型，整文件重写而非精确修复。

**投入评估**: 低（纯正则+解析器，无新架构依赖）
**收益评估**: 高（直接提升所有编译/运行时错误的处理速度）

---

### 2.2 无真正的 Debugger

**现状**:
- 只有 `exec` 跑命令看输出
- 运行时 bug（panic/死锁/竞态）只能靠手动加 log → 重跑 → 再加 log 循环
- 无法查看变量值、调用栈、goroutine 状态

**竞品支持**:
- 断点调试（条件断点、命中断点执行表达式）
- 单步执行（Step Over / Step Into / Step Out）
- 变量检查（局部变量、watch 表达式、展开结构体）
- 调用栈查看（当前帧、切换帧、查看上层变量）
- Goroutine/线程列表（查看所有协程状态）

**影响**: 复杂运行时 bug 调试时间从竞品的 1-2 分钟拉长到 10-30 分钟。

**投入评估**: 高（需要集成 delve/gdb/LLDB 协议）
**收益评估**: 高（解决最痛的调试场景）

---

### 2.3 edit_file 是字符串替换，非 AST 感知

**现状**:
```go
// edit_file("main.go", old_string, new_string)
// 精确文本匹配替换 — 如果空格/缩进变了就匹配不上
// 无法跨文件追踪引用
```

**竞品做法**:
```go
// AST 级编辑
RenameSymbol("Login", "Authenticate")     // 自动更新所有 12 个文件的 23 处调用
ExtractMethod("auth.go:50-80", "validateUser") // 提取方法并更新所有调用点
ChangeSignature(addParam="role string")   // 自动更新所有调用处的参数列表
FindAllReferences("UserRepository")       // 找到所有使用位置（含接口实现）
```

**影响**: 接口变更/重构时漏改调用点 → 编译错误 → 额外修复轮次。

**投入评估**: 中（可基于已有 LSP references/rename 扩展）
**收益评估**: 高（重构场景效率提升 5-10x）

---

### 2.4 测试结果未结构化解析

**现状**:
```
SE exec("go test -v ./...") → 拿到几百行原始文本
--- FAIL: TestLogin (0.00s)
    main_test.go:23: Expected <200>, got <401>
SE 自己在文本中找哪个测试失败了、期望值是什么...
```

**竞品做法**:
```json
{
  "summary": { "total": 47, "passed": 45, "failed": 2, "skipped": 0 },
  "failures": [{
    "test_name": "TestLogin",
    "file": "main_test.go",
    "line": 23,
    "expected": "200",
    "actual": "401",
    "source_func": "Login() in auth.go:50-80",
    "assertion_type": "Equal"
  }]
}
```

**影响**: 测试失败后定位慢，无法精准关联源码位置。

**投入评估**: 低（`go test --json` / `pytest --json` 解析即可）
**收益评估**: 中高

---

### 2.5 交互式终端能力不足

**现状**:
- 每次 `exec` 是独立进程，cd/env 不保持
- `exec_session` 有基础持久化 shell，但缺少 tab 补全、命令历史、多窗口

**竞品做法**:
- 持久化 shell session（完整 bash/zsh/powershell）
- Tab 补全（文件名、命令、参数）
- 命令历史（上下键翻阅、Ctrl+R 搜索）
- 多终端标签页/分屏
- 语法高亮输出

**影响**: 多步构建/调试流程需要反复设置环境，打断 SE 思路。

**投入评估**: 中低（增强现有 exec_session）
**收益评估**: 中

---

## 3. 明显影响开发体验的差距 (P1)

| # | 差距 | 我们 | 竞品 | 投入 | 收益 |
|---|------|------|------|------|------|
| 6 | **无 File Watcher** | 不知道外部文件变化 | fsnotify 实时监听，自动刷新上下文 | 低 | 中 |
| 7 | **LSP 能力浅层** | hover/completion/diagnostic | 缺 references/rename/call hierarchy/code lens | 中 | 高 |
| 8 | **无 MCP 协议** | 工具全部内置硬编码 | 动态接入任意工具（数据库/CI/CD/云服务） | 高 | 极高（长期） |
| 9 | **无浏览器工具** | fetch_url 抓内容 | 打开浏览器/截图/点击元素/DOM读取 | 中 | 中（前端项目刚需） |
| 10 | **无 Docker 工具** | 手动 docker 命令 | compose up/down/logs 一键操作 + 容内 exec | 中 | 中（后端项目刚需） |
| 11 | **Context Window 无智能管理** | 全量塞给 LLM | 相关性排序/滑动窗口/token预算分配 | 高 | 高（省 token） |

---

## 4. 锦上添花但差异化明显 (P2)

| # | 差距 | 说明 |
|---|------|------|
| 12 | **Git Blame / 历史 AI 分析** | "这个 bug 是哪次提交引入的" — git bisect + AI 分析 |
| 13 | **代码审查 / PR Comment** | 自动 review MR 并写评论 |
| 14 | **Profiling / 性能分析** | pprof 集成、火焰图生成、瓶颈定位 |
| 15 | **数据库工具** | 连接 DB、执行查询、查看 schema、ER 图 |
| 16 | **多光标并行编辑** | 同时改多个文件的相同模式 |
| 17 | **代码生成预览** | 修改前先展示 diff，确认后再应用（show_diff 已有雏形） |
| 18 | **国际化 i18n 支持** | 多语言 UI、多语言注释/文档生成 |

---

## 5. 综合评分雷达图数据

### v0.7.0 vs 竞品（满分 10 分）

| 维度 | Argus v0.7.0 | Cursor | Windsurf | Cline | 差距方向 |
|------|-------------|--------|----------|-------|---------|
| **代码生成质量** | 7.0 | 9.0 | 8.5 | 8.0 | ↑ 需提升 |
| **工具链完善度** | 6.0 | 9.0 | 8.5 | 8.0 | ↑↑ 需重点补齐 |
| **自我纠错能力** | 5.0 | 8.5 | 8.0 | 7.5 | ↑↑ 最大短板 |
| **错误处理机制** | 5.5 | 8.0 | 7.5 | 7.0 | ↑ 需结构化 |
| **调试与测试** | 4.0 | 8.5 | 8.0 | 6.5 | ↑↑ 最大短板 |
| **项目管理能力** | **8.5** ✅ | 5.0 | 5.5 | N/A | **✅ 核心优势** |
| **角色协作模型** | **9.0** ✅ | N/A | N/A | N/A | **✅ 独有优势** |
| **文档处理能力** | **8.0** ✅ | 6.0 | 5.5 | 4.0 | **✅ 领先** |
| **架构可扩展性** | 6.5 | 8.0 | 7.5 | 7.0 | ↑ MCP 是关键 |

**加权平均**: Argus **6.5/10** (v0.6.0 时为 6.0，提升 0.5)

### 雷达图可视化

```
                    代码生成 (7.0)
                         │
                         │  ← 需提升
项目管理 (8.5) ─────┼───── 工具链 (6.0)  ← 重点补齐
    ✅                 │  ↑↑
                         │
角色协作 (9.0) ──────┼───── 自我纠错 (5.0)  ← 最大短板
    ✅✅               │  ↑↑
                         │
文档处理 (8.0)        │
    ✅                ├── 错误处理 (5.5)  ← 需结构化
                      │
                  调试测试 (4.0)  ← 最大短板
                      │  ↑↑↑
```

**强项（护城河）**: 角色协作模型、项目管理、文档处理
**弱项（需追赶）**: 调试测试、自我纠错、工具链深度

---

## 6. 建议路线图

### 第一批：最大 ROI（解决 P0 #1-#3，预计 1-2 周）

```
① 结构化错误解析器 (ErrorParser)
   - 解析 Go/Python/Node/Java/Rust 编译器输出
   - 输出: {type, file, line, col, message, code_context, suggested_fix}
   - 集成到 executeActions 后，自动注入给 SE
   - 投入: ~500 行 Go 代码（正则为主）
   - 收益: 所有编译/运行时错误处理速度提升 3-5x

② 测试结果结构化解析 (TestResultParser)
   - go test --json / pytest --json / npm test --json
   - 输出: {summary, failures[], coverage}
   - 关联源码位置（失败测试行 → 对应源码函数）
   - 投入: ~300 行 Go 代码
   - 收益: 测试失败定位从分钟级降到秒级

③ 增强 LSP: References + Rename
   - 基于已有 LSPClient 扩展 textDocument/references + textDocument/rename
   - 注册为新 SE 工具: find_references, rename_symbol
   - 投入: ~400 行（LSPClient 扩展 + 工具注册）
   - 收益: 跨文件重构不再漏改调用点
```

### 第二批：体验升级（P0 #4-#5 + P1 #6-7，预计 2-3 周）

```
④ 交互式终端增强
   - exec_session 增加: tab 补全、命令历史、多标签
   - 终端输出 ANSI 颜色渲染
   - 投入: ~600 行（前端 + 后端）

⑤ AST 感知编辑（Go 优先）
   - 利用 go/parser + go/ast 做 rename/find-references
   - 不依赖外部 LSP server（离线可用）
   - 投入: ~800 行

⑥ File Watcher
   - fsnotify 监听工作目录文件变化
   - 自动刷新 CodeIndexer 和 LSP 缓存
   - 投入: ~200 行
```

### 第三批：架构升级（P1 #8-#11，长期）

```
⑧ MCP 协议支持
   - 实现 MCP Client（JSON-RPC over stdio/SSE）
   - 用户可通过配置添加任意 MCP Server
   - 内置常用 Server: filesystem, git, memory
   - 投入: ~1500 行（新模块）
   - 收益: 工具生态无限扩展

⑨ Context Window 智能 Token 管理
   - 滑动窗口（保留最近 N 条消息）
   - 相关性排序（按与当前任务的相关性裁剪）
   - Token 预算分配（PM 30% / SE 50% / 系统 20%）
   - 投入: ~1000 行
```

### 第四批：差异化功能（P2 挑选）

```
⑩ Git Blame + Bisect AI 分析
⑪ Profiling/pprofl 集成
⑫ 数据库工具（SQLite/MySQL/PostgreSQL）
⑬ 浏览器工具（playwright/cdp 集成）
⑭ PR Review 自动生成
```

---

## 7. 结论

Argus v0.7.0 在 **PM-SE-AP 三角色协作模型** 和 **文档处理能力** 上建立了明确的差异化优势。但在 **编码调试的核心闭环**（写代码→编译→发现错误→理解错误→精确修复→验证）上，与主流竞品存在 **P0 级别的结构性差距**：

1. **错误不可理解**（原始字符串 vs 结构化对象）
2. **无法深入调试**（只能看输出 vs 断点/单步/变量检查）
3. **无法安全重构**（字符串替换 vs AST 编辑）

**建议优先实施第一批（ErrorParser + TestResultParser + LSP 增强）**，这三项投入最小、对日常编码调试体验的提升最直接。特别是 ErrorParser，本质上是把竞品已有的"观察层"补上，让我们的 ReAct 循环真正闭合。
