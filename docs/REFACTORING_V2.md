# Argus V2 Architecture Refactoring

## 1. Core Philosophy

### The Insight

**串行工作 + 同一API = 本质就是一个角色，换提示词而已。**

就像一个演员演独角戏，或者说评书、口技表演——一张嘴，换不同的"嘴型"，演出不同角色的效果。

民间歇后语：**癞蛤蟆日青蛙——长得丑玩得花**

| 表面 | 实际 |
|------|------|
| PM分析任务 | Phase 1: 换PM帽(分析prompt) |
| SE执行代码 | Phase 2: 换SE帽(执行prompt) |
| AP审核结果 | Phase 3: 换AP帽(审核prompt) |
| 角色间协作对话 | 同一个大脑，共享记忆 |

---

## 2. Problem Statement (V1 Issues)

### Current Architecture Problems

```
V1 架构:
┌─────────┐    @SE     ┌─────────┐   MessageBus   ┌─────────┐
│   PM    │ ────────→ │   SE    │ ─────────────→ │   AP    │
│Processor│           │Processor│                │Processor│
└─────────┘           └─────────┘                └─────────┘
     ↓                     ↓                         ↓
  独立记忆               独立记忆                   独立记忆
     ↓                     ↓                         ↓
  MessageBus ←── ACK/校验码/超时检测/待确认队列 ──→ 前端
```

### Pain Points

| Issue | Root Cause | Impact |
|-------|-----------|--------|
| PM消息不显示 | addPMToUserMsg未触发回调 | 用户看不到分配 |
| SE卡住/闪退 | nil pointer dereference (client=nil) | 流程中断 |
| PM多@问题 (@SE @) | 正则清理不够彻底 | 消息格式错乱 |
| 空actions死循环 | AI返回无效JSON无重试机制 | 资源浪费 |
| API超时卡死 | 无流式超时保护 | 整个流程挂起 |
| 消息丢失/顺序混乱 | 多角色交接靠MessageBus传递 | 数据不一致 |
| **核心问题** | **3个独立struct + 消息总线 + ACK机制 = 过度工程化** | **4000+行代码维护噩梦** |

### Code Complexity

```
V1 文件统计:
manager.go          ~2500行  → 核心调度(太重)
se_prompt.go        ~600行   → SE处理器
pm_prompt.go        ~400行   → PM处理器  
ap_processor.go     ~200行   → AP处理器
message_bus.go      ~400行   → 消息总线(本不需要)
router.go           ~200行   → 路由器(本不需要)
────────────────────────────
总计:               ~4300行
```

---

## 3. V2 Architecture Design

### Core Principle

```
🎭 一个演员 + 一个大脑 + 一衣柜衣服 + 一双手 + 一个监工
```

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                      Argus V2 Core                          │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              ArgusCore (演员+大脑)                    │   │
│  │                                                      │   │
│  │   SharedMemory ← 全程共享的记忆                       │   │
│  │      ├── user: "创建hello.go"                        │   │
│  │      ├── pm: "这是编程任务"                           │   │
│  │      ├── se: "write_file + exec"                     │   │
│  │      └── ap: "审核通过"                               │   │
│  │                                                      │   │
│  │   PromptKit (衣柜)                                    │   │
│  │      ├── PM帽: 分析意图                               │   │
│  │      ├── SE帽: 生成代码执行                           │   │
│  │      └── AP帽: 校验结果                               │   │
│  │                                                      │   │
│  │   AICaller (一张嘴)                                   │   │
│  │      └── Call(role, prompt, memory) → Response        │   │
│  └─────────────────────────────────────────────────────┘   │
│                            ↓                              │
│  ┌──────────────────┐    ┌──────────────────────────────┐  │
│  │  Executor (双手)  │    │  CMonitor (监工+鞭子)         │  │
│  │                  │    │                              │  │
│  │  write_file      │    │  - 超时检测                   │  │
│  │  exec            │    │  - 卡死恢复                   │  │
│  │  read            │    │  - 空闲提醒                   │  │
│  │  edit            │    │  - context cancel             │  │
│  └──────────────────┘    └──────────────────────────────┘  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Processing Flow

