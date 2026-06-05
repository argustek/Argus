# Argus 多 Agent 协作方案

> 版本: v1.0 | 日期: 2026-06-05
> 基于代码实测（e31f7ac），非文档推断

---

## 一、现状分析

### 1.1 当前架构

```
用户输入
   ↓
[PM] 分析需求 → 拆解为任务JSON
   ↓ @SE + task JSON
[SE] 单Agent串行执行所有编码任务（23个工具）
   ↓ complete_task / 结果汇报
[PM] 审核 → @AP 或 @SE(返工)
   ↓ @AP
[AP] 最终审批
```

**核心文件：**

| 组件 | 文件 | 职责 |
|------|------|------|
| PM Processor | [pm_prompt.go](internal/ai/pm_prompt.go) | 需求分析、任务拆解、审核 |
| SE Processor | [se_prompt.go](internal/ai/se_prompt.go) | 编码执行、工具调用、自我验证 |
| AP Processor | [ap_prompt.go](internal/ai/ap_prompt.go) | 安全合规审批 |
| Pipeline 调度 | [manager.go](internal/chat/manager.go) | 消息路由、状态机、防死循环 |
| 核心流程 | [argus.go](internal/core/argus.go) | Process() 串行流水线 |

### 1.2 现有能力

- **PM**: 需求理解 → 任务拆分 → 审核 → 调度
- **SE**: 23个工具（文件操作/搜索/LSP/终端/Git/多模态/Web）
- **AP**: 安全审查、合规检查
- **工具集**: write_file, edit_file, read_file, search_files, semantic_search, exec, exec_session, git_operation, run_tests, LSP(5操作), vision, undo_file, web_search, fetch_url 等

### 1.3 痛点

| 问题 | 影响 |
|------|------|
| **单SE串行执行** | 全栈任务（前端+后端+DB）只能一个个来，效率低 |
| **无并行能力** | "重构API + 改前端页面 + 加单元测试" 必须排队 |
| **无专业分工** | 同一个SE既要写Go又要改Vue还要写SQL |
| **上下文膨胀** | 大项目单SE的history快速增长，token浪费 |
| **故障隔离差** | 一个子任务失败可能拖垮整个任务链 |

---

## 二、目标架构

### 2.1 多Agent模式选择

**对比两种模式：**

| 模式 | 描述 | 优势 | 劣势 | 适用场景 |
|------|------|------|------|---------|
| **A: 并行分发** | PM拆成N个子任务→N个SE并行跑→合并结果 | 速度快 | 冲突处理复杂 | 全栈改造、多模块并行 |
| **B: 角色管道** | 前端SE→后端SE→DB SE→测试SE 串行流水线 | 专业分工 | 有依赖时慢 | 复杂全栈开发 |
| **C: 混合模式** | 无依赖的并行 + 有依赖的管道 | 最灵活 | 架构复杂 | 通用场景 |

**结论：选 C（混合模式）**，分两阶段实现：

### Phase 1: 并行分发（最小可行）

```
用户: "给用户模块加权限控制，同时优化首页加载速度"

                    [PM Planner]
                   /      |      \
            ┌──────┐  ┌──────┐  ┌──────┐
            │SE-FE │  │SE-BE │  │SE-DB │  ← 并行执行
            │前端  │  │后端  │  │数据库│
            └──┬───┘  └──┬───┘  └──┬───┘
               │         │         │
               └─────────┼─────────┘
                         ↓
                  [Result Merger]
                         ↓
                    [PM Review]
                         ↓
                      [AP 审批]
```

### Phase 2: 角色管道（增强）

```
用户: "开发完整的用户认证功能"

[PM Planner]
    ↓ 拆解为有依赖关系的子任务图(DAG)
    ↓
┌─────────────────────────────────────────┐
│  Task 1: 设计DB Schema (SE-DB)          │ → 完成
│       ↓                                 │
│  Task 2: 实现API (SE-BE) ← 依赖Task1    │ → 完成
│       ↓                                 │
│  Task 3: 实现前端页 (SE-FE) ← 依赖Task2  │ → 完成
│       ↓                                 │
│  Task 4: 写单元测试 (SE-Test) ← 依赖全部 │ → 完成
└─────────────────────────────────────────┘
    ↓
[Result Merger] → [PM Review] → [AP]
```

---

