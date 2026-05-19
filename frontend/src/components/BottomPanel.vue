<template>
  <div class="bottom-panel" ref="panelRef" :style="{ height: panelHeight + 'px' }">
    <div class="resize-handle-bar" @mousedown.stop="startPanelResize"></div>
    <div class="panel-tabs">
      <div 
        class="panel-tab"
        :class="{ active: activePanel === 'terminal' }"
        @click="$emit('panel-change', 'terminal')"
      >
        <span class="tab-icon">⌨️</span>
        {{ t('terminal.title') }}
      </div>
      <div 
        class="panel-tab"
        :class="{ active: activePanel === 'ai' }"
        @click="$emit('panel-change', 'ai')"
      >
        <span class="tab-icon">🤖</span>
        AI
      </div>
      <div 
        class="panel-tab"
        :class="{ active: activePanel === 'problems' }"
        @click="$emit('panel-change', 'problems')"
      >
        <span class="tab-icon">⚠️</span>
        {{ t('preview.terminalOutput') }}
        <span v-if="errorCount > 0" class="badge error">{{ errorCount }}</span>
        <span v-if="warningCount > 0" class="badge warning">{{ warningCount }}</span>
      </div>
    </div>

    <div class="panel-content">
      <div v-if="activePanel === 'terminal'" class="terminal-panel">
        <XtermTerminal
          ref="xtermRef"
          title="Argus Terminal"
          :show-toolbar="true"
          theme="dark"
          @data="handleTerminalInput"
          @resize="handleTerminalResize"
          @command="handleHistoryCommand"
        />
      </div>

      <div v-if="activePanel === 'ai'" class="ai-panel">
        <div class="ai-messages" ref="aiRef">
          <div class="ai-message ai">
            <div class="avatar">🤖</div>
            <div class="message-content">
              <div class="sender">{{ t('aiMonitor.aiAssistant') }}</div>
              <div class="text">{{ t('terminal.started') }}</div>
            </div>
          </div>
          <div v-for="(msg, index) in aiMessages" :key="index" :class="['ai-message', msg.role]">
            <div class="avatar">{{ msg.role === 'user' ? '👤' : '🤖' }}</div>
            <div class="message-content">
              <div class="sender">{{ msg.role === 'user' ? '你' : 'AI 助手' }}</div>
              <div class="text" v-html="renderMarkdown(msg.content)"></div>
            </div>
          </div>
        </div>
        <div class="ai-input-area">
          <input 
            type="text" 
            class="ai-chat-input"
            v-model="aiChatInput"
            @keyup.enter="submitAI"
            placeholder="输入消息与 AI 对话..."
          />
          <button class="btn btn-primary" @click="submitAI">发送</button>
        </div>
      </div>

      <div v-if="activePanel === 'problems'" class="problems-panel">
        <div class="problem-item" v-for="(prob, idx) in problems" :key="idx">
          <span class="problem-icon" :class="prob.severity">{{ prob.icon }}</span>
          <span class="problem-file" @click="goToFile(prob.file, prob.line)">{{ prob.file }}:{{ prob.line }}</span>
          <span class="problem-msg">{{ prob.message }}</span>
        </div>
        <div v-if="problems.length === 0" class="no-problems">
          ✅ {{ t('review.noChanges') }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import XtermTerminal from './XtermTerminal.vue'
import { StartTerminal, WriteToTerminal, StopTerminal, IsTerminalRunning } from '../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../wailsjs/runtime'

const { t } = useI18n()

const props = defineProps<{
  activePanel: string
  terminalOutput?: string
  problems?: Array<{severity: string, file: string, line: number, message: string, icon: string}>
}>()

const emit = defineEmits(['panel-change', 'terminal-input', 'go-to-file'])

const xtermRef = ref<InstanceType<typeof XtermTerminal>>()
const aiChatInput = ref('')
const aiMessages = ref<Array<{role: string, content: string}>>([])
const panelRef = ref<HTMLElement>()
const panelHeight = ref(220)
let panelResizing = false
let resizeStartY = 0
let resizeStartH = 0

function startPanelResize(e: MouseEvent) {
  panelResizing = true
  resizeStartY = e.clientY
  resizeStartH = panelHeight.value
  e.preventDefault()
}

function onPanelMove(e: MouseEvent) {
  if (!panelResizing) return
  const deltaY = resizeStartY - e.clientY
  panelHeight.value = Math.max(100, Math.min(800, resizeStartH + deltaY))
  document.body.style.cursor = 'ns-resize'
}

function stopPanelResize() {
  if (panelResizing) {
    panelResizing = false
    document.body.style.cursor = ''
  }
}

onMounted(async () => {
  document.addEventListener('mousemove', onPanelMove)
  document.addEventListener('mouseup', stopPanelResize)
  EventsOn('terminal:output', handleTerminalOutput)
const aiRef = ref<HTMLElement>()
const outputLines = ref<Array<{text: string, type: string}>>([])
let currentBuffer = ''

const errorCount = computed(() => (props.problems || []).filter(p => p.severity === 'error').length)
const warningCount = computed(() => (props.problems || []).filter(p => p.severity === 'warning').length)

onMounted(async () => {
  EventsOn('terminal:output', handleTerminalOutput)

  try {
    const running = await IsTerminalRunning()
    if (!running) {
      await StartTerminal('')
    }
  } catch (e) {
    console.error('终端启动失败:', e)
  }
})

onUnmounted(() => {
  EventsOff('terminal:output')
  document.removeEventListener('mousemove', onPanelMove)
  document.removeEventListener('mouseup', stopPanelResize)
})

function handleTerminalOutput(data: string) {
  currentBuffer += data
  
  const lines = currentBuffer.split('\n')
  currentBuffer = lines.pop() || ''
  
  for (const line of lines) {
    const cleanLine = line.replace(/\r/g, '')
    if (cleanLine.trim()) {
      xtermRef.value?.writeColorful(cleanLine)
    }
  }

  outputLines.value.push(...lines.filter(l => l.trim()).map(l => ({
    text: l.replace(/\r/g, ''),
    type: detectLogType(l)
  })))

  if (outputLines.value.length > 5000) {
    outputLines.value = outputLines.value.slice(-3000)
  }
}

function detectLogType(line: string): string {
  const lower = line.toLowerCase()
  if (lower.includes('[error]') || lower.includes('[err]') || lower.includes('fatal')) return 'error'
  if (lower.includes('[warn]') || lower.includes('[warning]')) return 'warn'
  if (lower.includes('[info]') || lower.includes('[debug]') || lower.includes('success')) return 'info'
  return 'default'
}

function getOutputStyle(line: {type: string}) {
  const colors: Record<string, string> = {
    error: '#cd3131',
    warn: '#e5c07b',
    info: '#0dbc79',
    default: '#d4d4d4'
  }
  return { color: colors[line.type] || colors.default }
}

function handleTerminalInput(data: string) {
  xtermRef.value?.addToHistory(data)
  
  if (data === '\r' || data === '\n') {
    emit('terminal-input', currentCommand)
    currentCommand = ''
  } else if (data === '\x7f') {
    currentCommand = currentCommand.slice(0, -1)
  } else if (data.charCodeAt(0) >= 32) {
    currentCommand += data
  } else {
    WriteToTerminal(data).catch(() => {})
  }
}

let currentCommand = ''

async function executeCommand(cmd: string) {
  try {
    xtermRef.value?.writeln(`\r\n\x1b[90m> ${cmd}\x1b[0m`)
    await WriteToTerminal(cmd + '\r\n')
  } catch (e: any) {
    xtermRef.value?.writeln(`\r\n\x1b[31m✗ 执行失败: ${e.message}\x1b[0m`)
  }
}

function handleHistoryCommand(cmd: string) {
  currentCommand = cmd
}

function handleTerminalResize({ cols, rows }: {cols: number, rows: number}) {
}

function submitAI() {
  if (!aiChatInput.value.trim()) return

  aiMessages.value.push({ role: 'user', content: aiChatInput.value })
  const userMsg = aiChatInput.value
  aiChatInput.value = ''

  setTimeout(() => {
    aiMessages.value.push({
      role: 'ai',
      content: `收到你的消息: "${userMsg}"。我正在处理中...`
    })
    scrollToBottom()
  }, 500)

  scrollToBottom()
}

function scrollToBottom() {
  nextTick(() => {
    if (aiRef.value) {
      aiRef.value.scrollTop = aiRef.value.scrollHeight
    }
  })
}

function renderMarkdown(text: string): string {
  let html = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')

  html = html.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/`(.*?)`/g, '<code>$1</code>')
  html = html.replace(/\n/g, '<br>')

  return html
}

function goToFile(file: string, line: number) {
  emit('go-to-file', file, line)
}
</script>

<style scoped>
.bottom-panel {
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
  position: relative;
}

.resize-handle-bar {
  position: absolute;
  top: -5px;
  left: 0;
  right: 0;
  height: 10px;
  cursor: ns-resize;
  z-index: 10;
}
.resize-handle-bar::after {
  content: '';
  position: absolute;
  left: 50%;
  top: 3px;
  transform: translateX(-50%);
  width: 80px;
  height: 4px;
  border-radius: 2px;
  background: rgba(255,255,255,0.2);
  transition: background 0.2s;
}
.resize-handle-bar:hover::after { background: rgba(74, 158, 255, 0.6); }

.panel-tabs {
  display: flex;
  background: var(--bg-tertiary);
  border-bottom: 1px solid var(--border-color);
  user-select: none;
}

.panel-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  font-size: 12px;
  cursor: pointer;
  border-right: 1px solid var(--border-color);
  position: relative;
  transition: all 0.15s;
}

.panel-tab:hover {
  background: var(--bg-secondary);
}

.panel-tab.active {
  background: var(--bg-secondary);
  border-bottom: 2px solid var(--accent-color);
}

.tab-icon {
  font-size: 14px;
}

.badge {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 8px;
  margin-left: 4px;
  font-weight: 600;
}

.badge.error {
  background: #cd3131;
  color: #fff;
}

.badge.warning {
  background: #e5c07b;
  color: #000;
}

.panel-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.terminal-panel {
  height: 100%;
  overflow: hidden;
}

.ai-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.ai-messages {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 12px;
}

.ai-message {
  display: flex;
  gap: 12px;
}

.ai-message .avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--bg-tertiary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  flex-shrink: 0;
}

