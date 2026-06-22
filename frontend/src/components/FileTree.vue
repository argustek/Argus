<template>
  <div class="file-tree-panel">
    <div class="panel-tabs">
      <span class="tab" :class="{ active: activeTab === 'files' }" @click="activeTab = 'files'">
        <span class="tab-icon">📁</span>
        <span class="tab-label">{{ t('topBar.fileTree') }}</span>
      </span>
      <span class="tab" :class="{ active: activeTab === 'docs' }" @click="activeTab = 'docs'">
        <span class="tab-icon">📋</span>
        <span class="tab-label">{{ t('docTree.title') }}</span>
      </span>
    </div>
    <template v-if="activeTab === 'files'">
      <div class="panel-header">
        <span class="address-bar" :title="workDir">{{ workDir || '未设置工作目录' }}</span>
        <button class="refresh-btn" @click.stop="refresh(true)" :title="t('common.refresh')">↻</button>
      </div>
      <div class="tree-body">
        <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
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
    </template>
    <DocTree v-else :work-dir="workDir" @open-doc="handleOpenDoc" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { EventsOn, EventsOff } from '../../wailsjs/runtime'
import FileTreeItem from './FileTreeItem.vue'
import DocTree from './DocTree.vue'

const { t } = useI18n()

const props = defineProps<{
  workDir: string
}>()

const emit = defineEmits(['select-file', 'select-binary-file', 'open-in-editor', 'run-in-terminal', 'add-to-chat'])
const showError = inject('showError') as ((msg: string) => void) || alert

const activeTab = ref('files')
const treeItems = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const selectedPath = ref('')

async function refresh(silent = false) {
  if (!props.workDir) { treeItems.value = []; return }
  if (!silent) { loading.value = true }
  error.value = ''
  try {
    // @ts-ignore Wails binding
    const files: any[] = await window.go.main.App.ListFiles()
    treeItems.value = buildTree(files || [])
  } catch (e: any) {
    if (!silent) { error.value = e?.message || String(e) }
    treeItems.value = []
  } finally {
    if (!silent) { loading.value = false }
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

function handleOpenDoc(path: string) {
  emit('select-file', { path, name: path.split('/').pop() || path, isDir: false })
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

onMounted(() => {
  if (props.workDir) refresh()
  EventsOn('file-tree-dirty', () => {
    if (props.workDir) refresh(true)
  })
})

onUnmounted(() => {
  EventsOff('file-tree-dirty')
})

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

.panel-tabs {
  display: flex;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
  user-select: none;
  flex-shrink: 0;
}

.tab {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 12px;
  font-size: 12px;
  color: var(--text-tertiary);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  transition: color 0.15s, border-color 0.15s;
}

.tab:hover {
  color: var(--text-primary);
  background: var(--bg-tertiary);
}

.tab.active {
  color: var(--text-primary);
  border-bottom-color: var(--accent-color);
}

.tab-icon {
  font-size: 14px;
}

.tab-label {
  font-weight: 500;
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