## 三、Phase 1 详细设计（并行分发）

### 3.1 新增数据结构

```go
// internal/ai/multi_agent.go (新文件)

// SubTask 子任务定义
type SubTask struct {
    ID          string   `json:"id"`           // 唯一ID (如 "sub_001")
    Title       string   `json:"title"`        // 任务标题
    Description string   `json:"description"`  // 详细描述
    Assignee    string   `json:"assignee"`     // 分配的角色: "se_frontend" / "se_backend" / "se_db" / "se_test" / "se_general"
    Files       []string `json:"files,omitempty"` // 相关文件列表
    DependsOn   []string `json:"depends_on,omitempty"` // 依赖的子任务ID（Phase 2用）
    Status      string   `json:"status"`       // pending / running / done / failed
    Result      string   `json:"result,omitempty"` // 执行结果摘要
    Error       string   `json:"error,omitempty"`  // 错误信息
}

// MultiAgentPlan 多Agent执行计划
type MultiAgentPlan struct {
    ID          string     `json:"id"`
    UserRequest string     `json:"user_request"`
    SubTasks    []SubTask  `json:"sub_tasks"`
    CreatedAt   int64      `json:"created_at"`
    Status      string     `json:"status"` // planning / executing / merging / reviewing / done
}

// SubAgentResult 子Agent执行结果
type SubAgentResult struct {
    TaskID    string     `json:"task_id"`
    Assignee  string     `json:"assignee"`
    Success   bool       `json:"success"`
    Content   string     `json:"content"`    // AI回复全文
    Actions   []SEAction `json:"actions"`   // 执行的操作列表
    Output    []string   `json:"output"`    // 工具输出
    Error     string     `json:"error"`
    Duration  float64    `json:"duration"`  // 执行耗时(秒)
}
```

### 3.2 子Agent角色定义

| 角色 | Assignee值 | System Prompt 特化 | 推荐模型 |
|------|-----------|-------------------|---------|
| **SE-General** | `se_general` | 通用SE prompt（现有） | 默认模型 |
| **SE-Frontend** | `se_frontend` | 强调 Vue/React/CSS/响应式 | 可配独立模型 |
| **SE-Backend** | `se_backend` | 强调 API/数据库/并发/性能 | 可配独立模型 |
| **SE-Database** | `se_database` | 强调 SQL/Migration/Schema/索引 | 可配独立模型 |
| **SE-Test** | `se_test` | 强调 边界条件/Mock/覆盖率 | 可配独立模型 |

**关键：每个子Agent是独立的 SEProcessor 实例，有自己的 history/context。**

### 3.3 PM 拆解增强

在现有 PM prompt 中增加多Agent拆解指令：

```
当任务可以并行拆分时，输出 multi_task_plan JSON：

{
  "mode": "parallel",  // parallel 或 sequential
  "sub_tasks": [
    {
      "title": "实现用户登录API",
      "assignee": "se_backend",
      "description": "在 internal/api/user.go 中添加 POST /api/login",
      "files": ["internal/api/user.go", "internal/auth/"]
    },
    {
      "title": "创建登录页面",
      "assignee": "se_frontend",
      "description": "在 frontend/src/views/Login.vue 创建登录表单",
      "files": ["frontend/src/views/", "frontend/src/components/"]
    }
  ]
}
```

**判断是否需要并行的启发式规则（PM内部逻辑）：**
1. 任务涉及 ≥ 2 个不同技术栈（Go + Vue, Python + SQL）
2. 子任务修改的文件无重叠（无冲突风险）
3. 子任务之间无强数据依赖
4. 总预估工作量 > 单次SE能处理的范围

### 3.4 并行调度器（新组件）

```go
// internal/chat/dispatcher.go (新文件)

type Dispatcher struct {
    mu           sync.Mutex
    plans        map[string]*MultiAgentPlan
    agentPool    map[string]*SEProcessor  // 角色名 → SE实例
    maxParallel  int                       // 最大并行数（默认3）
    mergeFunc    func(results []SubAgentResult) error
}

// 核心方法:
func (d *Dispatcher) Dispatch(plan *MultiAgentPlan) error
func (d *Dispatcher) RunSubTask(task *SubTask) (*SubAgentResult, error)
func (d *Dispatcher) MergeResults(plan *MultiAgentPlan) ([]SubAgentResult, error)
func (d *Dispatcher) DetectConflicts(results []SubAgentResult) []ConflictInfo
```

