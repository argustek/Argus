# PathStatus 追踪导致后端卡死 — 调查报告

**日期**：2026-06-01  
**状态**：已修复 ✅  
**严重等级**：P0（后端完全卡死）

---

## 问题描述

**症状**：发送 Hello World 任务后，后端完全卡死，无任何响应。前端界面停留"busy"状态。

**影响版本**：`6158581`（消息追踪commit）引入，基准版本 `05c6c47` 不受影响。

---

## 根因

**PathStatus 消息追踪 = 唯一根因**

`MessageBus.shouldTrack()` 中，`PathStatus` 设为 `true` 时导致后端卡死。

---

## 调查过程

### 第一阶段：二分法定位

从 `05c6c47`（基准）到 `6158581`（最新），逐个 cherry-pick 测试，确认问题出在 `6158581`（消息追踪 commit）。

### 第二阶段：shouldTrack 路径二分法

10 个路径逐个开关测试：

| # | 路径 | 单独开 | 结果 |
|---|------|--------|------|
| 1 | PathCoreOutput | true | ✅ 正常 |
| 2 | PathPMStream | true | ✅ 正常 |
| 3 | PathSEStream | true | ✅ 正常 |
| 4 | PathUserInput | true | ✅ 正常 |
| 5 | PathSystem | true | ✅ 正常 |
| 6 | **PathStatus** | **true** | **❌ 卡死** |
| 7 | PathPMToUser | true | ✅ 正常 |
| 8 | PathSEToUser | true | ✅ 正常 |
| 9 | PathAPToUser | true | ✅ 正常 |
| 10 | PathSEExec | true | ✅ 正常 |

**结论**：仅 PathStatus=true 导致卡死，9/10 其他路径正常。

### 第三阶段：探针研究

在 MessageBus.Send()、Bridge.emitStatus()、Core.emitStatus()、App.go status 回调中添加探针，发现关键时序：

```
启动时发送：
  5 条 project-state-changed (tracked=true)
+ 1 条 messages-cleared (tracked=true)
+ 1 条 terminal (tracked=true)
+ 1 条 ai-thinking (tracked=true)
+ 1 条 new-message (tracked=true)
= 9 条未ACK消息在 pendingQueue
↓
第一条 status 消息到达时 queueSize=10
```

**关键发现**：Core.emitStatus() 持有 `Core.mu` 锁时调用 onMessage 回调，回调链路为：

```
Core.emitStatus() [持有 Core.mu 锁]
  → onMessage("status", statusStr)
    → App.onCoreMessage()
      → emitToFrontend()
        → MessageBus.Send() [PathStatus=true]
          → 加入 pendingQueue [此时已有10条未ACK]
          → runtime.EventsEmit()
```

**在 Core.mu 锁保护下执行了消息发送和 EventEmit，如果这些操作涉及阻塞（如前端 ACK 超时等待），就会导致锁无法释放，后续所有需要 Core.mu 的操作全部阻塞。**

---

## 修复方案

**shouldTrack 最终配置**（`internal/chat/message_bus.go`）：

| 路径 | 追踪 | 说明 |
|------|------|------|
| PathCoreOutput | ✅ | 内部日志 |
| PathPMStream / SEStream | ✅ | 流式输出 |
| PathUserInput | ✅ | 用户输入 |
| PathSystem | ✅ | 系统事件 |
| **PathStatus** | **❌** | **唯一排除项 — 持锁内发送导致死锁** |
| PathPMToUser | ✅ | AI 消息 |
| PathSEToUser | ✅ | AI 消息 |
| PathAPToUser | ✅ | AI 消息 |
| PathSEExec | ✅ | 执行结果 |
| default | ✅ | 安全兜底 |

**追踪覆盖率：9/10 = 90%**

---

## 当前代码变更状态

```
Modified:
  app.go                         - 恢复：移除探针代码，保留原始 [Bridge-Status] 日志
  internal/chat/bridge.go        - 修复：添加 writeDebugLog 回调（V2 conversation.log）
  internal/chat/message_bus.go   - 修复：shouldTrack(PathStatus=false)，探针已清理
  internal/core/argus.go         - 恢复：移除探针代码
  frontend/src/App.vue           - 修复：project-state-changed 兼容两种数据格式，messages-cleared 探针
```

---

## 本次修复的其他问题

| 问题 | 根因 | 修复 |
|------|------|------|
| Reset 后"Send failed"弹窗 | App.isSending 锁未释放 | ExecuteReset() 中添加 isSending=false |
| Reset 后任务不处理 | Bridge.isProcessing 锁未释放 | 新增 Bridge.ResetIsProcessing() |
| MESSAGES-CLEARED 丢失报警 | 系统事件误走 emitToFrontend | 系统事件仅用 runtime.EventsEmit |
| About 菜单空白 | SettingsPanel.vue 多余 `</div>` | 修正 HTML 结构 |
| V2 架构 conversation.log 丢失 | Bridge 绕过 Manager 的日志 | Bridge 添加 writeDebugLog 回调 |
| PM 状态闪一下就消失 | project-state-changed 数据格式不兼容 | App.vue 兼容两种格式 |
| **后端卡死** | **PathStatus 追踪在持锁内发送** | **shouldTrack(PathStatus=false)** |

---

## 待深挖

- PathStatus 追踪的精确死锁机制：Core.mu 锁 + pendingQueue ACK 超时是否形成循环等待
- 前端 ACK 机制对 PathStatus 类型消息的处理时序
- 是否可以让 status 消息的发送在锁外执行（异步化）

---

## 回归测试结果

**测试时间**：2026-06-01 03:19  
**测试命令**：`--send "Write a Go program that prints Hello World to console."`

```
[2026-06-01 03:19:00] AP: 🔒 AP final approval...
[2026-06-01 03:19:06] AP: ✅ 交付完成！PM Review + AP Approval 全部通过
[2026-06-01 03:19:06] SYS_C: V2-Done success=true actions=1
```

✅ 全链路正常，Hello World 回归测试通过。