# MessageBus ACK 追踪 Bug

## 问题现象
前端频繁报 "🚨 消息丢失"（MessageBus-LOST），但消息实际已送达。

## 根因

**ACK ID 不匹配** — pendingQueue 存的是 batch ID，前端收到的 _msgId 是个体 ID，ACK 永远对不上。

### 数据流

```
bufferToBatch (message_bus.go:250)
  └─ 每10个chunk打包一个batch
  └─ pendingQueue["xxx_batch"] = msg    ← 存的是 batch ID

emitToFrontend (message_bus.go:226)
  └─ 发给前端的 _msgId = "xxx"         ← 个体 ID（无 _batch 后缀）

App.vue:534
  └─ 前端 ACK "xxx"                    ← 用个体 ID
  └─ 查 pendingQueue["xxx"] → 找不到  → ACK 无效
  └─ 5s后 pendingQueue["xxx_batch"] 超时 → "🚨 消息丢失"
```

### 关键代码位置

| 位置 | 文件 | 作用 |
|------|------|------|
| L250 | `internal/chat/message_bus.go` | `bufferToBatch` — 每10chunk合成batch，用 `msgId_batch` 入pendingQueue |
| L226 | `internal/chat/message_bus.go` | `emitToFrontend` — 发给前端的 `_msgId` 是原始个体 ID，不带batch后缀 |
| L534 | `frontend/src/App.vue` | 前端收到 `_msgId` 后原样返回 ACK，永远等不到匹配 |

### 影响范围
所有走 ACK 追踪的消息路径（`PathPMToUser`, `PathSEToUser`, `PathAPToUser`, `PathSEExec` 等），一旦触发 stream batching（超过10个chunk），必报 LOST。

## 当前缓解措施
- 全局超时 5s → 15s（`message_bus.go:116`），减少误报率但不解决根因
- 恢复 `PathSEExec` 追踪（`message_bus.go:409` 改为 `return true`）

## 修复方向
`bufferToBatch` 入 pendingQueue 时，需要同时注册个体 ID → batch ID 的映射，让前端用个体 ID ACK 时能找到对应的 batch 记录。
