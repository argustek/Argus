# Argus 分层记忆模型 v3 开发指南

> 本文档用于指导 AI 开发者（或 AI agent）实现 Argus 的文档树分层记忆模型。
> 本文档与代码同步更新。每次 Phase 完成后，必须更新本文档以及映实现现状。

## 目录

1. [核心模型](#核心模型)
2. [目录结构](#目录结构)
3. [文档规范（frontmatter）](#文档规范frontmatter)
4. [命令行接口](#命令行接口)
5. [提炼调度器与脏标记传播](#提炼调度器与脏标记传播)
6. [依赖表管理](#依赖表管理)
7. [工具函数实现要求](#工具函数实现要求)
8. [审核清单集成](#审核清单集成)
9. [实现优先级与测试](#实现优先级与测试)

---

## 核心模型

- 项目文档组织为**有根树**，根节点唯一：`PROJECT_PLAN.md`。
- 每个节点是一份 Markdown 文件，包含 YAML frontmatter。
- 每个节点有明确管理员角色（PM / SE / AP）。
- 父子关系由子节点的 `parent` 字段指向父节点的 `id` 定义，**与文件路径无关**。
- 文档树与代码分离，叶子文档通过 `code_ref` 引用代码文件。
- 变更通过 SE 调用 `complete_task` 信号触发向上脏标记传播，逐层标记父文档。
- 本系统用于 Argus IDE 帮助用户开发项目时管理项目架构，不用于 Argus 自身开发。

---

## 目录结构

```
项目根目录/
├── .argus/
│   ├── PROJECT_PLAN.md           # 根节点（唯一）
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

### 实现参考

| 逻辑 | 文件 |
|------|------|
| 文档扫描 | `internal/doclib/doclib.go:ScanForDocs` |

---

## 文档规范（frontmatter）

### 必填字段

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `id` | string | 文档唯一标识，推荐使用相对于项目根的路径形式（如 `tree/auth/jwt.md`） | `"tree/auth/README.md"` |
| `parent` | string | 父文档的 `id`。根节点填空字符串 `""` | `"tree/auth/README.md"` 或 `""` |
| `owner_role` | string | 管理员角色：`"PM"` / `"SE"` / `"AP"` | `"SE"` |
| `title` | string | 文档标题 | `"JWT 令牌模块"` |

### 推荐字段

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `summary` | string | 供父节点提炼的摘要（建议 ≤ 150 字符） | `"提供 JWT 生成和验证功能"` |
| `code_ref` | string | 引用的代码文件路径（相对于项目根） | `"src/auth/jwt.go"` |
| `code_ref_type` | string | `"file"` / `"function"` / `"package"` | `"file"` |

### 系统维护字段（AI 不应手动修改）

| 字段 | 类型 | 说明 |
|------|------|------|
| `dirty` | boolean | 是否有未向上传播的变更（由系统标记） |
| `last_updated` | timestamp | 最后更新时间（ISO 8601） |
| `exports` | array | 对外接口列表（自动从代码提取或手动维护） |
| `dependencies` | array | 依赖的其他文档 `id` 列表（自动更新） |

### Frontmatter 完整示例

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

### 实现参考

| 逻辑 | 文件 |
|------|------|
| 解析 frontmatter | `internal/doclib/doclib.go:ParseFrontmatter` |
| 写入 frontmatter | `internal/doclib/doclib.go:WriteFrontmatter` |
| 读写文档文件 | `internal/doclib/doclib.go:ReadDocFile` / `WriteDocFile` |
| 数据结构 | `internal/doclib/doclib.go:DocNode` |

---

## 命令行接口

四个命令，均通过 `argus` 可执行文件调用。不依赖 Wails 初始化，直接在 `main()` 开头拦截。

### 注册位置

`main.go:44-63` — 在 `NewApp()` 之前拦截并 return。

### 1. `argus --tree`

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

### 2. `argus --rebuild-tree`

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

### 3. `argus --check-impact <doc_id>`

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

### 实现参考

| 逻辑 | 文件 |
|------|------|
| CLI 入口 | `main.go:44-63` |
| CLI 处理器 | `internal/doclib/cli.go` |
| 树构建 | `internal/doclib/doclib.go:BuildTree` |
| 树打印 | `internal/doclib/doclib.go:PrintTree` |
| 缓存读写 | `internal/doclib/doclib.go:SaveCache` / `LoadCache` |
| 影响分析 | `internal/doclib/doclib.go:GetImpactedDocs` |

---

## 提炼调度器与脏标记传播

### 触发时机

当 SE 在 `ProcessTaskWithTools` 多轮循环中调用 `complete_task` 时触发。仅在 SEProcessor 路径中生效，ArgusCore 旧路径（`executeActions`）不触发提炼。

### 数据结构

- 每个文档有一组系统维护字段（存储于 frontmatter）：`dirty`、`last_updated`。
- `complete_task` 工具额外接受 `docs` 参数（影响到的文档 ID 列表）。

### 传播规则

**关键设计决策**：传播是**系统行为**，不校验角色权限。SE 调用 `complete_task` 时传入 `docs`，系统自动沿父链向上传播脏标记。传播只修改系统维护字段（`dirty`、`last_updated`），不改 `owner_role` / `title` / `id` / `code_ref`。

PM 看到 `PROJECT_PLAN.md` 的 `dirty=true`，就知道下游有变更，需要 review。

### 流程

1. **收集脏文档**：SE 调用 `complete_task(docs=[...])`，传入本次影响到的文档 ID 列表。
2. **展开父链**：对每个脏文档，沿 parent 链向上展开，直到根节点。去重，确保每个节点只处理一次。
3. **按深度排序**：从最深的孩子开始处理（确保自底向上）。
4. **标记脏**：对每个节点，读取 frontmatter，设置 `dirty=true`、更新 `last_updated`，写回磁盘。
5. **更新缓存**：所有标记完成后，刷新 `.argus/cache/tree.json`。
6. PM/AP 审核通过后调用 `ClearDirty` 清除标记。

### 伪代码

```go
func PropagateDirty(rootDir string, docIDs []string) error {
    // 1. 构建 / 加载文档树
    tree := BuildTree(rootDir)

    // 2. 展开父链，去重
    toProcess := expandToAncestors(tree, docIDs)

    // 3. 按深度降序排序
    sortByDepthDesc(tree, toProcess)

    // 4. 逐个标记 dirty
    for _, id := range toProcess {
        node, body := ReadDocFile(idToPath[id])
        node.SetDirty(true)
        WriteDocFile(idToPath[id], node, body)
    }

    // 5. 更新缓存
    SaveCache(tree, rootDir)
}
```

### 实现参考

| 逻辑 | 文件 |
|------|------|
| 传播函数 | `internal/doclib/doclib.go:PropagateDirty` |
| 清除标记 | `internal/doclib/doclib.go:ClearDirty` |
| 触发点 | `internal/ai/se_prompt.go:227-234` |
| 深度计算 | `internal/doclib/doclib.go:GetDepth` |

---

## 依赖表管理

### 依赖表存储位置

每个文档的 frontmatter 中包含 `dependencies` 字段（系统维护的数组），存储该文档直接依赖的其他文档 `id` 列表。

父文档中不单独维护"子文档之间的依赖表"，而是通过每个文档自身的 `dependencies` 字段，结合 `argus check-impact` 时的反向索引来查询影响范围。

### 依赖关系更新

- **当前 MVP**：`dependencies` 由用户在文档中手动维护，系统不自动修改。
- **未来**：可集成 LSP 自动提取代码中的 import 语句，自动更新对应文档的 `dependencies`。
- **环检测**：在 `rebuild-tree` 时，除了检测父子环，还应检测依赖环。若检测到环，输出警告但仍允许存在。

### 实现参考

| 逻辑 | 文件 |
|------|------|
| 反向依赖查询 | `internal/doclib/doclib.go:GetImpactedDocs` |
| CLI 封装 | `internal/doclib/cli.go:CLICheckImpact` |

---

## 工具函数实现要求

Argus 提供以下内部工具函数（供 AI 模型调用）。定义在 `internal/ai/se_prompt.go` 的 `SETools` 变量中。

### 1. `update_doc(doc_id, content)`

- **功能**：更新文档内容（只修改正文，frontmatter 保留）。
- **权限校验**：SE 只能修改 `owner_role=SE` 的文档。PM/AP 文档拒绝修改。
- **副作用**：标记文档 `dirty=true`，更新 `last_updated`。
- **定义位置**：`internal/ai/se_prompt.go:2771`
- **Dispatch**：`internal/ai/se_prompt.go:939-957`

### 2. `complete_task(files, docs, summary)`

- **功能**：标记当前任务完成，触发脏标记传播。
- **`docs` 参数**：可选，影响到的文档 ID 列表。不传则不触发传播。
- **`files` 参数**：创建/修改的代码文件列表（已有）。
- **触发行为**：若 `docs` 非空，调用 `doclib.PropagateDirty(s.workDir, docs)`。
- **定义位置**：`internal/ai/se_prompt.go:2071`
- **拦截点**：`internal/ai/se_prompt.go:224-234`
- **注意**：传播是系统行为，不校验角色权限。

### 3. `log_change(role, task_id, action, affected_files, affected_docs, summary)`

- **功能**：向 `.argus/logs/CHANGELOG.md` 追加变更记录（只追加）。
- **格式**：包含时间戳、角色、任务 ID、变更摘要。
- **定义位置**：`internal/ai/se_prompt.go:2802`
- **Dispatch**：`internal/ai/se_prompt.go:959-987`

### 4. `get_impacted_docs(doc_id)` — 暂未实现为 AI 工具

- 当前仅通过 CLI 命令 `argus --check-impact` 可用。
- 后续可注册为 AI 工具供 PM/SE 在决策时调用。

---

## 审核清单集成

在 AP 审核流程中，需要加入以下检查项：

### 必须项
- [ ] **代码与文档一致性**：变更的代码与对应文档的 `exports`/`summary` 一致。
- [ ] **任务范围合规**：变更内容完全符合 `PROJECT_PLAN.md` 中分配的任务描述。
- [ ] **CHANGELOG 记录**：本次变更已追加到 `CHANGELOG.md`。

### 建议项
- [ ] **影响分析**：运行 `argus --check-impact` 查看受影响的其他文档。
- [ ] **集成测试**：对于可能破坏依赖的变更，建议运行集成测试。

审核通过后，系统调用 `ClearDirty` 清除本次任务涉及文档的脏标记。

---

## 实现优先级与测试

### Phase 0：基础设施（已完成 ✅）

- [x] 实现 frontmatter 解析器（读/写） — `internal/doclib/doclib.go:45-120`
- [x] 实现 `argus --rebuild-tree`（扫描、验证、缓存） — `internal/doclib/cli.go:17-34`
- [x] 实现 `argus --tree`（打印树） — `internal/doclib/cli.go:6-15`
- [x] 实现 `argus --check-impact`（影响分析） — `internal/doclib/cli.go:36-63`
- [x] 实现 `update_doc` 工具（含权限校验和 dirty 标记） — `internal/ai/se_prompt.go:939-957`
- [x] 实现 `log_change` 工具（写 CHANGELOG） — `internal/ai/se_prompt.go:959-987`

**验证方法**：手动创建测试文档，运行 `argus --rebuild-tree` 和 `argus --tree`，检查输出。

### Phase 1：提炼调度器（当前阶段 🔄）

- [x] 给 `complete_task` 工具添加 `docs` 参数 — `internal/ai/se_prompt.go:463-473`
- [x] 实现脏文档收集和父链展开逻辑 — `internal/doclib/doclib.go:PropagateDirty`
- [ ] 实现自底向上 dirty 标记传播
- [ ] 实现审核通过后的脏标记清除 — `internal/doclib/doclib.go:ClearDirty`

**实现决策**：
- 传播不校验角色权限（系统行为）
- 只修改系统字段（dirty、last_updated），不改正文
- 触发点挂在 SEProcessor.ProcessTaskWithTools 的 complete_task 拦截处
- PM 直执行路径（pmDirectExecute）不触发传播

**验证方法**：模拟 SE 调用 `complete_task(docs=[...])`，检查父文档的 `dirty` 标志是否被正确标记。

### Phase 2：依赖管理与影响分析（待开始）

- [ ] 为 AI 注册 `get_impacted_docs` 工具
- [ ] 集成静态分析（至少一种语言，如 Go），自动从代码提取 `exports` 和 `dependencies`
- [ ] 自动增量更新 `dependencies` 字段

**验证方法**：修改一个被其他文档依赖的接口，运行 `check-impact` 应列出依赖方。

### Phase 3：AP 审核集成（待开始）

- [ ] 在 AP 的提示词中加入审核清单（作为 skills 规则）
- [ ] 实现代码与文档对比工具（读取 `code_ref` 指向的代码文件，解析导出符号，与文档 `exports` 对比）
- [ ] 审核通过后自动清除任务的脏文档标记

---

## 附录

### 错误处理建议

- **孤儿文档**（parent 指向不存在的 id）：`rebuild-tree` 时警告，不阻断，但建议手动修复。
- **循环依赖**（父子环或依赖环）：警告，但仍然允许存在；在递归传播时使用 visited 集合避免死循环。
- **权限越权**：`update_doc` 返回权限拒绝错误。

### 性能考虑

- 缓存文档元数据（`.argus/cache/tree.json`），避免每次命令都重新解析所有 frontmatter。
- `PropagateDirty` 每次触发都重新构建树（非增量），大型项目需优化。

---

**本文档作为 Argus 开发者的权威实现指南。所有代码变更后必须同步更新本文档。**
