<template>
  <div class="left-panel">
    <!-- {{ t('topBar.fileTree') }} -->
    <div v-if="panel === 'explorer'" class="panel-content">
      <div class="panel-header">
        <span>{{ t('topBar.fileTree') }}</span>
        <button class="btn-icon" @click="refreshFiles">🔄</button>
      </div>
      <div class="file-tree">
        <FileTreeItem 
          v-for="item in files" 
          :key="item.name + item.path"
          :item="item"
          @select="(e: any) => emit('file-select', e)"
        />
      </div>
    </div>

    <!-- {{ t('common.search') }} -->
    <div v-if="panel === 'search'" class="panel-content">
      <div class="panel-header">{{ t('common.search') }}</div>
      <input type="text" class="input" :placeholder="t('common.search') + '...'" v-model="searchQuery" />
      <div class="search-results">
        <div v-for="result in searchResults" :key="result" class="search-item">
          {{ result }}
        </div>
      </div>
    </div>

    <!-- Git -->
    <div v-if="panel === 'git'" class="panel-content">
      <div class="panel-header">{{ t('topBar.gitVersionControl') }}</div>
      <div class="git-section">
        <div class="git-header">{{ t('git.workspace') }}</div>
        <div v-if="files.length === 0" class="git-empty">{{ t('git.noChanges') }}</div>
        <div v-for="f in files" :key="f.path" class="git-file">📄 {{ f.path }} <span v-if="f.status" class="git-status" :class="f.status">{{ f.status }}</span></div>
      </div>
      <button v-if="files.length > 0" class="btn btn-primary" style="width: 100%; margin-top: 12px;">
        {{ t('git.commit') }}
      </button>
    </div>

    <!-- {{ t('aiMonitor.aiAssistant') }} -->
    <div v-if="panel === 'ai'" class="panel-content">
      <div class="panel-header">{{ t('aiMonitor.aiAssistant') }}</div>
      <div class="ai-commands">
        <button class="ai-cmd" @click="$emit('ai-command', '解释代码')">{{ t('leftPanel.explainCode') }}</button>
        <button class="ai-cmd" @click="$emit('ai-command', '修复错误')">{{ t('leftPanel.fixError') }}</button>
        <button class="ai-cmd" @click="$emit('ai-command', '生成测试')">{{ t('leftPanel.generateTest') }}</button>
        <button class="ai-cmd" @click="$emit('ai-command', '优化代码')">{{ t('leftPanel.optimizeCode') }}</button>
      </div>
    </div>

    <!-- {{ t('aiMonitor.title') }} -->
    <div v-if="panel === 'monitor'" class="panel-content">
      <div class="panel-header">{{ t('aiMonitor.title') }}</div>
      <div class="monitor-section">
        <div class="monitor-item">
          <span>{{ t('leftPanel.panelA') }}</span>
          <span class="status-badge ok">{{ t('statusBar.ready') }}</span>
        </div>
        <div class="monitor-item">
          <span>{{ t('leftPanel.panelB') }}</span>
          <span class="status-badge ok">{{ t('statusBar.ready') }}</span>
        </div>
        <div class="monitor-item">
          <span>{{ t('leftPanel.workerB') }}</span>
          <span class="status-badge busy">{{ t('aiMonitor.busy') }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import FileTreeItem from './FileTreeItem.vue'

const { t } = useI18n()

const props = defineProps<{
  panel: string
}>()

const emit = defineEmits(['file-select', 'ai-command'])

const searchQuery = ref('')
const searchResults = ref<string[]>([])
const files = ref<any[]>([])
const fileLoading = ref(false)

async function refreshFiles() {
  fileLoading.value = true
  try {
    // @ts-ignore Wails binding
    const raw: any[] = await window.go.main.App.ListFiles()
    files.value = buildTree(raw || [])
  } catch (e) {
    console.error('[LeftPanel] refresh error:', e)
    files.value = []
  } finally {
    fileLoading.value = false
  }
}

// 扁平列表 → 树形结构
function buildTree(fileList: any[]): any[] {
  const root: Record<string, any> = {}
  const sorted = [...fileList].sort((a, b) => {
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
  refreshFiles()
})
</script>

<style scoped>
.left-panel {
  width: 250px;
  background: var(--bg-secondary);
  border-right: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
}

.panel-content {
  flex: 1;
  overflow: auto;
  padding: 12px;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-secondary);
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border-color);
}

.btn-icon {
  background: transparent;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
}

.file-tree {
  font-size: 13px;
}

.search-results {
  margin-top: 12px;
}

.search-item {
  padding: 6px 8px;
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}

.search-item:hover {
  background: var(--bg-tertiary);
}

.git-section {
  margin-top: 12px;
}

.git-header {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 8px;
}

.git-file {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 8px;
  font-size: 13px;
  cursor: pointer;
  border-radius: 4px;
}
.git-empty {
  padding: 12px 8px;
  font-size: 12px;
  color: var(--text-secondary);
  text-align: center;
}

.git-file:hover {
  background: var(--bg-tertiary);
}

.git-status {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 3px;
}

.git-status.modified {
  background: var(--warning-color);
  color: #000;
}

.git-status.added {
  background: var(--success-color);
  color: #000;
}

.ai-commands {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ai-cmd {
  padding: 10px;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  font-size: 13px;
}

.ai-cmd:hover {
  background: var(--border-color);
}

.monitor-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.monitor-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px;
  background: var(--bg-tertiary);
  border-radius: 6px;
  font-size: 13px;
}

.status-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
}

.status-badge.ok {
  background: var(--success-color);
  color: #000;
}

.status-badge.busy {
  background: var(--accent-color);
  color: #fff;
}

.status-badge.error {
  background: var(--error-color);
  color: #fff;
}
</style>
