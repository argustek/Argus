# 🔴 重大待处理 Bug / 技术债务

> **规则：开始任何开发任务前，先读这个文件。每条都是踩过坑的教训。**
> **最后更新：2026-06-06**

---

## TD-1 ⚠️⚠️⚠️ MessageBus 高频路径追踪 → 前端卡死、AI 全停（致命）

| 属性 | 值 |
|------|-----|
| 严重度 | **🔴 致命 — 整个系统不可用** |
| 发现日期 | 2026-06-06（更早的 PathStatus 事件） |
| 影响范围 | 全系统：前端 UI 冻结 + AI 回调阻塞 → 死锁 |
| 状态 | **🔴 未解决 — 临时规避中** |

### 现象

`message_bus.go` 的 `shouldTrack()` 函数中：
```go
case PathStatus:      // 或 PathCoreOutput
    return true        // ← 改这行后...
```

结果：**前端完全无响应，所有 AI 角色停止工作**，只能 taskkill 强杀进程。

### 根因链（G点分析）

```
shouldTrack(PathStatus) = true
  ↓
status 事件每秒 20-50 条全部进 pendingQueue（map[string]*PendingMessage）
  ↓
CheckPending() O(n) 遍历全队列 + 每条计算超时
  ↓
CPU 100% 卡在 pendingQueue 操作上
  ↓
前端 Wails 事件循环被阻塞（Go runtime 单线程模型）
  ↓
UI 冻结 + AI 的 onChunk/onMessage 回调也走同一通道
  ↓
🔥 死锁：AI 等前端响应 → 前端等 CPU 空闲 → 永远等不到
```

### 为什么 PathPMStream/PathSEStream 追踪没事？

因为它们走**批量缓冲模式**（bufferToBatch），不是每条单独进 pendingQueue。
但 PathCoreOutput/PathStatus 走的是普通 Send 路径，每条都单独入队。

### 当前临时方案

1. `PathCoreOutput` 和 `PathStatus` 保持 `return false`（NO_TRACK）
2. 需要追踪的重要事件改用 **PathSystem** 发送（如 `agent-thought`）
3. 代价：这些路径的消息丢失不会报警

### 正确解法方向（三选一）

| 方案 | 改动量 | 风险 | 推荐度 |
|------|--------|------|--------|
| A) pendingQueue 改 ring buffer + 异步清理线程 | 大 | 中 | ⭐⭐⭐ 最彻底 |
| B) 高频路径采样追踪（每 N 条追 1 条） | 小 | 低 | ⭐⭐⭐ 最快落地 |
| C) shouldTrack 加 QPS 频率限制（超阈值自动降级） | 中 | 低 | ⭐⭐ 最灵活 |

### 谁会提醒你？

1. **代码注释**：[message_bus.go:374](../internal/chat/message_bus.go#L374) 有 `⚠️ TECH-DEBT` 注释
2. **本文件**：放在项目根目录 docs 下，显眼位置
3. **gap_analysis.md** 第四章：已知技术债务表

---

## TD-2 🟠 前端 EventsOn handler 数据类型不一致（消息丢失）

| 属性 | 值 |
|------|-----|
| 严重度 | 🟠 高 — 功能不可用但不影响其他功能 |
| 发现日期 | 2026-06-06 |
| 影响范围 | agent-thought Dashboard 不显示数据 |
| 状态 | ✅ 已修复 |

### 现象

MessageBus 通过 `EventsEmit("agent-thought", jsonString)` 发送 JSON 字符串，
但前端 `EventsOn('agent-thought', (data) => { data.type ... })` 当对象用，
导致 `data.type === undefined`，条件不满足，消息被静默丢弃。

### 根因

MessageBus `emitToFrontend` 对不同路径可能传 string 或 object，
前端 handler 未做类型兼容。

### 修复

前端 handler 加了类型判断：
```typescript
const data = typeof raw === 'string' ? JSON.parse(raw) : raw
```

### 教训

**新增 EventsOn handler 时，第一行必须是类型检查。**

---

## TD-3 🟡 Shell Session 空闲清理未实现（文档写了代码没写）

| 属性 | 值 |
|------|-----|
| 严重度 | 🟡 中 — 资源泄漏 |
| 发现日期 | 2026-06-06 |
| 状态 | ✅ 已修复 |

### 现象

文档和 gap_analysis 都写了 "60s 自动清理空闲 Session"，但代码里没有 idle timer。

### 修复

在 [shell_session.go](../internal/executor/shell_session.go) 添加 `idleChecker()` goroutine。

### 教训

**文档承诺的功能必须有对应代码实现，否则标记为 TODO 而非已完成。**

---

## 快速参考：如何判断新事件该走哪个 Path？

```
你的事件频率？
├─ > 10次/秒（chunk/thinking/status）→ PathCoreOutput (NO_TRACK)
│   └─ 如果需要追踪？→ 用 PathSystem 替代（见 TD-1）
├─ 1-10次/秒（step/tool_done）          → PathSystem (TRACKED)
└─ < 1次/秒（approval/error/done）      → PathSystem (TRACKED)
```
