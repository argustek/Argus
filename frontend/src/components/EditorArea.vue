<template>
  <div class="editor-area">
    <!-- 标签栏 -->
    <div class="tab-bar">
      <div 
        v-for="tab in tabs" 
        :key="tab.id"
        class="tab"
        :class="{ active: activeTab === tab.id, modified: tab.modified }"
        @click="$emit('tab-change', tab.id)"
      >
        <span class="tab-name">{{ tab.modified ? '● ' : '' }}{{ tab.name }}</span>
        <button class="tab-close" @click.stop="$emit('tab-close', tab.id)">×</button>
      </div>
    </div>
    
    <!-- 编辑器内容 -->
    <div class="editor-content">
      <div v-if="tabs.length === 0" class="empty-state">
        <div class="empty-icon">👁️</div>
        <div class="empty-title">Argus IDE</div>
        <div class="empty-desc">打开文件开始编辑，或使用 AI 助手</div>
        <div class="empty-actions">
          <button class="btn" @click="openFile">打开文件</button>
          <button class="btn btn-primary" @click="newFile">新建文件</button>
        </div>
      </div>
      
      <div v-else ref="editorContainer" class="monaco-container"></div>
    </div>
    
    <!-- 状态栏 -->
    <div class="editor-status-bar" v-if="tabs.length > 0">
      <span class="status-item">{{ currentLanguage }}</span>
      <span class="status-item">Ln {{ cursorPosition.lineNumber }}, Col {{ cursorPosition.column }}</span>
      <span class="status-item">Spaces: 2</span>
      <span class="status-item">UTF-8</span>
    </div>
    
    <!-- AI 输入框 -->
    <div class="ai-input-bar">
      <input 
        type="text" 
        class="ai-input" 
        placeholder="输入指令让 AI 帮你写代码..."
        v-model="aiMessage"
        @keyup.enter="sendMessage"
      />
      <button class="btn btn-primary" @click="sendMessage">发送</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import * as monaco from 'monaco-editor'
import { OpenFileDialog, SaveFile, ReadFile as ReadFileWails, WriteFile as WriteFileWails } from '../../wailsjs/go/main/App'

const props = defineProps<{
  tabs: Array<{id: string, name: string, content: string, path?: string, modified?: boolean}>
  activeTab: string
}>()

const emit = defineEmits(['tab-change', 'tab-close', 'send-message', 'file-saved', 'file-opened'])

const editorContainer = ref<HTMLElement>()
const aiMessage = ref('')
let editorInstance: monaco.editor.IStandaloneCodeEditor | null = null

const cursorPosition = ref({ lineNumber: 1, column: 1 })

const currentTabContent = computed(() => {
  const tab = props.tabs.find(t => t.id === props.activeTab)
  return tab?.content || ''
})

const currentLanguage = computed(() => {
  const tab = props.tabs.find(t => t.id === props.activeTab)
  if (!tab?.name) return 'Plain Text'
  
  const ext = tab.name.split('.').pop()?.toLowerCase()
  const langMap: Record<string, string> = {
    'go': 'go',
    'js': 'javascript',
    'ts': 'typescript',
    'tsx': 'typescriptreact',
    'jsx': 'javascriptreact',
    'py': 'python',
    'java': 'java',
    'c': 'c',
    'cpp': 'cpp',
    'h': 'c',
    'hpp': 'cpp',
    'cs': 'csharp',
    'rb': 'ruby',
    'php': 'php',
    'swift': 'swift',
    'kt': 'kotlin',
    'rs': 'rust',
    'html': 'html',
    'css': 'css',
    'scss': 'scss',
    'less': 'less',
    'json': 'json',
    'xml': 'xml',
    'yaml': 'yaml',
    'yml': 'yaml',
    'md': 'markdown',
    'sql': 'sql',
    'sh': 'shell',
    'bash': 'shell',
    'bat': 'batch',
    'ps1': 'powershell',
    'dockerfile': 'dockerfile',
    'vue': 'html',
    'svelte': 'html'
  }
  
  return langMap[ext || ''] || 'Plain Text'
})

function initMonacoEditor() {
  if (!editorContainer.value) return
  
  editorInstance = monaco.editor.create(editorContainer.value, {
    value: currentTabContent.value,
    language: currentLanguage.value.toLowerCase(),
    theme: 'vs-dark',
    automaticLayout: true,
    fontSize: 14,
    fontFamily: "'Consolas', 'Monaco', 'Courier New', monospace",
    lineNumbers: 'on',
    minimap: { enabled: true },
    scrollBeyondLastLine: false,
    wordWrap: 'on',
    tabSize: 2,
    insertSpaces: true,
    renderWhitespace: 'selection',
    bracketPairColorization: { enabled: true },
    guides: {
      indentation: true,
      bracketPairs: true
    },
    suggest: {
      showKeywords: true,
      showSnippets: true,
      showFunctions: true,
      showVariables: true
    }
  })
  
  editorInstance.onDidChangeCursorPosition((e) => {
    cursorPosition.value = e.position
  })
  
  editorInstance.onDidChangeModelContent(() => {
    if (editorInstance) {
      const content = editorInstance.getValue()
      emit('content-changed', props.activeTab, content)
    }
  })
}

