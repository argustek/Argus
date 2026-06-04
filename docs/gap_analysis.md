# Argus SE AI 能力差距分析

> 对比对象: Cursor / Claude Code / GitHub Copilot
> 分析日期: 2026-06-04
> 方法: 三次独立代码扫描 → 交叉比对 → 修正
> 状态: ✅ 定稿

---

## 一、当前工具集（13 个）

| 工具 | 类型 | 实现 | 状态 |
|------|------|------|------|
| write_file | 文件写入 | 受保护文件检查 + 路径安全检查 | ✅ |
| edit_file | 文件编辑 | old_str/new_str 精确替换 | ✅ |
| read_file | 文件读取 | offset/limit 行范围，默认 100 行 | ✅ |
| delete_file | 文件删除 | 工作目录范围检查 + 安全拒绝 | ✅ |
| list_files | 文件列表 | 递归遍历 + glob 过滤 + ** 双星展开 | ✅ |
| glob | 文件查找 | glob 模式匹配 + ** 双星展开 + Walk 回退 | ✅ |
| search_files | 内容搜索 | filepath.Walk + 正则 + 上下文行 + 跳过 12 种忽略目录 | ✅ |
| exec | 命令执行 | CMD 独立进程，30s 超时 | ✅ |
| exec_session | 持久化 shell | 保持 cd/env 状态跨命令，60s 超时 | 🆕 |
| run_tests | 测试运行 | go test，支持 pattern/coverage/verbose | ✅ |
| git_operation | 版本控制 | status/diff/commit/push/pull/log/branch/show 8 种操作 | ✅ |
| web_search | 网络搜索 | DuckDuckGo HTML 解析，提取 5 条结果 | ⚠️ |
| semantic_search | 语义搜索 | 倒排索引（函数名/类型名/注释/字段tag），跨 20+ 语言 | 🆕 |
| complete_task | 任务完成 | 文件清单 + 摘要 + 状态标记 | ✅ |

---

## 二、Argus 已有核心优势

### 🔧 工具结果反馈回路（Tool Result Feedback Loop）

每个工具执行后，结果通过 `AddResult()` 立即注入 SE 对话历史。
SE 基于实际结果（非假设）决定下一步。**"执行→观察→调整" 闭环**。

| 反馈点 (manager.go executeSEActions: 23 处) | |
|------|---|
| write_file | 写入成功/失败 → SE 知道文件状态 |
| edit_file | 替换匹配/失败 → SE 知道精确结果 |
| exec | stdout/stderr 全文 → 触发 AnalyzeError() 错误分类 |
| read_file | 文件内容带行号 → SE 逐段理解代码 |
| search_files | 搜索结果列表 → SE 知道匹配数量和位置 |
| git_operation | commit/push/diff 结果 → SE 知道版本状态 |
| run_tests | 测试通过/失败/覆盖率 → SE 知道质量状态 |

### 🔄 自动修复循环（Auto-Fix）

```
action 执行失败
  → 构造 fixPrompt（错误详情 + 之前 actions + 常见错误修复指南）
  → SE AI 生成修复 actions
  → 执行修复 → 成功则合并到原始 actions
  → 失败 → 重试（最多 3 次）
  → 3 次全失败 → 降级:
      ① continueSETask() SE 自主尝试其他方案（最多 10 次）
      ② handleSEAskPM() 请求 PM 人工介入
      ③ PM 分析后给 SE 新指令
```

### 🛡️ 多层防死循环防护

| 层 | 机制 | 阈值 | 动作 |
|----|------|------|------|
| 1 | 空 actions 检测 | 连续 3 次 | 强制路由到 PM 审核 |
| 2 | JSON 格式重试 | 第 1 次空 actions | 用严格 JSON 提示词重试 AI |
| 3 | continue 次数限制 | > 10 次 | 强制结束任务 |
| 4 | 语义完成兜底 | SE 说"完成"但无 JSON | 自动触发 PM 审核流程 |
| 5 | PM 审核轮次限制 | 超限 | 强制结束审核流程 |

### 🧠 错误智能分析（ExecuteWithAnalysis）

`AnalyzeError()` 6 类错误分类 + `FormatErrorForSE()` 结构化反馈：

| 错误类型 | 分类依据 | 返回信息 |
|----------|----------|----------|
| Syntax/Compile | "syntax error" / "expected" / "undefined" | 文件 + 行号 + 列号 |
| Runtime | panic / nil pointer / index out of range | panic 消息 + 行号 |
| Test Fail | go test 输出 | 解析测试结果 + 断言差异 |
| Permission | 权限拒绝 / 文件被锁 | 建议 |
| Timeout | 超时 > 30s | 优化建议 |
| Import/Missing | "cannot find package" | 包名 + 安装命令 |

### 📨 消息总线 + 前端一致性（G63 MessageBus）

```
每个消息 → msgBusSend() → 两个路径:
  ① msgBus 历史记录（前端通过 SSE 同步）
  ② 后端事件发射（内部模块通信）
  前端任意时刻重连 → msgBus 全量同步 → 前后端一致
```

### 🏥 自动健康恢复

| 机制 | 触发条件 | 动作 |
|------|----------|------|
| PM 不健康检测 | 连续 API 失败 ≥ 3 次（60s 内） | 清理会话 + 重置健康 |
| SE 僵尸检测 | busy 状态 > 5 分钟 | 强制复位为 idle |
| 处理超时检测 | isProcessing > 60s | 强制清理旧任务 |
| 消息队列 | 忙时新消息 | 暂存队列，完成后自动处理 |

---

## 三、竞品差距（按严重级）

