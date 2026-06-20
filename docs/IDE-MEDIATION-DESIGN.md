# Argus IDE 外接对话协调方案

> 最后更新：2026-06-18
> 目标版本：v1.0.21

---

## 一、背景与目标

### 1.1 动机

Argus 内部有 PM（项目经理）、SE（软件工程师）、AP（审批人）三个 agent 协作。但实际开发中，**外部 IDE 的参与**是刚需：

- 多个 IDE 各自独立分析同一个问题，交叉验证降低误判
- 一方写测试，另一方 Review，PM 协调讨论直到方案成熟
- 形成"用户定目标 → PM 组织讨论 → IDE 执行 → PM 审核 → 完成"的自动化闭环

### 1.2 目标

1. **任意 IDE** 都能通过 HTTP/SSE 与 Argus 建立双向通信
2. **PM 作为协调人**，传递 2 个外部 IDE 之间的对话，参与讨论
3. **PM 根据用户设定的目标**，判断何时终止讨论并发出执行指令
4. **PM 用自然语言**与外部 IDE 对话
5. **保留原有 SSE 调试功能**（单连接调试模式仍然可用）

---

## 二、架构概览

```
┌──────────────────────────────────────────────────────────────────┐
│                        Argus                                     │
│  ┌──────────┐    ┌──────────┐    ┌───────────────────────────┐   │
│  │ SSEBridge │◄───│ Manager  │◄──►│      PM Processor        │   │
│  │ (多连接)  │───►│          │    │  + ide_send tool         │   │
│  └────┬─────┘    └──────────┘    └───────────────────────────┘   │
│       │                                                          │
└───────┼──────────────────────────────────────────────────────────┘
        │
   ┌────┴────┐   ┌────┴────┐
   │  IDE-A  │   │  IDE-B  │
   │ (SSE)   │   │ (SSE)   │
   └─────────┘   └─────────┘
```

### 2.1 通信方式

| 方向 | 协议 | 端点 | 说明 |
|------|------|------|------|
| IDE → Argus (发现) | HTTP GET | `/` | **欢迎页/连接指南**：返回版本号、端点列表、当前在线 IDE（v1.0.21+） |
| IDE → Argus | HTTP POST | `/api/v1/sse/subscribe` | 建立 SSE 长连接（初始消息可选） |
| IDE → Argus | HTTP POST | `/api/v1/sse/ide-input` | 对话期间发送跟进消息 |
| IDE → Argus | HTTP POST | `/api/v1/sse/ide-ack` | IDE 确认收到 `ide_message`（message_id ACK） |
| Argus → IDE | SSE event | `ide_message` | PM 通过 `ide_send` 工具推送给指定 IDE（含 `message_id`） |
| Argus → IDE | SSE event | 全部标准事件 | 保留原有 pm_message/se_action 等调试事件 |

### 2.2 连接模型

- **原有调试模式**：单连接，发一条消息 → 收全流程事件 → 断开（保持不变）
- **IDE 对话模式**：多连接，每个 IDE 一条 SSE 长连接，可双向收发多条消息

两种模式通过连接时是否指定 `source` 字段区分：
- 不传 `source` → 调试模式（保持单连接限制）
- 传 `source`（如 `"IDE-A"`）→ IDE 对话模式（允许多连接）

---

## 三、详细设计

### 3.1 SSEBridge 改造

**文件：** `internal/chat/sse_bridge.go`

**改动：**

```go
type SubscriberInfo struct {
    ID        string    // 唯一ID (sse-<timestamp>)
    Name      string    // IDE标识 (IDE-A / IDE-B / debug)
    Connected time.Time // 连接时间
}

type SSEBridge struct {
    subscribers     map[string]chan SSEEvent
    subscriberInfo  map[string]SubscriberInfo  // ← 新增元数据
    mu              sync.RWMutex
    heartbeatStop   chan struct{}
}
```

**关键方法变更：**

| 方法 | 变更 |
|------|------|
| `Subscribe(id, name)` | 去掉单连接限制，保存 subscriberInfo |
| `Unsubscribe(id)` | 清理 info，保留心跳（有其他连接时不停止） |
| `PushToSubscriber(id, event)` | **新增**：定向推送给指定订阅者 |
| `PushToAll(event)` | 广播给所有订阅者（用于原调试模式） |
| `GetSubscriberIDs()` | **新增**：返回所有 subscriber ID 列表 |
| `GetSubscriberNames()` | **新增**：返回所有 IDE 名称列表 |

