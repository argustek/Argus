<template>
  <div 
    class="floating-window terminal-window" 
    v-show="!minimized"
    :style="{ left: windowPos.x + 'px', top: windowPos.y + 'px', width: windowSize.w + 'px', height: windowSize.h + 'px' }"
  >
    <div class="top-bar" @mousedown="startDrag">
      <div class="tabs-row">
        <div 
          v-for="tab in tabs" 
          :key="tab.id"
          :class="['tab-item', { active: activeTabId === tab.id }]"
          @click="switchTab(tab.id)"
        >
          <span class="tab-name">{{ tab.name }}</span>
          <button class="tab-close" @click.stop="closeTab(tab.id)" v-if="tabs.length > 1">✕</button>
        </div>
      </div>
      <div class="top-right">
        <select v-model="encoding" class="enc-select" title="编码" @mousedown.stop>
          <option value="gbk">GBK</option>
          <option value="utf-8">UTF-8</option>
          <option value="gb2312">GB2312</option>
          <option value="shift-jis">Shift-JIS</option>
        </select>
        <button class="action-btn" @click.stop="clearScreen" title="清屏">🗑️</button>
        <button class="action-btn" @click.stop="addTab" title="新建标签页">＋</button>
        <button class="win-btn" @click.stop="minimize" title="最小化">─</button>
        <button class="win-btn" @click.stop="maximize" title="最大化">□</button>
        <button class="close-btn" @click.stop="$emit('close')">×</button>
      </div>
    </div>

    <div class="term-body" @contextmenu.prevent="onContextMenu">
      <div class="output-area" ref="outputRef" @click="focusInput" @mousedown="onOutputMouseDown" @mouseup="onOutputMouseUp" @scroll="onOutputScroll">
        <div v-for="(line, idx) in currentTabLines" :key="idx" class="output-line" v-html="line"></div>
        <div class="current-input-line">
          <span class="prompt-text">{{ promptText || '>' }}</span>
          <input
            ref="inputRef"
            type="text"
            v-model="currentInput"
            class="term-input-inline"
            spellcheck="false"
            autocomplete="off"
            @keydown.enter="sendCommand"
            @keydown.up.prevent="historyUp"
            @keydown.down.prevent="historyDown"
            @focus="onInputFocus"
            @blur="onInputBlur"
          />
        </div>
      </div>
    </div>

    <!-- 右键菜单 -->
    <div v-show="showContextMenu" class="context-menu" :style="{ left: ctxPos.x + 'px', top: ctxPos.y + 'px' }" @mousedown.stop>
      <div class="context-item" @click="copySelection">📋 复制</div>
      <div class="context-item" @click="pasteClipboard">📌 粘贴</div>
      <div class="context-divider"></div>
      <div class="context-item" @click="selectAll">📑 全选</div>
      <div class="context-item" @click="clearScreen">🗑️ 清屏</div>
    </div>

    <div class="resize-handle resize-e" @mousedown.stop="startResize($event, 'e')"></div>
    <div class="resize-handle resize-s" @mousedown.stop="startResize($event, 's')"></div>
    <div class="resize-handle resize-se" @mousedown.stop="startResize($event, 'se')"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { StartTerminal, WriteToTerminal, StopTerminal, SetTerminalEncoding } from '../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../wailsjs/runtime'
import { useDraggable } from '../composables/useDraggable'

const { t, locale } = useI18n()
const props = defineProps<{ logs?: string[] }>()
const emit = defineEmits(['close', 'minimize'])

let tabCounter = 0

function calcDefaultPosition() {
  const appW = window.innerWidth || 1280
  const appH = window.innerHeight || 720
  const termW = Math.floor(appW * 0.55)
  const termH = Math.floor(appH * 0.5)
  const termX = Math.floor((appW - termW) / 2)
  const termY = Math.floor((appH - termH) / 2) + 30
  return { x: Math.max(10, termX), y: Math.max(50, termY), w: termW, h: termH }
}

const { windowPos, windowSize, startDrag, startResize } = useDraggable(
  calcDefaultPosition().x,
  calcDefaultPosition().y,
  calcDefaultPosition().w,
  calcDefaultPosition().h
)

interface SessionData {
  id: string
  lines: string[]
  history: string[]
  historyIndex: number
}

