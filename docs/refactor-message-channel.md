# 消息通道重构方案

> 2026-06-19 | 决策: 统一 MessageBus + OpenCode 风格消息分框

---

## 0. 核心原则（不可违反）

**所有消息, 无一例外, 不管来自 AI 还是系统内部, 全部走 MessageBus。**

| 来源 | MessageBus | EventsEmit | PushSSEEvent |
|------|:---------:|:----------:|:------------:|
| AI 消息 (PM/SE/AP) | ✅ 唯一通道 | ❌ 禁止 | ❌ 禁止 |
| 系统事件 (git/status/reset/error/warning) | ✅ 唯一通道 | ❌ 禁止 | ❌ 禁止 |
| 执行事件 (exec_start/done/output) | ✅ 唯一通道 | ❌ 禁止 | ❌ 禁止 |
| 诊断事件 (message_lost) | ✅ 唯一通道 | ❌ 禁止 | ❌ 禁止 |
| IDE 事件 (ide_message/ide_status) | ✅ 唯一通道 | ❌ 禁止 | ❌ 禁止 |
| SSE 外部事件 (se_message/review_result/ap_result) | ✅ 唯一通道 | ❌ 禁止 | ❌ 禁止 |

**结论**: `runtime.EventsEmit` 和 `PushSSEEvent` 从后端代码中彻底删除。

## 1. 最终架构

```
后端 (Go)                         前端 (Vue)
┌──────────────────┐            ┌──────────────────┐
│  msgBus.Send()   │──MessageBus──→│  OnCoreMessage   │
│  (唯一出口)       │            │  (唯一入口)       │
└──────────────────┘            └────────┬─────────┘
                                         │
                                    messages.value[]
                                         │
                                   ChatPanel.vue
                                   纯文本消息分框渲染
```

**规则**:
- 后端 → 前端: **只走 MessageBus**, 移除所有直接 `EventsEmit` / `PushSSEEvent`
- 前端 → 后端: **只走 MessageBus**, 通过 `SendMessage` / `ackMessage`
- conversation.log = MessageBus Ack = 对话框内容, **三者完全一致**

---

## 2. 消息结构

```typescript
interface Message {
  role: 'usr' | 'pm' | 'se' | 'ap' | 'mc'
  content: string          // 自然语言摘要（说人话）
  sections?: Section[]     // 子框列表（可选）
  timestamp: number
  meta?: {
    status?: 'running' | 'done' | 'error'
    actionCount?: number   // 如 "3/3 完成"
  }
}

interface Section {
  type: 'terminal' | 'file_diff' | 'actions' | 'changes' | 'tasklist'
  label: string            // "终端输出 (3条)" 或 "[1/3] 写代码"
  content: string          // 原始内容
  collapsed?: boolean      // 默认 true（终端/长内容）
}
```

---

## 3. 消息块渲染结构

```
┌──────────────────────────────────────┐
│  19:35  PM    ✅ 3/3 完成            │  ← 角色 + 时间 + 状态
│  已创建 hello.go 并运行通过           │  ← 自然语言摘要
│  ┌─ 终端输出 ──────────────────────┐ │
│  │ $ go run hello.go              │ │  ← type=terminal, 可滚动
│  │ Hello, World!                  │ │
│  └─────────────────────────────────┘ │
│  [▶] 文件变更 (2个)                  │  ← type=changes, 默认折叠
├──────────────────────────────────────┤
│  19:36  SE    ✅ 3/3 完成            │
│  写入了 hello.go 并运行验证通过       │
│  [▶] 终端输出 (1条)                  │
├──────────────────────────────────────┤
│  19:37  USR                          │
│  改成输出中文                        │
└──────────────────────────────────────┘
```

### 每种 Section 的渲染方式

| type | 显示方式 | 默认折叠 | 说明 |
|------|---------|:-------:|------|
| `terminal` | 灰底等宽字体 + 滚动 | ✅ | exec 输出, `$ 命令` 前缀 |
| `file_diff` | 绿(+)/红(-) diff | ✅ | write_file/edit_file 变更 |
| `actions` | 列表: ✅/🔄/❌ | ❌ | 操作步骤摘要 |
| `changes` | 列表: +hello.go -old.go | ✅ | 文件变更 |
| `tasklist` | `[▶] 标题 (N/M 完成)` | ✅ | 任务列表 |