**执行流程：**

```
Dispatch(plan)
  → 过滤 depends_on 为空的子任务（Phase 1全部并行）
  → 按 assignee 分组到对应 SEProcessor
  → goroutine 并行调用 RunSubTask()
  → 每个 RunSubTask:
      1. 创建独立的 SEProcessor（或从池中取）
      2. 设置特化的 system prompt（按角色）
      3. 调用 ProcessTaskWithTools()
      4. 收集 actions 并执行
      5. 返回 SubAgentResult
  → WaitGroup 等待全部完成
  → MergeResults(): 合并结果
  → DetectConflicts(): 检查文件冲突
  → 返回合并后的结果给 PM 审核
```

### 3.5 冲突检测与解决

```go
type ConflictType int

const (
    ConflictEditSameFile   ConflictType = iota  // 同一文件被多个agent编辑
    ConflictDeleteEdit                          // 一个删除另一个编辑
    ConflictOppositeEdit                        // 同一处反向修改
)

type ConflictInfo struct {
    Type      ConflictType `json:"type"`
    File      string       `json:"file"`
    Agents    []string     `json:"agents"`     // 涉及的agent
    Suggestion string       `json:"suggestion"` // 解决建议
}

// 检测策略：
// 1. 文件级：多个agent修改了同一个文件 → 报告冲突
// 2. 行级（可选）：同一文件的编辑范围重叠 → 报告冲突
// 3. 解决策略：
//    - 自动：后写入的agent看到前一个的diff，基于最新版本编辑
//    - 人工：推送给PM/用户决定
```

### 3.6 前端变更

#### 3.6.1 多Agent状态展示

在现有的 [ChatPanel.vue](frontend/src/components/ChatPanel.vue) 或新建组件中展示并行执行状态：

```
┌─────────────────────────────────────────────┐
│ 🔄 多Agent并行执行中 (3/3)                   │
├─────────────────────────────────────────────┤
│ ✅ SE-Frontend: 登录页面完成                 │
│    → 编辑 Login.vue (+45行)                  │
│    → 编辑 LoginForm.vue (+23行)              │
│                                              │
│ 🔄 SE-Backend: API开发中...                  │
│    → 读取 user.go                            │
│    → 写入 auth.go                            │
│                                              │
│ ⏳ SE-Database: 等待中...                     │
└─────────────────────────────────────────────┘
```

#### 3.6.2 SSE事件扩展

新增事件类型：

| 事件名 | 数据 | 说明 |
|--------|------|------|
| `multi_agent_start` | `{plan_id, sub_tasks[]}` | 开始并行执行 |
| `sub_agent_start` | `{task_id, assignee, title}` | 子agent开始 |
| `sub_agent_action` | `{task_id, action_type, path}` | 子agent执行操作 |
| `sub_agent_done` | `{task_id, result}` | 子agent完成 |
| `multi_agent_merge` | `{conflicts[], results[]}` | 合并结果 |
| `multi_agent_conflict` | `{conflict}` | 发现冲突，需用户决策 |

### 3.7 配置扩展

在现有 Config 中新增字段：

```go
type Config struct {
    // ... 现有字段 ...

    // 多Agent配置
    MultiAgentEnabled    bool              `json:"multiAgentEnabled"`     // 是否启用多Agent
    MaxParallelAgents    int               `json:"maxParallelAgents"`     // 最大并行数(1-5)
    AutoConflictResolve  bool              `json:"autoConflictResolve"`   // 自动解决冲突
    SubAgentModels       map[string]string `json:"subAgentModels"`       // 子Agent模型覆盖
    // 例: {"se_frontend": "config_xxx", "se_backend": "config_yyy"}
}
```

**复用已有的角色模型绑定机制：**
- 已有的 `PMConfigID / SEConfigID / APConfigID` 作为默认模型
- `SubAgentModels` 允许每个子角色覆盖默认配置
- 设置面板中扩展现有的"角色模型绑定"表

---

## 四、Phase 2 详细设计（DAG管道）

### 4.1 DAG任务图

