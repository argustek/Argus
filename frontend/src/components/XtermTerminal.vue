<template>
  <div class="xterm-terminal" ref="terminalContainer">
    <div class="terminal-wrapper" ref="terminalWrapper"></div>
    
    <!-- 底部工具栏 -->
    <div class="terminal-toolbar" v-if="showToolbar">
      <div class="toolbar-left">
        <span class="terminal-title">{{ title }}</span>
      </div>
      <div class="toolbar-right">
        <button 
          class="toolbar-btn" 
          :class="{ active: autoScroll }"
          @click="toggleAutoScroll"
          title="自动滚动"
        >🔒</button>
        <button class="toolbar-btn" @click="clearTerminal" title="清屏 (Ctrl+L)">🗑️</button>
        <button class="toolbar-btn" @click="$emit('close')" v-if="closable" title="关闭">×</button>
      </div>
    </div>

    <!-- 右键菜单 -->
    <div 
      v-show="showContextMenu" 
      class="context-menu"
      :style="{ left: contextMenuPos.x + 'px', top: contextMenuPos.y + 'px' }"
    >
      <div class="context-item" @click="copySelection">📋 复制</div>
      <div class="context-item" @click="pasteFromClipboard">📌 粘贴</div>
      <div class="context-divider"></div>
      <div class="context-item" @click="selectAll">📑 全选</div>
      <div class="context-item" @click="clearTerminal">🗑️ 清屏</div>
      <div class="context-divider"></div>
      <div class="context-item" @click="findDialogVisible = true">🔍 查找...</div>
    </div>

    <!-- 查找对话框 -->
    <div v-show="findDialogVisible" class="find-dialog">
      <input 
        ref="findInputRef"
        type="text" 
        v-model="searchText"
        placeholder="查找..."
        @input="performSearch"
        @keydown.escape="findDialogVisible = false"
        @keydown.enter="findNext"
      />
      <button @click="findPrev">▲</button>
      <button @click="findNext">▼</button>
      <button @click="findDialogVisible = false">✕</button>
      <span class="find-count">{{ findResultCount }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { SearchAddon } from 'xterm-addon-search'
import { WebLinksAddon } from 'xterm-addon-web-links'
import 'xterm/css/xterm.css'

const props = withDefaults(defineProps<{
  title?: string
  showToolbar?: boolean
  closable?: boolean
  theme?: 'dark' | 'light'
}>(), {
  title: '终端',
  showToolbar: true,
  closable: false,
  theme: 'dark'
})

const emit = defineEmits(['data', 'resize', 'close', 'command'])

const terminalContainer = ref<HTMLElement>()
const terminalWrapper = ref<HTMLElement>()
const findInputRef = ref<HTMLInputElement>()

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let searchAddon: SearchAddon | null = null

const showContextMenu = ref(false)
const contextMenuPos = ref({ x: 0, y: 0 })
const autoScroll = ref(true)
const findDialogVisible = ref(false)
const searchText = ref('')
const findResultCount = ref('')

const commandHistory = ref<string[]>([])
const historyIndex = ref(-1)
const currentInputLine = ref('')

function getThemeConfig() {
  return props.theme === 'dark' ? {
    background: '#1e1e1e',
    foreground: '#d4d4d4',
    cursor: '#ffffff',
    cursorAccent: '#000000',
    selectionBackground: '#264f78',
    black: '#000000',
    red: '#cd3131',
    green: '#0dbc79',
    yellow: '#e5c07b',
    blue: '#61afef',
    magenta: '#c678dd',
    cyan: '#56b6c2',
    white: '#d4d4d4',
    brightBlack: '#808080',
    brightRed: '#f14c4c',
    brightGreen: '#23d18b',
    brightYellow: '#f5f543',
    brightBlue: '#61afef',
    brightMagenta: '#d19aee',
    brightCyan: '#56b6c2',
    brightWhite: '#ffffff'
  } : {
    background: '#ffffff',
    foreground: '#333333',
    cursor: '#333333',
    cursorAccent: '#ffffff',
    selectionBackground: '#add6ff',
    black: '#000000',
    red: '#cd3131',
    green: '#00bc00',
    yellow: '#949800',
    blue: '#0451a5',
    magenta: '#bc05bc',
    cyan: '#0598bc',
    white: '#555555',
    brightBlack: '#666666',
    brightRed: '#cd3131',
    brightGreen: '#14ce14',
    brightYellow: '#b5ba00',
    brightBlue: '#0451a5',
    brightMagenta: '#bc05bc',
    brightCyan: '#0598bc',
    brightWhite: '#a5a5a5'
  }
}

onMounted(() => {
  initTerminal()
  
  document.addEventListener('click', hideContextMenu)
  document.addEventListener('keydown', handleGlobalKeydown)
})

onUnmounted(() => {
  disposeTerminal()
  document.removeEventListener('click', hideContextMenu)
  document.removeEventListener('keydown', handleGlobalKeydown)
})

function initTerminal() {
  if (!terminalWrapper.value) return

  term = new Terminal({
    theme: getThemeConfig(),
    fontFamily: "'Consolas', 'Monaco', 'Courier New', monospace",
    fontSize: 13,
    lineHeight: 1.4,
    cursorBlink: true,
    cursorStyle: 'bar',
    scrollback: 10000,
    tabStopWidth: 4,
    allowProposedApi: true,
    allowTransparency: true
  })

  fitAddon = new FitAddon()
  searchAddon = new SearchAddon()
  const webLinksAddon = new WebLinksAddon()

  term.loadAddon(fitAddon)
  term.loadAddon(searchAddon)
  term.loadAddon(webLinksAddon)

  term.open(terminalWrapper.value)
  
  nextTick(() => {
    fitAddon?.fit()
  })

  term.onData((data) => {
    emit('data', data)
  })

  term.onResize(({ cols, rows }) => {
    emit('resize', { cols, rows })
  })

  term.onScroll(() => {
    const scrollTop = term?.element?.scrollTop || 0
    const scrollHeight = term?.element?.scrollHeight || 0
    const clientHeight = term?.element?.clientHeight || 0
    autoScroll.value = (scrollHeight - scrollTop - clientHeight) < 50
  })

  term.attachCustomKeyEventHandler((event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === 'c' && term?.hasSelection()) {
      copySelection()
      event.preventDefault()
      return false
    }

    if ((event.ctrlKey || event.metaKey) && event.key === 'v') {
      pasteFromClipboard()
      event.preventDefault()
      return false
    }

    if ((event.ctrlKey || event.metaKey) && event.key === 'l') {
      clearTerminal()
      event.preventDefault()
      return false
    }

    if ((event.ctrlKey || event.metaKey) && event.key === 'f') {
      findDialogVisible.value = true
      nextTick(() => findInputRef.value?.focus())
      event.preventDefault()
      return false
    }

    if ((event.ctrlKey || event.metaKey) && event.key === 'k') {
      clearCurrentLine()
      event.preventDefault()
      return false
    }

    if (event.key === 'ArrowUp' && !event.ctrlKey && !event.metaKey) {
      navigateHistory(-1)
      return false
    }

    if (event.key === 'ArrowDown' && !event.ctrlKey && !event.metaKey) {
      navigateHistory(1)
      return false
    }

    return true
  })

  terminalWrapper.value.addEventListener('contextmenu', handleContextMenu)

  writeWelcomeMessage()

  setTimeout(() => {
    term?.focus()
  }, 200)
}