.ai-message .message-content {
  flex: 1;
  background: var(--bg-tertiary);
  padding: 10px 14px;
  border-radius: 12px;
  border-top-left-radius: 4px;
}

.ai-message.user {
  flex-direction: row-reverse;
}

.ai-message.user .message-content {
  background: var(--accent-color);
  border-top-left-radius: 12px;
  border-top-right-radius: 4px;
  color: #fff;
}

.ai-message .sender {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.ai-message.user .sender {
  color: rgba(255, 255, 255, 0.8);
}

.ai-message .text {
  font-size: 13px;
  line-height: 1.5;
}

.ai-message .text :deep(code) {
  background: rgba(0, 0, 0, 0.2);
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
}

.ai-input-area {
  display: flex;
  gap: 8px;
  padding: 12px;
  border-top: 1px solid var(--border-color);
  background: var(--bg-secondary);
}

.ai-chat-input {
  flex: 1;
  padding: 8px 14px;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: border-color 0.2s;
}

.ai-chat-input:focus {
  border-color: var(--accent-color);
}

.btn-primary {
  padding: 8px 20px;
  background: var(--accent-color);
  color: #fff;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-weight: 500;
  transition: opacity 0.2s;
}

.btn-primary:hover {
  opacity: 0.9;
}

.output-panel {
  height: 100%;
  overflow: auto;
  padding: 8px;
}

.output-content {
  margin: 0;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

.output-content span {
  display: block;
}

.problems-panel {
  height: 100%;
  overflow: auto;
  padding: 8px;
}

.problem-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  background: var(--bg-tertiary);
  border-radius: 6px;
  margin-bottom: 6px;
  font-size: 13px;
  transition: background 0.15s;
}

.problem-item:hover {
  background: rgba(255, 255, 255, 0.05);
}

.problem-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.problem-icon.error {
  color: #cd3131;
}

.problem-icon.warning {
  color: #e5c07b;
}

.problem-icon.info {
  color: #61afef;
}

.problem-file {
  color: var(--accent-color);
  font-family: 'Consolas', monospace;
  cursor: pointer;
  flex-shrink: 0;
}

.problem-file:hover {
  text-decoration: underline;
}

.problem-msg {
  color: var(--text-secondary);
  flex: 1;
}

.no-problems {
  text-align: center;
  padding: 40px 20px;
  color: var(--text-secondary);
  font-size: 14px;
}
</style>