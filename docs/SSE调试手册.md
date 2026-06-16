# Argus SSE 调试 & IDE 对话手册

> 最后更新：2026-06-16
> 适用版本：v0.9.6+（IDE 对话模式：v0.9.6+）

---

## 一、架构概览

```
┌──────────────┐    POST /api/v1/sse/subscribe     ┌─────────────────┐
│ HTTP 客户端   │ ───────────────────────────────▶ │  http_server.go │
│ (调试/IDE)    │ ◀── text/event-stream 流式响应 ──│ handleSSESubscribe│
└──────────────┘                                  └────────┬────────┘
                                                           │
                                              ┌────────────┤
                                              ▼            ▼
                                       Subscribe(id)  Subscribe(id,name)
                                              │            │
                                              ▼            ▼
                                       ┌─────────────────────────┐
                                       │       SSEBridge          │
                                       │    sse_bridge.go         │
                                       │                         │
                                       │  subscribers: map       │◀── PushEvent()
                                       │  subscriberInfo: map    │◀── PushToSubscriber()
                                       │  onChange: func()       │──→ ide_status → 前端TopBar
                                       └──────┬──────────────────┘
                                              │
                          ┌───────────────────┼───────────────────────┐
                          ▼                   ▼                       ▼
                  调试模式(单连接)        IDE模式(多连接)          IDE消息确认
                  manager.go           manager.go               POST /api/v1/sse/ide-ack
                  → pm_started         → ide_message + msg_id
                  → pm_message         → ide-input 双向对话
                  → se_action          → terminate → done
```

### 核心组件

| 组件 | 文件 | 职责 |
|------|------|------|
| **SSEBridge** | `internal/chat/sse_bridge.go` | 多订阅者事件总线（调试=单连接，IDE=多连接），channel 缓冲 64，元数据跟踪 |
| **HTTP Handler** | `http_server.go` | SSE 订阅+IDE 输入+IDE ACK 三个端点 |
| **埋点** | `internal/chat/manager.go` | PushEvent + PushToSubscriber，覆盖调试+IDE 双模式 |

---

## 二、端点与用法

### 2.1 订阅 + 发送消息（一步到位）

```
POST /api/v1/sse/subscribe
Content-Type: application/json
Authorization: Bearer <token>

{"message": "写个Hello World"}
```

**响应格式：** `text/event-stream`（长连接）

### 2.2 快速测试命令

#### PowerShell（推荐 Windows）
```powershell
# 基本测试
$headers = @{ "Authorization" = "Bearer your-token" }
$body = '{"message": "你好"}'
Invoke-RestMethod -Uri "http://localhost:8080/api/v1/sse/subscribe" `
  -Method POST -Headers $headers -Body $body -ContentType "application/json"

# 流式查看原始数据（推荐调试用）
curl.exe -N -X POST http://localhost:8080/api/v1/sse/subscribe `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d "{\"message\": \"hello\"}"
```

#### curl（Linux/Mac/WSL）
```bash
# 基本测试
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe \
  -H "Authorization: Bearer your-token" \
  -H "Content-Type: application/json" \
  -d '{"message":"hello"}'

# 带超时控制（120秒自动断开）
curl -N --max-time 130 -X POST http://localhost:8080/api/v1/sse/subscribe \
  -H "Authorization: Bearer your-token" \
  -d '{"message":"写个Hello World"}'