function disposeTerminal() {
  term?.dispose()
  term = null
  fitAddon = null
  searchAddon = null
}

function write(text: string) {
  term?.write(text)
}

function writeln(text: string) {
  term?.writeln(text)
}

function writeColorful(line: string) {
  let colored = line
  
  const patterns = [
    { regex: /\[ERROR\]|\[ERR\]|error|Error|❌/gi, color: '\x1b[31m' },
    { regex: /\[WARN\]|\[WARNING\]|warn|Warning|⚠️|🟡/gi, color: '\x1b[33m' },
    { regex: /\[INFO\]|\[DEBUG\]|info|Info|✅|🟢|SUCCESS/gi, color: '\x1b[32m' },
    { regex: /\[FATAL\]|fatal|Fatal|💀/gi, color: '\x1b[1;31m' },
    { regex: /(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})/g, color: '\x1b[90m' },
    { regex: /(file:\/\/[^\s]+|[A-Za-z]:\\[^\s]+\.go|[A-Za-z]:\\[^\s]+\.py)/g, color: '\x1b[36m' },
    { regex: /(https?:\/\/[^\s]+)/g, color: '\x1b[34;4m' },
    { regex: /(\d+ms|\d+\.\d+s|\d+MB|\d+KB)/g, color: '\x1b[35m' }
  ]

  for (const pattern of patterns) {
    colored = colored.replace(pattern.regex, `${pattern.color}$&\x1b[0m`)
  }

  writeln(colored)
}

function writeWelcomeMessage() {
  term?.write('\x1b[90mArgus Terminal ready.\x1b[0m\r\n')
}

function focus() {
  term?.focus()
}

function clearTerminal() {
  term?.clear()
  hideContextMenu()
}

function clearCurrentLine() {
  term?.write('\r\x1b[K')
}

function toggleAutoScroll() {
  autoScroll.value = !autoScroll.value
  if (autoScroll.value) {
    scrollToBottom()
  }
}

function scrollToBottom() {
  if (!autoScroll.value) return
  term?.scrollToBottom()
}

function fit() {
  nextTick(() => {
    fitAddon?.fit()
  })
}

function handleContextMenu(e: MouseEvent) {
  e.preventDefault()
  contextMenuPos.value = { x: e.clientX, y: e.clientY }
  showContextMenu.value = true
}

function hideContextMenu() {
  showContextMenu.value = false
}