### 3.2 HTTP 端点

**文件：** `http_server.go`

#### 3.2.1 修改 `POST /api/v1/sse/subscribe`

请求体增加可选字段：

```json
{
  "message": "可选的初始消息",
  "source": "IDE-A"           // ← 可选，IDE对话模式标识
}
```

逻辑：
- 传了 `source` → IDE 对话模式，允许和其他 IDE 共享连接
- 没传 `source` → 传统调试模式，保持单连接限制

响应：`text/event-stream`，新增事件类型 `ide_message`

#### 3.2.2 新增 `POST /api/v1/sse/ide-ack`

IDE 收到 `ide_message` 后通过此端点确认投递：

```json
{
  "message_id": "ide_msg_1739512345_1"
}
```

响应 200 表示 ACK 成功，404 表示 message_id 不存在或已确认。

#### 3.2.3 新增 `POST /api/v1/sse/ide-input`

```json
{
  "session_id": "sse-1739512345",
  "message": "我觉得应该先重构再写测试"
}
```

逻辑：
1. 查找 session_id 对应的订阅者是否存在
2. 将消息注入 PM 上下文（格式：`[IDE-A] <message>`）
3. 触发 PM 处理（`ProcessMessage`）
4. PM 的处理结果通过 `ide_send` 工具流回对应 IDE

### 3.3 PM 新工具：`ide_send`

**文件：** `internal/ai/pm_prompt.go`

#### 3.3.1 工具定义

```json
{
  "name": "ide_send",
  "description": "向外部 IDE 发送自然语言消息。PM 作为协调人主持 IDE 之间的讨论。",
  "parameters": {
    "target": {
      "type": "string",
      "description": "目标 IDE 名称（从「当前在线 IDE」列表中选择），或 \"all\" 表示全部在线 IDE"
    },
    "message": {
      "type": "string",
      "description": "要发送的自然语言消息内容"
    },
    "action": {
      "type": "string",
      "description": "discuss=征求意见/讨论, instruct=发指令执行任务, terminate=结束对话",
      "enum": ["discuss", "instruct", "terminate"]
    }
  },
  "required": ["target", "message", "action"]
}
```

> **v1.0.21 变更**：`target` 不再使用硬编码 enum（原 `["IDE-A", "IDE-B", "all"]`），改为自由输入。PM 从 system prompt 中动态注入的「当前在线 IDE」列表中选择目标。

#### 3.3.2 执行逻辑

```go
case "ide_send":
    var args struct {
        Target  string `json:"target"`
        Message string `json:"message"`
        Action  string `json:"action"`
    }
    json.Unmarshal([]byte(argsJSON), &args)
    // 通过 ideMessageEmitter 推送给目标 IDE
    if p.ideMessageEmitter != nil {
        p.ideMessageEmitter(args.Target, args.Message, args.Action)
    }
    return fmt.Sprintf("✅ 已向 %s 发送消息: %s", args.Target, args.Message)
```

#### 3.3.3 PMProcessor 新增字段

```go
type PMProcessor struct {
    // ... 现有字段

    ideMessageEmitter func(target, message, action string) bool  // IDE消息推送，返回是否成功投递
    ideList         string                                        // [v1.0.21] 当前在线 IDE 列表（动态注入到 system prompt）
}

func (p *PMProcessor) SetIDEMessageEmitter(emitter func(target, message, action string) bool) {
    p.ideMessageEmitter = emitter
}

// [v1.0.21] 动态设置当前在线 IDE 列表，PM 会据此知道可以给哪些 IDE 发消息
func (p *PMProcessor) SetIDEList(ides []string) {
    if len(ides) == 0 {
        p.ideList = "（无在线 IDE）"
    } else {
        p.ideList = strings.Join(ides, ", ")
    }
}
```

**动态注入机制：** SSEBridge 的 `onChange` 回调触发时，Manager 调用 `pmProcessor.SetIDEList(ides)` 更新列表。PM 每次调用 `getSystemPrompt()` 时自动拼接 `=== 当前在线 IDE ===\n{ideList}` 到 system prompt 末尾。

### 3.4 Manager 改造

**文件：** `internal/chat/manager.go`

#### 3.4.1 IDE 输入处理

新增方法 `HandleIDEInput(sessionID, message, source string)`：

