# Argus Web 化可行性评估与改造方案

> 评估日期：2026-06-07
> 当前架构：Wails v2 (Go 后端 + WebView 前端)
> 目标架构：双模式（Wails Desktop + Browser Web 共享 Go 后端）

---

## 一、现状盘点

### 1. 现有 HTTP API（已具备，覆盖率 ~15%）

`http_server.go` 已实现 **18 个 REST 端点**：

| 分类 | 端点 | 功能 |
|------|------|------|
| **聊天** | `POST /api/v1/chat/send` | 发送消息 |
| | `GET /api/v1/chat/history` | 对话历史 |
| | `GET/POST/DELETE /chat/pending` | 待处理队列 |
| **执行** | `POST /api/v1/exec` | 执行命令 |
| | `POST /api/v1/tool/exec-session` | 持久化 Shell |
| **文件** | `POST /api/v1/write` | 写文件 |
| | `GET /api/v1/read` | 读文件 |
| **搜索** | `POST /tool/semantic-search` | 语义搜索 |
| | `POST /tool/search-files` | 文件内容搜索 |
| | `GET /tool/shell-status` | Shell 状态 |
| **SSE** | `POST /sse/subscribe` | 流式推送 |
| **管理** | `GET/POST /admin/*` | 状态/记忆/监控/恢复/配置 |
| **健康** | `GET /health/ping` | 健康检查 |

### 2. Wails 绑定方法（需转换，共 130+ 个）

按功能分类：

| 分类 | 方法数 | 代表方法 | Web化难度 |
|------|--------|----------|-----------|
| **聊天核心** | ~12 | SendMessage, GetMessages, ClearMessages, StopCurrentTask | 低 - 已有部分 API |
| **文件操作** | ~10 | ReadFile, WriteFile, CreateFile, DeleteFile, ListFiles, OpenFileDialog, SaveFile | 中 - 文件对话框需替换 |
| **Git 操作** | ~14 | GetGitStatus, GitPull, GitPush, CreateBranch, GetCommitLog, GitClone | 中 - 底层都是 exec.Command |
| **终端** | ~8 | NewTerminalSession, StartTerminal, WriteToTerminal, OpenPowerShell | 高 - 需 WebSocket 终端 |
| **SE Worker** | ~6 | AddSEWorker, AssignTaskToSE, ReviewSEWork, VerifySEWork | 低 - 纯逻辑 |
| **权限/决策** | ~12 | CheckDecisionForOperation, AddPermissionRule, CheckCommandSafety | 低 - 纯逻辑 |
| **环境检测** | ~8 | CheckEnv, InstallEnv, CheckAllEnv, LearnTool | 高 - 调用系统命令 |
| **监控状态** | ~12 | GetStatus, IsAIThinking, IsPMThinking, StartCMonitor | 低 - 纯状态查询 |
| **配置管理** | ~10 | GetConfig, SaveConfig, GetDingTalkConfig, SwitchAPIConfig | 低 - 已有 admin 接口 |
| **窗口/桌面** | ~8 | FixPosition, ShowWindow, ForceQuit, HandleBeforeClose | **不可用** - 纯桌面功能 |
| **事件推送** | ~30+ | EventsOn (project-state-changed, new-message, ai-stream-chunk...) | 中 - 需改 SSE/WebSocket |

### 3. 前端通信方式

- **Wails 绑定调用**：95%+ 的操作（直接 import from `wailsjs/go/main/App`）
- **HTTP fetch**：仅 3 处（pending 队列操作，调 `http://127.0.0.1:8080`）
- **Wails EventsOn**：30+ 种实时事件监听

### 4. 系统原生依赖（Web 无法直接使用）

| 依赖 | 出现位置 | 影响 |
|------|----------|------|
| `exec.Command` (60处) | app.go, argus.go, pm_prompt.go, shell_session.go, manager.go | **核心依赖** - git/build/shell 全靠它 |
| `syscall.SysProcAttr{HideWindow}` | app.go 多处 | Windows 进程隐藏 |
| `syscall.NewLazyDLL("user32.dll")` | app.go:312 | 窗口位置获取 |
| `runtime.OpenFileDialog/SaveFileDialog/OpenDirectoryDialog` | app.go:2012, 2033, 1755 | 原生文件选择框 |
| `cmd.exe` Shell Session | shell_session.go:55 | 持久化终端会话 |
| SingleInstanceLock | main.go | 单实例锁定 |
| 托盘/最小化 | app.go:428 | 系统托盘 |
| xterm.js 终端 | Terminal 组件 | 终端模拟器 |

