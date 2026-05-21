# PM 三层消息模型实施 - 工作进度汇报

**日期**: 2026-05-21
**状态**: ✅ 已验证通过（PM/SE TaskList 全场景）
**分支**: temp/rich-message-v1

---

## 一、项目目标

实现 **PM/SE/AP 通用三层消息模型**，将 AI 工作过程可视化展示：

```
┌─ 📋 TaskList（任务列表）────────────┐
│  ✓ 分析用户需求                     │
│     hi                              │ ← 具体内容 (detail)
│  ✓ 分配 SE 任务                     │
│  ✓ 审核 SE 结果                     │
│  你好！今天是周一...                 │ ← 结果文本
└────────────────────────────────────┘

┌─ 💻 Shell（命令输出）───────────────┐
│  ▶ write_file hello.go             │
│     ✅ exit 0                       │
│  ▶ go run hello.go                 │
│     Hello, World!                  │
│     ✅ exit 0 (0.1s)               │
└────────────────────────────────────┘

┌─ 📝 Result（结论文本）─────────────┐
│  @USR ✅ AP审批通过                │
└────────────────────────────────────┘
```

---

## 二、本次完成的工作（2026-05-21 晚）

### ✅ 2.1 PM TaskList 不显示问题修复

**G点**: [manager.go:1594](../internal/chat/manager.go#L1594) 的 `@USR 闲聊分支` 缺少 `CompleteTaskList` 调用

| 修改位置 | 改动 | 说明 |
|---------|------|------|
| [manager.go:1553-1566](../internal/chat/manager.go#L1553-L1566) | +CompleteTaskList("done") | 闲聊分支正常路径 |
| [manager.go:1321-1328](../internal/chat/manager.go#L1321-L1328) | +CompleteTaskList("error") | 错误路径兜底 |

**修复前**: PM 回复只显示纯文本，无 TaskList
**修复后**: 所有 PM 路径都有完整的 TaskList 生命周期

### ✅ 2.2 TaskList 内容智能化（告别指示灯）

**核心改进**: TaskItem 新增 `Detail` 字段，每一步携带具体任务内容

| 文件 | 改动 |
|------|------|
| [rich_message.go:23](../internal/types/rich_message.go#L23) | TaskItem 添加 `Detail string` |
| [rich-message.ts:4](../frontend/src/types/rich-message.ts#L4) | TS 接口添加 `detail?: string` |
| [rich_message_builder.go:69](../internal/chat/rich_message_builder.go#L69) | UpdateTask 支持 detail 可选参数 |
| [TaskListBlock.vue:29-32](../frontend/src/components/chat/TaskListBlock.vue#L29-L32) | detail 展示（等宽字体+灰色背景） |
| [App.vue:536](../frontend/src/App.vue#L536) | 前端事件监听处理 detail |
| [manager.go:1263](../internal/chat/manager.go#L1263) | PM 传入用户需求作为 detail |
| [manager.go:1830](../internal/chat/manager.go#L1830) | SE 传入任务描述作为 detail |

**效果对比**:

```
❌ 之前（空步骤 - 指示灯）:
   ○ 分析用户需求
   ○ 分配 SE 任务
   ○ 审核 SE 结果

✅ 现在（有血有肉）:
   ○ 分析用户需求
      SSE自动化验证                    ← detail: 用户输入内容
   ✓ 分配 SE 任务
   ○ 审核 SE 结果
      你好！今天是周一...              ← result: PM回复内容
```

### ✅ 2.3 SSE 双通道推送（Wails + HTTP）

**G点**: TaskList 事件只走 Wails Events（GUI），不走 SSE HTTP（自动化测试收不到）

**修复**: [manager.go:2217](../internal/chat/manager.go#L2217) `emitWailsEvent` 同时推送到 SSE Bridge

```go
// emitWailsEvent 安全触发Wails前端事件（ctx为nil时跳过），同时推送到SSE
func (m *Manager) emitWailsEvent(eventName string, data interface{}) {
    if m.ctx != nil {
        runtime.EventsEmit(m.ctx, eventName, data)
    }
    m.pushSSEEvent(eventName, data)  // ← 新增：SSE通道
}
```

**效果**: GUI 和 HTTP 客户端都能收到完整事件流

### ✅ 2.4 SSE 连接管理增强

新增 `/admin/sse-reset` 端点，解决单连接残留问题：

| 文件 | 改动 |
|------|------|
| [sse_bridge.go:94-106](../internal/chat/sse_bridge.go#L94-L106) | 新增 ForceReset() 方法 |
| [http_server.go:79](../http_server.go#L79) | 注册 POST /admin/sse-reset 路由 |
| [http_server.go:213-220](../http_server.go#L213-L220) | handleSSEReset handler |

### ✅ 2.5 自动化测试脚本

创建 [test-sse-tasklist.ps1](../test-sse-tasklist.ps1)，验证 6 个关键检查点：

| 检查项 | 验证内容 |
|--------|---------|
| pm_started | PM 开始工作事件 |
| PM tasklist_start | PM TaskList 初始化（含 roleId:"pm"）|
| tasklist_update | 任务状态更新事件 |
| detail field | 更新事件包含具体内容 |
| tasklist_complete | TaskList 完成事件 |
| done | 整体流程结束 |

---

## 三、已验证的功能

### ✅ 3.1 PM 闲聊场景（新验证）

**测试输入**: `hello` / `hi` / `SSE自动化验证`

**SSE 事件流**:
```
event: tasklist_start      → {roleId:"pm", tasks:[分析需求,分配SE,审核结果]}
event: tasklist_update     → {detail:"hello", status:"running", taskIndex:0}
event: tasklist_update     → {detail:"hello", status:"done", taskIndex:0}
event: tasklist_update     → {status:"done", taskIndex:1}       // 分配SE(跳过)
event: tasklist_update     → {status:"done", taskIndex:2}       // 审核(跳过)
event: tasklist_complete   → {result:{text:"你好！今天是周一..."}, status:"done"}
```

**GUI 显示**: ✅ 完整 TaskList + Result

### ✅ 3.2 PM→SE 分配任务场景（新验证）

**测试输入**: `写一个Hello World程序`

**SSE 事件流**:
```
event: tasklist_start (PM)  → PM 分配任务
event: tasklist_update      → detail: "写一个Hello World..."
event: se_task_assigned     → SE 收到任务
event: tasklist_start (SE)  → SE 执行: "请创建 hello.go..."
event: tasklist_update      → detail: "请创建 hello.go, 内容为输出..."
event: exec_start           → write_file hello.go
event: exec_done            → write_file done
event: exec_start           → go run hello.go
event: shell_start          → command: "go run hello.go"
event: shell_output         → "Hello, World!\n"
event: shell_done           → exitCode: 0
event: exec_done            → exec done
event: tasklist_complete    → SE 完成
event: tasklist_complete    → PM 完成
```

**GUI 显示**: ✅ PM TaskList + SE TaskList + Shell 输出 + Result

### ✅ 3.3 数据流完整性

```
用户消息
  ↓ SendMessage()
  ↓ handleToPM() / startSETaskWithFrom()
  ↓ richBuilder.StartTaskList("pm"/"se", title, tasks)
  ↓   ├─ runtime.EventsEmit(ctx, "tasklist_start", data)  → GUI 前端
  ↓   └─ pushSSEEvent("tasklist_start", data)              → SSE HTTP
  ↓ richBuilder.UpdateTask(id, idx, "running", detail)
  ↓   ├─ runtime.EventsEmit(ctx, "tasklist_update", data) → GUI 前端
  ↓   └─ pushSSEEvent("tasklist_update", data)            → SSE HTTP
  ↓ ... (shell events 同理)
  ↓ richBuilder.CompleteTaskList(id, "done", result)
  ↓   ├─ runtime.EventsEmit(ctx, "tasklist_complete", data)
  ↓   └─ pushSSEEvent("tasklist_complete", data)
```

---

## 四、技术架构图

### 4.1 通用化 TaskList 组件结构

```
TaskListBlock.vue (通用任务列表)
├── role 标识 (pm / se / ap)
├── title 标题 ("PM 分配任务" / "SE 执行: xxx")
├── Task Items[]
│   ├── text: 步骤名称 ("分析用户需求")
│   ├── status: pending/running/done/error
│   └── detail: 具体内容 ("SSE自动化验证")  ← 新增!
├── result (完成后显示)
└── 收起/展开按钮
```

### 4.2 后端事件流（双通道）

```
RichMessageBuilder.emitFunc(event, data)
    │
    ├─→ runtime.EventsEmit(Wails ctx)  ──→ GUI 前端 (WebView2)
    │                                        └── App.vue EventsOn()
    │                                            └── window.__richMessages[]
    │                                                └── <RichMessage> 渲染
    │
    └─→ pushSSEEvent(SSEBridge)  ──→ SSE HTTP (/api/v1/sse/subscribe)
                                          └── curl / EventSource
                                              └── 自动化测试脚本
```

### 4.3 SSE 连接生命周期

```
HTTP POST /api/v1/sse/subscribe
  │
  ├─ 1. SSEBridge.Subscribe(id) → 获取 channel
  │     └─ 如果 activeConnID != "" → 返回错误 "已有活跃连接"
  │
  ├─ 2. 写入 ": connected\n\n"
  │
  ├─ 3. SendMessage(req.Message) → 触发处理流程
  │     └─ emitWailsEvent() → PushEvent() → channel
  │
  ├─ 4. 循环读取 channel → 写入 SSE 响应
  │     └─ 收到 "done"/"error" → 断开
  │
  └─ 5. SSEBridge.Unsubscribe(id) → 清理
         └─ activeConnID = ""
         
  ⚠️ 异常情况: curl 超时断开但 Unsubscribe 未调用
     → 解决: POST /admin/sse-reset → ForceReset()
```

---

## 五、文件变更清单

### 5.1 本次修改文件

| 文件路径 | 修改类型 | 行数 | 说明 |
|---------|---------|------|------|
| `internal/chat/manager.go` | 修改 | +20 | PM CompleteTaskList、emitWailsEvent双通道、detail传入 |
| `internal/chat/rich_message_builder.go` | 修改 | +5 | UpdateTask 支持 detail 参数 |
| `internal/types/rich_message.go` | 修改 | +1 | TaskItem.Detail 字段 |
| `internal/chat/sse_bridge.go` | 修改 | +15 | ForceReset() 方法 |
| `http_server.go` | 修改 | +10 | /admin/sse-reset 端点 |
| `frontend/src/types/rich-message.ts` | 修改 | +1 | TaskItem.detail 接口 |
| `frontend/src/components/chat/TaskListBlock.vue` | 修改 | +10 | detail 展示样式 |
| `frontend/src/App.vue` | 修改 | +3 | tasklist_update 处理 detail |
| `test-sse-tasklist.ps1` | 新建 | ~80 | SSE 自动化测试脚本 |

### 5.2 已有文件（未修改但相关）

| 文件路径 | 角色 |
|---------|------|
| `frontend/src/components/chat/RichMessage.vue` | 主容器组件 |
| `frontend/src/components/chat/ShellBlock.vue` | Shell 输出组件 |
| `docs/SSE调试手册.md` | SSE 协议说明 |

---

## 六、测试报告

### 6.1 通过的测试 ✅

| # | 测试场景 | 输入 | 关键验证点 | 状态 |
|---|---------|------|-----------|------|
| T1 | PM 闲聊 TaskList | `hello` | tasklist_start + update(detail) + complete | ✅ Pass |
| T2 | PM→SE 分配 | `写Hello World` | PM+SE 双 TaskList + Shell 事件 | ✅ Pass |
| T3 | Detail 字段 | 任意 | tasklist_update 包含具体内容 | ✅ Pass |
| T4 | SSE 双通道 | curl HTTP | 与 GUI 收到相同事件 | ✅ Pass |
| T5 | SSE Reset | POST /admin/sse-reset | 清理残留连接 | ✅ Pass |
| T6 | GUI 渲染 | 任意 | TaskList 有血有肉非指示灯 | ✅ Pass |

### 6.2 已知限制

| # | 限制 | 影响 | 计划 |
|---|------|------|------|
| L1 | SSE 单连接 | 同时只能一个 HTTP 客户端 | 已有 reset 端点 |
| L2 | PM 闲聊时 SE 步骤跳过 | TaskList 第2-3步直接 done | 正常行为（无实际操作）|

---

## 七、下一步计划

### Phase 2: AP 角色支持（待开始）

AP 审批也使用三层模型展示：
- 新建 `handleAPReviewWithRich()` 函数
- AP Processor 集成 RichMessageBuilder
- 复用已有的 TaskListBlock 组件

### Phase 3: SSE 多连接优化（可选）

当前单连接限制可能影响：
- GUI 前端同时使用 SSE
- 多个自动化测试并行

方案：改为多连接或连接池模式

---

## 八、经验教训

### ✅ 成功经验

1. **G点法定位根因**: 从现象（0/6失败）追踪到 G点（SSE 单通道 vs 双通道）
2. **渐进式验证**: 先手动 curl 确认数据正确，再修脚本
3. **双通道设计**: emitWailsEvent + pushSSEEvent 一处推送两处受益

### ❌ 教训

1. **PowerShell 文件读取时机**: `& curl.exe` 是异步的，必须用 `WaitForExit()` 或管道等待
2. **SSE 单连接残留**: curl 超时不会触发服务端 Unsubscribe，需要 ForceReset
3. **不要假设**: 手动 curl 能读到 3397 bytes ≠ 脚本能读到（环境差异）

---

## 九、附录

### A. 相关文档

- [RICH-MESSAGE-DESIGN.md](./docs/RICH-MESSAGE-DESIGN.md) - 三层模型设计文档
- [SSE调试手册.md](./docs/SSE调试手册.md) - SSE 事件协议说明
- [.trae/rules/project_rules.md](./.trae/rules/project_rules.md) - 项目编译规则

### B. Git 提交历史

```
分支: temp/rich-message-v1
最近提交: PM TaskList 修复 + 智能化 + SSE 双通道 (2026-05-21)
```

### C. 环境信息

- **操作系统**: Windows
- **Go 版本**: go1.26.2 windows/amd64
- **Wails 版本**: v2.12.0
- **Node 版本**: v25.2.1
- **默认 API**: NVIDIA (qwen/qwen3-coder-480b-a35b-instruct)

---

**文档编写**: AI Assistant
**最后更新**: 2026-05-21 00:30
