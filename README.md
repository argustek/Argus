<p align="center">
  <img src="0526(2).gif" alt="Argus Demo" width="700">
</p>

<p align="center">
  <strong>Argus: AI 编程助手 with 4-role architecture (PM/SE/AP/C) + layered prompt system + document tree memory.</strong>
</p>

# Argus

**AI-powered development desktop app** — A four-role AI Agent (PM / SE / AP / C) with a split-prompt architecture (core + reference rules + skills) and a hierarchical document tree for project knowledge management. Features 4-level task weighting for optimal AI resource allocation.

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Wails-v2.12-DF0000?logo=wails" alt="Wails">
  <img src="https://img.shields.io/badge/Vue-3.3+-4FC08D?logo=vue.js" alt="Vue">
  <img src="https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript" alt="TypeScript">
  <img src="https://img.shields.io/badge/license-MIT-blue" alt="License">
  <img src="https://img.shields.io/badge/version-v0.9.4-green" alt="Version">
</p>

---

## ✨ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Argus Core                             │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              ArgusCore (Unified Brain)               │   │
│  │                                                      │   │
│  │   SharedMemory ← Full-context visibility             │   │
│  │      ├── user → pm → se → ap (complete pipeline)    │   │
│  │                                                      │   │
│  │   Prompt Architecture (Layered)                      │   │
│  │      ├── Core Prompt (~50 lines English):            │   │
│  │      │  identity, decision tree, principles, anti-loop│   │
│  │      ├── PMRules (~70 lines reference):              │   │
│  │      │  task weight, tool table, QA process,         │   │
│  │      │  permissions, error handling, AP resolution   │   │
│  │      └── Language Instruction (runtime injected)     │   │
│  │                                                      │   │
│  │   DocTree (Hierarchical Memory)                      │   │
│  │      ├── YAML frontmatter documents in .argus/tree/  │   │
│  │      ├── Dirty propagation on complete_task          │   │
│  │      ├── Go AST export extraction & verification     │   │
│  │      └── Impact analysis & audit integration         │   │
│  └─────────────────────────────────────────────────────┘   │
│                            ↓                              │
│  ┌──────────────────┐    ┌──────────────────────────────┐  │
│  │  Executor (Hands) │    │  CMonitor (Watchdog)         │  │
│  │                  │    │                              │  │
│  │  write_file      │    │  - Timeout detection          │  │
│  │  exec            │    │  - Hang recovery              │  │
│  │  read            │    │  - Idle alerts                │  │
│  └──────────────────┘    └──────────────────────────────┘  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 4-Level Task Weight System

| Weight | Path | When | Example |
|--------|------|------|---------|
| ⚡ Featherweight | PM direct (no SE/AP) | One round of tools | "write hello.go", "clean this dir" |
| Lightweight | PM → SE only (no AP) | Multi-step, clear scope | "refactor a function across 2 files" |
| Medium (baseline) | PM → SE → PM → AP | Needs analysis + review | "add authentication module" |
| Heavy | PM → SE → PM → AP + impact analysis | Cross-module, risky | "change database schema" |

PM decides the weight from the decision tree. User can override with `/level featherweight|lightweight|medium|heavy`.

### Complete Pipeline

```
USR Input → PM Decision Tree → [Featherweight: PM direct execute]
                               → [Lightweight: PM → SE only]
                               → [Medium/Heavy: PM → SE → PM Review → AP Approval]
```

### Role Pipeline

```
┌─────────────────────────────────────────────┐
│                    👤 USR                    │
│         Natural Language Input               │
└──────────────┬──────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────┐
│                    🎯 PM Decision Tree               │
│                                                     │
│  User message                                        │
│    ├─ greeting/chat/thanks → @USR                   │
│    ├─ unclear → investigate → @USR                  │
│    ├─ simple (one round) → ⚡ EXECUTE DIRECTLY       │
│    └─ complex → assign to SE                        │
│         └─ lightweight → PM→SE→PM (no AP)           │
│         └─ medium/heavy → PM→SE→PM→AP               │
└─────────────────────────────────────────────────────┘
               │
       ┌───────┴───────────┐
       ▼                   ▼
  ⚡ Direct Exec     Complex Task
  ┌──────────────┐  ┌──────────────────────┐
  │ write_file   │  │   💻 SE              │
  │ exec         │  │  Phase 2: Execute    │
  │ delete_file  │  │  write/edit/exec     │
  │ read_file    │  └──────────┬───────────┘
  └──────────────┘             │ SE done
                               ▼
                    ┌──────────────────────┐
                    │   🎯 PM Code Review  │
                    │  Phase 3: Verify     │
                    │  read_file + exec    │
                    │  Pass? → @AP or done │
                    │  Fail? → @SE rework  │
                    └──────────┬───────────┘
                               │ PM verified
                               ▼
                    ┌──────────────────────┐
                    │   🔍 AP Final Audit  │
                    │  Phase 4: Approve    │
                    │  Independent review  │
                    │  Impact analysis     │
                    │  Veto power          │
                    │  Clears dirty flags  │
                    └──────────────────────┘

  ┌─────────────────────────────────────────────┐
  │  📊 C (Background Monitor — always running)│
  │  Health checks, Git monitoring, alerts      │
  │  Read-only — never acts autonomously        │
  └─────────────────────────────────────────────┘
```

