
# Argus 开发指南：分层记忆模型 + 提示词重构（合并版）

> 本文档包含两个紧密关联的子系统：
> 1. **分层记忆模型（v3 树状文档）**：解决 AI 记忆有限、变更影响分析。
> 2. **提示词重构**：解决提示词膨胀、角色行为一致性问题。
> 两者合并实施，共同构成 Argus 的核心 AI 协作框架。
> **本文档是权威指南。所有实现以本文件为准。**


## 目录

1. [总览：两系统关系](#1-总览两系统关系)
2. [分层记忆模型 v3](#2-分层记忆模型-v3)
   - 2.1 核心模型
   - 2.2 目录结构
   - 2.3 文档规范（frontmatter）
   - 2.4 命令行接口
   - 2.5 提炼调度器与脏标记传播
   - 2.6 依赖表管理
   - 2.7 工具函数
   - 2.8 审核清单集成
3. [提示词重构](#3-提示词重构)
   - 3.1 目标与现状
   - 3.2 新架构：核心提示词 + 参考规则 + 外置 skills
   - 3.3 PM 核心提示词设计（~50 行英文）
   - 3.4 PMRules 参考内容（~50 行）
   - 3.5 SE / AP 提示词精简
   - 3.6 语言控制与 `ChatWithTools` 修复
   - 3.7 代码修改清单
   - 3.8 Skills 目录使用规范
4. [合并实施顺序](#4-合并实施顺序)
5. [测试与验收](#5-测试与验收)


## 1. 总览：两系统关系

| 系统 | 解决的问题 | 输出物 | 使用者 |
|------|-----------|--------|--------|
| **分层记忆模型** | AI 记忆有限、改了这里坏了那里 | 文档树（.argus/tree/）、命令（tree/rebuild/check-impact） | AI（通过工具读写文档） |
| **提示词重构** | 提示词过长导致注意力衰减、角色行为不一致 | 精简的 PM/SE/AP 核心提示词 + 外置规则文件（.argus/skills/） | AI（系统提示词） |

**协同工作流**：
- 分层记忆模型定义了"文档树"和"管理员"机制，AI 通过工具（`update_doc`, `complete_task` 等）维护这棵树。
- 提示词重构确保 PM/SE/AP 角色知道如何使用这些工具，并且按照统一的规则行事（例如 PM 不应写代码，应委派给 SE）。
- 外置 skills（如 `workflow.md`, `permissions.md`）指导 AI 如何操作文档树，实现真正的"Prompt 不膨胀"。


## 2. 分层记忆模型 v3

> 此部分详细定义文档树结构、命令、提炼调度器、工具函数等。

### 2.1 核心模型

- 节点 = 一组文档的集合，包含根节点和子节点。
- 根节点是 `PROJECT_PLAN.md`（项目规划文档），代表 L0 顶层文档集。
- L0 层是顶层文档集合，包含多个专有文档，分别承载不同维度的项目信息：
  - `ARCHITECTURE.md` — 架构蓝图（模块划分、技术栈、接口契约）
  - `PROJECT_PLAN.md` — 项目规划（目标、范围、资源、里程碑、风险框架）
  - `PROJECT_SCHEDULE.md` — 项目排程（活动清单、依赖关系、关键路径、实际进度）
  - `DECISIONS.md` — 技术决策记录（ADR）
- L1 及以下层级的节点是模块/子模块文档集，各自包含该模块的说明、接口、依赖等信息。
- 每个节点有明确管理员角色（PM / SE / AP）。
- 父子关系由子节点的 `parent` 字段指向父节点的 `id` 定义，**与文件路径无关**。
- 文档树与代码分离，叶子文档通过 `code_ref` 引用代码文件。
- 变更通过 SE 调用 `complete_task` 信号触发向上脏标记传播，逐层标记父文档。
- 本系统用于 Argus IDE 帮助用户开发项目时管理项目架构，不用于 Argus 自身开发。
- **两个执行路径**：`SEProcessor.ProcessTaskWithTools`（新、多轮 AI 循环，处理 `complete_task`）vs `ArgusCore.callSEWithTools + executeActions`（旧，无 complete_task 处理）。文档树系统只挂在 SEProcessor 路径上。

#### 2.1.1 根节点：PROJECT_PLAN.md

`PROJECT_PLAN.md` 是 L0 层中唯一被系统约定为根节点的文档。它代表项目顶层，作为整个文档树的入口。子节点的 `parent` 字段可以指向它，形成树结构。

#### 2.1.2 L0 文档的职责分工

| 文档 | 职责 | 主要维护者 | 更新时机 |
|------|------|------------|----------|
| **ARCHITECTURE.md** | 模块划分、技术栈、接口契约、非功能性需求 | PM | 架构变更时 |
| **PROJECT_PLAN.md** | 项目目标、范围、资源总览、风险框架、里程碑 | PM | 项目启动、里程碑完成、重大变更时 |
| **PROJECT_SCHEDULE.md** | 活动清单、依赖关系、关键路径、实际进度 | PM | 每个模块/任务完成后 |
| **DECISIONS.md** | ADR，记录每个重要决策的背景、方案、结论 | PM | 发生重要决策时 |

#### 2.1.3 PM 驱动的全流程推进

**PM 是全流程的主动推进者**，控制权在每个任务/模块完成后移交给 PM，由 PM 决定下一步做什么。这符合 PMP 框架中项目经理的核心职责——不是被动响应，而是主动调度。

**PM 的工作流**：

```
用户下达任务 → PM 拆解 → 分配 SE → SE 执行 → PM 审核 → AP 审批 → PM 检查依赖 → PM 决定下一个任务 → ...
```

PM 在每个任务完成后，执行以下操作：
1. 更新 `PROJECT_SCHEDULE.md` 中对应任务的状态。
2. 检查所有未开始的任务，找出依赖已满足的下一批任务。
3. 主动向用户汇报进度，并询问是否继续下一任务。

**PM 是唯一有权"推进任务"的角色。** C 不参与任务调度。

#### 2.1.4 C 的角色定位：仅兜底（文档树场景）

> ⚠️ **本文档的 C 特指"文档树维护场景"下的兜底职责**，与 `internal/monitor/c_monitor.go` 中的通用监控器（负责 PM/SE 状态监控、超时接管等）是两回事。CMonitor 的自动提交、强插手、社交调度等逻辑不受本文档约束。

**C 不做任务调度，只做异常兜底。**

C 的职责：
- 定期检查项目状态（`status` 字段）。
- 当项目状态不是 `done`/`approved`，且超过阈值（如 2 小时）无任何 AI 活动时，发送提醒消息给 PM。
- **不做任何任务分析、依赖扫描、自动推进。**

C 的提醒消息示例：
> "项目状态尚未完成，但已超过 X 小时没有活动。请检查是否需要继续推进。"

**C 的判断依据**：
- `last_activity` 时间戳（PM/SE/AP 每次更新文档或执行任务时更新）。
- `project_status` 是否为 `done`/`approved`。

如果 `project_status` 不是 `done` 且 `now - last_activity > 阈值`，C 发送提醒。

#### 2.1.5 Project Schedule 的文本化表达

`PROJECT_SCHEDULE.md` 以文本形式表达活动清单、依赖关系和关键路径。文本格式便于 AI 解析和人类阅读，不依赖图形化渲染。

**关键路径用缩进和层级表达**：

```
# Project Schedule: Argus 0.9.5 → 1.0

## 已完成
- [x] M-A: 数据库重构 (SE-1, done, 2.5d)

## 可开始（依赖已满足）
- [ ] M-B: API 网关 (SE-2, 2d, depends_on: M-A)

## 阻塞中（依赖未满足）
- [ ] M-C: Web 前端 (SE-3, 4d, depends_on: M-B, blocked)
```

### 2.2 目录结构

```
项目根目录/
├── .argus/
│   ├── PROJECT_PLAN.md           # 根节点（唯一，L0 顶层文档集之一）
│   ├── tree/                     # 开发分层文档（参与树）
│   │   ├── <任意深度的文档>.md
│   │   └── ...
│   ├── skills/                   # AI 技能/规则（提示词外放，与文档树无关）
│   │   ├── code-review.md
│   │   └── ...
│   ├── docs/                     # 非开发类文档（不参与树，如设计笔记）
│   │   └── ...
│   ├── logs/
│   │   └── CHANGELOG.md          # 只追加变更日志
│   ├── cache/
│   │   └── tree.json             # 文档树缓存（系统维护）
│   └── settings.json             # 项目配置（可选）
├── AGENTS.md                     # 可选，给 AI 的行为规则（兼容行业标准）
└── src/                          # 代码（被 tree/ 文档通过 code_ref 引用）
```

- `tree/` 下的文档可以任意嵌套子目录，但树结构由 `parent` 决定而非物理路径。
- 所有文档文件必须使用 `.md` 扩展名。
- `.argus/` 目录本身可能与 Argus IDE 的其他配置文件共存（如 `config.yaml`、`state.json` 等）。
- 文档树系统在用户项目中创建 `.argus/tree/`、`.argus/PROJECT_PLAN.md`、`.argus/cache/tree.json`、`.argus/logs/CHANGELOG.md`。

### 2.3 文档规范（frontmatter）

**必填字段**：

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `id` | string | 文档唯一标识，推荐使用相对于项目根的路径形式（如 `tree/auth/jwt.md`） | `"tree/auth/README.md"` |
| `parent` | string | 父文档的 `id`。根节点填空字符串 `""` | `"tree/auth/README.md"` 或 `""` |
| `owner_role` | string | 管理员角色：`"PM"` / `"SE"` / `"AP"` | `"SE"` |
| `title` | string | 文档标题 | `"JWT 令牌模块"` |

**推荐字段**：

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `summary` | string | 供父节点提炼的摘要（建议 ≤ 150 字符） | `"提供 JWT 生成和验证功能"` |
| `code_ref` | string | 引用的代码文件路径（相对于项目根） | `"src/auth/jwt.go"` |
| `code_ref_type` | string | `"file"` / `"function"` / `"package"` | `"file"` |

**系统维护字段（AI 不应手动修改）**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `dirty` | boolean | 是否有未向上传播的变更（由系统标记） |
| `last_updated` | timestamp | 最后更新时间（ISO 8601） |
| `exports` | array | 对外接口列表（自动从代码提取或手动维护） |
| `dependencies` | array | 依赖的其他文档 `id` 列表（自动更新） |

**Frontmatter 完整示例**：

```yaml
---
id: "tree/auth/jwt.md"
parent: "tree/auth/README.md"
owner_role: "SE"
title: "JWT 令牌模块"
summary: "生成和验证 JWT，支持 HS256 和 RS256"
code_ref: "src/auth/jwt.go"
code_ref_type: "file"
dirty: false
last_updated: "2026-01-15T10:30:00Z"
exports:
  - name: "GenerateToken"
    signature: "GenerateToken(userID string) (string, error)"
  - name: "ValidateToken"
    signature: "ValidateToken(token string) (*Claims, error)"
dependencies:
  - "tree/shared/logger.md"
  - "tree/core/config.md"
---
```

### 2.4 命令行接口

四个命令，均通过 `argus` 可执行文件调用。不依赖 Wails 初始化，直接在 `main()` 开头拦截。

**注册位置**：`main.go:44-63` — 在 `NewApp()` 之前拦截并 return。

#### `argus --tree`

**功能**：打印当前项目的文档树结构。

**输出格式**（纯文本，UTF-8）：

```
PROJECT_PLAN.md (PM)
├── tree/auth/README.md (SE)
│   ├── tree/auth/jwt.md (SE)
│   └── tree/auth/oauth.md (SE)
├── tree/core.md (SE)
└── tree/shared/ratelimit.md (SE)
```

**实现要点**：
- 扫描 `.argus/tree/` 下所有 `.md` 文件及 `.argus/PROJECT_PLAN.md`。
- 读取每个文件的 frontmatter 中的 `id`、`parent`、`owner_role`、`title`。
- 构建父子映射（`parent` → children list），检测孤儿节点和循环依赖。
- 从根节点（`parent: ""`）开始递归打印，使用树形字符（`├──`、`└──`、`│`）。
- 若存在多个根节点（不应发生），全部打印并提示警告。

#### `argus --rebuild-tree`

**功能**：重建内部索引，验证文档树结构，更新缓存。

**行为**：
1. 遍历所有文档（`.argus/PROJECT_PLAN.md` 和 `.argus/tree/**/*.md`）。
2. 解析 frontmatter，验证：
   - 每个文档 `id` 唯一。
   - 每个 `parent` 指向的 `id` 存在（除非 `parent` 为空）。
   - 无循环依赖（从根出发 DFS 检测）。
3. 若有错误，输出错误列表（不阻断）。
4. 若无错误，更新内部缓存 `.argus/cache/tree.json`。

**输出示例**：
```
✓ 扫描到 12 个文档
✓ 树结构验证通过
✓ 已更新缓存 .argus/cache/tree.json
```

#### `argus --check-impact <doc_id>`

**功能**：输出修改指定文档可能影响的其他文档列表（被依赖方）。

**参数**：`<doc_id>` 必须是已存在的文档 `id`。

**输出格式**：
```
文档 tree/auth/jwt.md 被以下文档直接依赖：
  - tree/auth/oauth.md (依赖原因: imports GenerateToken)
  - tree/shared/middleware.md

建议检查这些文档是否需要更新。
```

**实现要点**：
- 优先从缓存中读取文档树。若缓存不存在或失效，自动从磁盘重建。
- 反向索引：遍历所有文档的 `dependencies` 字段，找出谁依赖了指定文档。
- 若没有依赖者，输出"无依赖"。

### 2.5 提炼调度器与脏标记传播

#### 触发时机

当 SE 在 `ProcessTaskWithTools` 多轮循环中调用 `complete_task` 时触发。仅在 SEProcessor 路径中生效，ArgusCore 旧路径（`executeActions`）和 PM 直执（`pmDirectExecute`）不触发传播。

#### 传播是系统行为，不校验角色

传播只修改系统维护字段（`dirty`、`last_updated`），不改 `owner_role` / `title` / `id` / `code_ref`。SE 调用 `complete_task` 时传入 `docs`，系统自动沿父链向上传播脏标记。

#### 流程

1. **收集脏文档**：SE 调用 `complete_task(docs=[...])`，传入本次影响到的文档 ID 列表。
2. **展开父链**：对每个脏文档，沿 parent 链向上展开，直到根节点。去重，确保每个节点只处理一次。
3. **按深度排序**：从最深的孩子开始处理（确保自底向上）。
4. **标记脏**：对每个节点，读取 frontmatter，设置 `dirty=true`、更新 `last_updated`，写回磁盘。
5. **更新缓存**：所有标记完成后，刷新 `.argus/cache/tree.json`。
6. **清除脏标记**：AP 审核通过后（`forceProjectApproved`），异步调用 `ClearDirty` 清除所有脏标记。

#### 伪代码

```go
func PropagateDirty(rootDir string, docIDs []string) error {
    tree := BuildTree(rootDir)
    toProcess := expandToAncestors(tree, docIDs)
    sortByDepthDesc(tree, toProcess)
    for _, id := range toProcess {
        node, body := ReadDocFile(idToPath[id])
        node.SetDirty(true)
        WriteDocFile(idToPath[id], node, body)
    }
    SaveCache(tree, rootDir)
}
```

#### 实现参考

| 逻辑 | 文件 |
|------|------|
| 传播函数 | `internal/doclib/doclib.go:PropagateDirty` |
| 清除标记 | `internal/doclib/doclib.go:ClearDirty` |
| 触发点 | `internal/ai/se_prompt.go:227-234`（complete_task 拦截） |
| 清除调用 | `internal/chat/manager.go:forceProjectApproved` |
| 深度计算 | `internal/doclib/doclib.go:GetDepth` |

### 2.6 依赖表管理

#### 依赖表存储位置

每个文档的 frontmatter 中包含 `dependencies` 字段（系统维护的数组），存储该文档直接依赖的其他文档 `id` 列表。

父文档中不单独维护"子文档之间的依赖表"，而是通过每个文档自身的 `dependencies` 字段，结合 `argus check-impact` 时的反向索引来查询影响范围。

#### 依赖关系更新

- **当前 MVP**：`dependencies` 由用户在文档中手动维护，系统不自动修改。
- **未来**：可集成 LSP 自动提取代码中的 import 语句，自动更新对应文档的 `dependencies`。
- **环检测**：在 `rebuild-tree` 时，除了检测父子环，还应检测依赖环。若检测到环，输出警告但仍允许存在。

#### 实现参考

| 逻辑 | 文件 |
|------|------|
| 反向依赖查询 | `internal/doclib/doclib.go:GetImpactedDocs` |
| CLI 封装 | `internal/doclib/cli.go:CLICheckImpact` |

### 2.7 工具函数

Argus 提供以下内部工具函数（供 AI 模型调用）。定义在 `internal/ai/se_prompt.go` 的 `SETools` 变量中。

#### `update_doc(doc_id, content)`

- **功能**：更新文档内容（只修改正文，frontmatter 保留）。
- **权限校验**：SE 只能修改 `owner_role=SE` 的文档。PM/AP 文档拒绝修改。
- **副作用**：标记文档 `dirty=true`，更新 `last_updated`。
- **定义位置**：`internal/ai/se_prompt.go`（Dispatch: 939-957）

#### `complete_task(files, docs, summary)`

- **功能**：标记当前任务完成，触发脏标记传播。
- **`docs` 参数**：可选，影响到的文档 ID 列表。不传则不触发传播。
- **`files` 参数**：创建/修改的代码文件列表（已有）。
- **触发行为**：若 `docs` 非空，调用 `doclib.PropagateDirty(s.workDir, docs)`。
- **拦截点**：`internal/ai/se_prompt.go:224-234`

#### `log_change(role, task_id, action, affected_files, affected_docs, summary)`

- **功能**：向 `.argus/logs/CHANGELOG.md` 追加变更记录（只追加）。
- **格式**：包含时间戳、角色、任务 ID、变更摘要。
- **定义位置**：`internal/ai/se_prompt.go`（Dispatch: 959-987）

#### `get_impacted_docs(doc_id)` / `sync_doc_exports(doc_id)` / `verify_doc_exports(doc_id)`

- `get_impacted_docs`：查询某个文档的依赖方列表（SE 工具 + AP 工具 `check_impact`）
- `sync_doc_exports`：从 `code_ref` 指向的 Go 文件提取导出符号，更新 frontmatter
- `verify_doc_exports`：双向对比代码 exports vs 文档 exports，报告差异

### 2.8 审核清单集成

在 AP 审核流程中，需要加入以下检查项：

#### 必须项
- [ ] **代码与文档一致性**：变更的代码与对应文档的 `exports`/`summary` 一致。
- [ ] **任务范围合规**：变更内容完全符合 `PROJECT_PLAN.md` 中分配的任务描述。
- [ ] **CHANGELOG 记录**：本次变更已追加到 `CHANGELOG.md`。

#### 建议项
- [ ] **影响分析**：运行 `argus --check-impact` 查看受影响的其他文档。
- [ ] **集成测试**：对于可能破坏依赖的变更，建议运行集成测试。

审核通过后，系统调用 `ClearDirty` 清除本次任务涉及文档的脏标记。


## 3. 提示词重构

### 3.1 目标与现状

**问题**：
- 存在两份 PM 提示词（`internal/core/prompts.go` 英文 27 行，`internal/ai/pm_prompt.go` 中文 173 行），行为矛盾。
- 提示词过长（173 行），模型只保留前 ~60 行有效信息。
- 详细规则（工具表、QA 流程）挤占核心行为指令的注意力。
- `ChatWithTools` 缺少语言注入，导致输出语言不可控。
- 分类未覆盖系统运维操作（清理目录、查磁盘等）。

**目标**：
- 统一为一份英文核心提示词（~50 行），规则外放到 `internal/ai/pm_rules.go` 和 `.argus/skills/`。
- 保持与现有基准测试相当的行为表现。
- 支持用户语言自动切换（通过 `GetLanguageInstruction`）。
- 决策树覆盖所有任务类型：闲聊、编码、系统运维、查询、文档处理。

### 3.2 新架构：核心提示词 + 参考规则 + 外置 skills

```
┌─────────────────────────────────────────────┐
│  Core Prompt (代码内嵌，~50行)               │
│  - 身份、第一原则、决策树、通信规则、防循环     │
├─────────────────────────────────────────────┤
│  Reference Rules (代码内嵌，~50行)           │
│  - 工具表、任务类型规范、错误处理示例          │
├─────────────────────────────────────────────┤
│  Skills (.argus/skills/ 外置文件)            │
│  - workflow.md, permissions.md, checklist   │
│  - 用户自定义技能（编码规范等）                │
├─────────────────────────────────────────────┤
│  Language Instruction (运行时注入)            │
│  - 根据用户输入语言决定回复语言               │
└─────────────────────────────────────────────┘
```

### 3.3 PM 核心提示词设计（~50 行英文）

**文件位置**：`internal/ai/pm_prompt.go`（替换现有内容）

**4 级重量概念**（PM 自然判断，无需硬编码）：

| 级别 | 路径 | 含义 |
|------|------|------|
| ⚡ Featherweight | PM 直执 | 一轮工具调用能干完 |
| Lightweight | PM→SE（无 AP） | 多步但范围清晰 |
| Medium | PM→SE→PM→AP 全流程 | 基线，需要分析和审核 |
| Heavy | 全流程 + 影响分析 | 跨模块，AP 审核时加影响分析 |

> **v0.9.4 变更**：删除了 `argus.go` 中的硬编码关键词启发式检测（hello world、fibonacci、create+run）。重量完全由 PM 决策树判断。用户可用 `/level featherweight|lightweight|medium|heavy` 手动覆盖。

**PM 核心提示词**（已实现）：

```go
// PMPrompt is the PM agent's core behavioral prompt.
const PMPrompt = `You are Argus PM — an autonomous project manager that uses tools to get things done.

Working directory: %s

=== IDENTITY ===
You are both Project Manager and QA engineer. Your job: understand what the user wants and get it done efficiently. You have two modes: execute simple tasks directly yourself, or delegate complex work to the Software Engineer (SE).

=== FIRST PRINCIPLES ===
1. Always use tools — never respond with just text unless it's a greeting. Every turn must call at least one tool.
2. Search before asking — if something is unclear, use list_files/grep/find_files/web_search to gather context first. Only @USR as last resort, with specific options.
3. Concise and direct — report results, don't add suggestions unless asked.

=== DECISION TREE ===
User message
  ├─ greeting/chat/thanks → @USR <reply>
  ├─ unclear/ambiguous → use tools to investigate, then @USR <question with options>
  ├─ simple task (one round of tool calls) → EXECUTE DIRECTLY (code/system/query/doc)
  └─ complex task (multi-step, needs analysis) → @SE <task breakdown>
       After SE completes → verify with tools → @AP for final approval

=== COMMUNICATION ===
@SE <task> — assign work to SE
@AP <result> — submit for final approval
@USR <message> — talk to the user
One @ per message maximum.

=== ANTI-LOOP ===
- If SE completes a task, do not re-assign the same task.
- If a tool errors twice on the same input, try a different approach.
- If no progress after 3 attempts, @USR with summary of attempts.
`
```

### 3.4 PMRules 参考内容（~50 行）

**文件位置**：`internal/ai/pm_rules.go`（新增，已实现）

**内容**：

```go
const PMRules = `
=== TOOL REFERENCE ===
exec <command> — run shell command. Non-zero exit is informational, not failure.
write_file <path> <content> — create/overwrite file (creates dirs).
edit_file <path> <old> <new> — string replacement.
delete_file <path> — delete file/empty dir.
read_file <path> — read file.
list_files [dir] — list directory.
grep_content <pattern> [glob] — search content.
find_files <name> — find by name.
web_search <query> — search web.
fetch_url <url> — fetch web page.
add_todo / update_todo — task tracking.

=== TASK TYPE NORMS ===
Code: write_file then exec to verify. Compile/run errors → fix and retry up to 3x.
System: exec command, check output. Exit code != 0 is normal (e.g., grep no match).
Query: use search/read tools, present findings concisely.
Document: use appropriate tool for format.

=== ERROR HANDLING ===
- Code errors (compile/syntax) → parse stderr, fix, retry.
- System command exit 1 → include output as-is, it's data not failure.
- Tool errors (file not exist) → try alternative, then @USR.

=== QA / REVIEW PROCESS ===
After SE completes → read_file + exec to verify.
Pass → @AP; Fail → auto-fix (compile only) or @SE rework.
`
```

### 3.5 SE / AP 提示词精简

- **SE 提示词**：目前位于 `internal/ai/se_prompt.go`，长度适中（~100 行），暂不强制精简，但应移除与 PMRules 重复的工具表，改为引用。
- **AP 提示词**：审核清单移到 `internal/core/prompts.go` 的 `APFullPrompt` 中，核心提示词只保留审核原则和驳回格式。文档审计项已添加。

### 3.6 语言控制与 `ChatWithTools` 修复

**现状**：`ChatStream` 和 `Chat` 已经调用了 `GetLanguageInstruction`，但 `ChatWithTools` 没有。

**修改**：在 `client.go` 的 `ChatWithTools` 函数中，构建请求前追加语言指令。

### 3.7 代码修改清单

| 文件 | 操作 | 状态 |
|------|------|------|
| `internal/ai/pm_prompt.go` | 替换 `PMPrompt` 常量为英文核心版（~70行） | ✅ 已完成 |
| `internal/ai/pm_rules.go` | 新增文件，包含 `PMRules` 常量 | ✅ 已完成 |
| `internal/ai/pm_prompt.go` | 修改 `getSystemPrompt()`，追加 `PMRules` | ✅ 已完成 |
| `internal/core/prompts.go` | 统一 `RolePM`，指向 `ai.PMPrompt`，删除旧常量 | ✅ 已完成 |
| `internal/ai/client.go` | `ChatWithTools` 增加 `GetLanguageInstruction` 调用 | ✅ 已完成 |
| `internal/core/argus.go` | **彻底删除** `seExecutionSatisfied` 函数，调用点简化为 `execErr == nil` | ✅ 已完成 |
| `internal/core/argus.go` | `isChatMessage` 移除 `taskKw` 关键词列表（ultra-short 检测），纯靠问候词列表 + PM 决策树 | ✅ 已完成 |
| `internal/core/argus.go` | 删除 `needsExecution` 函数（关键词硬编码），由 PM 决策树判断 | ✅ 已完成 |
| `internal/ai/pm_prompt.go` | `ide_send` 工具定义、`SetIDEMessageEmitter`/`SetIDEList` 方法、`wantsIDEDelegation` 函数全部注释保留（标 TODO 择机启用） | ✅ 已完成 |
| `internal/chat/manager.go` | IDE 消息推送初始化调用全部注释保留 | ✅ 已完成 |

### 3.8 Skills 目录使用规范

`.argus/skills/` 下的文件在 AI 启动时自动加载。加载顺序：
1. `_system/*.md`（系统级，必须）
2. 其他 `*.md`（按字母序）
3. 用户自定义（后续）

每个 skill 文件是 Markdown，包含 YAML frontmatter 标明名称、版本、适用角色等。


## 4. 合并实施顺序

### Phase 0：基础设施（已完成 ✅）

- [x] 实现 frontmatter 解析器（读/写） — `internal/doclib/doclib.go`
- [x] 实现 `argus --rebuild-tree`（扫描、验证、缓存） — `internal/doclib/cli.go`
- [x] 实现 `argus --tree`（打印树） — `internal/doclib/cli.go`
- [x] 实现 `argus --check-impact`（影响分析） — `internal/doclib/cli.go`
- [x] 实现 `update_doc` 工具（含权限校验和 dirty 标记）
- [x] 实现 `log_change` 工具（写 CHANGELOG）
- [x] 写 `PMPrompt` 和 `PMRules`（提示词重构核心：英文核心 + 规则分层）
- [x] 统一 `PromptKit.Get(RolePM)` 指向 `ai.PMPrompt`
- [x] `ChatWithTools` 加语言注入
- [x] 移除 `seExecutionSatisfied` 中的 exit status / command failed 硬检查
- [x] `pmDirectExecute` 重试提示改通用版
- [x] 删除 `argus.go` 硬编码关键词启发式检测（hello world / fibonacci 等）

### Phase 1：提炼调度器（已完成 ✅）

- [x] 给 `complete_task` 工具添加 `docs` 参数
- [x] 实现脏文档收集和父链展开逻辑 — `PropagateDirty`
- [x] 自底向上 dirty 标记传播
- [x] 审核通过后脏标记清除 — `ClearDirty`（`forceProjectApproved`）

### Phase 2：依赖管理与影响分析（已完成 ✅）

- [x] 注册 `get_impacted_docs` AI 工具（SE 工具集）
- [x] 实现 Go AST 导出符号提取 — `ExtractExportsFromFile`
- [x] 实现 `sync_doc_exports` AI 工具
- [x] `check_impact` 注册为 AP 审核工具

### Phase 3：AP 审核集成（已完成 ✅）

- [x] AP 提示词加入文档审计检查项
- [x] 实现 `verify_doc_exports` 双向对比工具
- [x] AP 批准后自动清除脏标记

### Phase 4：清理旧代码（已完成 ✅）

- [x] 统一 `PromptKit.Get(RolePM)` 指向 `ai.PMPrompt`
- [x] 删除 `internal/core/prompts.go` 中的旧 PMPrompt 常量
- [x] **彻底删除** `seExecutionSatisfied` 函数（不只是移除硬检查，整个函数删除，调用点简化为 `execErr == nil`）
- [x] 删除 `needsExecution` 关键词硬编码函数
- [x] `isChatMessage` 移除 `taskKw` 关键词列表
- [x] 注释掉 IDE 相关代码（`ide_send` 工具、`SetIDEList`、`SetIDEMessageEmitter`、`wantsIDEDelegation`），标 TODO 择机启用
- [x] 更新文档与代码状态一致


## 5. 测试与验收

### 分层记忆模型验收
- [x] 创建测试文档，运行 `argus --tree` 输出正确树形。
- [x] 修改子文档，调用 `complete_task`，检查父文档 dirty 传播。
- [x] 运行 `argus --check-impact` 输出正确依赖列表。
- [x] AP 审核通过后 dirty 标志清除（已通过单元测试验证 `ClearDirty` / `TestPropagateAndClearDirty_Integrated`）。

### 提示词重构验收
- [ ] 启动 Argus，输入"写一个 hello.go"，PM 应直接执行（写入文件并运行），回复语言与用户输入一致。
- [ ] 输入"帮我设计一个认证模块"，PM 应拆解任务并 @SE 委派。
- [ ] 输入复杂任务后，SE 完成并调用 `complete_task`，PM 应自动验证并 @AP。
- [ ] 检查 `conversation.log`，确认提示词长度明显缩短（对比旧版）。

### 端到端验收
- [ ] 一个完整的任务闭环：用户需求 → PM 拆解 → SE 实现 → AP 审核 → 通过。过程中文档树自动更新，CHANGELOG 记录变更。
- [ ] C 的兜底机制验证：人为制造任务卡住，超过阈值后 C 是否发送提醒。

---

**本文档是 Argus 开发的权威指南。所有实现必须严格遵循上述定义。如有歧义，以本指南为准。**