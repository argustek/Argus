# Argus SSE 调试手册

> 最后更新：2026-05-14
> 适用版本：v0.1.0+

---

## 一、架构概览

```
┌──────────────┐    POST /api/v1/sse/subscribe     ┌─────────────────┐
│  HTTP 客户端  │ ───────────────────────────────▶ │   http_server.go │
│ (curl/脚本)   │ ◀── text/event-stream 流式响应 ──│  handleSSESubscribe│
└──────────────┘                                  └────────┬────────┘
                                                          │
                                                   Subscribe(id)
                                                          ▼
                                                  ┌──────────────┐
                                                  │  SSEBridge    │
                                                  │ sse_bridge.go │
                                                  │              │
                                                  │ ch(64)       │◀── PushEvent()
                                                  └──────┬───────┘
                                                         │
                              ┌──────────────────────────┼──────────────────────────┐
                              ▼                          ▼                          ▼
                     manager.go:800             manager.go:1236            manager.go:1451
                     handleToPM()               executeSEActions()         PM审核完成
                     → pm_started               → se_action / se_output   → done
                     → pm_message              → se_completed
```

### 核心组件

| 组件 | 文件 | 职责 |
|------|------|------|
| **SSEBridge** | `internal/chat/sse_bridge.go` | 单订阅者事件总线，channel 缓冲 64 |
| **HTTP Handler** | `http_server.go:117-198` | SSE 长连接入口，转发事件到客户端 |
| **埋点** | `internal/chat/manager.go` | 14 处 PushEvent 调用，覆盖完整生命周期 |

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
| **pm_started** | [manager.go:800](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L800) | `{}` | PM 开始处理 |
| **pm_message** | [manager.go:1430](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1430), [1509](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1509) | `{"delta":"..."}` | PM 输出内容片段 |
| **se_task_assigned** | [manager.go:1510](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1510) | `{"task":"...","steps":3}` | PM 分配任务给 SE |
| **se_action** | [manager.go:1236](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1236) | `{"action":"write_file","path":"..."}` | SE 执行动作 |
| **se_output** | [manager.go:1248,1263,1277,1295](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1248) | `{"output":"...","exit_code":0}` | 命令执行输出 |
| **se_completed** | [manager.go:1090](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1090) | `{"result":"..."}` | SE 任务完成 |
| **done** | [manager.go:1451,1992,2145](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L1451) | `{"status":"completed\|cancelled"}` | 全流程结束 |
| **error** | [manager.go:831,978,1927](file:///E:/Argus/argus-desktop/internal/chat/manager.go#L831) | `{"error":"...","stage":"pm\|se\|system"}` | 出错 |
| **heartbeat** | [sse_bridge.go:109](file:///E:/Argus/argus-desktop/internal/chat/sse_bridge.go#L109) | `{"pm_status":"idle","se_status":"idle"}` | 每10秒心跳保活 |

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

## 四、常见问题排查

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
{"status":"error","error":"已有一个活跃的SSE连接，请稍后重试"}
```

**原因：** SSE 是**单连接模型**，同一时间只能有一个客户端订阅。

**解决：**
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

## 五、关键代码位置索引

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

## 六、调试技巧

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

## 七、已知限制

| # | 限制 | 影响 | 状态 |
|---|------|------|------|
| 1 | 单连接模型 | 同时只能一个 HTTP 客户端 | 设计如此 |
| 2 | channel 缓冲 64 | 高频事件可能丢失 | #25 待修 |
| 3 | 超时固定 120s | 长任务可能被截断 | 可配置化 |
| 4 | 心跳数据硬编码 | pm_status/se_status 固定 idle | 低优先级 |
| 5 | 前端未接入 | se_output 事件无人消费 | #19 待修 |
| 6 | 无断线重连 | 客户端断开后状态丢失 | P2 |