---

## 二、改造方案

### 方案总览：双模式架构

```
┌─────────────────────────────────────────────┐
│              Go 后端 (共享)                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │ ChatMgr  │  │ Executor │  │ AI Client│   │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘   │
│       │              │             │          │
│  ┌────▼──────────────▼─────────────▼─────┐    │
│  │        HTTP API Server (:8080)       │    │
│  │  REST + SSE + WebSocket (Terminal)    │    │
│  └──────────────────┬───────────────────┘    │
└─────────────────────┼────────────────────────┘
                      │
          ┌───────────┼───────────┐
          ▼           ▼           ▼
   ┌──────────┐ ┌──────────┐ ┌──────────┐
   │ Wails    │ │ Browser  │ │ CLI/API  │
   │ Desktop  │ │ Web UI   │ │ 客户端   │
   │ (现有)   │ │ (新增)   │ │ (已有)   │
   └──────────┘ └──────────┘ └──────────┘
```

**核心理念**：Go 后端不变，把 Wails Binding 层替换为完整 REST API + WebSocket，前端加一个适配层。

### 分阶段实施路线图

#### Phase 1：补齐 REST API（预计工作量：3-5 天）

将 130+ Wails 绑定方法中**非桌面特有**的方法转为 HTTP 端点。

**优先级 P0 - 核心功能（必须）：**
```
POST   /api/v1/chat/send          → 已有 ✅
GET    /api/v1/chat/history       → 已有 ✅
POST   /api/v1/chat/reset         → 已有 ✅
GET    /api/v1/status             → 已有 (admin)
GET    /api/v1/config             → 已有 (admin)

POST   /api/v1/file/read          → 已有 (/read)
POST   /api/v1/file/write         → 已有 (/write)
POST   /api/v1/file/create        → 新增
DELETE /api/v1/file/delete         → 新增
GET    /api/v1/file/list          → 新增

POST   /api/v1/git/status         → 新增
POST   /api/v1/git/log            → 新增
POST   /api/v1/git/diff           → 新增
POST   /api/v1/git/checkout       → 新增
POST   /api/v1/git/push           → 新增
POST   /api/v1/git/pull           → 新增
POST   /api/v1/git/commit         → 新增
POST   /api/v1/git/branch/create  → 新增
POST   /api/v1/git/branch/switch  → 新增
GET    /api/v1/git/branches       → 新增

POST   /api/v1/exec               → 已有 ✅
POST   /api/v1/exec/session       → 已有 (/tool/exec-session)
GET    /api/v1/exec/shell-status  → 已有 (/tool/shell-status)

POST   /api/v1/search/semantic    → 已有 ✅
POST   /api/v1/search/files       → 已有 ✅
```

**优先级 P1 - 管理功能（重要）：**
```
GET    /api/v1/memory             → 已有 (admin)
POST   /api/v1/recover            → 已有 (admin)
GET    /api/v1/monitor            → 已有 (admin)
POST   /api/v1/config/save        → 新增
POST   /api/v1/env/check          → 新增
POST   /api/v1/env/install        → 新增
GET    /api/v1/env/all            → 新增
POST   /api/v1/task/stop          → 新增 (StopCurrentTask)
GET    /api/v1/logs               → 新增
```

**优先级 P2 - SE/权限（锦上添花）：**
```
POST   /api/v1/se/worker/add      → 新增
POST   /api/v1/se/task/assign     → 新增
POST   /api/v1/se/review          → 新增
POST   /api/v1/permission/check   → 新增
POST   /api/v1/permission/rule/add→ 新增
POST   /api/v1/command/safety     → 新增
```

#### Phase 2：实时事件通道（预计工作量：2-3 天）

当前前端通过 `EventsOn` 监听 30+ 种事件，统一为 SSE 或 WebSocket。

**方案 A - SSE（推荐，改动最小）：**

```
POST /api/v1/sse/subscribe   → 已有 ✅（扩展为多事件类型）

SSE Event 类型映射：
  project-state-changed  → event: state-change
  new-message            → event: message
  ai-stream-chunk        → event: stream
  pm_message             → event: pm-stream
  ap_message             → event: ap-stream
  exec_start             → event: exec:start
  exec_done              → event: exec:done
  exec_output            → event: exec:output
  pm_review_completed    → event: review:done
  error                  → event: error
  ai-thinking            → event: thinking
  agent-thought          → event: thought
  messages-cleared       → event: cleared
  reset-completed        → event: reset
  ... (其余 20+ 种)
```