```go
type TaskNode struct {
    ID       string   `json:"id"`
    Task     SubTask  `json:"task"`
    Status   string   `json:"status"`
    Result   *SubAgentResult `json:"result,omitempty"`
}

type TaskEdge struct {
    From string `json:"from"` // 依赖的任务ID
    To   string `json:"to"`   // 依赖它的任务ID
}

type TaskDAG struct {
    Nodes []*TaskNode  `json:"nodes"`
    Edges []TaskEdge   `json:"edges"`
}

// 拓扑排序 + 并行层计算
func (dag *TaskDAG) GetParallelLayers() [][]string
func (dag *TaskDAG) Validate() error  // 检测环依赖
```

### 4.2 分层执行

```
Layer 0 (无依赖): [Task1: DB Schema] [Task4: 写Mock]     → 并行
Layer 1 (依赖L0):  [Task2: 后端API]                        → 独立
Layer 2 (依赖L1):  [Task3: 前端页面] [Task5: 集成测试]     → 并行
```

**Phase 2 在 Phase 1 完成后再设计细节，当前不展开。**

---

## 五、实施路线图

### Step 1: 数据层（预计改动量: 小）

| 文件 | 改动 |
|------|------|
| `internal/ai/multi_agent.go` | **新建** — SubTask / MultiAgentPlan / SubAgentResult 类型定义 |
| `app.go` Config | 新增 MultiAgentEnabled / MaxParallelAgents / SubAgentModels 字段 |

### Step 2: PM 拆解增强（预计改动量: 中）

| 文件 | 改动 |
|------|------|
| `internal/ai/pm_prompt.go` | 增加 multi_task_plan JSON 格式的指令和示例 |
| `internal/ai/pm_processor.go` | 解析 multi_task_plan，构造 MultiAgentPlan 对象 |

### Step 3: 并行调度器（预计改动量: 中大）

| 文件 | 改动 |
|------|------|
| `internal/chat/dispatcher.go` | **新建** — Dispatcher 核心逻辑 |
| `internal/chat/manager.go` | 在 handleToSE 入口处增加分支：单Agent vs 多Agent |
| `internal/ai/se_prompt.go` | 新增 CreateSpecializedSE() 工厂方法（按角色生成不同prompt） |

### Step 4: 冲突检测（预计改动量: 小）

| 文件 | 改动 |
|------|------|
| `internal/chat/dispatcher.go` | DetectConflicts() 方法 |
| `internal/executor/file_tracker.go` | 可能复用已有快照做 diff 比较 |

### Step 5: 前端展示（预计改动量: 中）

| 文件 | 改动 |
|------|------|
| `frontend/src/components/MultiAgentPanel.vue` | **新建** — 并行执行状态面板 |
| `frontend/src/components/ChatPanel.vue` | 监听新的 SSE 事件，渲染子agent状态 |
| `frontend/src/i18n/locales/*.ts` | 新增多Agent相关文本 |

### Step 6: 设置页扩展（预计改动量: 小）

| 文件 | 改动 |
|------|------|
| `frontend/src/components/SettingsPanel.vue` | 角色模型绑定表增加 SE-Frontend/SE-Backend/SE-Test 行 |

---

## 六、不改什么（边界）

| 不做的 | 原因 |
|--------|------|
| 分布式Agent | 单机足够，跨机器是P3 |
| Agent间通信协议 | 共享内存+channel就够了 |
| 动态注册新角色 | 5个预定义角色够用 |
| 可视化DAG编辑器 | Phase 2再说 |
| Agent市场/插件 | 过度工程 |

---

## 七、风险评估

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| API 并发限流 | 中 | 高 | MaxParallelAgents 默认=3，可配 |
| Token 成本翻倍 | 高 | 中 | 子Agent用更便宜的模型（如SE-Test用轻量模型） |
| 文件冲突 | 中 | 中 | 先检测再执行，冲突时降级为串行 |
| PM 拆解质量 | 中 | 高 | 少量few-shot示例 + 拆解后人工确认（可选） |
| 调试复杂度 | 低 | 高 | 每个子Agent独立日志，前端可视化 |

---

## 八、成功指标

| 指标 | 当前 | 目标 |
|------|------|------|
| 全栈任务耗时 | 串行累加 | 并行 ≈ max(子任务) |
| 单任务最大Token | SE history线性增长 | 每个子Agent独立context |
| 故障隔离 | 一个失败全挂 | 其他子Agent继续 |
| 用户感知 | 看不到分工 | 看到哪个Agent在干什么 |