const inputRef = ref<HTMLInputElement>()
const outputRef = ref<HTMLElement>()
const currentInput = ref('')
const promptText = ref('')
const isFocused = ref(false)
const minimized = ref(false)
const showContextMenu = ref(false)
const ctxPos = ref({ x: 0, y: 0 })
let savedSize = { w: 0, h: 0 }
const defaultEncoding = locale.value.startsWith('zh') ? 'gbk' : 'utf-8'
const encoding = ref(defaultEncoding)
let autoScrollEnabled = true
let lastCommand = ''

let outputBuffer = ''
let bufferTimer: ReturnType<typeof setTimeout> | null = null

const tabs = reactive<Array<{id: string, name: string}>>([])
const activeTabId = ref('')
const sessionData = reactive<Record<string, SessionData>>({})

const currentSession = computed(() => sessionData[activeTabId.value])
const currentTabLines = computed(() => currentSession.value?.lines || [])

function createSessionData(id: string): SessionData {
  return { id, lines: [], history: [], historyIndex: -1 }
}

function clearScreen() {
  const data = currentSession.value
  if (data) data.lines = []
}

function addLine(text: string) {
  if (!activeTabId.value) return
  const data = sessionData[activeTabId.value]
  if (!data) return
  data.lines.push(text)
  nextTick(() => {
    if (outputRef.value && autoScrollEnabled) {
      outputRef.value.scrollTop = outputRef.value.scrollHeight
    }
  })
}

function handleOutput(data: string) {
  outputBuffer += data
  if (bufferTimer) clearTimeout(bufferTimer)
  bufferTimer = setTimeout(flushBuffer, 30)
}

function flushBuffer() {
  if (!outputBuffer) return
  const text = outputBuffer
  outputBuffer = ''
  
  const lines = text.split('\n')
  for (const raw of lines) {
    if (!raw) continue
    const trimmed = raw.replace(/\r/g, '').trim()
    const promptMatch = trimmed.match(/^PS\s+(.+?)>\s*$/)
    if (promptMatch) {
      promptText.value = `PS ${promptMatch[1]}>`
      continue
    }
    const formatted = formatOutput(raw)
    if (formatted && formatted !== escapeHtml(lastCommand)) addLine(formatted)
  }
}