**方案 B - WebSocket（更适合终端场景）：**

```
WS /api/v1/ws  ← 双向通信，特别适合 xterm.js 终端
```

建议：**SSE 做 Server→Frontend 推送 + WebSocket 专门做终端**。

#### Phase 3：前端适配层（预计工作量：3-5 天）

创建 `web-adapter.ts`，让前端在 Wails 和 Web 模式间无缝切换：

```typescript
// frontend/src/adapters/web-adapter.ts

// Wails 模式（现有，不变）
import * as WailsApp from '../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime'

// Web 模式（新增）
const API_BASE = window.location.origin  // 同源部署

const webAdapter = {
  SendMessage: async (msg: string) => {
    await fetch(`${API_BASE}/api/v1/chat/send`, {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ message: msg })
    })
  },
  GetMessages: async () => {
    const res = await fetch(`${API_BASE}/api/v1/chat/history`)
    return res.json()
  },
  // ... 其余 130 个方法的映射
}

// 统一导出 - 根据运行环境自动选择
export const App = typeof window !== 'undefined' && window._go?.runtime
  ? WailsApp   // Wails 环境
  : webAdapter // Web 环境

// 事件系统适配
export function useEvents() {
  if (window._go?.runtime) {
    return { on: EventsOn, emit: EventsEmit }
  } else {
    return createSSEClient(API_BASE)
  }
}
```

#### Phase 4：终端 Web 化（预计工作量：3-5 天）

最复杂的部分。当前终端通过 `cmd.exe` Shell Session + xterm.js 实现。

```
当前链路：
  前端 xterm.js → Wails binding → Go cmd.exe Shell Session → PTY → 输出回传

目标链路：
  前端 xterm.js → WebSocket (/api/v1/ws/terminal) → Go cmd.exe Shell Session → PTY → WS 推送
```

需要新增 WebSocket 终端端点：
```go
func (a *App) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
    conn, _ := upgrader.Upgrade(w, r, nil)
    session := a.chatManager.GetExecutor().NewShellSession()

    // 读取前端输入 → 写入 shell
    go func() {
        for {
            _, msg, err := conn.ReadMessage()
            session.Write(msg)
        }
    }()
    // shell 输出 → 推送给前端
    go func() {
        for output := range session.OutputChan() {
            conn.WriteMessage(1, output)
        }
    }()
}
```

#### Phase 5：桌面专属功能的降级处理

以下功能在 Web 模式下无法使用，需要优雅降级：

| 功能 | Web 替代方案 |
|------|-------------|
| `OpenFileDialog` | 用 `<input type="file">` + File API |
| `OpenFolderDialog` | 提示用户输入路径，或用 Directory Picker API |
| `SaveFileDialog` | 触发浏览器下载 (`blob:` URL) |
| `FixPosition` / `ShowWindow` | 无效操作，静默忽略 |
| `ForceQuit` / `Shutdown` | 改为断开连接/登出 |
| 系统托盘 | 不适用，移除 UI 入口 |
| `SingleInstanceLock` | 服务端做 session 管理 |
| `BrowserOpenURL` | `window.open()` |
| 剪贴板 | `navigator.clipboard` API |

---

## 三、工作量和风险评估

| 维度 | 评估 |
|------|------|
| **总工作量** | 约 2-3 周（1 人全职） |
| **Phase 1 (REST API)** | 3-5 天 — 机械性工作，大量样板代码 |
| **Phase 2 (SSE/WS)** | 2-3 天 — MessageBus 已经有 SSE Bridge 基础 |
| **Phase 3 (前端适配)** | 3-5 天 — 130 个方法的适配层 |
| **Phase 4 (终端)** | 3-5 天 — 最复杂，PTY + WebSocket |
| **风险点** | Shell 执行安全性、跨域问题、长连接稳定性 |
| **可并行性** | Phase 1 和 Phase 3 可并行；Phase 4 依赖 Phase 2 |

## 四、最终建议

**推荐采用"渐进式 Web 化"策略**，不追求一步到位：

1. **先做 Phase 1 + Phase 2**：补齐 API + SSE 事件，此时已经可以用 Postman/curl 完整操控 Argus
2. **再做 Phase 3**：前端适配层，实现"同一套前端代码，Wails/Web 双模式运行"
3. **最后做 Phase 4**：终端 WebSocket，这是体验提升的关键但不是必须的
4. **桌面版保留**：Wails 版继续维护，两个版本共享 Go 后端代码

这样每阶段结束都有可用产物，风险可控。
