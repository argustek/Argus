# PLAN: Document Tree (文档树) Frontend UI

> 现状分析见底部附录

## 目标

后端 `internal/doclib/` 已完整实现了文档树（`DocTree`），但前端**从未构建**对应的 UI 组件。目前前端只有 `FileTree.vue`（项目文件树），两者完全不同。

本计划将文档树可视化，让用户能直观浏览 `.argus/tree/` 下的分层文档，查看 dirty 标记、owner_role、exports 信息。

---

## Step 1: 后端 Wails Binding

`app.go` 在 `ListFiles()` 后新增 `GetDocTree()` 方法：

```go
import "argus/internal/doclib"

// GetDocTree 返回文档树嵌套 JSON（前端可直接消费）
func (a *App) GetDocTree() (interface{}, error) {
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
    return buildDocTreeNested(tree), nil
}
```

**序列化策略**（否决方案说明）：
- ❌ ~~新建 `DocTreeNode` 结构体~~ → 冗余，`doclib.DocNode` 已有 `json` tag
- ✅ 递归函数 `buildDocTreeNested`：利用 `DocTree.Children map[string][]*DocNode` 从根开始构建嵌套 `[]map[string]interface{}`
- ✅ 字段名跟 `DocNode` json tag 保持一致：`id`, `parent`, `owner_role`, `title`, `summary`, `dirty`, `last_updated`, `exports`, `children`

```go
func buildDocTreeNested(tree *doclib.DocTree) []map[string]interface{} {
    // tree.Children[""] 或 tree.Root.ID 为根
    rootID := ""
    if tree.Root != nil {
        rootID = tree.Root.ID
    }
    return buildChildren(tree, rootID)
}

func buildChildren(tree *doclib.DocTree, parentID string) []map[string]interface{} {
    var result []map[string]interface{}
    for _, node := range tree.Children[parentID] {
        // 用 json.Marshal/Unmarshal 利用已有 json tag 保证字段一致
        item := nodeToMap(node)
        if children := buildChildren(tree, node.ID); len(children) > 0 {
            item["children"] = children
        }
        result = append(result, item)
    }
    return result
}
```

---

## Step 2: 前端 FileTree.vue 改造 — 新增 Tab 栏

否决方案：
- ❌ ~~SideBar 新增按钮~~ → 当前 UI 没有 SideBar，窗口切换在 TopBar
- ❌ ~~LeftPanel 新增 section~~ → 当前没有 LeftPanel 组件
- ✅ FileTree.vue 自身增加 tab 栏（"📁 文件" / "📋 文档"），切换显示文件树或文档树

改造后结构：

```vue
<template>
  <div class="file-tree-panel">
    <!-- Tab 栏 -->
    <div class="panel-tabs">
      <span class="tab" :class="{ active: activeTab === 'files' }" @click="activeTab = 'files'">
        📁 {{ t('topBar.fileTree') }}
      </span>
      <span class="tab" :class="{ active: activeTab === 'docs' }" @click="activeTab = 'docs'">
        📋 {{ t('docTree.title') }}
      </span>
    </div>
    <!-- 文件树内容 -->
    <template v-if="activeTab === 'files'">
      <div class="panel-header">
        <span class="address-bar" :title="workDir">{{ workDir || '未设置工作目录' }}</span>
        <button class="refresh-btn" @click.stop="refresh(true)" title="刷新">↻</button>
      </div>
      <div class="tree-body">
        ... 现有 FileTree 内容 ...
      </div>
    </template>
    <!-- 文档树内容 -->
    <DocTree v-else :work-dir="workDir" @open-doc="handleOpenDoc" />
  </div>
</template>
```

---

## Step 3: 前端 DocTree.vue 组件

新建 `frontend/src/components/DocTree.vue`，功能：

