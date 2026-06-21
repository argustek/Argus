# PLAN: Document Tree (文档树) Frontend UI

> 现状分析见底部附录

## 目标

后端 `internal/doclib/` 已完整实现了文档树（`DocTree`），但前端**从未构建**对应的 UI 组件。目前前端只有 `FileTree.vue`（项目文件树），两者完全不同。

本计划将文档树可视化，让用户能直观浏览 `.argus/tree/` 下的分层文档，查看 dirty 标记、owner_role、exports 信息。

---

## Step 1: 后端 Wails Binding

`app.go` 新增方法：

```go
import "argus/internal/doclib"

// GetDocTree 返回文档树数据给前端
func (a *App) GetDocTree() (map[string]interface{}, error) {
    rootDir := a.getProjectDir()
    if rootDir == "" {
        return nil, fmt.Errorf("未设置工作目录")
    }
    // 优先读缓存，没有则构建
    tree, err := doclib.LoadCache(rootDir)
    if err != nil {
        tree, err = doclib.BuildTree(rootDir)
        if err != nil {
            return nil, err
        }
        doclib.SaveCache(tree, rootDir)
    }
    return serializeTree(tree), nil
}
```

序列化辅助函数 `serializeTree` 将 `DocTree` 转为前端友好的嵌套结构：

```go
type DocTreeNode struct {
    ID          string   `json:"id"`
    Parent      string   `json:"parent"`
    OwnerRole   string   `json:"owner_role"`
    Title       string   `json:"title"`
    Summary     string   `json:"summary,omitempty"`
    Dirty       bool     `json:"dirty"`
    LastUpdated string   `json:"last_updated"`
    Exports     []Export `json:"exports,omitempty"`
    Depth       int      `json:"depth"`
    Children    []*DocTreeNode `json:"children"`
}
```

---

## Step 2: 前端 DocTree.vue 组件

新建 `frontend/src/components/DocTree.vue`，功能：

- **展开/折叠**：树形层级，点击可展开子节点
- **节点信息**：每行显示：
  - 📄 图标
  - `owner_role` 彩色标签（PM=🔵, SE=🟢, AP=🟠）
  - 标题（`title`）
  - 摘要预览（`summary`，可截断）
  - dirty 标记（`🟡` 黄色圆点）
  - exports 数量角标
- **点击事件**：点击文件打开对应 `.md` 文档
- **空状态**：`no documents found` 提示
- **加载状态**：loading spinner

```vue
<template>
  <div class="doc-tree-panel">
    <div class="panel-header">
      <span>📋 文档树</span>
      <button class="refresh-btn" @click="refresh">↻</button>
    </div>
    <div v-if="loading" class="loading">加载中...</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <div v-else-if="!treeData" class="empty">暂无文档</div>
    <div v-else class="tree-body">
      <DocTreeNode
        v-for="node in treeData.children"
        :key="node.id"
        :node="node"
        @select="openDoc"
      />
    </div>
  </div>
</template>
```

`DocTreeNode` 子组件（递归）：

- `v-if="node.children?.length"` 显示展开/折叠按钮
- `owner_role` 标签：`PM` `SE` `AP` 各自颜色
- dirty 圆点：`v-if="node.dirty"`
- 导出计数：`v-if="node.exports?.length"`

---

## Step 3: 集成 SideBar

`SideBar.vue` 新增文档树按钮（在 explorer 下面）：

```html
<div class="icon-btn" :class="{ active: activePanel === 'doc' }"
     @click="$emit('panel-change', 'doc')" title="文档树">
  📋
</div>
```

---

## Step 4: 集成 LeftPanel

`LeftPanel.vue` 新增 panel section：

```html
<div v-if="panel === 'doc'" class="panel-content">
  <DocTree @open-doc="handleOpenDoc" />
</div>
```

导入 `DocTree` 组件。

---

## Step 5: 国际化

`zh-CN.ts` 和 `en-US.ts` 新增：

```ts
docTree: {
  title: '文档树',
  noDocuments: '暂无文档',
  role: {
    PM: '需求',
    SE: '实现',
    AP: '审核',
  },
}
```

---

## Step 6（可选）：实时刷新

后端在文档树变更时 emit `doc-tree-dirty` 事件，前端 `DocTree` 监听并静默刷新。类似 `file-tree-dirty` 的模式。

---

## 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `app.go` | 修改 | 新增 `GetDocTree()` binding |
| `frontend/src/components/DocTree.vue` | **新建** | 文档树主组件 |
| `frontend/src/components/DocTreeNode.vue` | **新建** | 递归树节点组件 |
| `frontend/src/components/SideBar.vue` | 修改 | 新增文档树 tab 按钮 |
| `frontend/src/components/LeftPanel.vue` | 修改 | 新增 `doc` panel section |
| `frontend/src/i18n/locales/zh-CN.ts` | 修改 | 新增中文翻译 |
| `frontend/src/i18n/locales/en-US.ts` | 修改 | 新增英文翻译 |

---

## 附录：现状分析

### 后端 `internal/doclib/` — ✅ 完整实现

| 模块 | 状态 | 文件 |
|------|------|------|
| DocNode/DocTree 数据模型 | ✅ | `doclib.go:18-43` |
| YAML frontmatter 解析 | ✅ | `doclib.go:48-84` |
| 树构建(BuildTree) | ✅ | `doclib.go:153-228` |
| 树验证(ValidateTree) | ✅ | `doclib.go:300-323` |
| 缓存(SaveCache/LoadCache) | ✅ | `doclib.go:325-393` |
| Dirty 传播 | ✅ | `doclib.go:516-539` |
| Go 导出提取 | ✅ | `doclib.go:541-596` |
| CLI 命令 | ✅ | `cli.go` |
| 测试覆盖 | ✅ | `doclib_test.go` (369行) |
| SE 工具集成 | ✅ | `se_prompt.go` |
| AP 工具集成 | ✅ | `ap_prompt.go` |

### 前端 — ❌ 从未实现

| 组件 | 现状 | 说明 |
|------|------|------|
| `FileTree.vue` | ✅ 已有 | 项目**文件树**（文件浏览器），**不是文档树** |
| `DocTree.vue` | ❌ 无 | 需要新建 |
| 文档树 UI | ❌ 无 | 从未构建 |

### 关键区别：文件树 vs 文档树

| 维度 | 文件树 (FileTree) | 文档树 (DocTree) |
|------|-------------------|-------------------|
| 数据源 | 磁盘文件系统 | `.argus/tree/` 下 `.md` 的 YAML frontmatter |
| 结构 | 目录层级 | `parent` 字段定义的父子关系 |
| 元数据 | 无 | `owner_role`, `title`, `dirty`, `exports` |
| 用途 | 浏览项目文件 | 分层记忆模型：需求→设计→实现 追溯 |

### Wails 绑定模式参考

现有 `ListFiles()` 在 `app.go:2266` 的模式：

```go
func (a *App) ListFiles() ([]map[string]interface{}, error) {
    rootDir := a.getProjectDir()
    // ... 遍历目录返回扁平列表
}
```

`GetDocTree()` 将遵循相同模式，返回前端可直接消费的嵌套 JSON。
