<template>
  <div 
    class="floating-window editor-window"
    :style="{ left: windowPos.x + 'px', top: windowPos.y + 'px', width: windowWidth + 'px', height: windowHeight + 'px' }"
  >
    <div class="window-header" @mousedown="startDrag">
      <div class="header-left">
        <span class="window-title">📝 {{ currentFileName }}</span>
        <span v-if="modified" class="modified-indicator">●</span>
      </div>
      <div class="header-actions">
        <button class="action-btn" @click.stop="handleSave" title="保存 (Ctrl+S)">💾</button>
        <button class="action-btn" @click.stop="handleOpenFile" title="打开文件">📂</button>
        <button class="action-btn" @click.stop="$emit('close')" title="关闭">×</button>
      </div>
    </div>
    
    <div class="window-content">
      <div v-show="error && !loading" class="empty-state error">
        <div class="empty-text">{{ error }}</div>
      </div>
      <div v-show="!currentFilePath && !fileContent && !error" class="empty-state welcome">
        <div class="welcome-icon">👁️</div>
        <div class="welcome-title">Argus IDE</div>
        <div class="welcome-desc">打开文件开始编辑</div>
        <div class="welcome-actions">
          <button class="btn btn-primary" @click="handleOpenFile">📂 打开文件</button>
          <button class="btn" @click="handleNewFile">📄 新建文件</button>
        </div>
      </div>
      <div ref="editorContainer" class="monaco-container" :style="{ display: (error && !loading) || (!currentFilePath && !fileContent && !error) ? 'none' : 'flex' }">
        <div v-if="loading" class="loading-overlay">{{ t('editor.loading') }}...</div>
      </div>
    </div>
    
    <div class="status-bar" v-if="file || fileContent">
      <span class="status-item">{{ languageMode }}</span>
      <span class="status-item">Ln {{ cursorPosition.lineNumber }}, Col {{ cursorPosition.column }}</span>
      <span class="status-item">Spaces: 2</span>
      <span class="status-item">UTF-8</span>
      <span v-if="modified" class="status-item modified">已修改</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import * as monaco from 'monaco-editor'
import { ReadFile as ReadFileWails, OpenFileDialog, SaveFile, WriteFile as WriteFileWails } from '../../wailsjs/go/main/App'
import { useDraggable } from '../composables/useDraggable'

const { t } = useI18n()

const props = defineProps<{
  file?: any
}>()

const emit = defineEmits(['close', 'file-saved', 'file-opened'])

const { windowPos, startDrag } = useDraggable(320, 60)

const editorContainer = ref<HTMLElement>()
const loading = ref(false)
const error = ref('')
const fileContent = ref('')
const modified = ref(false)
let editorInstance: monaco.editor.IStandaloneCodeEditor | null = null

const windowWidth = ref(700)
const windowHeight = ref(500)

const cursorPosition = ref({ lineNumber: 1, column: 1 })

const internalFilePath = ref('')  // 🔑 内部状态：跟踪通过 📂 按钮打开的文件

const currentFilePath = computed(() => props.file?.path || internalFilePath.value)

const currentFileName = computed(() => {
  if (props.file?.name) return props.file.name
  if (!currentFilePath.value) return '未命名'
  return currentFilePath.value.split(/[/\\]/).pop() || 'untitled'
})

const languageMode = computed(() => {
  const name = currentFileName.value
  const ext = name.split('.').pop()?.toLowerCase()
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
    'svelte': 'html',
    'txt': 'plaintext'
  }
  return langMap[ext || ''] || 'plaintext'
})

function initMonacoEditor() {
  if (!editorContainer.value) {
    setTimeout(() => {
      if (editorContainer.value) {
        initMonacoEditor()
      } else {
        console.error('[EditorWindow] editorContainer 延迟后仍不存在')
      }
    }, 100)
    return
  }

  if (editorInstance) {
    editorInstance.dispose()
  }

  try {
  editorInstance = monaco.editor.create(editorContainer.value, {
    value: fileContent.value || '',
    language: languageMode.value,
    theme: 'vs-dark',
    automaticLayout: true,
    fontSize: 14,
    fontFamily: "'Consolas', 'Monaco', 'Courier New', monospace",
    lineNumbers: 'on',
    // [FIX] minimap 默认值: size='proportional'(按比例缩小,小窗口显小), scale=1, renderCharacters=true(文字模式)
    // 上一版/默认: minimap: { enabled: true }
    minimap: {
      enabled: true,
      size: 'fill',             // fill=填满编辑区高度 (默认 proportional 按比例缩小)
      scale: 2,                 // 放大缩略图内容 (默认 1)
      renderCharacters: false   // 色块模式更清晰 (默认 true 文字模式)
    },
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
    },
    autoIndent: 'full',
    formatOnPaste: true,
    formatOnType: true
  })
  } catch (e: any) {
  console.error('[EditorWindow] ❌ Monaco 创建失败:', e)
}

  if (editorInstance) {
    const model = editorInstance.getModel()
    if (model) {
      monaco.editor.setModelLanguage(model, languageMode.value)
    }

  editorInstance.onDidChangeCursorPosition((e) => {
    cursorPosition.value = e.position
  })

  editorInstance.onDidChangeModelContent(() => {
    if (editorInstance && !modified.value) {
      modified.value = true
    }
  })
  }
}