### 折叠展开机制（纯文本控制）

```
初始: [▶] 终端输出 (3条)
点击: [▶] → [▼], 显示子内容
再点: [▼] → [▶], 隐藏子内容

// 前端维护一个 collapsedSections: Set<string>
// key = `${msgIndex}-${sectionIndex}`
// 切换: set.has(key) ? set.delete(key) : set.add(key)
```

纯文本折叠不需要 CSS `display:none`/`v-if`，直接用 `v-show` 控制的 `<pre>`/`<div>` 即可。

---

## 4. 需要移除的旧代码

### 后端 — 全部清理清单

| 位置 | 代码 | 原因 |
|------|------|------|
| `bridge.go:245` | `pushSSEEvent("pm_message", ...)` | PM 内容已走 MessageBus |
| `bridge.go:254-261` | `pushSSEEvent("se_message"/"review_result"/"ap_result"/"error")` | 改为只走 MessageBus |
| `manager.go:3490-3493` | `emitWailsEvent` 函数 | 不再需要，全部走 `msgBusSend` |
| `manager.go:3490` | `PushSSEEvent` 第二次调用 | emitWailsEvent 内的冗余 SSE |
| `manager.go:所有 msgBusSend 调用` | 检查 path 是否正确 | 确保所有 PathStatus 改为 tracked |
| `message_bus.go:525` | `runtime.EventsEmit("message_lost")` | 诊断事件也走 MessageBus |
| `app.go:所有 emitToFrontend 调用` | `msgBus.Send` 以外的路径 | 全部收敛到 msgBus.Send |
| `manager.go:所有 PushSSEEvent 调用` | 直接 SSE 推送 | 全部删除，只留 MessageBus |

**搜索清单**（在代码中搜索并逐个处理）:
```
grep -rn "PushSSEEvent" internal/   # 删除所有
grep -rn "EventsEmit" internal/     # 删除所有（除 Wails 框架自身调用）
grep -rn "emitWailsEvent" internal/ # 删除所有调用
```

### 前端（App.vue EventsOn 处理器 — 全部移除）

| 行 | 事件 | 原因 |
|----|------|------|
| 582 | `pm_message` | new-message 已覆盖 |
| 601 | `ap_message` | new-message 已覆盖 |
| 657 | `exec_start` | 不再需要 exec 卡片 |
| 683 | `exec_done` | 同上 |
| 694 | `exec_output` | 同上 |
| 706 | `exec_completed` | 同上 |
| 853 | `pm_streaming_done` | 不再需要 |
| 721 | `pm_review_completed` | 不再需要 |
| 391 | `token_stats` | new-message / role-state 已覆盖 |
| 395 | `context_built` | 系统事件走 MessageBus |
| 399 | `compress_done` | 系统事件走 MessageBus |
| 404 | `project-state-changed` | 系统事件走 MessageBus |
| 427 | `project-level` | 系统事件走 MessageBus |
| 436 | `git:repo-info` | new-message 已覆盖 |
| 440 | `git:status` | new-message 已覆盖 |
| 446 | `terminal:output` | 系统事件走 MessageBus |
| 547 | `ai-stream-chunk` | new-message 已覆盖 |
| 621 | `messages-cleared` | 系统事件走 MessageBus |
| 634 | `todo_update` | 系统事件走 MessageBus |
| 740 | `message_lost` | 系统事件走 MessageBus |
| 769 | `warning` | 系统事件走 MessageBus |
| 793 | `role-state` | new-message 已覆盖 |
| 810 | `ide_status` | 系统事件走 MessageBus |
| 823 | `role-status` | role-state 已覆盖 |
| 841 | `error` | new-message 已覆盖 |
| 871 | `ai-thinking` | 系统事件走 MessageBus |
| 878 | `agent-thought` | 系统事件走 MessageBus |
| 898 | `reset-completed` | 系统事件走 MessageBus |
| 903 | `reset` | 系统事件走 MessageBus |
| 906 | `tasks_cleared` | 系统事件走 MessageBus |
| 909 | `done` | 系统事件走 MessageBus |
| 914 | `project_approved` | 系统事件走 MessageBus |
| 922 | `se-file-written` | 系统事件走 MessageBus |
| 928 | `task-recovered` | 系统事件走 MessageBus |
| 953-993 | `tasklist_*` (4个事件) | 不再需要 RichMessage |
| 1006-1029 | `shell_*` (3个事件) | 不再需要 RichMessage |
| 1140 | `diff_preview` | 系统事件走 MessageBus |

