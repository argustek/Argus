# Argus 当前状态文档

**最后更新：** 2026-06-10  
**当前版本：** v0.8.0  
**最新 Commit：** `820333a` (main) + 本地 MessageBus 独立追踪修改  
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

---

## 3. 待处理问题

### 3.1 TopBar 级别指示器 `[short]`/`[normal]`/`[full]`

后端 `argus.go` 通过 `MessageBus.Send()` 已推送 `project-level` 事件，但**前端未实现渲染**。⽤户看不到当前级别。

- 后端：`argus.go` `emitLevelEvent()` 已实现，EventName=`project-level`
- 前端：`App.vue:429` EventsOn(`project-level`) **只 ack 不渲染**
- 需要：TopBar 上出现 `[short]` / `[normal]` / `[full]` Badge

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

### 半截⼦⼯作

| ⽂件 | ⼤⼩ | 做了什么 | 还差什么 |
|------|------|----------|----------|
| `_status.txt` | 0 字节 | 仅创建 | 没写内容 |
| `cmd/testdb/main.go` | 90 ⾏ | 数据库测试 code | 未集成、未运⾏ |
| `docs/ui_plan_api_settings.md` | ~100 ⾏ | API 设置⻚⾯设计 稿 | 前端代码⼀⾏没写 |

---

## 5. 相关版本

| Tag | Commit | 说明 |
|-----|--------|------|
| `v0.7.3` | `79a75f6` | 热重载修复 + MessageBus 可靠性 |
| `v0.8.0` | `5975593` | PM 直执 / 智能化分级 / Bridge 重构 |
| — | `820333a` | 4 个 fix commit + ⽹络排查⽂档 |