- **加载**：调用 `window.go.main.App.GetDocTree()`，loading spinner
- **展开/折叠**：树形层级，点击可展开子节点
- **节点信息**：每行显示：
  - `owner_role` 彩色标签（PM=🔵, SE=🟢, AP=🟠）
  - 标题（`title`）
  - 摘要预览（`summary`，截断）
  - dirty 标记（🟡 圆点）
  - exports 数量角标
- **点击文件**：emit `open-doc` → App.vue 在 EditorWindow 打开 `.md`
- **刷新**：手动刷新按钮 + 监听 `doc-tree-dirty` 事件（先做手动，后续加自动）

```vue
<template>
  <div class="doc-tree">
    <div class="panel-header">
      <span>{{ t('docTree.title') }}</span>
      <button class="refresh-btn" @click="refresh" title="刷新">↻</button>
    </div>
    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <div v-else-if="!treeData || treeData.length === 0" class="empty">{{ t('docTree.noDocuments') }}</div>
    <div v-else class="tree-body">
      <DocTreeNode
        v-for="node in treeData"
        :key="node.id"
        :node="node"
        @select="(path: string) => emit('open-doc', path)"
      />
    </div>
  </div>
</template>
```

---

## Step 4: DocTreeNode.vue 递归组件

新建 `frontend/src/components/DocTreeNode.vue`，递归渲染：

```vue
<template>
  <div class="doc-tree-node">
    <div class="node-row" @click="toggle" :style="{ paddingLeft: (depth * 16) + 'px' }">
      <span class="toggle-icon">{{ expanded ? '▼' : '▶' }}</span>
      <span class="role-tag" :class="node.owner_role">{{ node.owner_role }}</span>
      <span class="node-title">{{ node.title }}</span>
      <span v-if="node.dirty" class="dirty-dot" title="已修改">🟡</span>
      <span v-if="node.exports?.length" class="export-badge">{{ node.exports.length }}</span>
    </div>
    <div v-if="expanded && node.children?.length" class="node-children">
      <DocTreeNode
        v-for="child in node.children"
        :key="child.id"
        :node="child"
        :depth="depth + 1"
        @select="(path: string) => emit('select', path)"
      />
    </div>
  </div>
</template>
```

---

## Step 5: 国际化

`zh-CN.ts` 和 `en-US.ts` 新增：

```ts
docTree: {
  title: '文档树',
  noDocuments: '暂无文档',
  roles: {
    PM: '需求',
    SE: '实现',
    AP: '审核',
  },
}
```

---

## Step 6（后续）：实时刷新

后端在检测到 `.argus/tree/` 下文件变更时 emit `doc-tree-dirty` 事件，前端监听并静默刷新。模式同现有 `file-tree-dirty` + `ListFiles()`。

---

## 否决方案记录

| 方案 | 原因 |
|------|------|
| 新建 DocTreeNode 结构体序列化 | doclib.DocNode 已有 json tag，重复造轮子 |
| SideBar 加按钮 | 当前 UI 没有激活的 SideBar 组件 |
| LeftPanel 加 section | 当前没有 LeftPanel 组件，文件树直接由 App.vue 渲染 |
| 独立窗口（如 GitWindow） | 文档树是导航工具，不适合浮动窗口 |

---

## 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `app.go` | 修改 | 新增 `GetDocTree()` binding + `buildDocTreeNested` |
| `frontend/src/components/FileTree.vue` | 修改 | 新增 tab 栏，导入 DocTree |
| `frontend/src/components/DocTree.vue` | **新建** | 文档树主组件 |
| `frontend/src/components/DocTreeNode.vue` | **新建** | 递归树节点组件 |
| `frontend/src/i18n/locales/zh-CN.ts` | 修改 | 新增 `docTree` 翻译 |
| `frontend/src/i18n/locales/en-US.ts` | 修改 | 新增 `docTree` 翻译 |
| `docs/PLAN_doctree_frontend.md` | 修改 | 本文件，保持与实现同步 |

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