async function copySelection() {
  try {
    const selection = term?.getSelection()
    if (selection) {
      await navigator.clipboard.writeText(selection)
    }
  } catch (err) {
    console.error('复制失败:', err)
  }
  hideContextMenu()
}

async function pasteFromClipboard() {
  try {
    const text = await navigator.clipboard.readText()
    emit('data', text)
  } catch (err) {
    console.error('粘贴失败:', err)
  }
  hideContextMenu()
}

function selectAll() {
  term?.selectAll()
  hideContextMenu()
}

function performSearch() {
  if (!searchText.value) {
    findResultCount.value = ''
    return
  }
  searchAddon?.findNext(searchText.value, {
    caseSensitive: false,
    wholeWord: false,
    regex: false,
    decorations: {
      activeMatchColorOverviewRuler: '#ff0000',
      matchBackground: '#ff000040'
    }
  })
}

function findNext() {
  if (searchText.value) {
    searchAddon?.findNext(searchText.value)
  }
}

function findPrev() {
  if (searchText.value) {
    searchAddon?.findPrevious(searchText.value)
  }
}

function addToHistory(cmd: string) {
  if (cmd.trim()) {
    commandHistory.value.push(cmd.trim())
    if (commandHistory.value.length > 500) {
      commandHistory.value.shift()
    }
    historyIndex.value = commandHistory.value.length
  }
}

function navigateHistory(direction: number) {
  if (commandHistory.value.length === 0) return

  historyIndex.value += direction

  if (historyIndex.value < 0) {
    historyIndex.value = 0
  } else if (historyIndex.value >= commandHistory.value.length) {
    historyIndex.value = commandHistory.value.length
    currentInputLine.value = ''
    return
  }

  currentInputLine.value = commandHistory.value[historyIndex.value]
  emit('command', currentInputLine.value)
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) {
    return
  }
}

watch(() => props.theme, () => {
  if (term) {
    const newTheme = getThemeConfig()
    Object.entries(newTheme).forEach(([key, value]) => {
      term!.options[key as keyof typeof newTheme] = value as string & number
    })
  }
})

defineExpose({
  write,
  writeln,
  writeColorful,
  focus,
  clearTerminal,
  fit,
  scrollToBottom,
  addToHistory,
  terminal: term
})
</script>

<style scoped>
.xterm-terminal {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: linear-gradient(180deg, #1a1a2e 0%, #16213e 100%);
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid #667eea30;
  box-shadow: 
    0 4px 20px rgba(0, 0, 0, 0.4),
    inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

.xterm-terminal:focus-within {
  border-color: #667eea80;
  box-shadow: 
    0 4px 24px rgba(102, 126, 234, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.terminal-wrapper {
  flex: 1;
  padding: 10px;
  overflow: hidden;
  cursor: text;
}

.terminal-wrapper:active {
  cursor: text;
}
  display: flex;

.terminal-toolbar {
  justify-content: space-between;
  align-items: center;
  padding: 4px 12px;
  background: linear-gradient(180deg, #0d1b3e 0%, #1a1a3e 100%);
  border-top: 1px solid #667eea30;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.terminal-title {
  color: #cccccc;
  font-weight: 500;
}

.toolbar-right {
  display: flex;
  gap: 4px;
}

.toolbar-btn {
  width: 26px;
  height: 26px;
  border: none;
  background: transparent;
  color: #999;
  cursor: pointer;
  font-size: 13px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: all 0.15s;
}

.toolbar-btn:hover {
  background: #3c3c3c;
  color: #fff;
}

.toolbar-btn.active {
  color: #61afef;
  background: rgba(97, 175, 239, 0.15);
}

.context-menu {
  position: fixed;
  z-index: 1000;
  min-width: 160px;
  background: #252526;
  border: 1px solid #454545;
  border-radius: 4px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
  padding: 4px 0;
}

.context-item {
  padding: 6px 24px;
  color: #cccccc;
  font-size: 13px;
  cursor: pointer;
  transition: background 0.1s;
}

.context-item:hover {
  background: #094771;
  color: #fff;
}

.context-divider {
  height: 1px;
  background: #454545;
  margin: 4px 0;
}

.find-dialog {
  position: absolute;
  top: 10px;
  right: 60px;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 10px;
  background: #252526;
  border: 1px solid #454545;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  z-index: 100;
}

.find-dialog input {
  width: 200px;
  padding: 4px 8px;
  background: #3c3c3c;
  border: 1px solid #555;
  border-radius: 3px;
  color: #ccc;
  font-size: 13px;
  outline: none;
}

.find-dialog input:focus {
  border-color: #61afef;
}

.find-dialog button {
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: #999;
  cursor: pointer;
  border-radius: 2px;
  font-size: 11px;
}

.find-dialog button:hover {
  background: #3c3c3c;
  color: #fff;
}

.find-count {
  color: #888;
  font-size: 11px;
  margin-left: 4px;
  min-width: 40px;
}
</style>