```

---

## 三、事件类型速查表

| 事件类型 | 触发位置 | 数据示例 | 说明 |
|---------|---------|---------|------|
| **connected** | [http_server.go](file:///E:/Argus/argus-desktop/http_server.go) | `{"session_id":"sse-...","source":"IDE-A"}` | 连接建立（含 session_id） |
| **pm_started** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{}` | PM 开始处理 |
| **pm_message** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"delta":"..."}` | PM 输出内容片段 |
| **se_task_assigned** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"task":"...","steps":3}` | PM 分配任务给 SE |
| **se_action** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"action":"write_file","path":"..."}` | SE 执行动作 |
| **se_output** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"output":"...","exit_code":0}` | 命令执行输出 |
| **se_completed** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"result":"..."}` | SE 任务完成 |
| **ide_message** | [manager.go:setupIDEMessageEmitter](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"from":"PM","message":"...","action":"discuss","message_id":"ide_msg_..."}` | **仅IDE模式** PM 通过 ide_send 发给 IDE |
| **ide_status** | [manager.go:onChange](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"ides":["IDE-A","VSCode"]}` | **前端消息总线** IDE 连接/断开时推送动态列表，谁连上显示谁 |
| **done** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"status":"completed\|cancelled"}` | 全流程结束 |
| **error** | [manager.go](file:///E:/Argus/argus-desktop/internal/chat/manager.go) | `{"error":"...","stage":"pm\|se\|system"}` | 出错 |
| **heartbeat** | [sse_bridge.go](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go) | `{"pm_status":"idle","se_status":"idle"}` | 每10秒心跳保活 |

### 完整事件流示例

```
: connected                          ← 连接建立确认

event: pm_started                    ← PM 开始思考
data: {}

event: pm_message                   ← PM 输出分析结果
data: {"delta":"好的，我来创建 Hello World"}

event: se_task_assigned             ← PM 分配任务给 SE
data: {"task":"创建 main.go","steps":2}

event: se_action                    ← SE 写文件
data: {"action":"write_file","path":"main.go"}

event: se_action                    ← SE 编译运行
data: {"action":"exec","command":"go run main.go"}

event: se_output                    ← 执行输出
data: {"output":"Hello, World!\n","exit_code":0}

event: se_completed                 ← SE 完成
data: {"result":"程序编译运行成功"}

event: pm_message                   ← PM 审核
data: {"delta":"✅ 已完成，程序输出正确"}

event: done                         ← 全部完成
data: {"status":"completed"}
```

---

## 四、IDE 对话模式（v0.9.6+）

### 4.1 概览

PM 可通过 `ide_send` 工具与外部 IDE 进行多轮对话协调。支持**任意数量、动态命名**的 IDE 同时参与——谁连上就显示谁，无需预注册。

| 角色 | 说明 |
|------|------|
| **PM** | 主持人，使用 `ide_send` 工具与 IDE 对话，决定何时 terminate |
| **IDE（动态）** | 通过 SSE 长连接接收 PM 消息，通过 `ide-input` 回复。名称由 `source` 字段决定（如 `IDE-A`、`VSCode`、`Cursor` 等） |
| **用户** | 设定目标，PM 围绕目标组织讨论 |

### 4.2 连接方式（动态命名）

IDE 建立 SSE 连接时通过 `source` 字段声明自身名称（任意值），进入多连接模式。同一名称允许多个客户端同时连接。

```powershell
# IDE-A 连接（source=IDE-A）
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"message":"准备就绪","source":"IDE-A"}'

# 任意名称连接（source 无限制）
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"message":"准备好","source":"VSCode"}'

curl -N -X POST http://localhost:8080/api/v1/sse/subscribe `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"message":"准备好","source":"Cursor"}'
```

连接成功后收到 `connected` 事件，含 `session_id`（后续发消息用）：

```
event: connected
data: {"session_id":"sse-1741234567","source":"IDE-A"}
```

### 4.3 消息收发

**IDE 发消息给 PM：**
```powershell
curl -X POST http://localhost:8080/api/v1/sse/ide-input `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"session_id":"sse-1741234567","message":"分析结果：需要重构3个函数"}'
```

**PM 发消息给 IDE（通过 `ide_message` 事件）：**
```
event: ide_message
data: {"from":"PM","message":"IDE-B 你的意见呢？","action":"discuss","message_id":"ide_msg_1741234567_1"}
```

每条 `ide_message` 包含 `message_id`，IDE 收到后应回传 ACK：

```powershell
curl -X POST http://localhost:8080/api/v1/sse/ide-ack `
  -H "Authorization: Bearer your-token" `
  -H "Content-Type: application/json" `
  -d '{"message_id":"ide_msg_1741234567_1"}'
```

### 4.4 PM 工具：ide_send

PM 内部使用 `ide_send` 工具与 IDE 通信，IDE 端无需关心此工具，只需接收 `ide_message` 事件。

| 参数 | 值 | 含义 |
|------|----|------|
| `target` | `ALL` 或任意连接的 IDE 名称（如 `IDE-A`、`VSCode`） | 目标（动态名称） |
| `action` | `discuss` / `instruct` / `terminate` | 讨论/指令/结束 |
| `message` | 自然语言 | 内容 |

`terminate` 后 PM 会向所有 IDE 发送 `done` 事件，对话结束。

### 4.5 完整对话示例

```
1. IDE-A 连接 → 收到 connected
2. IDE-B 连接 → 收到 connected
3. IDE-A 发消息: "建议用策略模式重构"
4. PM 处理后用 ide_send 转发给 IDE-B:
   → event: ide_message {"message":"IDE-B 你觉得呢？","action":"discuss","message_id":"..."}