| Role | Prefix | Intelligence | Responsibility |
|------|--------|-------------|----------------|
| **👤 USR** | `USR` | Human | Provides requirements, makes decisions |
| **🎯 PM** | `PM` | AI (LLM) | Task planning, routing, quality control |
| **💻 SE** | `SE` | AI (LLM) | Code generation, file operations, command execution |
| **🔍 AP** | `AP` | AI (LLM) | Independent approval, QA verification, veto power |
| **📊 C** | `Sys_C` | Mechanical | Health monitoring, change detection, handover fallback |

---

## 🔥 Core Features

### ✅ Implemented Capabilities

#### 1️⃣ Four-Role AI Collaboration (Unique)
- **Natural language interaction**: Chat with PM in Chinese/English; PM automatically breaks down tasks for SE
- **@mention routing**: Use `@PM`, `@SE`, `@AP` to direct messages to specific agents
- **Triple quality assurance**:
  - PM Code Review (mandatory review of SE output, verified with tools)
  - AP Independent Approval (uninfluenced by PM, personally runs compile/test)
  - SE Self-test Verification (must pass before submission)
- **AP Veto Power**: If AP rejects, the task cannot be closed — SE must rework

#### 2️⃣ SSE Streaming Output
- **Real-time visibility into AI thinking process**: Token-by-token display
- **Event-driven push**: pm_started → se_started → writing_file → executing → done
- **Heartbeat keep-alive mechanism**: Automatic disconnect recovery

#### 3️⃣ Complete Task Lifecycle Management
- **Four-state state machine**: idle → running → done → approved
- **Anti-infinite-loop mechanism**: PM review max 10 rounds, SE execution threshold
- **Crash recovery system**: SQLite persistence + task memory recovery

#### 4️⃣ Robust Stability Assurance
- **Rate limiting & circuit breaker protection**: Prevents API overload and cascading failures
- **C monitoring system**: 30s health checks, Git change detection, progressive timeouts, handover fallback
- **Path security sandbox**: File operations restricted to working directory

#### 5️⃣ Rich Integration Capabilities
- **Multi-model support**: OpenAI-compatible API (Qwen, DeepSeek, GLM, GPT, Claude, etc.)
- **Multi-config management**: Switch API providers anytime
- **IM multi-channel integration**: DingTalk (bidirectional, Stream mode), Enterprise WeChat/Feishu (interface reserved)
- **Git integration**: View changes, manual commits, SE output verification

#### 6️⃣ User Experience
- **Modern GUI**: Wails desktop app (no CLI/TUI)
- **Role-differentiated display**: Color-coded messages (USR/PM/SE/Sys_C/Sys_SE)
- **Monaco editor**: VS Code's editor with syntax highlighting
- **Embedded terminal**: xterm.js terminal
- **File tree browser**: Sidebar project navigation
- **Draggable windows**: Freely arrange panels
- **Internationalization**: Chinese/English interface
- **Multi-level notifications**: Silent / Popup / Emergency (IM push)
- **Dark theme**

#### 7️⃣ Security & Permission Control
- **Three-tier decision authority**: Auto / Ask / Block
- **IM switch guard**: No accidental messages
- **Message deduplication**: Frontend + backend filtering
- **Auto-backup**: Before file modification, backup to `.argus/backups/`
- **Global panic recovery**: Goroutine panic protection

#### 8️⃣ Prompt Architecture & Task Weight System (v0.9.4+)
- **Layered prompt design**: Core prompt (~50 lines English) handles identity, decision tree, principles; reference rules (PMRules) handle tool table, QA process, error handling — appended separately to avoid attention dilution
- **4-level task weight**: Featherweight ⚡ (PM direct), Lightweight (PM→SE only), Medium (full pipeline), Heavy (Medium + impact analysis). PM decides from the decision tree; user overrides with `/level`
- **No hardcoded heuristics**: Removed keyword-based classification from engine — PM's own LLM decides the weight naturally
- **ChatWithTools language injection**: All chat paths now respect the user's language for replies

