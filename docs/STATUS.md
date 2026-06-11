# Argus 当前状态文档

**最后更新：** 2026-06-12  
**当前版本：** v0.8.5  
**最新 Commit：** `16a682a` (main)  
**分支：** `main`

---

## 1. 工作流

| 组件 | 可用性 | 说明 |
|------|--------|------|
| Short 直执 | ✅ | PM 直接执⾏单⽂件/轻量任务，输出标记 `[short]` |
| Normal 流程 | ✅ | 标准 PM→SE→AP 流程（默认） |
| Full 流程 | ✅ | 全套流程 + ⽂档（后端已预留，前端标记未实现） |

---

## 2. 核⼼修复

### 2.1 ACK 超时 (v0.8.2 → v0.8.4)

| 版本 | 超时时间 | 说明 |
|------|----------|------|
| v0.8.2 | 5s | 过短，前端流式渲染期间 ACK 延迟导致频繁 LOST |
| v0.8.3 (`ab3ec1a`) | **15s** | 普通消息恢复正 常，但流式消息改为 `return false` 不追踪 |
| v0.8.4 (本地) | **15s / 5s** | 普通消息 15s，流式消息 5s 快速路径 |

### 2.2 MessageBus 追踪策略演进

| 版本 | 流式消息 | ⾮流式消息 | 问题 |
|------|----------|------------|------|
| v0.8.1 | Batch 追踪 | 独⽴追踪 | Batch ID ≠ 个体 msgId → ACK 永远失败 |
| v0.8.2 | Batch 追踪 | 独⽴追踪 | 同上，LOST 误报严重 |
| v0.8.3 (`ab3ec1a`) | **不追踪** `return false` | 独⽴追踪 | 不报 LOST 了，但统⼀架构被破坏 |
| v0.8.4 (本地未编译) | **独⽴追踪 + 5s 快速路径** | 独⽴追踪 15s | 统⼀架构恢复，Batch 代码保留备⽤ |

### 2.3 PathSEExec 恢复

`ab3ec1a` 从 `PathCoreOutput` 中移除了 `PathSEExec` 追踪限制，SE 执⾏事件（exec_start/done/output）现在正确追踪。

### 2.4 PM 提示词全面重写 (v0.8.5)

**核心改动**：
- **去矛盾**：旧 prompt 同时说"所有编程必须 @SE"和"Featherweight 自己干"，模型矛盾。新 prompt 用决策树统一
- **第一原则锚定**：prompt 开头写"永远用工具做事，绝不纯文本回复"，解决 ToolCalls=0
- **新增工具**：write_file, edit_file, delete_file — PM 可以直接执行 featherweight 任务
- **ToolCalls=0 兜底**：ProcessStream 中检测到无工具调用时注入系统提醒，让模型重试一次
- **exec 超时**：30s → 60s

---

## 3. 待处理问题

### 3.1 TopBar 级别指示器 `[short]`/`[normal]`/`[full]`

**状态**：✅ 已实现（v0.8.1）

- 后端：`argus.go` `emitLevelEvent()` → MessageBus `project-level` ✅
- 前端：`TopBar.vue:77` `<span class="project-level-badge">` 渲染 ✅
- `App.vue:429` EventsOn(`project-level`) 接收并传递 prop

### 3.2 通知报警前后不对应

**根因：** 前端 42 处 `ackMessage()` 分散在各 EventsOn handler 中，每个 event 的 _msgId 必须与后端 `pendingQueue` 完全匹配。任何⼀处 handler 漏调或事件名拼错都会导致 15s 后 LOST 报警。

### 3.3 ⽂件树右侧菜单删除功能半截⼦

`_status.txt` 是**空⽂件**，后端 `cmd/testdb/main.go` 是 90 ⾏测试数据库代码，`docs/ui_plan_api_settings.md` 是 100⾏纯设计稿。三 个都是建了没搞完。

---

## 4. Commit 清单（v0.8.0 增量）

| Commit | 标题 | 状态 |
|--------|------|------|
| `eabb688` | 静默模式 / ⾃动语⾔检测 / @USR 去重 / C监控修复 / pmConfigId | ✅ 已完成 |
| `521810f` | ToolCalls=0 重试逻辑 (max 2 次) | ✅ 已完成 |
| `ab3ec1a` | ACK 超时 5s→15s + PathSEExec 恢复 | ✅ 已完成 |
| `820333a` | GitHub ⽹络排查⽂档 | ✅ ⽂档已补充 |
| `351b7dc` | **v0.8.4**: CMonitor 双空闲复位 / MessageBus 2s / FileTree 绝对路径 | ✅ 已发布 |
| `5a0acbc` | **v0.8.5**: PM 提示词重写 + write_file/edit_file/delete_file 工具 + ToolCalls=0 兜底 | ✅ 已发布 |

### 半截⼦⼯作

| ⽂件 | ⼤⼩ | 做了什么 | 还差什么 |
|------|------|----------|----------|
| `cmd/testdb/main.go` | 90 ⾏ | 数据库测试 code | 未集成、未运⾏ |

~~`docs/ui_plan_api_settings.md`~~ — ✅ **已实现**（`SettingsPanel.vue` 两张表 + 角色绑定完整实现）

---

## 5. 相关版本

| Tag | Commit | 说明 |
|-----|--------|------|
| `v0.7.3` | `79a75f6` | 热重载修复 + MessageBus 可靠性 |
| `v0.8.0` | `5975593` | PM 直执 / 智能化分级 / Bridge 重构 |
| — | `820333a` | 4 个 fix commit + ⽹络排查⽂档 |
| `v0.8.4` | `351b7dc` | CMonitor 双空闲复位 / MessageBus 2s / FileTree 绝对路径 |
| `v0.8.5` | `5a0acbc` | PM 提示词重写 + 新工具 + ToolCalls=0 兜底 |