```go
func (m *Manager) HandleIDEInput(sessionID, message, source string) error {
    // 1. 格式化 IDE 消息
    input := fmt.Sprintf("[%s] %s", source, message)
    
    // 2. 调用 ProcessMessage（PM 处理）
    go func() {
        _, err := m.ProcessMessage(input)
        if err != nil {
            // 通过 sessionID 推送错误
            m.sseBridge.PushToSubscriber(sessionID, SSEEvent{
                Type: "error",
                Data: map[string]string{"error": err.Error(), "stage": "system"},
            })
        }
    }()
    return nil
}
```

#### 3.4.2 PMProcessor 回调绑定

在创建 PMProcessor 时绑定 `ideMessageEmitter`：

```go
pmProcessor.SetIDEMessageEmitter(func(target, message, action string) {
    if m.sseBridge == nil {
        return
    }
    // 发送给特定 IDE
    if target == "all" {
        // 广播给所有带 source 的订阅者
        for _, info := range m.sseBridge.GetSubscriberInfos() {
            if info.Name != "debug" {
                m.sseBridge.PushToSubscriber(info.ID, SSEEvent{
                    Type: "ide_message",
                    Data: map[string]string{
                        "from": "PM",
                        "message": message,
                        "action": action,
                    },
                })
            }
        }
    } else {
        // 发送给指定 IDE
        for _, info := range m.sseBridge.GetSubscriberInfos() {
            if info.Name == target {
                m.sseBridge.PushToSubscriber(info.ID, SSEEvent{
                    Type: "ide_message",
                    Data: map[string]string{
                        "from": "PM",
                        "message": message,
                        "action": action,
                    },
                })
            }
        }
    }
    
    // terminate → 发送 done 事件
    if action == "terminate" {
        for _, info := range m.sseBridge.GetSubscriberInfos() {
            if info.Name != "debug" {
                m.sseBridge.PushToSubscriber(info.ID, SSEEvent{
                    Type: "done",
                    Data: map[string]string{"status": "completed"},
                })
            }
        }
    }
})
```

### 3.5 IDE 连接状态 → 前端 TopBar

SSEBridge 订阅/取消订阅时触发 `onChange` 回调，Manager 通过 MessageBus 推送 `ide_status` 事件到前端：

- 事件名：`ide_status`
- 数据：`{ ide_a: bool, ide_b: bool }`
- 路径：`PathIDEEvent`（无 ACK 追踪，高频状态同步）

前端 `App.vue` 监听 `ide_status`，更新 `ideConnected` 响应式状态（`{ ideA, ideB }`），传入 `TopBar.vue` 渲染为绿色圆点指示器。

### 3.6 消息投递 ACK 确认

每条 `ide_message` SSE 事件附带唯一 `message_id`：

```
event: ide_message
data: {"from":"PM","message":"你的意见呢？","action":"discuss","message_id":"ide_msg_1739512345_1"}
```

IDE 端需在收到后调用 `POST /api/v1/sse/ide-ack` 回传 `message_id`。Manager 跟踪待确认消息，通过 `GetIDEAckStats()` 查询统计（已确认/待确认）。

### 3.6 PM Prompt 更新

**文件：** `internal/ai/pm_prompt.go`

在 PMPrompt 中新增 IDE 协调规则：

```
=== IDE COORDINATION (MANDATORY) ===
Messages with [xxx] prefix come from external IDEs. You MUST use ide_send tool — do NOT use exec/write_file/read_file or any other tool.
1. ide_send(target="IDE名称"|"all", message="你的回复", action="discuss")
   - target 从「当前在线 IDE」列表中选择，或用 "all" 广播给所有在线IDE
2. When the goal is met: action="terminate" to end discussion
3. You may analyze and contribute your own perspective
4. Before terminate, output the final conclusion
```

> **v1.0.21 变更**：
> - 去掉写死的 `@IDE-A` / `@IDE-B` 引用，改为从动态「当前在线 IDE」列表选择目标
> - system prompt 末尾自动注入 `=== 当前在线 IDE ===\n{实际列表}`（由 `SetIDEList()` 驱动）
> - **消息路由规则**：用户消息中包含在线 IDE 名称时 → 用 `ide_send` 转发；不包含 → PM 自己处理（调用 SE/AP 等内部工具）

### 3.7 欢迎页 / 连接发现（v1.0.21+）

**文件：** `http_server.go`

新增 `GET /` 端点，任何客户端访问 Argus 根路径即可获取完整连接指南：