#### 9️⃣ Document Tree Memory System (v0.9.4+)
- **Hierarchical document tree**: Project documents organized in `.argus/tree/` with YAML frontmatter (owner, status, code_ref, dirty flags, exports, dependencies)
- **Dirty propagation**: `complete_task` triggers ancestor-chain dirty marking; AP approval auto-clears via `ClearDirty`
- **Go AST export extraction**: Auto-sync exported functions/structs/interfaces from code to document frontmatter
- **Bidirectional verification**: AP can verify documented exports match actual code (`verify_doc_exports`), and check impact scope before approval (`check_impact`)
- **CLI tools**: `--tree` (print tree), `--rebuild-tree` (scan + validate + cache), `--check-impact <doc_id>` (reverse dependency query)
- **SE/AP tool integration**: `update_doc`, `log_change`, `get_impacted_docs`, `sync_doc_exports`, `verify_doc_exports`, `check_impact`

---

## 🚀 Quick Start

### Download Pre-built Binary (Recommended)

For the quickest experience, download the latest `argus-desktop.exe` from the [Releases](https://github.com/ArgusTek/argus/releases) page. No build required — just run the exe.

### Build from Source

#### Prerequisites

- **Go** 1.22+
- **Node.js** 18+
- **Wails CLI**: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **OS**: Windows (primary; macOS/Linux builds may work but are untested)

```bash
go version        # go1.22.0+
node --version    # v18.0.0+
wails doctor      # should pass all checks
```

#### One-Click Build (Windows)

```batch
build.bat
```

#### Manual Build Steps

```bash
# Clone
git clone https://github.com/ArgusTek/argus.git
cd argus

# Install frontend deps
cd frontend && npm install && cd ..

# Configure AI API (see Configuration section)

# Build frontend
cd frontend && npm run build && cd ..

# Build Go app
wails build

# Run
./build/bin/argus-desktop.exe
```

> ⚠️ After modifying frontend code, you must run `npm run build` first, then `wails build`.

---

## ⚙️ Configuration

### AI Model Configuration

Configure in the app Settings panel, or copy and edit the template:

```bash
cp config/config.example.json config/config.json
```

```json
{
  "apiConfigs": [
    {
      "id": "1",
      "name": "Qwen Turbo",
      "provider": "qwen",
      "baseUrl": "https://dashscope.aliyuncs.com/compatible-mode/v1",
      "apiKey": "sk-your-api-key-here",
      "modelName": "qwen-turbo",
      "isDefault": true
    }
  ]
}
```

See `config/config.example.json` for the full configuration template.

### IM Integration Configuration

Settings → IM Integration, or copy and edit:

```bash
cp config/dingtalk.example.json config/dingtalk.json
```

```json
{
  "enabled": true,
  "clientId": "your-dingtalk-app-client-id",
  "clientSecret": "your-dingtalk-app-client-secret"
}
```

See `config/dingtalk.example.json` for the full template.

---

## 📖 Usage Guide

### Basic Chat

| Input Example | Effect |
|---------------|--------|
| `Help me write a Hello World` | Send to PM (default), PM analyzes then assigns to SE for execution |
| `@PM analyze this project's architecture` | Explicitly send to PM |
| `@SE fix the bug at line 20 in main.go` | Directly ask SE to execute fix task |
| `@AP review the current changes` | Request AP to perform independent review |

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+Enter` | Send message |
| `Ctrl+L` | Clear chat history |
| `Ctrl+S` | Save current file |
| `Esc` | Stop current task |

### Typical Workflows

#### ⚡ Featherweight — Simple, direct (no SE/AP)

```
👤 User: Write a hello.go that prints "hello world"
   ↓
🎯 PM: [Decision Tree → simple task, direct execute]
      write_file hello.go
      exec go run hello.go
   ↓
👤 User: hello world printed ✓
```

#### 🔄 Medium — Full pipeline (PM → SE → PM → AP)

```
👤 User: Create a Go REST API with health endpoint
   ↓
🎯 PM: [Decision Tree → complex task → @SE]
     @SE Create main.go with HTTP server + /health endpoint
   ↓
💻 SE: [write_file main.go → exec go build]
     @PM Task complete, build passes
   ↓
🎯 PM: [read_file main.go → exec go run]
     ✓ Verified. @AP please approve
   ↓
🔍 AP: [Independent review + compile check]
     ✅ Approved (dirty flags auto-cleared)
   ↓
👤 User: REST API is ready ✓
```

---

<details>
<summary><b>📁 Project Structure (click to expand)</b></summary>

```
argus/
├── main.go                  # Wails application entry point
├── app.go                   # Core business logic & API bindings
├── wails.json               # Wails configuration
├── go.mod / go.sum          # Go dependencies
│
├── cmd/                     # CLI tools (testing/debugging)
│   ├── argus/               # Main launcher
│   └── pm/                  # Standalone PM test
│
├── config/                  # Configuration files
│   ├── config.example.json  # API configuration template
│   └── dingtalk.example.json # DingTalk configuration template
│
├── internal/
│   ├── ai/                  # AI client & prompts
│   │   ├── client.go        # OpenAI-compatible API client
│   │   ├── pm_prompt.go     # PM core prompt (~50 lines) + processor + tools
│   │   ├── pm_rules.go      # PMRules reference layer (task weight, QA, perms)
│   │   ├── se_prompt.go     # SE prompt + processor (+ doc tree tools: update_doc, etc.)
│   │   ├── se_prompt_test.go
│   │   ├── ap_prompt.go     # AP approval prompt (+ doc audit tools: verify, check_impact)
│   │   └── p0_test.go       # AI package tests
│   ├── doclib/              # Document tree library
│   │   ├── doclib.go        # Core: DocNode/DocTree, frontmatter, propagation, export extraction
│   │   ├── doclib_test.go   # 19 tests covering BuildTree, PropagateDirty, ClearDirty, etc.
│   │   └── cli.go           # CLI handlers: --tree, --rebuild-tree, --check-impact
│   ├── chat/                # Chat management
│   │   ├── manager.go       # Unified ChatManager (PM/SE/AP/C orchestration + ClearDirty)
│   │   ├── router.go        # @mention message router
│   │   ├── sse_bridge.go    # SSE streaming bridge
│   │   └── sse_bridge_test.go
│   ├── monitor/             # Background monitoring
│   │   └── c_monitor.go     # C monitor (health, git, alerts, handover fallback)
│   ├── memory/              # Memory & context system
│   │   ├── manager.go       # SQLite-backed memory store
│   │   ├── compressor.go    # Context compression
│   │   ├── context.go       # Context builder
│   │   ├── session.go       # Session management
│   │   └── working.go       # Working memory
│   ├── executor/            # Command executor
│   │   └── executor.go      # Secure command execution with sandboxing
│   ├── board/               # Task board (Kanban)
│   │   └── board.go         # Board state management
│   ├── dingtalk/            # DingTalk integration
│   │   ├── dingtalk.go      # Bot client
│   │   └── stream.go        # Stream mode handler
│   ├── limiter/             # Rate limiting & safety
│   │   ├── ratelimit.go     # API rate limiter
│   │   ├── circuit_breaker.go
│   │   └── logger.go        # Request logger
│   ├── git/                 # Git operations
│   │   └── git.go           # Git integration (status, diff, commit)
│   ├── i18n/                # Internationalization
│   │   └── i18n.go          # Locale management
│   ├── core/                # Core orchestration
│   │   └── argus.go         # Unified brain: PM/SE/AP orchestration, /level, pmDirectExecute
│   └── types/               # Shared type definitions
│       └── types.go
│
├── frontend/                # Vue 3 frontend
│   ├── src/
│   │   ├── App.vue          # Root component
│   │   ├── main.ts          # Vue entry point
│   │   ├── style.css        # Global styles
│   │   ├── components/      # UI components
│   │   │   ├── ChatPanel.vue      # Chat interface
│   │   │   ├── EditorArea.vue     # Monaco editor
│   │   │   ├── FileTreeWindow.vue # File browser
│   │   │   ├── TerminalWindow.vue # xterm.js terminal
│   │   │   ├── GitWindow.vue      # Git status panel
│   │   │   ├── SettingsDialog.vue # Settings modal
│   │   │   ├── StatusBar.vue      # Bottom status bar
│   │   │   └── ...                # 22+ components
│   │   └── composables/
│   │       └── useDraggable.ts    # Drag & drop
│   ├── wailsjs/             # Auto-generated Wails bindings
│   ├── package.json
│   └── vite.config.ts
│
└── build/                   # Build output (gitignored)
    └── bin/
        └── argus-desktop.exe
```

</details>

### Limitations

- **Windows only** (macOS/Linux builds possible but untested)
- **Solo project**: one maintainer, so response time varies

---

## 💬 Community

- **Discussions**: https://github.com/ArgusTek/Argus/discussions — Q&A, ideas, chat
- **Issues**: https://github.com/ArgusTek/Argus/issues — bug reports, feature requests

## 🤝 Contributing

We welcome all forms of contribution! Whether it's code, documentation, bug reports, or feature suggestions.

### How to Contribute

1. Fork this repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Create a Pull Request

### Contribution Guidelines

- Check [Good First Issue](../../issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22) for beginner-friendly tasks
- Feel free to open an issue for any bug report or feature request

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](./LICENSE) file for details.

---

<p align="center">
  <strong>Built with ❤️ by a solo developer</strong>
  <br>
  <em>One actor, multiple roles, brilliant performance</em>
</p>
```