```
用户输入: "Create hello.go print Hello World and run it"
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Phase 1: Analyze [戴PM帽]                                     │
│                                                              │
│ memory.Add("user", userInput)                                │
│ response = AICall(PROMPT_PM, memory.GetAll())                │
│ memory.Add("pm", response)          ← 共享记忆！             │
│ emit("pm_to_user", response.display) ← 用户看到PM在分析      │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Phase 2: Execute [戴SE帽]                                    │
│                                                              │
│ response = AICall(PROMPT_SE, memory.GetAll())                │
│            ↑ 能看到PM的分析！不用重复介绍背景                 │
│ actions = ParseActions(response)                             │
│ result = Executor.Run(actions)                               │
│ memory.Add("se", actions, result)   ← 共享记忆！             │
│ emit("se_to_user", result.display) ← 用户看到SE在干活        │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Phase 3: Review [戴AP帽] — 仅失败时调用                       │
│                                                              │
│ if !result.OK {                                             │
│     response = AICall(PROMPT_AP, memory.GetAll())            │
│                 ↑ 能看到完整过程！PM+SE全知道                 │
│     memory.add("ap", response)                              │
│     emit("ap_to_user", response)                            │
│ }                                                           │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
  返回 Result 给前端
```

---

## 4. Key Design Decisions

### 4.1 Why Multi-Role (Not Single Unified)

| Aspect | Single Role | Multi-Role (Ours) |
|--------|------------|-------------------|
| **Token Cost** | ❌ 大prompt(全部工具+规则) | ✅ 小prompt(按需加载) |
| **Output Quality** | ❌ 容易跑偏(太多指令) | ✅ 专注精准(单一目标) |
| **Debugging** | ❌ 不清楚哪步出错 | ✅ 阶段隔离易定位 |
| **Marketing Value** | 😐 "AI助手" | 🔥 "多角色协同系统" |

**Token对比示例:**
```
Single Role Prompt (~8000 tokens):
"You are an AI assistant that can analyze tasks, write code, 
execute commands, review results... Tools: A,B,C,D,E,F,G...
Rules: 1,2,3,4,5,6,7,8..."

Multi-Role Prompts:
  PM: "你是产品经理，只负责分析需求"       ~1500t
  SE: "你是程序员，负责写代码执行"          ~2000t  
  AP: "你是审核员，负责检查结果"            ~1200t
  Total: ~4700t (省41%!)
```

### 4.2 Why Shared Memory (Not Independent)

| Aspect | Independent Memory | Shared Memory |
|--------|-------------------|--------------|
| **Context Continuity** | ❌ 靠消息传递，可能丢失/截断 | ✅ 全程可见 |
| **Token Efficiency** | ❌ 每次重复背景介绍 | ✅ 只说一次 |
| **Understanding Depth** | ❌ SE不知道PM为什么这么分 | ✅ 全局视角 |
| **Debug Ease** | ❌ 不知道哪步丢信息 | ✅ 完整链路可追溯 |

**Shared Memory结构:**
```go
type MemoryEntry struct {
    Role      string    // "user"/"pm"/"se"/"ap"
    Content   string    // 该阶段的内容
    Timestamp time.Time
    Metadata  map[string]interface{} // actions/result等结构化数据
}

type SharedMemory struct {
    entries []MemoryEntry
    maxLen  int // 防止无限增长
}
```

### 4.3 Why Keep C-Monitor

C是独立的**监工/鞭子**，必须存在：

| Reason | Explanation |
|--------|------------|
| **AI可能挂起** | API网络问题、模型超时 |
| **流式响应断连** | HTTP连接中途断开 |
| **第三方视角** | Core自己不能救自己(自己卡了怎么自救?) |
| **用户体验** | 不能让用户干等着不知道发生了什么 |

```
CMonitor职责:
1. Watch() — 持续监控Core活跃状态
2. TimeoutCheck — 超时未响应? → Cancel Context!
3. IdleAlert — 角色空闲太久? → 提醒检查
4. ForceRecovery — 彻底卡死? → 强制恢复
```

---

## 5. File Structure

### New Structure (V2)

```
internal/
├── core/
│   ├── argus.go          ← 核心调度器 (~300行)
│   ├── memory.go         ← 共享记忆管理 (~150行)
│   └── prompts.go        ← 提示词模板库 (~200行)
├── ai/
│   └── client.go         ← 统一API调用 (保留已有)
├── executor/
│   ├── executor.go       ← 执行引擎 (保留已有)
│   └── result.go         ← 结构化结果 (保留已有)
├── monitor/
│   └── c_monitor.go      ← C监控 (保留已有)
└── chat/
    └── bridge.go         ← 前端桥接 (~100行)

Total: ~850 lines (vs V1's ~4300 lines)
```

### Files to Remove/Deprecate