5. IDE-B 回复 ACK + 通过 ide-input 回复
6. 循环直到 PM 认为方案成熟
7. PM 发 terminate → 所有 IDE 收到 done 事件
```

### 4.6 前端 TopBar 连接状态（动态）

IDE 连接/断开时，前端 TopBar 右侧会动态显示已连接 IDE 的绿色圆点指示器。谁连上就显示谁，无需预配置：

```
... [PM] [MC] [SE] [AP] | ●A ●R ●r
                          ↑  ↑    ↑
                        IDE-A VSCode Cursor
```

- 绿色圆点：已连接（取名称末位字母）
- 无指示器：未连接（过一会儿自动消失）

---

## 五、常见问题排查

### 问题 1：连接后无任何事件返回

**症状：** `curl` 卡住不动，只收到 `: connected\n\n`

**排查步骤：**

```powershell
# 1. 确认 HTTP 服务已启动
curl http://localhost:8080/api/v1/status

# 2. 检查 ChatManager 是否初始化
# 返回中应包含 chatManager 字段

# 3. 检查日志是否有 [HTTPServer/SSE] SendMessage:
# 如果没有 → JSON 解析失败或消息为空

# 4. 最可能原因：SendMessage 成功但 AI 无响应（API Key 问题）
# 查看 app 日志中的 API 调用错误
```

**常见原因：**
- `message` 字段为空或 JSON 格式错误
- API Key 无效/额度用尽
- AI 服务超时无响应

---

### 问题 2：收到 `409 Conflict`

```json
{"status":"error","error":"调试模式已有活跃连接，请稍后重试"}
```

**原因：** 调试模式（不传 `source`）是**单连接模型**，同一时间只能有一个调试客户端订阅。

**注意：** IDE 模式（传了 `source`）**没有此限制**，多个 IDE 可同时连接。

**解决（调试模式）：**
- 关闭之前的连接（Ctrl+C 或关闭终端）
- 或等待旧连接超时（120 秒自动断开）
- 检查是否有其他进程占用

---

### 问题 3：收到 `error` 事件后断开

**正常情况：** AI 处理出错，事件携带 stage 字段标识阶段：

```json
event: error
data: {"error":"API 返回空响应","stage":"pm"}
```

| stage | 含义 |
|-------|------|
| `pm` | PM 调用 AI 失败 |
| `se` | SE 执行失败 |
| `system` | 系统级错误（SendMessage 本身失败） |

---

### 问题 4：事件丢失（⚠️ channel 满）

**日志中出现：**
```
[SSEBridge] ⚠️ 订阅者 channel 已满，丢弃事件: se_output
```

**原因：** channel 缓冲只有 64，客户端消费太慢或网络阻塞。

**当前行为：** 静默丢弃，仅打印警告。

**缓解：** 监控此日志频率，高频出现说明需要：
- 加大 channel 缓冲（改 `make(chan SSEEvent, 256)`）
- 或实现背压机制（盲扫 #25 待修复）

---

### 问题 5：120 秒超时自动断开

**现象：** 长时间任务中途收到：
```
event: error
data: {"error":"timeout"}
```

**原因：** [http_server.go:186](file:///E:/Argus/argus-desktop/http_server.go#L186) 设置了 `time.After(120 * time.Second)`。

**这是设计如此：** 防止僵尸连接。如果任务确实需要更长时间，需调整超时值。

---

## 六、关键代码位置索引

### SSE Bridge 核心

| 功能 | 文件:行号 | 说明 |
|------|----------|------|
| 结构定义 | [sse_bridge.go:10-16](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L10) | SSEEvent, SSEBridge |
| Subscribe | [sse_bridge.go:25-38](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L25) | 单连接，返回 chan |
| Unsubscribe | [sse_bridge.go:41-55](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L41) | 清理 + 停止心跳 |
| Push | [sse_bridge.go:58-72](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L58) | 推送事件到 channel |
| StartHeartbeat | [sse_bridge.go:75-100](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L75) | 每 10 秒 heartbeat |
| FormatSSE | [sse_bridge.go:139-142](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L139) | 格式化为 SSE 文本 |

### HTTP 入口

| 功能 | 文件:行号 | 说明 |
|------|----------|------|
| 路由注册 | [http_server.go:57](file:///E:/Argus/argus-desktop/http_server.go#L57) | POST /api/v1/sse/subscribe |
| Handler | [http_server.go:117-198](file:///E:/Argus/argus-desktop/http_server.go#L117) | 长连接循环 |
| 超时控制 | [http_server.go:186](file:///E:/Argus/argus-desktop/http_server.go#L186) | 120 秒 |
| 断连检测 | [http_server.go:173](file:///E:/Argus/argus-desktop/http_server.go#L173) | channel 关闭即退出 |

### 埋点位置（14 处）

| 行号 | 事件 | 触发场景 |
|------|------|---------|
| [manager.go:800](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L800) | `pm_started` | handleToPM 进入 |
| [manager.go:831](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L831) | `error` | PM AI 调用失败 |
| [manager.go:978](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L978) | `error` | PM 回复解析失败 |
| [manager.go:1090](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1090) | `se_completed` | SE 任务结束 |
| [manager.go:1236](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1236) | `se_action` | SE 执行 write/exec/read |
| [manager.go:1248](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1248) | `se_output` | exec 标准输出 |
| [manager.go:1263](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1263) | `se_output` | exec 标准错误 |
| [manager.go:1277](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1277) | `se_output` | exec 组合输出 |
| [manager.go:1295](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1295) | `se_output` | exec 超时输出 |
| [manager.go:1430](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1430) | `pm_message` | PM 直接回复用户 |
| [manager.go:1451](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1451) | `done` | 正常完成 |
| [manager.go:1509](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1509) | `pm_message` | PM 审核后回复 |
| [manager.go:1510](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1510) | `se_task_assigned` | PM @SE 分配任务 |
| [manager.go:1927](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1927) | `error` | SE actions 执行异常 |
| [manager.go:1992](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1992) | `done` | PM 审核通过完成 |
| [manager.go:2145](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L2145) | `done` | 用户取消 |

---

## 七、调试技巧

### 6.1 开启详细日志

程序启动后，以下关键字出现在 stdout 即表示 SSE 工作正常：

```
[HTTPServer] SSE 端点 (/api/v1/sse/):
[HTTPServer/SSE] SendMessage: <你的消息>
[SSEBridge] 订阅者 sse-xxx 已连接 (活跃连接: 1)
[SSEBridge] ⚠️ 订阅者 channel 已满，丢弃事件: xxx   ← 警告
[SSEBridge] 订阅者 sse-xxx 已断开 (活跃连接: 0)      ← 正常断开
```

### 6.2 用 curl 做最简测试

```bash
# 最小可用命令（无需 token 时）
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe \
  -d '{"message":"hi"}'

