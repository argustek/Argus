<template>
  <div class="file-tree-panel">
    <div class="panel-header">
      <span class="address-bar" :title="workDir">{{ workDir || '未设置工作目录' }}</span>
      <button class="refresh-btn" @click.stop="refresh" :title="'刷新'">↻</button>
    </div>
    <div class="tree-body">
      <div v-if="loading" class="loading">加载中...</div>
      <div v-else-if="error" class="error">{{ error }}</div>
      <div v-else-if="!workDir" class="empty">未设置工作目录</div>
      <div v-else-if="treeItems.length === 0" class="empty">空目录</div>
      <FileTreeItem
        v-for="item in treeItems"
        :key="item.name + item.path"
        :item="item"
        :selected-path="selectedPath"
        :work-dir="workDir"
        @select="onSelectFile"
        @context="onContextAction"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, inject } from 'vue'
import FileTreeItem from './FileTreeItem.vue'

const props = defineProps<{
  workDir: string
}>()

const emit = defineEmits(['select-file', 'select-binary-file', 'open-in-editor', 'run-in-terminal', 'add-to-chat'])
const showError = inject('showError') as ((msg: string) => void) || alert

const treeItems = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const selectedPath = ref('')

// 调用后端 ListFiles()，将扁平列表转为树形结构
async function refresh() {
  if (!props.workDir) { treeItems.value = []; return }
  loading.value = true; error.value = ''
  try {
    // @ts-ignore Wails binding
    const files: any[] = await window.go.main.App.ListFiles()
    treeItems.value = buildTree(files || [])
  } catch (e: any) {
    error.value = e?.message || String(e)
    treeItems.value = []
  } finally {
    loading.value = false
  }
}

const EXECUTABLE_EXTS = ['.exe', '.bat', '.cmd', '.ps1', '.com', '.msi']

function isExecutable(path: string) {
  const ext = path?.toLowerCase().split('.').pop()
  return ext ? EXECUTABLE_EXTS.includes('.' + ext) : false
}

function onSelectFile(item: any) {
  selectedPath.value = item.path
  if (isExecutable(item.path)) {
    emit('select-binary-file', item)
  } else {
    emit('select-file', item)
  }
}

async function onContextAction(data: { action: string; item: any }) {
  const { action, item } = data

  switch (action) {
    case 'run': {
      emit('run-in-terminal', { command: item.path })
      break
    }
    case 'add-to-chat': {
      emit('add-to-chat', item.path)
      break
    }
    case 'open-explorer': {
      try {
        // @ts-ignore Wails binding
        await window.go.main.App.OpenFileLocation(item.path)
      } catch (e) {
        console.error('打开文件位置失败:', e)
      }
      break
    }
    case 'delete': {
      try {
        // @ts-ignore Wails binding — 需要后端有 DeleteFile 方法
        // 如果没有就提示用户
        if (window.go.main.App.DeleteFile) {
          // @ts-ignore
          await window.go.main.App.DeleteFile(item.path)
          refresh()
        } else {
          showError('删除功能暂未实现')
        }
      } catch (e: any) {
        showError('删除失败: ' + e.message)
      }
      break
    }
  }
}

// 扁平列表 → 树形结构（按 path 分层）
function buildTree(files: any[]): any[] {
  const root: Record<string, any> = {}
  const sorted = [...files].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    return a.path.localeCompare(b.path)
  })

  for (const f of sorted) {
    const parts = f.path.replace(/\\/g, '/').split('/')
    let current = root
    for (let i = 0; i < parts.length - 1; i++) {
      const part = parts[i]
      if (!current[part]) current[part] = { name: part, type: 'folder', path: parts.slice(0, i + 1).join('/'), children: {} }
      current = current[part].children
    }
    const leaf = parts[parts.length - 1]
    current[leaf] = {
      name: f.name,
      type: f.isDir ? 'folder' : 'file',
      path: f.path,
      size: f.size,
      modTime: f.modTime,
      children: f.isDir ? {} : undefined,
    }
  }

  function objToArray(obj: Record<string, any>): any[] {
    if (!obj) return []
    return Object.values(obj).map((node: any) => ({
      ...node,
      children: node.children ? objToArray(node.children) : undefined,
    }))
  }
  return objToArray(root)
}

onMounted(() => { if (props.workDir) refresh() })

// 监听 workDir 变化（App.vue 异步加载，FileTree 先挂载）
watch(() => props.workDir, (newDir) => {
  if (newDir) refresh()
})
</script>

<style scoped>
.file-tree-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-secondary);
  border-right: 1px solid var(--border-color);
  overflow: hidden;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  font-size: 12px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-color);
  user-select: none;
  gap: 4px;
  min-height: 30px;
}
.address-bar {
  flex: 1;
  font-size: 11.5px;
  font-family: 'Consolas', 'Cascadia Code', monospace;
  color: var(--text-secondary);
  padding: 2px 6px;
  border-radius: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: default;
  background: var(--bg-tertiary);
  border: 1px solid transparent;
  transition: border-color 0.15s;
}
.address-bar:hover {
  border-color: var(--border-color);
  color: var(--text-primary);
}

.refresh-btn {
  background: none; border: none; cursor: pointer;
  font-size: 14px; color: var(--text-tertiary);
  padding: 2px 4px; border-radius: 4px;
}
.refresh-btn:hover { color: var(--text-primary); background: var(--bg-tertiary); }

.tree-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}

.loading, .error, .empty {
  padding: 20px; text-align: center; font-size: 13px; color: var(--text-tertiary);
}
.error { color: #f87171; }
</style>
