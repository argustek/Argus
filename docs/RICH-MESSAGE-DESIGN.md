# Argus 三层消息模型 (TaskList + Shell + Result) 设计方案

> **版本**: v1.0 | **日期**: 2026-05-21 | **状态**: 设计阶段

---

## 1. 现状分析

### 1.1 已有能力（代码证据）

| 能力 | 位置 | 说明 |
|------|------|------|
| **SE 执行面板** | [ChatPanel.vue:126-163](../frontend/src/components/ChatPanel.vue#L126-L163) | `_execData` 渲染：步骤列表 + 终端输出 |
| **SE exec 事件推送** | [manager.go:2142](../internal/chat/manager.go#L2142) | `executeSEActions` 推送 `exec_start`/`exec_done`/`exec_output` |
| **PM 工具执行** | [pm_prompt.go:662](../internal/ai/pm_prompt.go#L662-L800) | `read_file`, `list_files`, `exec`(30s超时), `add_todo`, `update_todo` |
| **PM autoVerify** | [pm_prompt.go:523](../internal/ai/pm_prompt.go#L523-L600) | git status + git diff 自动验证 |
| **SSE Bridge** | [sse_bridge.go:1](../internal/chat/sse_bridge.go#L1-L146) | 完整的订阅/发布机制，buffer=64 |
| **Wails Events** | [App.vue:429-473](../frontend/src/App.vue#L429-L473) | 前端已监听 `exec_start/exec_done/exec_output/exec_completed` |
| **Thinking 动画** | [ChatPanel.vue:550](../frontend/src/components/ChatPanel.vue#L550-L593) | 基础的 thinking-step 监听 |

### 1.2 核心问题

```
PM 执行了 exec/read_file/autoVerify → 但不推事件 → 用户看不到
SE 执行了 actions → 推了 exec_start/done/output → 用户能看到（_execData 面板）
AP/C → 没有执行能力或没暴露
```

**本质：PM 有 Shell 能力但缺事件推送；前端已有渲染 _execData 的组件但只绑定 SE。**

---

## 2. 目标架构

### 2.1 统一三层模型

```
┌─────────────────────────────────────────────┐
│           🎭 Role Message                    │
│                                             │
│  ┌─ Layer 1: TaskList ─────────────────┐   │
│  │  所有角色都有                         │   │
│  │  ☐ 步骤1: xxx          [pending]    │   │
│  │  ▶ 步骤2: xxx          [running]    │   │  ← 当前高亮
│  │  ☑ 步骤3: xxx          [done] ✅2s  │   │
│  └───────────────────────────────────┘   │
│                                             │
│  ┌─ Layer 2: Shell Details ────────────┐   │
│  │  按需出现（有命令执行时）              │   │
│  │  💻 ▶ command                        │   │
│  │     output...                        │   │
│  │     ✅ exit N  (duration)            │   │
│  └───────────────────────────────────┘   │
│                                             │
│  ┌─ Layer 3: Result ──────────────────┐   │
│  │  最终结论 / 输出                     │   │
│  │  可包含 code blocks, markdown       │   │
│  └───────────────────────────────────┘   │
└─────────────────────────────────────────────┘
```

### 2.2 四角色映射

| 角色 | TaskList 内容 | Shell 示例 | Result 形式 |
|------|-------------|-----------|------------|
| **PM** | 分析需求→制定计划→分配任务→审核验证→回复用户 | `git status`, `git diff`, `go test`, `go vet` | 文字结论 + 审批 JSON |
| **SE** | 接收任务→写文件→编译运行→测试→报告结果 | `write_file main.go`, `go run main.go`, `go test ./...` | 代码块 + 测试输出 |
| **AP** | 收到审批请求→阅读代码→静态分析→给出结论 | `read_file main.go`, 可能 `golangci-lint` | approve/reject + 理由 |
| **C** | 启动检测→检查 PM 状态→检查 SE 状态→心跳等待 | 无（纯监控） | 状态报告文字 |

### 2.3 与现有系统的关系

```
现有系统                          新系统
─────────                        ─────────

Message { role, content }        RoleMessage { role, content,
                                    taskList, shells[], result }

_execData (仅 SE)                ← 合并进通用 shells[]

exec_start/done/output (仅 SE)   ← 改为通用 event，带 role 字段

renderStructured (v-html字符串)  ← 重构为组件树渲染

PM executeTool (静默执行)         ← 加上事件推送 = 可视化
```

---

## 3. 数据模型设计

### 3.1 后端 Go 结构体

```go
// internal/types/rich_message.go (新文件)

// RichMessage 富文本消息（三层模型）
type RichMessage struct {
    Base      *Message              // 原始消息（兼容）
    TaskList  *TaskList             // Layer 1: 任务列表
    Shells    []ShellBlock          // Layer 2: Shell 执行记录
    Result    *ResultBlock          // Layer 3: 最终结果
}

// TaskList 任务列表（Layer 1）
type TaskList struct {
    ID        string       // 唯一标识 "pm_20260521_001"
    Role      string       // "pm" | "se" | "ap" | "sys_c"
    Title     string       // "PM 审核 SE 提交的 Hello World"
    Tasks     []TaskItem   // 步骤数组
    Status    string       // "pending" | "running" | "completed" | "error" | "cancelled"
    StartedAt int64
    EndedAt   int64
}

type TaskItem struct {
    ID          string // "t1", "t2", ...
    Text        string // "自动检测代码变更"
    Status      string // "pending" | "running" | "done" | "error" | "skipped"
    StartedAt   int64
    CompletedAt int64
    Duration    string // "0.3s"
    Error       string
}

// ShellBlock Shell 执行块（Layer 2）
type ShellBlock struct {
    TaskID    string // 关联到 TaskItem.ID
    Type      string // "write_file" | "exec" | "read_file" | "list_files"
    Command   string // 命令或路径
    Output    string // 输出内容（可追加）
    ExitCode  int
    Duration  string
    Status    string // "running" | "done" | "error" | "blocked"
    Timestamp int64
    Extra     map[string]string // write_file 的 path/size 等
}

// ResultBlock 最终结果（Layer 3）
type ResultBlock struct {
    Text       string         // 结论文字
    CodeBlocks []CodeBlock    // 代码块
    FileDiffs  []FileDiffRef  // 文件变更引用
    JSONData   interface{}    // review_result / approval_result 等
}
```

### 3.2 前端 TypeScript 类型

```typescript
// frontend/src/types/rich-message.ts (新文件)

export interface RichMessage {
  id: string
  role: 'pm' | 'se' | 'ap' | 'sys_c' | 'user'
  content?: string
  timestamp: number
  
  taskList: TaskList
  shells: ShellBlock[]
  result?: ResultBlock
  _streaming?: boolean
}

export interface TaskList {
  id: string
  role: string
  title: string
  tasks: TaskItem[]
  status: 'pending' | 'running' | 'completed' | 'error' | 'cancelled'
  startedAt: number
  endedAt?: number
}

export interface TaskItem {
  id: string
  text: string
  status: 'pending' | 'running' | 'done' | 'error' | 'skipped'
  startedAt?: number
  completedAt?: number
  duration?: string
  error?: string
}

export interface ShellBlock {
  taskId: string
  type: 'write_file' | 'exec' | 'read_file' | 'list_files'
  command: string
  output: string
  exitCode: number
  duration: string
  status: 'running' | 'done' | 'error' | 'blocked'
  timestamp: number
  extra?: Record<string, string>
}

export interface ResultBlock {
  text: string
  codeBlocks?: CodeBlock[]
  fileDiffs?: FileDiffRef[]
  jsonData?: any
}
```

---

## 4. SSE 事件协议设计

### 4.1 新增事件类型

```
=== TaskList 事件 ===
tasklist_start     → { roleId, taskId, title, tasks: [{id, text}] }
tasklist_update    → { taskId, taskIndex, status, error?, duration? }
tasklist_complete  → { taskId, status, result? }

=== Shell 事件（通用化现有 exec_*）===
shell_start        → { roleId, taskId, taskIndex, type, command, extra? }
shell_output       → { roleId, taskId, output }           // 流式追加
shell_done         → { roleId, taskId, exitCode, duration, status }

=== Thinking 事件（增强现有 ai-thinking-step）===
thinking_start     → { roleId, steps: [{id, text}] }
thinking_step      → { roleId, stepIndex, text }
thinking_end       → { roleId }
```

### 4.2 与现有事件的兼容映射

```
现有事件                    新事件                      兼容策略
────────                   ────────                    ────────
exec_start (se only)  →    shell_start (role="se")    保留旧事件，新事件并行
exec_done (se only)   →    shell_done  (role="se")    前端同时监听两套
exec_output (se only) →    shell_output(role="se")    迁移期后移除旧事件
exec_completed        →    tasklist_complete           同上
ai-stream-chunk       →    保持不变（流式文本）        不变
ai-thinking-step      →    thinking_step (增强)       增强
```

### 4.3 典型事件流示例

#### 场景：PM 审核 SE 结果

```json
// 1. PM 开始审核 → 创建 TaskList
{"type": "tasklist_start", "data": {"roleId": "pm", "taskId": "pm_v01", "title": "审核 SE 提交的代码",
  "tasks": [{"id": "t1", "text": "接收 SE 完成报告"},
            {"id": "t2", "text": "自动检测代码变更 (git status)"},
            {"id": "t3", "text": "审阅变更详情 (git diff)"},
            {"id": "t4", "text": "运行验证测试 (go vet)"},
            {"id": "t5", "text": "给出审核结论"}]}}

// 2. 步骤 t1 完成
{"type": "tasklist_update", "data": {"taskId": "pm_v01", "taskIndex": 0, "status": "done", "duration": "0.05s"}}

// 3. 步骤 t2 开始 + Shell 开始
{"type": "tasklist_update", "data": {"taskId": "pm_v01", "taskIndex": 1, "status": "running"}}
{"type": "shell_start", "data": {"roleId": "pm", "taskId": "pm_v01", "taskIndex": 1, "type": "exec", "command": "git status --porcelain"}}

// 4. Shell 输出
{"type": "shell_output", "data": {"roleId": "pm", "taskId": "pm_v01", "output": " M main.go\n A hello_test.go\n"}}

// 5. Shell 完成
{"type": "shell_done", "data": {"roleId": "pm", "taskId": "pm_v01", "exitCode": 0, "duration": "0.08s", "status": "done"}}
{"type": "tasklist_update", "data": {"taskId": "pm_v01", "taskIndex": 1, "status": "done", "duration": "0.1s"}}

// ... t3, t4 类似 ...

// 6. 全部完成
{"type": "tasklist_complete", "data": {"taskId": "pm_v01", "status": "completed",
  "result": {"text": "@USR ✅ 代码质量合格，测试全部通过"}}}
```

#### 场景：SE 编码任务

```json
// 1. SE 收到任务
{"type": "tasklist_start", "data": {"roleId": "se", "taskId": "se_v02", "title": "创建 Hello World",
  "tasks": [{"id": "t1", "text": "创建 main.go 文件"},
            {"id": "t2", "text": "编译并运行"},
            {"id": "t3", "text": "向 PM 报告结果"}]}}

// 2. t1: write_file
{"type": "tasklist_update", "data": {"taskId": "se_v02", "taskIndex": 0, "status": "running"}}
{"type": "shell_start", "data": {"roleId": "se", "taskId": "se_v02", "taskIndex": 0, "type": "write_file", "command": "main.go", "extra": {"size": "15"}}}
{"type": "shell_done", "data": {"roleId": "se", "taskId": "se_v02", "exitCode": 0, "duration": "0.01s", "status": "done"}}
{"type": "tasklist_update", "data": {"taskId": "se_v02", "taskIndex": 0, "status": "done", "duration": "0.01s"}}

// 3. t2: exec go run
{"type": "tasklist_update", "data": {"taskId": "se_v02", "taskIndex": 1, "status": "running"}}
{"type": "shell_start", "data": {"roleId": "se", "taskId": "se_v02", "taskIndex": 1, "type": "exec", "command": "go run main.go"}}
{"type": "shell_output", "data": {"roleId": "se", "taskId": "se_v02", "output": "Hello, World!\n"}}
{"type": "shell_done", "data": {"roleId": "se", "taskId": "se_v02", "exitCode": 0, "duration": "0.32s", "status": "done"}}
{"type": "tasklist_update", "data": {"taskId": "se_v02", "taskIndex": 1, "status": "done", "duration": "0.32s"}}

// 4. 完成
{"type": "tasklist_complete", "data": {"taskId": "se_v02", "status": "completed"}}
```

---

## 5. 后端改动

### 5.1 新增 RichMessageBuilder

```go
// internal/chat/rich_message_builder.go (新文件)

type RichMessageBuilder struct {
    mu         sync.Mutex
    current    *RichMessage
    emitFunc   func(eventType string, data interface{})
}

func NewRichMessageBuilder(emitFunc func(string, interface{})) *RichMessageBuilder {
    return &RichMessageBuilder{emitFunc: emitFunc}
}

// StartTaskList 创建新的 TaskList 并推送 tasklist_start
func (b *RichMessageBuilder) StartTaskList(role, title string, tasks []TaskItemDef) string {
    b.mu.Lock()
    defer b.mu.Unlock()
    
    taskId := fmt.Sprintf("%s_%d", role, time.Now().UnixNano())
    items := make([]TaskItem, len(tasks))
    for i, t := range tasks {
        items[i] = TaskItem{ID: fmt.Sprintf("t%d", i+1), Text: t.Text, Status: "pending"}
    }
    
    b.current = &RichMessage{
        TaskList: &TaskList{
            ID: taskId, Role: role, Title: title,
            Tasks: items, Status: "running", StartedAt: time.Now().Unix(),
        },
    }
    
    b.emitFunc("tasklist_start", map[string]interface{}{
        "roleId": role, "taskId": taskId, "title": title,
        "tasks": items,
    })
    return taskId
}

// UpdateTask 更新单个任务状态
func (b *RichMessageBuilder) UpdateTask(taskId string, index int, status string) {
    if b.current == nil || b.current.TaskList == nil { return }
    if index < len(b.current.TaskList.Tasks) {
        item := &b.current.TaskList.Tasks[index]
        item.Status = status
        if status == "running" { item.StartedAt = time.Now().Unix() }
        if status == "done" || status == "error" {
            item.CompletedAt = time.Now().Unix()
            item.Duration = formatDuration(item.StartedAt, item.CompletedAt)
        }
        
        updateData := map[string]interface{}{"taskId": taskId, "taskIndex": index, "status": status}
        if item.Duration != "" { updateData["duration"] = item.Duration }
        b.emitFunc("tasklist_update", updateData)
    }
}

// PushShellStart 推送 Shell 开始
func (b *RichMessageBuilder) PushShellStart(role, taskId string, taskIndex int, cmdType, command string, extra map[string]string) {
    shell := ShellBlock{
        TaskID: taskId, Type: cmdType, Command: command,
        Status: "running", Timestamp: time.Now().Unix(), Extra: extra,
    }
    b.current.Shells = append(b.current.Shells, shell)
    
    data := map[string]interface{}{"roleId": role, "taskId": taskId, "taskIndex": taskIndex, "type": cmdType, "command": command}
    if extra != nil { data["extra"] = extra }
    b.emitFunc("shell_start", data)
}

// PushShellOutput 流式推送 Shell 输出
func (b *RichMessageBuilder) PushShellOutput(taskId, output string) {
    if len(b.current.Shells) == 0 { return }
    last := &b.current.Shells[len(b.current.Shells)-1]
    last.Output += output
    b.emitFunc("shell_output", map[string]interface{}{"roleId": b.current.TaskList.Role, "taskId": taskId, "output": output})
}

// PushShellDone Shell 完成
func (b *RichMessageBuilder) PushShellDone(role, taskId string, exitCode int, duration string, status string) {
    if len(b.current.Shells) == 0 { return }
    last := &b.current.Shells[len(b.current.Shells)-1]
    last.ExitCode = exitCode
    last.Duration = duration
    last.Status = status
    
    b.emitFunc("shell_done", map[string]interface{}{
        "roleId": role, "taskId": taskId, "exitCode": exitCode, "duration": duration, "status": status,
    })
}

// CompleteTaskList 完成 TaskList
func (b *RichMessageBuilder) CompleteTaskList(taskId, status string, result *ResultBlock) {
    if b.current == nil || b.current.TaskList == nil { return }
    b.current.TaskList.Status = status
    b.current.TaskList.EndedAt = time.Now().Unix()
    b.current.Result = result
    
    b.emitFunc("tasklist_complete", map[string]interface{}{
        "taskId": taskId, "status": status, "result": result,
    })
}

func (b *RichMessageBuilder) GetCurrent() *RichMessage { return b.current }
```

### 5.2 Manager 集成 Builder

```go
// internal/chat/manager.go 改动

type Manager struct {
    // ... 现有字段 ...
    richBuilder *RichMessageBuilder  // 新增
}

func NewManager(...) *Manager {
    m := &Manager{...}
    m.richBuilder = NewRichMessageBuilder(m.emitWailsEvent)  // 新增
    return m
}

// emitWailsEvent 复用现有方法
func (m *Manager) emitWailsEvent(eventType string, data interface{}) {
    if m.ctx != nil {
        runtime.EventsEmit(m.ctx, eventType, data)
    }
}
```

### 5.3 PM ProcessReview 改造（核心改动）

```go
// internal/chat/manager.go - handlePMReview 改动点

func (m *Manager) handlePMReview(reviewMsg string) error {
    // === 新增：启动 TaskList ===
    taskId := m.richBuilder.StartTaskList("pm", "PM 代码审核", []TaskItemDef{
        {Text: "接收 SE 完成报告"},
        {Text: "自动检测代码变更 (git status)"},
        {Text: "审阅变更详情 (git diff)"},
        {Text: "运行验证测试"},
        {Text: "给出审核结论"},
    })
    defer func() {
        m.richBuilder.CompleteTaskList(taskId, "completed", &ResultBlock{Text: finalConclusion})
    }()
    
    // t1: 接收报告
    m.richBuilder.UpdateTask(taskId, 0, "running")
    // ... 现有逻辑 ...
    m.richBuilder.UpdateTask(taskId, 0, "done")
    
    // t2: autoVerify (git status) — 在 pmProcessor.ProcessReview 内部改造
    m.richBuilder.UpdateTask(taskId, 1, "running")
    // ProcessReview 内部调用 executeTool("exec", ...) 时会触发 shell 事件
    resp, err := m.pmProcessor.ProcessReviewWithBuilder(reviewMsg, pmHistory, m.richBuilder, taskId)
    m.richBuilder.UpdateTask(taskId, 1, "done")
    
    // t3-t5 类似...
}
```

### 5.4 PM Processor executeTool 改造

```go
// internal/ai/pm_prompt.go - executeTool 改动点

func (p *PMProcessor) executeTool(name, argsJSON string) string {
    // 新增：如果绑定了 builder，推送 shell 事件
    if p.builder != nil && p.currentTaskId != "" && p.currentTaskIndex >= 0 {
        switch name {
        case "exec":
            var args struct{ Command string }
            json.Unmarshal([]byte(argsJSON), &args)
            p.builder.PushShellStart("pm", p.currentTaskId, p.currentTaskIndex, "exec", args.Command, nil)
            // ... 执行命令 ...
            p.builder.PushShellDone("pm", p.currentTaskId, exitCode, duration, status)
        case "read_file":
            var args struct{ Path string }
            json.Unmarshal([]byte(argsJSON), &args)
            p.builder.PushShellStart("pm", p.currentTaskId, p.currentTaskIndex, "read_file", args.Path, nil)
            // ... 读文件 ...
            p.builder.PushShellDone("pm", p.currentTaskId, 0, duration, "done")
        case "list_files":
            p.builder.PushShellStart("pm", p.currentTaskId, p.currentTaskIndex, "list_files", ".", nil)
            // ...
            p.builder.PushShellDone("pm", p.currentTaskId, 0, duration, "done")
        }
    }
    // ... 其余不变 ...
}
```

### 5.5 SE executeSEActions 改造（增量）

```go
// internal/chat/manager.go - executeSEActions 改动点

func (m *Manager) executeSEActions(actions []ai.SEAction) error {
    // === 新增：包装成 TaskList ===
    taskDefs := make([]TaskItemDef, len(actions)+1)
    taskDefs[0] = TaskItemDef{Text: "接收 PM 任务指令"}
    for i, action := range actions {
        label := fmt.Sprintf("%s %s", action.Type, getActionLabel(action))
        taskDefs[i+1] = TaskItemDef{Text: label}
    }
    taskDefs[len(actions)] = TaskItemDef{Text: "向 PM 报告结果"}
    
    seTaskId := m.richBuilder.StartTaskList("se", "SE 执行任务", taskDefs)
    m.richBuilder.UpdateTask(seTaskId, 0, "done")
    
    for i, action := range actions {
        taskIdx := i + 1
        m.richBuilder.UpdateTask(seTaskId, taskIdx, "running")
        
        // === 复用现有的 exec_start/done/output 事件 ===
        // 同时也推送新的 shell_* 事件
        m.emitWailsEvent("exec_start", {...})  // 保留旧事件
        
        // 执行 action...
        
        m.emitWailsEvent("exec_done", {...})    // 保留旧事件
        m.emitWailsEvent("shell_start", {...})   // 新事件
        m.emitWailsEvent("shell_done", {...})    // 新事件
        
        m.richBuilder.UpdateTask(seTaskId, taskIdx, "done")
    }
    
    m.richBuilder.CompleteTaskList(seTaskId, "completed", nil)
}
```

---

## 6. 前端改动

### 6.1 新增组件结构

```
frontend/src/
├── components/
│   ├── chat/                          # 新目录：聊天子组件
│   │   ├── RichMessage.vue            # 主容器：组装三层
│   │   ├── TaskListBlock.vue          # Layer 1: 任务列表
│   │   ├── ShellBlock.vue             # Layer 2: Shell 执行
│   │   ├── ResultBlock.vue            # Layer 3: 最终结果
│   │   └── TerminalOutput.vue         # Shell 内部的终端样式输出
│   └── ChatPanel.vue                  # 改造：使用 RichMessage
├── types/
│   └── rich-message.ts                # 类型定义
```

### 6.2 RichMessage.vue 主容器

```vue
<template>
  <div class="rich-message" :class="[role, { streaming: _streaming }]">
    <!-- 角色头 -->
    <div class="rm-header">
      <span class="role-badge" :class="role">{{ roleLabel }}</span>
      <span v-if="taskList" class="status-tag" :class="taskList.status">
        {{ statusLabel }}
      </span>
    </div>

    <!-- Layer 1: TaskList -->
    <TaskListBlock
      v-if="taskList"
      :task-list="taskList"
      :expanded="expanded"
      @toggle="$emit('toggle-expand')"
    />

    <!-- Layer 2: Shell Details -->
    <div v-if="shells?.length" class="shells-container">
      <ShellBlock
        v-for="(shell, idx) in visibleShells"
        :key="idx"
        :shell="shell"
        :task-label="getTaskLabel(shell.taskId)"
        :collapsed="!expanded"
      />
    </div>

    <!-- Layer 3: Result -->
    <ResultBlock
      v-if="result"
      :result="result"
    />
  </div>
</template>
```

### 6.3 TaskListBlock.vue

```vue
<template>
  <div class="tasklist-block">
    <div class="tl-header" @click="$emit('toggle')">
      <span class="tl-title">📋 {{ taskList.title }}</span>
      <span class="tl-progress">{{ doneCount }}/{{ total }} {{ progressPercent }}%</span>
      <span class="expand-hint">{{ expanded ? '收起 ▲' : '展开 ▼' }}</span>
    </div>
    
    <!-- 进度条 -->
    <div class="tl-progress-bar">
      <div class="tl-progress-fill" :style="{ width: progressPercent + '%' }"></div>
    </div>

    <!-- 任务步骤 -->
    <transition name="slide">
      <div v-show="expanded" class="tl-steps">
        <div
          v-for="(task, idx) in taskList.tasks"
          :key="task.id"
          class="tl-step"
          :class="[task.status, { active: task.status === 'running' }]"
        >
          <span class="step-icon">
            <template v-if="task.status === 'pending'">○</template>
            <template v-else-if="task.status === 'running'">▶</template>
            <template v-else-if="task.status === 'done'">✅</template>
            <template v-else-if="task.status === 'error'">❌</template>
            <template v-else-if="task.status === 'skipped'">⏭</template>
          </span>
          <span class="step-text">{{ task.text }}</span>
          <span v-if="task.duration" class="step-duration">{{ task.duration }}</span>
          <span v-if="task.error" class="step-error">{{ task.error }}</span>
          
          <!-- running 状态动画 -->
          <span v-if="task.status === 'running'" class="step-spinner"></span>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{ taskList: TaskList; expanded: boolean }>()
const doneCount = computed(() => props.taskList.tasks.filter(t => t.status === 'done').length)
const total = computed(() => props.taskList.tasks.length)
const progressPercent = computed(() => total.value > 0 ? Math.round(doneCount.value / total.value * 100) : 0)
</script>
```

### 6.4 ShellBlock.vue

```vue
<template>
  <div class="shell-block" :class="[type, status]">
    <div class="shell-header">
      <span class="shell-icon">
        {{ type === 'exec' ? '💻' : type === 'write_file' ? '📝' : type === 'read_file' ? '📖' : '📂' }}
      </span>
      <span class="shell-command">{{ displayCommand }}</span>
      <span class="shell-status">
        {{ status === 'running' ? '⏳' : status === 'done' ? '✅' : status === 'error' ? '❌' : '🚫' }}
        {{ duration }}
      </span>
    </div>

    <!-- 终端风格输出 -->
    <TerminalOutput
      v-if="output"
      :output="output"
      :command="type === 'exec' ? command : ''"
      :exit-code="exitCode"
      :streaming="status === 'running'"
    />

    <!-- write_file 特有信息 -->
    <div v-if="type === 'write_file' && extra" class="file-info">
      <span>📄 {{ extra.path }}</span>
      <span>{{ extra.size }} bytes</span>
    </div>
  </div>
</template>
```

### 6.5 ChatPanel.vue 改造要点

```vue
<!-- 改造前：按角色分 template -->
<template v-if="msg.role === 'pm'">...</template>
<template v-else-if="msg.role === 'se'">...</template>
<template v-if="msg._execData">...</template>

<!-- 改造后：统一用 RichMessage 或 fallback -->
<template v-if="msg._richData">
  <RichMessage :data="msg._richData" :_streaming="msg._streaming" />
</template>
<template v-else-if="msg._execData">
  <!-- 兼容旧数据：迁移期内保留 -->
  <LegacyExecPanel :data="msg._execData" />
</template>
<template v-else-if="msg.role === 'pm' || msg.role === 'se'">
  <!-- 兼容无 rich 数据的老消息 -->
  <LegacyRoleMessage :msg="msg" />
</template>
```

### 6.6 App.vue 事件监听改造

```typescript
// 新增：统一事件处理器
function setupRichEvents() {
  const activeTasks = new Map<string, RichMessage>()

  EventsOn('tasklist_start', (data: any) => {
    const msg: RichMessage = {
      id: generateId(), role: data.roleId,
      taskList: { id: data.taskId, role: data.roleId, title: data.title, tasks: data.tasks, status: 'running', startedAt: Date.now() },
      shells: [], _streaming: true,
    }
    activeTasks.set(data.taskId, msg)
    messages.value.push(msg)
  })

  EventsOn('tasklist_update', (data: any) => {
    const msg = activeTasks.get(data.taskId)
    if (msg?.taskList?.tasks[data.taskIndex]) {
      msg.taskList.tasks[data.taskIndex].status = data.status
      if (data.duration) msg.taskList.tasks[data.taskIndex].duration = data.duration
    }
  })

  EventsOn('shell_start', (data: any) => {
    const msg = activeTasks.get(data.taskId)
    if (!msg) return
    msg.shells.push({
      taskId: data.taskId, type: data.type, command: data.command,
      output: '', status: 'running', timestamp: Date.now(), extra: data.extra,
    })
  })

  EventsOn('shell_output', (data: any) => {
    const msg = activeTasks.get(data.taskId)
    if (!msg || !msg.shells.length) return
    msg.shells[msg.shells.length - 1].output += data.output
  })

  EventsOn('shell_done', (data: any) => {
    const msg = activeTasks.get(data.taskId)
    if (!msg || !msg.shells.length) return
    const shell = msg.shells[msg.shells.length - 1]
    shell.exitCode = data.exitCode
    shell.duration = data.duration
    shell.status = data.status
  })

  EventsOn('tasklist_complete', (data: any) => {
    const msg = activeTasks.get(data.taskId)
    if (!msg) return
    msg.taskList.status = data.status
    msg.result = data.result
    msg._streaming = false
    activeTasks.delete(data.taskId)
  })
}
```

---

## 7. CSS 样式设计

### 7.1 整体视觉规范

```css
/* 色彩体系 */
--rm-pm-bg:      #f8f0ff;  /* PM: 淡紫 */
--rm-se-bg:      #f0f8ff;  /* SE: 淡蓝 */
--rm-ap-bg:      #fff8f0;  /* AP: 淡橙 */
--rm-sys-c-bg:   #f5f5f5;  /* C: 灰 */

/* TaskList */
.tl-step.pending   { opacity: 0.5; }
.tl-step.running   { background: rgba(59,130,246,0.1); border-left: 3px solid #3b82f6; }
.tl-step.done      { opacity: 0.8; }
.tl-step.error     { background: rgba(239,68,68,0.1); border-left: 3px solid #ef4444; }

/* Shell */
.shell-block.running { border-left: 3px solid #3b82f6; }
.shell-block.done    { border-left: 3px solid #22c55e; }
.shell-block.error   { border-left: 3px solid #ef4444; }

/* Terminal */
.terminal-output {
  font-family: 'Cascadia Code', 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  background: #1e1e1e;
  color: #d4d4d4;
  border-radius: 6px;
  padding: 10px;
  max-height: 200px;
  overflow-y: auto;
}
.cmd-prompt { color: #569cd6; }
```

### 7.2 动画效果

```css
/* 步骤切换动画 */
.slide-enter-active { transition: all 0.3s ease; }
.slide-enter-from { opacity: 0; transform: translateY(-10px); }

/* running 脉冲动画 */
.step-spinner {
  display: inline-block; width: 12px; height: 12px;
  border: 2px solid transparent; border-top-color: #3b82f6;
  border-radius: 50%; animation: spin 1s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* 进度条动画 */
.tl-progress-fill { transition: width 0.5s ease; }

/* Shell 输入流式打字效果 */
.terminal-output .cursor::after {
  content: '▊'; animation: blink 1s step-end infinite;
}
@keyframes blink { 50% { opacity: 0; } }
```

---

## 8. 实施路线图

### Phase 1: PM autoVerify 可视化（MVP，预计 1-2 天）

**目标**：让用户看到 PM 审核时实际做了什么

| 步骤 | 改动文件 | 内容 |
|------|---------|------|
| 1 | `internal/types/rich_message.go` | 新建类型定义 |
| 2 | `internal/chat/rich_message_builder.go` | 新建 Builder |
| 3 | `internal/ai/pm_prompt.go` | executeTool 加 builder 集成 |
| 4 | `internal/chat/manager.go` | handlePMReview 加 TaskList 包装 |
| 5 | `frontend/src/types/rich-message.ts` | TS 类型定义 |
| 6 | `frontend/src/components/chat/TaskListBlock.vue` | TaskList 组件 |
| 7 | `frontend/src/components/chat/ShellBlock.vue` | Shell 组件 |
| 8 | `frontend/src/App.vue` | 加事件监听 |
| 9 | `frontend/src/components/ChatPanel.vue` | 集成新组件 |

**验收标准**：
- PM 审核时显示 TaskList（5 个步骤）
- 每个 exec/read_file 显示为 Shell 块
- 进度实时更新（running → done）

### Phase 2: SE 任务可视化（预计 1 天）

**目标**：SE 也用同样的框架展示

| 步骤 | 改动文件 | 内容 |
|------|---------|------|
| 1 | `internal/chat/manager.go` | executeSEActions 加 TaskList |
| 2 | 前端 | 无需额外改动（Phase 1 已通用） |

**验收标准**：SE 消息显示与 PM 相同格式的 TaskList + Shell

### Phase 3: AP / C 支持（预计 0.5 天）

**目标**：所有角色统一

| 步骤 | 内容 |
|------|------|
| AP | 如果调了 read_file 则显示 Shell |
| C | 纯 TaskList（无 Shell），显示心跳状态 |

### Phase 4: 清理旧代码（预计 0.5 天）

- 移除 `_execData` 专用模板
- 移除旧的 `exec_start/done/output` 事件（保留新 `shell_*`）
- 移除 `renderStructured` v-html，全部组件化

---

## 9. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| 事件风暴：高频 shell_output 导致前端卡顿 | UI 卡死 | Shell 输出节流（100ms 合并一次）；虚拟滚动 |
| 向后兼容：老消息没有 richData | 显示异常 | Fallback 到 renderStructured；渐进式迁移 |
| PM 工具执行超时（30s）导致 TaskList 卡在 running | 用户困惑 | 超时时自动标记 error + 错误信息；可重试按钮 |
| Builder 生命周期泄漏 | 内存泄漏 | tasklist_complete 必须清理；加 GC 定时器 |
| Wails Events 性能（大量小事件） | CPU 高 | 批量合并：shell_output 100ms batch；tasklist_update debounce |

---

## 10. 附录：完整事件协议速查表

| 事件名 | 方向 | 数据 | 触发时机 |
|--------|------|------|---------|
| `tasklist_start` | B→F | `{roleId, taskId, title, tasks[]}` | 角色开始工作 |
| `tasklist_update` | B→F | `{taskId, taskIndex, status, duration?, error?}` | 单步状态变化 |
| `tasklist_complete` | B→F | `{taskId, status, result?}` | 全部完成 |
| `shell_start` | B→F | `{roleId, taskId, taskIndex, type, command, extra?}` | 开始执行命令 |
| `shell_output` | B→F | `{roleId, taskId, output}` | 命令输出（可多次） |
| `shell_done` | B→F | `{roleId, taskId, exitCode, duration, status}` | 命令结束 |