**共移除 ~43 个 EventsOn 处理器**, 只保留 `new-message` 一个入口。

### 前端（ChatPanel.vue 渲染分支）

| 行 | 代码 | 原因 |
|----|------|------|
| 184-216 | `_execData` 卡片模板 | 改为 sections 数组渲染 |
| 80-180 | `RichMessage`/`SERichMessage` 分支 | 不再需要 |
| 全局 | `renderStructured`/`getRichMessage`/`getSummary`/`getPreviewText` | 不再需要 |
| 全局 | `renderSEText`/`getSEActions`/`getSEShellOutput` | 不再需要 |

### 后端（RichMessage/TaskList 系统）

RichMessage 的 `tasklist_start/update/replace/complete` 和 `shell_start/output/done` 事件系统需要清理干净, 改用纯文本形式通过 MessageBus 发送。



---

## 5. 实施步骤

### Step 1: 后端收敛到 MessageBus
- bridge.go: 移除 `pushSSEEvent` 调用（pm_message/se_message/review_result/ap_result/error）
- 确保所有事件走 `msgBusSend` 或 `msgBus.Send`
- 验证: 前端只通过 `OnCoreMessage` 收到消息

### Step 2: 前端移除 SSE 处理器
- App.vue: 移除 `pm_message` / `ap_message` / `exec_*` / `pm_streaming_done` 处理器
- 只保留 `new-message` 作为消息入口

### Step 3: 前端 ChatPanel 改为消息分框渲染
- 移除 `_execData` / `RichMessage` / `SERichMessage` 渲染分支
- 渲染 `msg.content` 作为自然语言摘要
- 渲染 `msg.sections[]` 作为子框列表
- 实现 `[▶]/[▼]` 折叠展开控制
- 实现 terminal 框的滚动

### Step 4: 清理 RichMessage/TaskList 遗留
- `rich_message_builder.go` 重构或移除
- `manager.go` 中所有 `StartTaskList` / `ReplaceTaskList` 等调用改为发送纯文本
- 移除 `emitWailsEvent` 函数

---

## 6. 影响范围

| 文件 | 改动量 | 说明 |
|------|:-----:|------|
| `internal/chat/bridge.go` | ~10行 | 移除 SSE push |
| `internal/chat/manager.go` | ~80行 | 移除 RichMessage 调用 + emitWailsEvent |
| `internal/chat/rich_message_builder.go` | ~240行 | 整个文件删除或重构 |
| `internal/chat/router.go` | ~5行 | 移除 RichTaskID 字段 |
| `frontend/src/App.vue` | ~150行 | 移除 EventsOn 处理器 |
| `frontend/src/components/ChatPanel.vue` | ~1000行 | 大幅简化渲染 |
| `frontend/src/components/*.vue` | 若干 | 子组件清理 |
| 总计 | ~1500行减少 | |

---

## 7. 风险与缓解

| 风险 | 缓解 |
|------|------|
| exec 进度实时反馈丧失 | terminal section 在 exec 完成后一次性出现, 不展示中间状态 |
| 长输出占屏 | terminal section 默认折叠, 展开后框内滚动 |
| 任务列表不可用 | 改为纯文本 `[1/3] 写代码 done` 格式 |
| 遗漏事件通道 | 对照本文第4节的事件清理清单逐一移除 |
