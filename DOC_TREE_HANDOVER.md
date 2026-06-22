# DocTree 功能交接文档

## 当前状态

DocTree 前端已实现（数据通了、能显示了），但架构需要按今天讨论的设计重写。

## Git Commits（按时间倒序）

| Commit | 内容 |
|---|---|
| `4a35a68` | 恢复面板双分隔条 + 根节点可见 |
| `0b8263e` | 树嵌套包含根节点 + 文件树面板宽度可拖拽 |
| `712ddca` | DocTreeNode 按 depth 缩进显示层级 |
| `fd8615d` | 从 message bus 信封提取 raw.data 解包 |
| `44f0281` | DocTree 改用 message bus 替代直接 Wails binding |
| `e96732e` | GetDocTree return JSON string 兼容 Wails 序列化 + addLog |
| `f1d567f` | GetDocTree return type 修复 + DocTreeNode 路径 bug 修复 |
| `3155f83` | 文档：分层记忆模型设计 |
| `d712424` | feat: DocTree 前端 + PathStatus 跟踪 |

## 当前实现的问题

当前是"以文档为节点"：每个 .md 文件就是一个树节点，`parent` 指向另一个文件的 `id`。这跟原设计不符。

### 按今天讨论结果——正确设计

每个树节点是一组文件的集合，用 `node_id` 标识：

- Frontmatter 增加字段：`node_id`（节点号）、`node_title`（节点显示名）、`parent`（父节点号）
- 节点号格式：`L0`（根）、`L1-1`（第一层第一个）、`L1-2`、`L2-1` 等
- 多个文档可以声明同一个 `node_id` → 归入同一节点
- `parent` 指向父节点的 `node_id`，不是文档 id
- `L0` 无 parent，其他节点 parent 必须存在，否则报错
- 每个文档必须属于某个 `node_id`，不允许独立文档

### 示例

```
PROJECT_PLAN.md → node_id: L0, parent: ""
auth-feature.md → node_id: L1-1, parent: L0
data-feature.md → node_id: L1-2, parent: L0
```

树显示：
```
▼ L0 项目总览
    PROJECT_PLAN.md
    project-schedule.md
    prs.md
  ▼ L1-1 需求分析
      auth-feature.md
  ▼ L1-2 系统设计
      design-doc.md
    ▼ L2-1 架构设计
        ...
```

节点名 `node_title` 在项目规划阶段自定义。

## 测试文档（未提交 git）

测试文档在 `E:\TempArgusTest` 目录下，**不在此仓库中**（独立测试目录）。

### config.json 配置

`config/config.json` 第 141 行：
```json
"workDir": "E:\\TempArgusTest"
```

### 文档清单

```
E:\TempArgusTest\.argus\
├── PROJECT_PLAN.md          ← 当前根节点
├── tree\
│   ├── api-spec.md
│   ├── auth-feature.md
│   ├── data-feature.md
│   ├── design-doc.md
│   ├── review-plan.md
│   └── security-audit.md
├── cache\
│   └── tree.json            ← 自动生成的树缓存
└── backups\
```

### 当前 frontmatter 格式（待改）

```yaml
---
id: "tree/auth-feature.md"
parent: "PROJECT_PLAN.md"
owner_role: "SE"
title: "Authentication Module"
dirty: false
last_updated: "2026-06-22T00:00:00Z"
---
```

### 目标 frontmatter 格式

```yaml
---
id: "auth-feature.md"
node_id: "L1-1"
node_title: "需求分析"
parent: "L0"
owner_role: "SE"
title: "Authentication Module"
dirty: false
last_updated: "2026-06-22T00:00:00Z"
---
```

## 待实现

1. **`doclib.go`** — `DocNode` 加 `NodeID`/`NodeTitle`/`ParentNode` 字段；`BuildTree` 按 `node_id` 分组建树（非按文档 id）；`parent` 校验（L0 无 parent，其他必须有）
2. **`app.go` `buildDocTreeNested`** — 改为两层：节点 → 节点下文件列表
3. **`DocTreeNode.vue`** — 节点行显示 `node_id + node_title`，展开后列出组内文件（可点击打开）
4. **测试文档** — 统一改为新 frontmatter 格式
5. **"消息丢失" 报警** — `doc-tree-data` 通过 `PathSystem` 发送导致被跟踪但未 ACK，换个非跟踪路径或加 ACK

## 相关文件清单

| 文件 | 说明 |
|---|---|
| `app.go:712-724` | `req-doc-tree` 事件监听（message bus handler） |
| `app.go:2301-2358` | `GetDocTree()` 和 `buildDocTreeNested()` |
| `internal/doclib/doclib.go` | 文档树核心逻辑：扫描、解析、建树、缓存 |
| `frontend/src/components/DocTree.vue` | 文档树面板组件（message bus 方式） |
| `frontend/src/components/DocTreeNode.vue` | 树节点递归组件 |
| `frontend/src/components/FileTree.vue:34` | Tab 切换 `<DocTree v-else>` |
| `frontend/src/App.vue:50-83` | 面板布局 + 双分隔条 |
| `config/config.json:141` | `workDir: "E:\\TempArgusTest"` |

## 启动方式

```bash
# 项目根目录 E:\ArgusTek\Argus
# 1. 杀死旧进程
Get-Process argus-desktop -ErrorAction SilentlyContinue | Stop-Process -Force
# 2. 清前端缓存
Remove-Item -Recurse -Force frontend\dist -ErrorAction SilentlyContinue
# 3. 构建前端（frontend/ 下）
npm run build
# 4. wails 打包（根目录下）
wails build
# 5. 启动
Start-Process -FilePath "$pwd\build\bin\argus-desktop.exe" -WorkingDirectory $pwd
```