### P0 — 致命差距

| # | 能力 | 竞品 | Argus | 评估 |
|---|------|------|-------|------|
| 1 | **语义搜索** | 嵌入模型向量搜索 + 全项目索引 | 🆕 倒排索引 + AI 概念提取（GenerateConcepts），双通道评分 | 🟡 基础 + AI 概念 |
| 2 | **终端状态管理** | 持久化 shell 进程 | 🆕 exec_session 保持 cd/env 跨命令 | ✅ 已实现 |
| 3 | **LSP 代码理解** | go-to-def / references / hover / rename / diagnostics | 零 LSP 能力 | 🔴 未实现 |

### P1 — 效率差距

| # | 能力 | 竞品 | Argus | 评估 |
|---|------|------|-------|------|
| 4 | **并行工具调用** | 多个独立工具并发执行 | 🆕 read_file/search_files 连续操作自动并行批处理 | 🟡 部分实现 |
| 5 | **多模态输入** | 截图/设计稿/PDF → 代码 | 不支持图片输入 | 🔴 未实现 |
| 6 | **文件变更追踪** | ws 监听 + 增量 diff | 无 | 🔴 未实现 |
| 7 | **撤销/回滚** | 每次编辑可 undo | 无 | 🔴 未实现 |

### P2 — 需打磨

| # | 能力 | 竞品 | Argus | 评估 |
|---|------|------|-------|------|
| 8 | **Agent 循环智能终止** | LLM 自主判断完成任务 | 上限高（100+ 轮）但无智能终止，靠语义兜底 | ⚠️ 量大但不智能 |
| 9 | **上下文管理** | 分层摘要 + 滑动窗口 + 优先级保留 | compressHistory() 保留 15 条 + 摘要旧消息 | ⚠️ 有但粗糙 |
| 10 | **web_search 增强** | 多引擎 + 网页抓取 | DuckDuckGo HTML 解析 | ⚠️ 基础可用 |

---

## 四、修复计划（优先级排序）

| 顺序 | 项目 | 优先级 | 预计影响 | 状态 |
|------|------|--------|----------|------|
| 1 | 语义搜索 | P0 | SE 理解代码能力质变 | 🟡 升级：AI 概念提取双通道 |
| 2 | 终端状态管理 | P0 | 多步构建流程可用 | ✅ 完成 |
| 3 | LSP 集成 | P0 | 精确代码理解和修改 | 🔴 |
| 4 | 并行工具调用 | P1 | 2-5x 速度提升 | 🟡 完成（read/search 自动批处理） |
| 5 | 多模态输入 | P1 | UI 开发场景可用 | 🔴 |
| 6 | 文件变更追踪 | P1 | 安全性，防止冲突 | 🔴 |
| 7 | 撤销/回滚 | P1 | 安全感，大胆改 | 🔴 |
| 8 | Agent 循环智能终止 | P2 | 减少浪费，平滑体验 | 🔴 |
| 9 | 上下文管理增强 | P2 | 长任务不丢失信息 | 🔴 |
| 10 | web_search 增强 | P2 | 文档查询更准确 | 🔴 |

---

## 五、修复日志

### 2026-06-04 上午
- 新增工具: glob, web_search, delete_file
- 升级工具: list_files(递归), read_file(offset/limit), search_files(增强版)
- 上下文压缩: compressHistory()
- 自愈循环: maxFixRetries=3 Auto-Fix

### 2026-06-04 下午
- 新增: semantic_search（倒排索引 + 代码结构解析，跨 20+ 语言）
- Bug: getConfigDir() 查找顺序修复（exe 优先于 .）
- Bug: 启动日志打印 ConfigPath
- Bug: %w 在 fmt.Sprintf 中无效的 format bug
- 文档: gap_analysis.md 三遍交叉校验定稿

### 2026-06-04 傍晚
- 新增: exec_session 持久化 shell 工具（cd/env 状态保持，ShellSession 会话管理）
- 新增: 并行工具执行（连续 read_file/search_files 自动批处理，goroutine + WaitGroup）
- 新增: 语义搜索向量升级（AI GenerateConcepts + 双通道评分，关键词+概念匹配）
- 新增: 直接工具 HTTP 端口（不经过 PM/SE 管道，毫秒级响应）：
  - `POST /api/v1/tool/exec-session` — 持久化 shell 命令
  - `POST /api/v1/tool/semantic-search` — 语义代码搜索（AI 概念 + 倒排索引）
  - `POST /api/v1/tool/search-files` — 文件内容搜索（正则 + glob）
  - `GET /api/v1/tool/shell-status` — shell 会话状态
- Bug: SE 反馈路由修复：`se_to_user` → `se_to_pm`（25 处），SE 不再直接 @USR 汇报
- 更新: gap_analysis.md 反映最新进度

---

## 六、总体评估

| 维度 | 自评 | 竞品对标 |
|------|------|----------|
| 工具覆盖度 | 13 个工具，文件/搜索/执行/版本控制/网络全覆盖 | 持平 Cursor |
| 反馈回路 | 工具结果实时反馈 + 错误分析 + 自动修复 | **领先** |
| 安全防护 | 多层防死循环 + 健康检测 + 消息队列 + 权限检查 | 持平 |
| 代码理解 | semantic_search 基础版（非向量），无 LSP | 落后 |
| 终端 / 环境 | 无状态 exec | 落后 |
| 多模态 / 并行 | 不支持 | 落后 |

**结论：基础工具链 + 反馈回路已达 T1 水平。差距主要集中在代码理解深度（语义搜索/LSP）和执行环境（终端/并行）。**