```json
{
  "name": "Argus · 驭码",
  "version": "1.0.21",
  "welcome": "欢迎接入 Argus IDE 协作平台",

  "how_to_connect": {
    "step1_subscribe": "POST /api/v1/sse/subscribe  Body: {\"source\": \"你的IDE名字\"}",
    "step2_send":      "POST /api/v1/sse/ide-input     Body: {\"session_id\": \"返回的ID\", \"message\": \"消息内容\"}",
    "step3_ack":        "POST /api/v1/sse/ack           Body: {\"msg_id\": \"消息ID\"}"
  },

  "endpoints": {
    "sse_subscribe": "POST /api/v1/sse/subscribe   — 建立SSE长连接",
    "ide_input":     "POST /api/v1/sse/ide-input     — IDE发送消息",
    "ide_ack":       "POST /api/v1/sse/ack           — 确认收到消息",
    "chat_send":     "POST /api/v1/chat/send         — 直接发消息(无需订阅)",
    "chat_history":  "GET  /api/v1/chat/history      — 获取对话历史"
  },

  "current_ides": ["TRAE-IDE"]   // 当前在线的 IDE 列表（实时）
}
```

**设计意图：** 外部 IDE 只需知道 Argus 的地址和端口，访问根路径即可自动获取连接方式。无需额外文档或约定。

---

## 四、IDE 端集成指南

### 4.1 建立连接

```powershell
# IDE-A 连接
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"message":"准备就绪","source":"IDE-A"}'

# IDE-B 连接  
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"message":"准备就绪","source":"IDE-B"}'
```

### 4.2 发送消息

```powershell
# IDE-A 发送消息
curl -X POST http://localhost:8080/api/v1/sse/ide-input `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"session_id":"sse-1739512345","message":"分析结果：有3个函数需要重构"}'
```

### 4.3 接收消息

SSE 事件流中新增 `ide_message` 事件：

```
event: ide_message
data: {"from":"PM","message":"IDE-B 你的意见呢？","action":"discuss"}
```

---

## 五、与现有功能的兼容

| 现有功能 | 兼容方式 |
|---------|---------|
| 单连接 SSE 调试 | 不传 `source` 时保持原行为，单连接 + 全量事件 |
| `pm_message` / `se_action` 事件 | IDE 连接也会收到这些标准事件，便于调试 |
| `manage.go` 现有处理流程 | 不变，仅在 `ide_send` 工具被调用时触发 SSE 推送 |
| 心跳 | 所有连接独立心跳，互不影响 |

---

## 六、边界情况处理

| 场景 | 处理 |
|------|------|
| IDE 断开重连 | SSE 断开触发 Unsubscribe，PM 上下文记录"IDE-A 已断开" |
| 两个 IDE 同时发消息 | Manager 串行处理，PM 一次只处理一条 |
| IDE 长时间不回复 | PM 可设置超时判断，超时后自行决策 |
| terminate 后还有消息 | 返回错误 "对话已结束，请重新建立连接" |
| 调试模式和 IDE 模式混用 | 调试模式占用单连接时会拒绝 IDE 连接（反之亦然），通过连接类型区分 |

---

## 七、实现计划

| # | 任务 | 文件 | 工作量 | 状态 |
|---|------|------|--------|------|
| 1 | SSEBridge 多连接 + 定向推送 | `sse_bridge.go` | 中 | ✅ |
| 2 | HTTP IDE 输入端点 | `http_server.go` | 中 | ✅ |
| 3 | PM 工具 `ide_send` | `pm_prompt.go` | 小 | ✅ |
| 4 | Manager 接入 IDE 消息 | `manager.go` | 中 | ✅ |
| 5 | PM Prompt 更新 | `pm_prompt.go` | 小 | ✅ |
| 6 | 连接状态 → 前端 TopBar | `sse_bridge.go`, `manager.go`, `App.vue`, `TopBar.vue` | 中 | ✅ |
| 7 | 消息投递 ACK | `manager.go`, `http_server.go` | 中 | ✅ |
| 8 | Running 状态闪烁 | `TopBar.vue` | 小 | ✅ |
| 9 | 编译验证 + 测试 | — | 小 | ✅ |
| 10 | **ide_send target 动态化**（去掉硬编码 enum） | `pm_prompt.go` | 小 | ✅ v1.0.21 |
| 11 | **PM 动态在线 IDE 列表注入**（SetIDEList） | `pm_prompt.go`, `manager.go` | 小 | ✅ v1.0.21 |
| 12 | **欢迎页 GET /**（连接发现） | `http_server.go` | 小 | ✅ v1.0.21 |

---

## 八、v1.0.23 架构转向：从跨厂商IDE协调 → 多Argus组网

### 8.1 认知转变（2026-06-20）

**原方案目标：** Argus 作为中心协调者，调度外部不同厂商的 IDE（TRAE、Cursor、OpenCode 等）协作。

**实践发现的问题：**

```
原设想:
  Argus PM ──ide_send──→ TRAE-AI (收到消息，但AI不知道)
  Argus PM ──ide_send──→ Cursor AI (收到消息，但AI不知道)
  Argus PM ──ide_send──→ OpenCode AI (收到消息，但AI不知道)