# 带 token
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe \
  -H "Authorization: Bearer test123" \
  -d '{"message":"hi"}'
```

### 6.3 测试单连接限制

```bash
# 终端1：先占住连接
curl -N -X POST http://localhost:8080/api/v1/sse/subscribe -d '{"message":"test"}'

# 终端2：应该收到 409
curl -X POST http://localhost:8080/api/v1/sse/subscribe -d '{"message":"test2"}'
# → {"status":"error","error":"已有一个活跃的SSE连接，请稍后重试"}
```

### 6.4 测试单元测试

```bash
go test ./internal/chat/ -run TestSSE -v
```

覆盖 7 个测试用例：
- `TestSSEBridge_SubscribeAndUnsubscribe` — 订阅/取消
- `TestSSEBridge_SingleSubscriberOnly` — 单连接限制
- `TestSSEBridge_PushEvent` — 事件推送
- `TestSSEBridge_PushWhenNoSubscriber` — 无订阅者不崩溃
- `TestSSEBridge_Heartbeat` — 心跳
- `TestSSEBridge_ConcurrentPush` — 100 并发推送
- `TestFormatSSE` — 格式化校验

---

## 八、已知限制

| # | 限制 | 影响 | 状态 |
|---|------|------|------|
| 1 | 调试模式单连接 | 同时只能一个调试客户端 | 设计如此 |
| 2 | IDE 模式多连接 | IDE-A/IDE-B 可同时在线 | ✅ v0.9.6 |
| 3 | channel 缓冲 64 | 高频事件可能丢失 | #25 待修 |
| 4 | 超时固定 120s | 长任务可能被截断 | 可配置化 |
| 5 | 心跳数据硬编码 | pm_status/se_status 固定 idle | 低优先级 |
| 6 | 无断线重连 | 客户端断开后状态丢失 | P2 |
| 7 | IDE ACK 无自动重试 | 未收到 ACK 不自动重推 | 待增强 |
