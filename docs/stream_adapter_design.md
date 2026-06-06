# StreamAdapter 设计方案 — 基于 Codex Responses API 架构

> **本质：** Argus client.go = 简易版 Open Responses API 核心
> **目标：** 在核心内嵌模型策略方法，消除硬编码，支持多模型扩展
> **参考：** Codex CLI (LLM Agnostic) / Claude Code (事件状态机) / OpenCode (声明式配置)

---

## 1. 问题

```go
// 现有代码 (client.go:483) — 硬编码，有 bug
if delta != "" {
    displayContent += delta
} else if reasoningDelta != "" {   // bug1: else if 同 chunk 双字段时丢一个
    fullContent += reasoningDelta   // bug2: 字段名硬编码
}
```

**后果：** DeepSeek thinking 模式下 PM 输出 JSON 截断（`{"is_programming":true件...`）

---

## 2. 架构（两层，同 Codex）

```
┌──────────────────────────────┐
│       上层 (ArgusCore/SE/PM)  │  接口不变：(string, error)
└──────────────┬───────────────┘
               │
┌──────────────▼───────────────┐
│                             │
│   Client Core (client.go)    │
│                             │
│   ┌─────────────────────┐   │
│   │ HTTP + SSE 解析      │   │  ← 通用，所有模型共用
│   │ (不变)               │   │
│   ├─────────────────────┤   │
│   │ parseDelta() 策略方法  │   │  ← 按模型族选解析方式
│   │  - deepseekParse()   │   │
│   │  - defaultParse()    │   │
│   └─────────────────────┘   │
│                             │
└──────────────────────────────┘
```

**没有独立 Adapter 层/接口/文件。** 策略方法内聚在 Client Core 内，和 SSE 循环紧耦合（本来就在同一个 for 里）。

---

## 3. 核心类型

```go
// StreamEventType 统一流事件类型
type StreamEventType int

const (
    StreamEventThinking StreamEventType = iota // 推理内容 → 只进日志，不返回上层
    StreamEventContent                        // 正式内容 → 返回给上层
)

// StreamEvent 统一流事件
type StreamEvent struct {
    Type StreamEventType
    Data string
}
```

### 模型解析策略

```go
// === DeepSeek：有 reasoning_content 字段 ===
func deepseekParse(delta Delta) []StreamEvent {
    var events []StreamEvent
    if delta.ReasoningContent != "" {
        events = append(events, StreamEvent{Thinking, delta.ReasoningContent})
    }
    if delta.Content != "" {
        events = append(events, StreamEvent{Content, delta.Content})
    }
    return events  // 一个 chunk 可能产出 2 个事件
}

// === GLM 及其他：只有标准 content ===
func defaultParse(delta Delta) []StreamEvent {
    if delta.Content == "" { return nil }
    return []StreamEvent{{Content, delta.Content}}
}

// === 工厂：根据模型名自动选择 ===
func (c *AIClient) parseDelta(delta Delta) []StreamEvent {
    if strings.Contains(strings.ToLower(c.config.Model), "deepseek") {
        return deepseekParse(delta)
    }
    return defaultParse(delta)
}
```

---

## 4. ChatStream 改造

### 改造前

```go
for _, line := range lines {
    var streamChunk StreamChunk
    json.Unmarshal([]byte(line), &streamChunk)

    delta := streamChunk.Choices[0].Delta.Content
    reasoningDelta := streamChunk.Choices[0].Delta.ReasoningContent

    if delta != "" {
        displayContent += delta          // 返回给调用方
        onChunk(delta)
    } else if reasoningDelta != "" {     // BUG: else if
        fullContent += reasoningDelta    // 不进 displayContent
    }
}
return displayContent, nil
```

### 改造后

```go
for _, line := range lines {
    var streamChunk StreamChunk
    json.Unmarshal([]byte(line), &streamChunk)

    if len(streamChunk.Choices) == 0 { continue }

    delta := streamChunk.Choices[0].Delta
    for _, event := range c.parseDelta(delta) {  // ← 策略方法替换硬编码
        switch event.Type {
        case StreamEventThinking:
            fullContent += event.Data           // 只进完整日志
        case StreamEventContent:
            fullContent += event.Data
            displayContent += event.Data         // 返回给调用方
            if onChunk != nil { onChunk(event.Data) }
        }
    }
}
return displayContent, nil
```

**改动量：~15 行。** 上层接口零变化。

---

## 5. 改动范围

| 操作 | 文件 | 内容 |
|------|------|------|
| **新增** | `internal/ai/stream_adapter.go` | `StreamEvent` 类型 + `deepseekParse()` / `defaultParse()` / `parseDelta()` |
| **修改** | `internal/ai/client.go` | ChatStream 的 stream 循环 (~15行)；删除之前加的冗余骨架代码 |
| **不改** | 其他所有文件 | 上层接口不变 |

---

## 6. 未来扩展（加模型只需改一处）

```go
// 例子：将来接 Claude
func (c *AIClient) parseDelta(delta Delta) []StreamEvent {
    modelLower := strings.ToLower(c.config.Model)
    switch {
    case strings.Contains(modelLower, "deepseek"):
        return deepseekParse(delta)
    case strings.Contains(modelLower, "claude"):
        return claudeParse(delta)     // 新增一行
    default:
        return defaultParse(delta)
    }
}
```

**一个 switch 就完事，不加接口、不加文件、不加抽象层。**

---

## 7. 关键决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 分几层 | **2层**（不是3层） | SSE解析和delta解析本就紧耦合，拆开反而增加复杂度 |
| Adapter 形式 | **策略方法**（不是 interface） | 接口太薄不值得；内聚在 Client 内更简单 |
| 新增文件数 | **1个** (`stream_adapter.go`) | 类型定义+策略函数放一起，不散落 |
| reasoning 是否暴露上层 | **否** | 上层只需要最终内容 |
| Claude/Gemini 现在做 | **否** | YAGNI；加时只改 parseDelta 一个 switch |
