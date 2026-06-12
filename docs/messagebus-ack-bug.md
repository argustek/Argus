# MessageBus ACK 追踪 — 历史问题 & 清理记录

## 历史问题（v0.8.3 及之前）

### 现象
前端频繁报 "🚨 消息丢失"（MessageBus-LOST），但消息实际已送达。

### 根因（v0.8.3 已修）

**ACK ID 不匹配** — `bufferToBatch` 每 10 个 chunk 打包一个 batch，`pendingQueue` 存的是 `xxx_batch`，但 `emitToFrontend` 发给前端的 `_msgId` 是个体 ID `xxx`。前端用个体 ID ACK，永远对不上 batch ID，导致所有流式消息超时报 LOST。

### 数据流（旧版，v0.8.3）

```
bufferToBatch (message_bus.go)
  └─ 每10个chunk打包一个batch
  └─ pendingQueue["xxx_batch"] = msg    ← batch ID

emitToFrontend (message_bus.go)
  └─ 发给前端的 _msgId = "xxx"         ← 个体 ID

App.vue
  └─ 前端 ACK "xxx"
  └─ 查 pendingQueue["xxx"] → 找不到 → ACK 无效
  └─ 5s后 pendingQueue["xxx_batch"] 超时 → LOST
```

## 修复（v0.8.4）

`Send()` 改为每条消息**独立追踪**，不再走 batch 缓冲：

- `pendingQueue[msgId]` 存个体 ID，`emitToFrontend` 发同样的 `_msgId` → ACK 完美匹配
- 流式消息走快速路径：短超时（5s） + 静默清理
- 非流式消息维持原有超时和 LOST 告警

## 死代码清理（2026-06-12）

v0.8.4 修复后遗留下大量 batch 死代码，已于 2026-06-12 清理：

| 删除内容 | 原因 |
|---------|------|
| `StreamBatch` struct | 不再使用 |
| `bufferToBatch()` | 无人调用 |
| `flushBatchLocked()` | 仅被其他死函数调用 |
| `FlushStreamBatch()` | 无人调用 |
| `FlushAllStreamBatches()` | 无人调用 |
| `streamBatches` / `batchMu` / `streamSampleN` 字段 | 不再使用 |
| `streamCounter` 字段 | 声明但从未使用 |
| backgroundChecker 批次 flush 逻辑 | streamBatches 已空 |
| GetStats 批次统计 | streamBatches 已空 |

### 保留
- `BatchAckInfo` / `ReceivedMessage.BatchAck` — `Ack()` 仍兼容此接口，前端可选回传