核心障碍: 外部IDE能收到SSE事件，但IDE内的AI无法自动监听/响应。
         消息到了IDE终端，进不了IDE的聊天界面。
         除非改对方代码或用对方API——这违背"零接入成本"原则。
```

**新方向：多 Argus 实例组网**

```
新架构（已验证可行）:
  ┌─────────────┐    SSE    ┌──────────────┐
  │  Argus-A    │◄────────►│   Argus-B     │
  │  (主机PM)   │          │  (从机PM)     │
  │  port 8080  │          │  作为客户端连接│
  └──────┬──────┘          └──────┬───────┘
         │                        │
    /sse/ide-input           自动唤醒本地PM
         │                        │
         ▼                        ▼
    PM 处理                  PM 处理 + ide_reply
```

**为什么 Argus 之间可以双向对话：**
- 每个 Argus 都有完整的 SSE 监听机制（`sse_bridge.go`）
- 收到 `ide_message` 时可自动唤醒本地 PM（`http_server.go` auto-wake）
- PM 有 `ide_send` 工具可以主动发消息给对方
- 形成真正的闭环：A→B→A→B...

### 8.2 已实现功能（v1.0.23）

| 功能 | 文件 | 说明 |
|------|------|------|
| **SSE 客户端自动唤醒 PM** | `http_server.go:268-289` | 收到 `ide_message` 时调用 `SendMessage()` 唤醒本地 PM |
| **去重防循环** | `app.go:3359-3383` | `isAutoWakeDuplicate()` 30秒窗口内容去重 |
| **Bridge pm_message 推送到 SSE** | `bridge.go:onCoreMessage` | PM 回复同时推送给所有 SSE 订阅者 |

### 8.3 实测验证结果

单实例自环测试（curl 连自己）：
- ✅ IDE 消息送达 PM
- ✅ PM 回复推送到 SSE 订阅者
- ✅ 无死循环（PM 智能判断是否需要 ide_send + 去重安全网）
- ✅ `wails build` 构建通过

### 8.4 未来组网 TODO

> 以下为下一阶段改进方向，当前版本已满足基本双向通信。

| # | 改进项 | 说明 | 优先级 |
|---|--------|------|--------|
| 1 | **唯一实例 ID** | 自动生成 ID（如 `Argus-A1`、`Argus-B2`），不再都叫 `TRAE-IDE` | P0 |
| 2 | **角色认知** | 服务端 PM 知道自己是"主机PM"，客户端 PM 知道自己是"从机PM" | P0 |
| 3 | **连接模式配置** | server / client / 自适应 三种模式可选 | P1 |
| 4 | **拓扑发现** | 自动发现邻居节点，无需手动指定地址 | P1 |
| 5 | **身份信息扩展** | 订阅时传 `role`、`capabilities` 等元数据 | P1 |
| 6 | **PM Prompt 动态注入** | 根据角色注入不同的 system prompt 片段 | P1 |
| 7 | **多跳路由** | A→B→C 链式通信，不仅限于直连 | P2 |
| 8 | **状态同步** | 各实例的项目状态、任务进度共享 | P2 |

### 8.5 最小改动方案（下次实现时参考）

```go
// 订阅请求扩展
type SubscribeRequest struct {
    Message      string   `json:"message"`
    Source       string   `json:"source"`        // 唯一实例ID: "Argus-B2"
    Role         string   `json:"role"`          // "server" | "client"
    Capabilities []string `json:"capabilities"`  // ["pm", "se", "file_edit"]
}

// PM Prompt 注入片段（服务端）
const ServerPMPrompt = `
你是 Argus 主机 PM。当前管理的从机节点: {节点列表}。
你可以通过 ide_send 向任意从机发送指令或讨论。
从机回复会自动唤醒你的处理流程。
`

// PM Prompt 注入片段（客户端）
const ClientPMPrompt = `
你是 Argus 从机 PM（{实例ID}）。你已连接到主机 {主机ID}。
收到主机消息时自动处理并回复。
你也可以主动向主机发送消息或请求协调。
`
```