watch(() => props.activeTab, async (newTabId) => {
  await nextTick()
  if (editorInstance) {
    const tab = props.tabs.find(t => t.id === newTabId)
    if (tab) {
      const model = editorInstance.getModel()
      if (model) {
        monaco.editor.setModelLanguage(model, currentLanguage.value.toLowerCase())
        model.setValue(tab.content)
      }
    }
  }
})

watch(() => currentTabContent.value, (newContent) => {
  if (editorInstance && !editorInstance.hasTextFocus()) {
    const model = editorInstance.getModel()
    if (model && model.getValue() !== newContent) {
      model.setValue(newContent)
    }
  }
})

async function openFile() {
  try {
    const result = await OpenFileDialog()
    if (result) {
      const content = await ReadFileWails(result)
      emit('file-opened', {
        id: Date.now().toString(),
        name: result.split(/[/\\]/).pop() || 'untitled',
        content: content,
        path: result,
        modified: false
      })
    }
  } catch (error) {
    console.error('打开文件失败:', error)
  }
}

async function saveCurrentFile() {
  if (!editorInstance || !props.activeTab) return
  
  try {
    const tab = props.tabs.find(t => t.id === props.activeTab)
    if (!tab) return
    
    const content = editorInstance.getValue()
    
    if (tab.path) {
      await WriteFileWails(tab.path, content)
      emit('file-saved', props.activeTab, false)
    } else {
      const savedPath = await SaveFile(tab.name, content)
      if (savedPath) {
        emit('file-saved', props.activeTab, false, savedPath)
      }
    }
  } catch (error) {
    console.error('保存文件失败:', error)
  }
}

function newFile() {
  const id = Date.now().toString()
  emit('file-opened', {
    id: id,
    name: `untitled-${id.slice(-4)}.txt`,
    content: '',
    modified: true
  })
}

function sendMessage() {
  if (aiMessage.value.trim()) {
    emit('send-message', aiMessage.value)
    aiMessage.value = ''
  }
}

function handleKeyDown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault()
    saveCurrentFile()
  }
}

onMounted(() => {
  initMonacoEditor()
  window.addEventListener('keydown', handleKeyDown)
})

onBeforeUnmount(() => {
  if (editorInstance) {
    editorInstance.dispose()
  }
  window.removeEventListener('keydown', handleKeyDown)
})

defineExpose({
  saveCurrentFile,
  openFile,
  newFile,
  getEditorContent: () => editorInstance?.getValue() || '',
  insertText: (text: string) => {
    if (editorInstance) {
      const position = editorInstance.getPosition()
      if (position) {
        editorInstance.executeEdits('', [{
          range: new monaco.Range(position.lineNumber, position.column, position.lineNumber, position.column),
          text: text
        }])
        editorInstance.focus()
      }
    }
  }
})
</script>

<style scoped>
.editor-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
  overflow: hidden;
}

.tab-bar {
  display: flex;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
  overflow-x: auto;
  flex-shrink: 0;
}

.tab {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
  border-right: 1px solid var(--border-color);
  min-width: 120px;
  max-width: 200px;
  position: relative;
}

.tab:hover {
  background: var(--bg-tertiary);
}

.tab.active {
  background: var(--bg-primary);
  border-top: 2px solid var(--accent-color);
}

.tab.modified .tab-name::before {
  content: '●';
  color: #f0ad4e;
  margin-right: 4px;
}

.tab-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tab-close {
  width: 16px;
  height: 16px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: 3px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}

.tab-close:hover {
  background: var(--error-color);
  color: #fff;
}

.editor-content {
  flex: 1;
  overflow: hidden;
  position: relative;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 64px;
  margin-bottom: 16px;
}

.empty-title {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.empty-desc {
  font-size: 14px;
  margin-bottom: 24px;
}

.empty-actions {
  display: flex;
  gap: 12px;
}

.monaco-container {
  width: 100%;
  height: 100%;
}

.editor-status-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 4px 12px;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
  font-size: 12px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.status-item {
  white-space: nowrap;
}

.ai-input-bar {
  display: flex;
  gap: 8px;
  padding: 12px 16px;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
  flex-shrink: 0;
}

.ai-input {
  flex: 1;
  padding: 10px 16px;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 14px;
}

.ai-input:focus {
  outline: none;
  border-color: var(--accent-color);
}
</style>