| File | Action | Lines Saved |
|------|--------|------------|
| `internal/chat/manager.go` | Deprecate | ~2500 |
| `internal/ai/se_prompt.go` | Merge into prompts.go | ~600 |
| `internal/ai/pm_prompt.go` | Merge into prompts.go | ~400 |
| `internal/ai/ap_processor.go` | Remove (merge into core) | ~200 |
| `internal/chat/message_bus.go` | Remove (not needed) | ~400 |
| `internal/chat/router.go` | Remove (state machine replaces) | ~200 |

---

## 6. Core Data Structures

### 6.1 ArgusCore

```go
package core

type ArgusCore struct {
    client      *ai.Client       // AI API调用
    executor    *executor.Executor // 执行器
    monitor     *monitor.CMonitor  // C监控
    memory      *SharedMemory     // 共享记忆
    prompts     *PromptKit        // 提示词库
    
    workDir     string
    language    string
    
    onMessage   func(source, content string) // 消息回调
    onChunk     func(delta string)          // 流式回调
    
    mu sync.RWMutex
}
```

### 6.2 Phase/Role Types

```go
type Role string

const (
    RoleUser  Role = "user"
    RolePM    Role = "pm"
    RoleSE    Role = "se"
    RoleAP    Role = "ap"
)

type Phase int

const (
    PhaseAnalyze Phase = iota  // PM阶段
    PhaseExecute               // SE阶段
    PhaseReview                // AP阶段
)
```

### 6.3 PromptKit

```go
type PromptKit struct {
    PM string // 产品经理提示词(精简版)
    SE string // 程序员提示词(精简版)
    AP string // 审核员提示词(精简版)
    
    Fix string // 错误修复提示词
}

func DefaultPromptKit(workDir string) *PromptKit {
    return &PromptKit{
        PM: pmPromptTemplate,
        SE: sePromptTemplate,
        AP: apPromptTemplate,
        Fix: fixPromptTemplate,
    }
}
```

### 6.4 ProcessResult

```go
type ProcessResult struct {
    Success   bool
    Actions   []executor.Action
    Outputs   []string
    Error     error
    Duration  time.Duration
    Phases    []PhaseResult // 各阶段详情
}

type PhaseResult struct {
    Phase    Phase
    Role     Role
    Input    string
    Output   string
    Raw      string
    Duration time.Duration
}
```

---

## 7. Implementation Plan

### Phase 1: Core Skeleton (MVP)
- [ ] Create `internal/core/argus.go`
- [ ] Create `internal/core/memory.go`
- [ ] Create `internal/core/prompts.go`
- [ ] Implement basic Process() flow
- [ ] Hello World test pass

### Phase 2: Integration
- [ ] Create `internal/chat/bridge.go` (frontend adapter)
- [ ] Integrate with existing Executor
- [ ] Integrate with existing CMonitor
- [ ] Frontend message emission working

### Phase 3: Robustness
- [ ] JSON repair/guard logic
- [ ] Empty actions retry mechanism
- [ ] Stream timeout protection
- [ ] Error recovery patterns

### Phase 4: Migration
- [ ] Migrate existing features (TODO, Git, DingTalk)
- [ ] Remove deprecated files
- [ ] Update tests
- [ ] Full regression testing

---

## 8. Comparison Summary

| Metric | V1 | V2 | Improvement |
|--------|----|----|-------------|
| **Lines of Code** | ~4300 | ~850 | **80% reduction** |
| **Struct Count** | 5+ (Manager, PM, SE, AP, Router) | 1 (ArgusCore) | **Simplified** |
| **API Calls per Task** | 3 (always) | 2-3 (AP optional) | **~33% fewer** |
| **Token Usage** | ~8150/task | ~4700-6000/task | **~30% savings** |
| **Bug Surface Area** | Huge (message passing) | Minimal (shared memory) | **Dramatically reduced** |
| **Debug Complexity** | Hard (which role dropped it?) | Easy (check shared memory) | **Much simpler** |
| **Perceived Complexity** | Complex (multi-agent) | Same (multi-role facade) | **Marketing preserved** |

---

## 9. Marketing Story

**对用户说:**
> "Argus采用多角色AI协同系统——产品经理(PM)分析需求，软件工程师(SE)执行代码，审核员(AP)验证结果。三个专业角色各司其职，确保每个任务都经过严格的质量保障流程。"

**实际实现:**
> 一个Core + 换prompt + 打标签输出 = 癞蛤蟆日青蛙，长得丑玩得花

---

*Document Version: 1.0*
*Date: 2026-05-30*
*Status: Design Approved, Pending Implementation*
