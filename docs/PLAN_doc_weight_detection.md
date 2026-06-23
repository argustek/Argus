# Doc Tree Weight Detection & Lifecycle

## 1. User Override Commands

| Command | Effect |
|---|---|
| `/doc on` | 强制启用 DocTree，无视 weight |
| `/doc off` | 强制禁用 DocTree，PM 不创建/不维护任何文档 |
| `/doc auto` | 恢复自动模式（按 weight 决定） |

状态持久化到 `config/state.json`。

## 2. Weight Classification（C Monitor 驱动）

### 级别定义

| 级别 | 标准（C 文件数统计） | 默认行为 |
|---|---|---|
| Featherweight ⚡ | C 文件 < 5 | 不开 DocTree |
| Lightweight | C 文件 5~20 | 不开 DocTree |
| Medium+ | C 文件 > 20 | 开 DocTree |

"文件"指工程代码文件（后端扫 `.go/.java/.py/.ts/.js`，不含 `.md/.json/yaml` 等配置文件）。C Monitor 每 30s tick 做一次 `ScanSourceFiles(rootDir)` 计数。

权重类别记录在 state 中，跨会话持久化：

```go
type ProjectState struct {
    DocEnabled      string `json:"doc_enabled"`      // "on" | "off" | "auto"
    DocWeight       string `json:"doc_weight"`        // "featherweight" | "lightweight" | "medium+"
    DocWeightFiles  int    `json:"doc_weight_files"`  // 判断时的文件数
}
```

### C Monitor 检测逻辑

在现有 `check()` → `handleProjectRunning()` 中追加：

```
check()
  ├─ ...
  ├─ detectDocWeightChange()    ← NEW: 每 30s 检查文件数/阈值
  │   ├─ ScanSourceFiles(rootDir) → count
  │   ├─ 比较 count 与 state.DocWeightFiles
  │   ├─ 没跨阈值 → 跳过
  │   ├─ 跨了阈值 → 更新 state + 如果跨入 Medium+ 则发送消息给 PM
  │   └─ 短时间（30s）内只触发一次
  └─ ...
```

**C Monitor 不做 AI 分析**，只做纯文件计数 + 阈值判定。阈值跨越后发送一条简短消息给 PM：

```
[C → PM] project file count: 18 → 42, crossed Medium+ threshold.
User doc mode: auto. Consider proposing doc tree structure.
```

### 触发时机

| 触发方式 | 实现 | 频率 |
|---|---|---|
| `SetWorkDir` | C Monitor Start() 中立即执行一次 check() | 必做 |
| C Monitor 30s tick | 现有 `monitorLoop` 周期检测 | 每 30s |
| 用户 `/doc on` | PM 收到后立即提案 | 手动 |

## 3. PM 文档创建工作流

### 新增 PM 工具

| 工具 | 用途 |
|---|---|
| `create_doc(node_id, node_title, parent, content)` | 创建文档文件（.argus/tree/xxx.md） |
| `propose_tree(structure_json)` | PM 提议树结构，用户确认后执行 |
| `analyze_project()` | AI 分析当前项目（目录结构、代码、依赖） |
| `get_doc_weight()` | 获取当前 weight 状态 |

### 场景处理

#### (A) 新项目 + `/doc on` 或 weight 自动启用

```
用户设 WorkDir（空目录），说"帮我写个 xx 系统"
  └─ PM 默认不开文档树（项目还是空的，没有东西可构造）
  └─ PM 按正常任务走，创建代码
  └─ 代码增多 → C Monitor 检测到跨阈值 → 通知 PM
  └─ PM 建议建文档树
  └─ 用户同意 → PM 分析项目 → 提议 WBS → 用户确认 → 执行
```

#### (B) 老项目 + 已有文档树

```
用户设 WorkDir（已有 .argus/tree/） 
  └─ PM 读取现有文档 → 向用户报告
  └─ "项目已有文档树（3 个节点），继续保持"
  └─ 正常干活
```

#### (C) 老项目 + 无 `.argus` + 跨阈值

```
用户设 WorkDir（已有大量代码）
  └─ C Monitor 检测到 200+ 文件 → Medium+
  └─ 通知 PM → PM 分析项目 → 提议 WBS → 用户确认 → 创建文档树
```

#### (D) 用户 `/doc on` + 老项目裸目录

```
用户设 WorkDir（有代码，无 .argus），然后 /doc on
  └─ PM 立即分析项目 → 提议 WBS → 用户确认 → 创建文档树
```

## 4. WBS 纲领 + 文档创建顺序

### WBS 是第一份文档

`project-schedule.md`（WBS）是整个文档树的纲领。PM 拿到项目理解后，先写 WBS。

**WBS 内容结构**（已在 `testdocs/.argus/tree/project-schedule.md` 中定义）：
- Task 列表（WBS 编号、任务名、耗时、依赖、负责人、状态）
- 任务依赖图（AI 可解析的 ASCII 图）
- 关键路径
- 资源日历

WBS 规定了需要哪些节点、依赖关系、时序。

### 阶段推进（Staggered Creation）

```
Phase 0: WBS
  → 创建 project-schedule.md（L0）
  → 在 WBS 中定义所有节点
  → 问用户"继续吗？"

Phase 1: 架构文档
  → 按 WBS 中的架构部分创建 L1 文档（系统设计、架构决策）
  → 更新 WBS：标记架构完成
  → 问用户"继续吗？"

Phase 2: 功能文档
  → 按架构创建 L2 功能文档（API 设计、数据库设计...）
  → 更新上级文档（追加实现摘要）
  → 进入标准 PM→SE→AP 循环

Phase 3: 代码生产 + 回写
  → SE/AP 干活
  → 代码变化 → 更新 dirty 标记 / exports 同步
  → 必要时更新 WBS（进度、耗时修正）

Phase 4: 循环回路
  → 下级文档完成 → 问用户"更新上级文档吗？"
  → 用户确认后更新
  → WBS 全程保持更新
```

### 关键原则

1. **WBS 是轴心**，不是 PROJECT_PLAN.md
2. **每个阶段完成问用户**，不自动跳级
3. **下级完成 → 按需更新上级**，不是自动全链更新
4. **用户可直接说"继续"**，快速跳过

## 5. 实现计划

### 第一阶段：Weight 检测（C Monitor）

| 文件 | 改动 |
|---|---|
| `internal/monitor/c_monitor.go` | 新增 `detectDocWeightChange()`、`ScanSourceFiles()`；`check()` 调用；state 持久化 |
| `internal/types/types.go` | `ProjectState` 加 `DocEnabled`/`DocWeight`/`DocWeightFiles` 字段 |
| `internal/chat/manager.go` | C Monitor callback 接线 |
| `app.go` | `/doc on/off/auto` 命令处理（或通过 PM 自然语言） |

### 第二阶段：PM 文档工具

| 文件 | 改动 |
|---|---|
| PM prompt | `create_doc`, `propose_tree`, `analyze_project`, `get_doc_weight` 工具定义 |
| `internal/ai/` | 新增工具处理函数 |
| `app.go` | 工具调用的后端实现 |

### 第三阶段：Staggered Creation + WBS

| 文件 | 改动 |
|---|---|
| PM prompt | Phase state machine 规则 |
| 文档模板 | 各阶段文档 frontmatter 模板 |