async function loadFile(filePath: string) {
  loading.value = true
  error.value = ''

  try {
    const content = await ReadFileWails(filePath)
    fileContent.value = content || ''
    modified.value = false

    loading.value = false
    await nextTick()

    if (editorInstance) {
      editorInstance.setValue(fileContent.value)
      editorInstance.layout()
      const model = editorInstance.getModel()
      if (model) {
        monaco.editor.setModelLanguage(model, languageMode.value)
      }
    } else {
      await new Promise(resolve => setTimeout(resolve, 50))
      initMonacoEditor()
    }
  } catch (e: any) {
    console.error('[EditorWindow] 文件加载失败:', e)
    error.value = t('editor.readError') + ': ' + e.message
    fileContent.value = ''
  } finally {
    loading.value = false
  }
}

function retryLoad() {
  if (currentFilePath.value) {
    loadFile(currentFilePath.value)
  }
}

watch(() => props.file, async (newFile) => {
  if (!newFile?.path) {
    fileContent.value = ''
    modified.value = false
    return
  }
  if ((newFile as any)._binary) {
    error.value = (newFile as any)._binaryError || `无法显示 "${newFile.name || newFile.path}"，因为它是二进制文件`
    fileContent.value = ''
    return
  }
  error.value = ''
  await loadFile(newFile.path)
})

onMounted(async () => {
  await nextTick()
  if (props.file?.path) {
    if ((props.file as any)._binary) {
      error.value = (props.file as any)._binaryError || `无法显示 "${props.file.name || props.file.path}"，因为它是二进制文件`
      fileContent.value = ''
    } else {
      await loadFile(props.file.path)
    }
  } else {
    initMonacoEditor()
  }
  window.addEventListener('keydown', handleKeyDown)
})

async function handleSave() {
  if (!editorInstance) return
  
  try {
    const content = editorInstance.getValue()
    
    if (currentFilePath.value) {
      await WriteFileWails(currentFilePath.value, content)
      modified.value = false
      emit('file-saved', currentFilePath.value)
    } else {
      const savedPath = await SaveFile(currentFileName.value, content)
      if (savedPath) {
        modified.value = false
        emit('file-saved', savedPath)
      }
    }
  } catch (e: any) {
    error.value = '保存失败: ' + e.message
  }
}

async function handleOpenFile() {
  try {
    const filePath = await OpenFileDialog()
    if (filePath) {
      internalFilePath.value = filePath
      await loadFile(filePath)
    }
  } catch (e: any) {
    error.value = '打开文件失败: ' + e.message
  }
}

function handleNewFile() {
  fileContent.value = ''
  modified.value = true
  
  nextTick(() => {
    initMonacoEditor()
  })
}

function handleKeyDown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault()
    handleSave()
  }
}

onBeforeUnmount(() => {
  if (editorInstance) {
    editorInstance.dispose()
  }
  window.removeEventListener('keydown', handleKeyDown)
})

defineExpose({
  saveFile: handleSave,
  openFile: handleOpenFile,
  newFile: handleNewFile,
  getContent: () => editorInstance?.getValue() || '',
  setContent: (content: string) => {
    if (editorInstance) {
      editorInstance.getModel()?.setValue(content)
    }
  }
})
</script>

<style scoped>
.floating-window {
  position: fixed;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.6);
  z-index: 100;
}

.window-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--bg-tertiary);
  border-radius: 8px 8px 0 0;
  border-bottom: 1px solid var(--border-color);
  cursor: move;
  user-select: none;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.window-title {
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.modified-indicator {
  color: #f0ad4e;
  font-size: 10px;
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.action-btn {
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
}

.action-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.window-content {
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
  padding: 20px;
}

.empty-state.error {
  color: #ff6b6b;
}

.empty-state.welcome .welcome-icon {
  font-size: 48px;
  margin-bottom: 12px;
}

.welcome-title {
  font-size: 20px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.welcome-desc {
  font-size: 13px;
  margin-bottom: 20px;
}

.welcome-actions {
  display: flex;
  gap: 12px;
}

.empty-text {
  font-size: 14px;
}

.retry-btn {
  margin-top: 12px;
  padding: 6px 16px;
  background: var(--accent-color);
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}

.retry-btn:hover {
  opacity: 0.9;
}

.monaco-container {
  width: 100%;
  height: 100%;
  position: relative;
}

.loading-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #aaa;
  font-size: 14px;
  z-index: 10;
}

.status-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 4px 12px;
  background: var(--bg-tertiary);
  border-top: 1px solid var(--border-color);
  font-size: 11px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.status-item {
  white-space: nowrap;
}

.status-item.modified {
  color: #f0ad4e;
  font-weight: 500;
}

.btn {
  padding: 8px 16px;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  cursor: pointer;
  font-size: 13px;
}

.btn:hover {
  background: var(--bg-hover);
}

.btn-primary {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: white;
}

.btn-primary:hover {
  opacity: 0.9;
}
</style>