function formatOutput(raw: string): string {
  let text = raw.replace(/\r/g, '').trim()
  if (!text) return ''
  text = escapeHtml(text)
  text = text.replace(/\x1b\[32m/g, '<span style="color:#0dbc79">')
    .replace(/\x1b\[31m/g, '<span style="color:#cd3131">')
    .replace(/\x1b\[33m/g, '<span style="color:#e5c07b">')
    .replace(/\x1b\[34m/g, '<span style="color:#61afef">')
    .replace(/\x1b\[36m/g, '<span style="color:#56b6c2">')
    .replace(/\x1b\[90m/g, '<span style="color:#808080">')
    .replace(/\x1b\[1m/g, '<span style="font-weight:bold">')
    .replace(/\x1b\[0m/g, '</span>')
    .replace(/\x1b\[[0-9;]*m/g, '')
  return text
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

async function initFirstTab() {
  await StartTerminal('')
  const tabId = `tab-${++tabCounter}`
  tabs.push({ id: tabId, name: `Terminal ${tabCounter}` })
  activeTabId.value = tabId
  sessionData[tabId] = createSessionData(tabId)
}

async function addTab() {
  const tabId = `tab-${++tabCounter}`
  tabs.push({ id: tabId, name: `Terminal ${tabCounter}` })
  sessionData[tabId] = createSessionData(tabId)
  activeTabId.value = tabId
  currentInput.value = ''
  nextTick(() => inputRef.value?.focus())
}

function switchTab(tabId: string) {
  if (tabId === activeTabId.value) return
  activeTabId.value = tabId
  currentInput.value = ''
  nextTick(() => inputRef.value?.focus())
}

function closeTab(tabId: string) {
  if (tabs.length <= 1) {
    emit('close')
    return
  }
  const idx = tabs.findIndex(t => t.id === tabId)
  tabs.splice(idx, 1)
  delete sessionData[tabId]
  if (activeTabId.value === tabId && tabs.length > 0) {
    switchTab(tabs[0].id)
  }
}

async function sendCommand() {
  const cmd = currentInput.value.trim()
  if (!cmd) {
    WriteToTerminal('\r\n').catch(() => {})
    currentInput.value = ''
    return
  }
  if (cmd.toLowerCase() === 'cls' || cmd.toLowerCase() === 'clear') {
    clearScreen()
    currentInput.value = ''
    return
  }
  const data = currentSession.value
  if (data) {
    data.history.push(cmd)
    data.historyIndex = data.history.length
  }
  addLine(`<span style="color:#667eea;font-weight:bold">${escapeHtml(promptText.value || '>')}</span>${escapeHtml(cmd)}`)
  lastCommand = cmd
  currentInput.value = ''
  autoScrollEnabled = true
  try {
    await WriteToTerminal(cmd + '\r\n')
  } catch (e) {
    addLine(`<span style="color:#cd3131">✗ 错误: ${(e as Error).message}</span>`)
  }
  nextTick(() => inputRef.value?.focus())
}

function historyUp() {
  const data = currentSession.value
  if (!data || data.history.length === 0) return
  if (data.historyIndex > 0) {
    data.historyIndex--
    currentInput.value = data.history[data.historyIndex]
  }
}

function historyDown() {
  const data = currentSession.value
  if (!data) return
  if (data.historyIndex < data.history.length - 1) {
    data.historyIndex++
    currentInput.value = data.history[data.historyIndex]
  } else {
    data.historyIndex = data.history.length
    currentInput.value = ''
  }
}

let selectionActive = false

function onOutputMouseDown() {
  const sel = window.getSelection()
  if (sel) sel.removeAllRanges()
  selectionActive = false
}

function onOutputMouseUp() {
  const sel = window.getSelection()
  if (sel && sel.toString()) {
    selectionActive = true
  }
}

function onOutputScroll() {
  if (!outputRef.value) return
  const { scrollTop, scrollHeight, clientHeight } = outputRef.value
  const distanceFromBottom = scrollHeight - scrollTop - clientHeight
  autoScrollEnabled = distanceFromBottom < 50
}

function focusInput() {
  if (!selectionActive) {
    inputRef.value?.focus()
  }
  selectionActive = false
}

function onInputFocus() {
  isFocused.value = true
}

function onInputBlur() {
  isFocused.value = false
}

function minimize() {
  emit('minimize')
}

function maximize() {
  if (windowSize.w === window.innerWidth && windowSize.h === window.innerHeight) {
    windowSize.w = savedSize.w || 700
    windowSize.h = savedSize.h || 400
    windowPos.x = Math.max(10, (window.innerWidth - windowSize.w) / 2)
    windowPos.y = Math.max(50, (window.innerHeight - windowSize.h) / 2 + 30)
  } else {
    savedSize.w = windowSize.w
    savedSize.h = windowSize.h
    windowPos.x = 10
    windowPos.y = 10
    windowSize.w = window.innerWidth - 20
    windowSize.h = window.innerHeight - 20
  }
}

function onContextMenu(e: MouseEvent) {
  e.preventDefault()
  ctxPos.value = { x: e.clientX, y: e.clientY }
  showContextMenu.value = true
}

function closeContextMenu() {
  showContextMenu.value = false
}

function copySelection() {
  const sel = window.getSelection()
  if (sel) {
    navigator.clipboard.writeText(sel.toString()).catch(() => {})
  }
  closeContextMenu()
}

async function pasteClipboard() {
  try {
    const text = await navigator.clipboard.readText()
    currentInput.value = text
    nextTick(() => inputRef.value?.focus())
  } catch (_) {}
  closeContextMenu()
}

function selectAll() {
  const sel = window.getSelection()
  if (!sel || !outputRef.value) return
  const range = document.createRange()
  range.selectNodeContents(outputRef.value)
  sel.removeAllRanges()
  sel.addRange(range)
  closeContextMenu()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'c' && (e.ctrlKey || e.metaKey)) {
    const sel = window.getSelection()
    if (sel && sel.toString()) {
      navigator.clipboard.writeText(sel.toString()).catch(() => {})
      e.preventDefault()
    }
    return
  }
  if (e.key === 'v' && (e.ctrlKey || e.metaKey)) {
    if (document.activeElement === inputRef.value) {
      pasteClipboard()
      e.preventDefault()
    }
    return
  }
}

onMounted(async () => {
  minimized.value = false
  EventsOn('terminal:output', handleOutput)
  await initFirstTab()
  nextTick(() => inputRef.value?.focus())
  document.addEventListener('mousedown', closeContextMenu)
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  EventsOff('terminal:output')
  document.removeEventListener('mousedown', closeContextMenu)
  document.removeEventListener('keydown', onKeydown)
})

watch(minimized, (val) => {
  if (!val) nextTick(() => inputRef.value?.focus())
})

watch(encoding, (newEnc) => {
  if (tabs.length > 0) {
    SetTerminalEncoding(newEnc).catch(() => {})
  }
})

watch(encoding, (newEnc) => {
  if (tabs.length > 0) {
    SetTerminalEncoding(newEnc).catch(() => {})
  }
})

onUnmounted(() => {
  EventsOff('terminal:output')
  document.removeEventListener('mousedown', closeContextMenu)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<style scoped>
.floating-window {
  position: fixed;
  background: #1e1e1e;
  border: 1px solid #333;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
  z-index: 100;
}

.top-bar {
  display: flex;
  align-items: center;
  padding: 6px 10px;
  background: #007acc;
  border-radius: 8px 8px 0 0;
  cursor: move;
  user-select: none;
  gap: 4px;
}

.tabs-row {
  display: flex;
  flex: 1;
  overflow-x: auto;
  gap: 2px;
}

.top-right {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
  flex-shrink: 0;
}

.tab-item {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 12px;
  font-size: 12px;
  color: rgba(255,255,255,0.7);
  cursor: pointer;
  border-radius: 4px;
  white-space: nowrap;
  transition: all 0.15s;
}
.tab-item:hover { background: rgba(255,255,255,0.15); color: #fff; }
.tab-item.active { background: rgba(255,255,255,0.25); color: #fff; font-weight: 500; }
.tab-name { font-size: 12px; }
.tab-close {
  background: none;
  border: none;
  color: rgba(255,255,255,0.5);
  cursor: pointer;
  font-size: 11px;
  padding: 0 2px;
}
.tab-close:hover { color: #ff6b6b; }

.action-btn {
  width: 22px;
  height: 22px;
  border: none;
  background: rgba(255,255,255,0.12);
  color: #fff;
  cursor: pointer;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
}
.action-btn:hover { background: rgba(255,255,255,0.25); }

.enc-select {
  height: 22px;
  border: none;
  background: rgba(255,255,255,0.12);
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  border-radius: 4px;
  padding: 0 6px;
}
.enc-select option { background: #2d2d2d; color: #fff; }

.close-btn {
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  color: rgba(255,255,255,0.7);
  cursor: pointer;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
}
.close-btn:hover { background: #e81123; color: #fff; }

.win-btn {
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  color: rgba(255,255,255,0.6);
  cursor: pointer;
  font-size: 13px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
}
.win-btn:hover { background: rgba(255,255,255,0.15); color: #fff; }

.term-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.output-area {
  flex: 1;
  overflow-y: auto;
  padding: 8px 14px;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.55;
  background: #1e1e1e;
  cursor: text;
  user-select: text;
}

.output-line {
  white-space: pre-wrap;
  word-break: break-all;
  color: #d4d4d4;
  min-height: 20px;
  user-select: text;
}

.current-input-line {
  display: flex;
  align-items: center;
  min-height: 20px;
  margin-top: 2px;
}

.prompt-text {
  color: #667eea;
  font-weight: bold;
  margin-right: 6px;
  white-space: nowrap;
  flex-shrink: 0;
}

.term-input-inline {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  color: #d4d4d4;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  caret-color: #fff;
  line-height: inherit;
  min-width: 50px;
}

.resize-handle {
  position: absolute;
  z-index: 10;
}
.resize-e { right: 0; top: 34px; bottom: 0; width: 6px; cursor: ew-resize; }
.resize-e:hover { background: rgba(74, 158, 255, 0.3); }
.resize-s { 
  bottom: 0; 
  left: 0; 
  right: 0; 
  height: 8px; 
  cursor: ns-resize; 
}
.resize-s::after {
  content: '';
  position: absolute;
  left: 50%;
  top: 2px;
  transform: translateX(-50%);
  width: 60px;
  height: 3px;
  border-radius: 2px;
  background: rgba(255,255,255,0.15);
  transition: background 0.2s;
}
.resize-s:hover::after { background: rgba(74, 158, 255, 0.5); }
.resize-se { right: 0; bottom: 0; width: 20px; height: 20px; cursor: nwse-resize; border-radius: 0 0 8px 0; }
.resize-se:hover { background: rgba(74, 158, 255, 0.3); }

/* 右键菜单 */
.context-menu {
  position: fixed;
  background: #2d2d2d;
  border: 1px solid #444;
  border-radius: 6px;
  padding: 4px 0;
  min-width: 130px;
  z-index: 1000;
  box-shadow: 0 4px 16px rgba(0,0,0,0.5);
}
.context-item {
  padding: 6px 16px;
  font-size: 12px;
  color: #d4d4d4;
  cursor: pointer;
  white-space: nowrap;
}
.context-item:hover { background: #094771; }
.context-divider {
  height: 1px;
  background: #444;
  margin: 4px 8px;
}
</style>
