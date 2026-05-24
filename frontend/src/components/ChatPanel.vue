<template>
  <div class="chat-panel">
    <!-- 搜索栏 -->
    <div v-if="showSearch" class="search-bar">
      <div class="search-input-wrapper">
        <span class="search-icon">🔍</span>
        <input 
          ref="searchInputRef"
          v-model="searchQuery" 
          class="search-input"
          :placeholder="t('chatPanel.searchPlaceholder')"
          @input="handleSearch"
          @keydown.escape="showSearch = false; searchQuery = ''"
          @keydown.enter="jumpToNextMatch"
        />
        <span class="search-count" v-if="searchMatches > 0">{{ searchCurrentIndex + 1 }}/{{ searchMatches }}</span>
        <button class="search-nav-btn" @click="jumpToPrevMatch" :disabled="searchMatches === 0">▲</button>
        <button class="search-nav-btn" @click="jumpToNextMatch" :disabled="searchMatches === 0">▼</button>
        <button class="search-close-btn" @click="showSearch = false; searchQuery = ''">×</button>
      </div>
    </div>

    <div class="messages" ref="messagesRef">
      <div 
        v-for="(msg, index) in filteredMessages" 
        :key="'msg-' + index"
        :id="'msg-' + index"
        :class="['message', msg.role, { 'search-highlight': isSearchMatch(index), streaming: (msg as any)._streaming }]"
        @contextmenu.prevent="showContextMenu($event, msg, index)"
      >
        <!-- 时间戳 -->
        <div class="msg-timestamp" :title="formatFullTime(msg.timestamp)">
          {{ formatTime(msg.timestamp) }}
        </div>

        <!-- 用户消息 -->
        <template v-if="msg.role === 'user'">
          <div class="message-header">
            <span class="role-badge usr">USR</span>
          </div>
          <div class="message-content user-content">
            {{ msg.raw || msg.content }}
          </div>
          <div class="message-actions-bar">
            <button class="action-icon-btn" @click="copyMessage(msg.raw || msg.content)" :title="t('chatPanel.copyMessage') + ' (Ctrl+C)'">📋</button>
            <button class="action-icon-btn" @click="quoteMessage(msg.raw || msg.content, index)" :title="t('chatPanel.quoteMessage')">💬</button>
          </div>
        </template>

        <!-- MC / C监控消息 -->
        <template v-else-if="msg.role === 'mc' || msg.role?.startsWith('Sys_')">
          <div class="message-header">
            <span class="role-badge mc">{{ msg.role === 'mc' ? 'Argus-MC' : 'SYS' }}</span>
          </div>
          <div class="message-content mc-content">
            {{ msg.content }}
          </div>
        </template>

        <!-- PM 消息 - 结构化渲染 -->
        <template v-else-if="msg.role === 'pm'">
          <div class="message-header">
            <span class="role-badge pm">PM</span>
            <span v-if="getMsgStatus(msg)" class="status-tag" :class="getMsgStatus(msg).type">{{ getMsgStatus(msg).text }}</span>
          </div>
          <div class="message-content pm-content structured-msg">
            <!-- 三层模型 RichMessage -->
            <RichMessage v-if="getRichMessage(msg)" :message="getRichMessage(msg)!" />
            <div v-else>
              <div v-if="getSummary(msg)" class="msg-summary" @click="toggleExpand(index)">
                <span class="summary-text">{{ getSummary(msg) }}</span>
                <span class="expand-hint">{{ expandedMessages.has(index) ? '收起 ▲' : '展开 ▼' }}</span>
              </div>
              <div v-show="expandedMessages.has(index) || !getSummary(msg)" class="msg-full" v-html="renderStructured(msg)"></div>
              <div v-if="!expandedMessages.has(index) && getSummary(msg)" class="msg-preview" @click="toggleExpand(index)">
                {{ getPreviewText(msg) }}
              </div>
            </div>
          </div>
        </template>

        <!-- SE 消息 - 操作卡片 -->
        <template v-else-if="msg.role === 'se'">
          <div class="message-header">
            <span class="role-badge se">SE</span>
            <span v-if="getMsgStatus(msg)" class="status-tag" :class="getMsgStatus(msg).type">{{ getMsgStatus(msg).text }}</span>
            <span v-if="getSEActionCount(msg)" class="action-count">{{ getSEActionCount(msg) }} 个操作</span>
          </div>
          <div class="message-content se-content structured-msg">
            <SERichMessage
              v-if="getSEActions(msg)"
              :message="msg"
              :actions="getSEActions(msg)"
              :shellOutput="getSEShellOutput(msg)"
              @open-file-in-editor="onOpenFileInEditor"
              @run-in-terminal="onRunInTerminal"
            />
            <RichMessage v-else-if="getRichMessage(msg)" :message="getRichMessage(msg)!" />
            <div v-else class="se-plain" v-html="renderSEText(msg)"></div>
          </div>
        </template>

        <!-- AP 消息 - 审批者 -->
        <template v-else-if="msg.role === 'ap'">
          <div class="message-header">
            <span class="role-badge ap">AP</span>
            <span v-if="getMsgStatus(msg)" class="status-tag" :class="getMsgStatus(msg).type">{{ getMsgStatus(msg).text }}</span>
          </div>
          <div class="message-content ap-content structured-msg">
            <RichMessage v-if="getRichMessage(msg)" :message="getRichMessage(msg)!" />
            <div v-else>
            <div v-if="getSummary(msg)" class="msg-summary" @click="toggleExpand(index)">
              <span class="summary-text">{{ getSummary(msg) }}</span>
              <span class="expand-hint">{{ expandedMessages.has(index) ? '收起 ▲' : '展开 ▼' }}</span>
            </div>
            <div v-show="expandedMessages.has(index) || !getSummary(msg)" class="msg-full" v-html="renderStructured(msg)"></div>
            <div v-if="!expandedMessages.has(index) && getSummary(msg)" class="msg-preview" @click="toggleExpand(index)">
              {{ getPreviewText(msg) }}
            </div>
            </div>
          </div>
        </template>

        <!-- 错误消息 -->
        <template v-else-if="msg.role === 'error'">
          <div class="message-header">
            <span class="role-badge err">ERR</span>
          </div>
          <div class="message-content error-content">
            {{ msg.error || msg.content }}
          </div>
        </template>

        <!-- 执行输出面板（通用：SE/PM均可）—— 必须接在角色链上，禁止独立 v-if -->
        <template v-else-if="(msg as any)._execData && !getRichMessage(msg)">
          <div class="message-header">
            <span class="role-badge" :class="(msg as any)._execData?.executor === 'pm' ? 'pm' : 'se'">
              {{ (msg as any)._execData?.executor === 'pm' ? '⚡ PM' : '⚡ SE' }}
            </span>
            <span class="action-count">{{ (msg as any)._execData?.actions?.length || 0 }}/{{ (msg as any)._execData?.totalActions || 0 }} 操作</span>
            <span v-if="(msg as any)._streaming" class="status-tag running">🔄 执行中...</span>
            <span v-else class="status-tag success">✅ 完成</span>
          </div>
          <div class="se-exec-panel">
            <div class="se-steps">
              <div v-for="step in (msg as any)._execData?.actions" :key="step.index" class="se-step" :class="step.status">
                <span class="step-icon">{{ step.status === 'done' ? '✅' : step.status === 'running' ? '🔄' : step.status === 'error' ? '❌' : '🚫' }}</span>
                <span class="step-label">{{ step.label }}</span>
                <span v-if="step.error" class="step-error">{{ step.error }}</span>
              </div>
            </div>
            <div v-if="(msg as any)._execData?.outputs?.length" class="se-terminal" :class="{ expanded: expandedMessages.has(index) }">
              <div class="terminal-header" @click="toggleExpand(index)">
                <span>🖥️ 终端输出 ({{ (msg as any)._execData.outputs.length }})</span>
                <span class="expand-hint">{{ expandedMessages.has(index) ? '收起 ▲' : '展开 ▼' }}</span>
              </div>
              <div v-show="expandedMessages.has(index)" class="terminal-body">
                <pre v-for="(out, oi) in (msg as any)._execData.outputs" :key="oi" class="terminal-output"><code><span v-if="out.command" class="cmd-prompt">$ {{ out.command }}</span>{{ out.output }}</code></pre>
              </div>
            </div>
          </div>
          <div class="message-actions-bar">
            <button class="action-icon-btn" @click="copyMessage(getExecContent(msg))" :title="'复制输出'">📋</button>
            <button class="action-icon-btn" @click="emit('send-to-terminal', getExecContent(msg))" :title="'发送到终端'">💻</button>
          </div>
        </template>

        <!-- 其他未捕获的消息（仅渲染已知role之外的fallback） -->
        <template v-else-if="!['user','pm','se','ap','mc','error'].includes(msg.role || '') && !(msg.role || '').startsWith('Sys_')">
          <div class="message-header">
            <span class="role-badge" :class="getRoleClass(msg.role)">{{ getRoleDisplayName(msg.role) }}</span>
          </div>
          <div class="message-content ai-content">
            <div v-if="msg.changes && msg.changes.length > 0" class="changes-list">
              <div v-for="(change, idx) in msg.changes" :key="idx" class="change-item">
                <span class="change-icon">{{ getChangeIcon(change.type) }}</span>
                <span class="change-file">{{ change.file }}</span>
              </div>
            </div>
            <div v-if="msg.description" class="description">{{ msg.description }}</div>
            <div v-if="msg.content" class="content" v-html="formatContent(msg.content)"></div>
            <div v-if="msg.codeBlocks && msg.codeBlocks.length > 0" class="code-blocks">
              <div v-for="(block, idx) in msg.codeBlocks" :key="idx" class="code-block">
                <div class="code-header">
                  <span class="code-lang">{{ block.language || 'code' }}</span>
                  <button class="copy-btn" @click="copyCode(block.code)">复制</button>
                </div>
                <pre class="code-content"><code v-html="highlightCode(block.code, block.language)"></code></pre>
              </div>
            </div>
            <div v-if="msg.error" class="error-message">
              <span class="error-icon">❌</span>
              <span>{{ msg.error }}</span>
            </div>
            <div v-if="msg.changes && msg.changes.length > 0" class="message-actions">
              <button class="action-btn" @click="viewDetails(index)">{{ t('chatPanel.viewDetails') }}</button>
              <button class="action-btn" @click="openInEditor(index)">{{ t('chatPanel.openInEditor') }}</button>
              <button class="action-btn" @click="modify(index)">{{ t('chatPanel.modify') }}</button>
            </div>
          </div>
          <div class="message-actions-bar">
            <button class="action-icon-btn" @click="copyMessage(getFullMessageContent(msg))" :title="t('chatPanel.copyMessage') + ' (Ctrl+C)'">📋</button>
            <button class="action-icon-btn" @click="quoteMessage(getFullMessageContent(msg), index)" :title="t('chatPanel.quoteMessage')">💬</button>
          </div>
        </template>
      </div>
    </div>

    <!-- 右键菜单 -->
    <div v-if="contextMenu.visible" class="context-menu" :style="{ top: contextMenu.y + 'px', left: contextMenu.x + 'px' }">
      <div class="context-item" @click="contextAction('copy')">
        <span>📋</span><span>{{ t('chatPanel.copyMessage') }}</span>
      </div>
      <div class="context-item" @click="contextAction('quote')">
        <span>💬</span><span>{{ t('chatPanel.quoteMessage') }}</span>
      </div>
      <div class="context-divider"></div>
      <div class="context-item" @click="contextAction('delete')">
        <span>🗑️</span><span>{{ t('chatPanel.deleteMessage') }}</span>
      </div>
      <div class="context-item danger" @click="contextAction('deleteAbove')">
        <span>⬆️</span><span>{{ t('chatPanel.deleteAbove') }}</span>
      </div>
    </div>

    <!-- 待发送消息队列 -->
    <div v-if="pendingQueue.length > 0" class="pending-queue">
      <div class="pending-header">
        <span>📨 {{ t('chatPanel.pendingMessages') }} ({{ pendingQueue.length }})</span>
        <button class="pending-clear-btn" @click="clearPendingQueue" :title="t('chatPanel.clearAll')">✕</button>
      </div>
      <div class="pending-list">
        <div v-for="(msg, idx) in pendingQueue" :key="idx" class="pending-item">
          <span class="pending-text">{{ msg }}</span>
          <div class="pending-actions">
            <button class="pending-send-btn" @click="sendPendingMessage(idx)" :title="t('chatPanel.sendNow')">➤</button>
            <button class="pending-delete-btn" @click="deletePendingMessage(idx)" :title="t('chatPanel.delete')">✕</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 全局任务栏 (opencode 模式) -->
    <GlobalTaskBar
      :tasks="globalTasks"
      @task-click="handleTaskClick"
      class="global-task-bar-wrapper"
    />

    <!-- 需求澄清（仅PM接收任务时触发） -->
    <TaskClarify
      :visible="clarifyVisible"
      :questions="clarifyQuestions"
      :is-follow-up="clarifyFollowUp"
      @submit="handleClarifySubmit"
      ref="clarifyRef"
    />

    <!-- 输入框 -->
    <div class="input-area">
      <div class="input-wrapper" :class="{ focused: inputFocused }">
        <textarea
          ref="textareaRef"
          v-model="inputMessage"
          class="message-input"
          :style="{ height: textareaHeight + 'px' }"
          :placeholder="t('chatPanel.inputPlaceholder')"
          @keydown.enter.exact.prevent="handleEnterKey($event)"
          @keydown.ctrl.enter.exact="handleSend"
          @keydown.escape="hideMentionMenu"
          @keydown.arrow-down.prevent="handleMentionKeydown($event)"
          @keydown.arrow-up.prevent="handleMentionKeydown($event)"
          @input="onTextareaInput"
          @focus="inputFocused = true"
          @blur="onTextareaBlur"
          rows="1"
        ></textarea>

        <!-- CLI面板 -->
        <CliPanel
          v-if="showCli"
          @command="handleCliCommand"
          @close="showCli = false"
        />

        <div class="input-actions">
          <div class="input-left-actions">
            <select v-model="replyLanguage" class="lang-select" title="AI回复语言" @change="onLanguageChange" @mousedown.stop>
              <option value="auto">🌐 Auto</option>
              <option value="zh">中文</option>
              <option value="en">English</option>
            </select>
            <button type="button" class="input-btn mention-btn" title="@SE" @click="insertMention">@</button>
            <button type="button" class="input-btn" :title="t('chatPanel.uploadFile')" @click="$emit('upload-file')">📎</button>
            <span v-if="props.supportsMultimodal" class="multimodal-badge" title="支持图片识别">🖼️</span>
            <button type="button" class="input-btn search-toggle-btn" title="搜索 (Ctrl+F)" @click="toggleSearch">🔍</button>
            <button type="button" class="input-btn cli-toggle-btn" title="CLI模式 (Ctrl+Shift+T)" @click="toggleCli" :class="{ active: showCli }">⌨️</button>
          </div>
          <div class="input-right-actions">
            <span class="shortcut-hint" v-show="!inputMessage.trim()">{{ t('chatPanel.inputHint') }}</span>
            <button type="button" class="send-btn" @click="handleSend" :title="inputMessage.trim() ? t('chatPanel.sendMessage') : (aiThinking ? t('chatPanel.stopAI') : '')">
              {{ inputMessage.trim() ? '➤' : (aiThinking ? '⏹️' : '⏹️') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- @ Mention Menu -->
    <div v-if="mentionMenu.visible" class="mention-popup" :style="{ left: mentionMenu.x + 'px', top: mentionMenu.y + 'px' }">
      <div
        v-for="(item, idx) in mentionMenu.items"
        :key="item.role"
        :class="['mention-popup-item', { selected: idx === mentionMenu.selectedIndex }]"
        @mousedown.prevent="selectMentionItem(item.role)"
        @mouseenter="mentionMenu.selectedIndex = idx"
      >
        <span class="mention-popup-icon">{{ item.icon }}</span>
        <span class="mention-popup-name">@{{ item.role }}</span>
        <span class="mention-popup-desc">{{ item.desc }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick, watch, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { EventsOn, EventsOff, EventsEmit } from '../../wailsjs/runtime/runtime.js'

const { t } = useI18n()

const API_BASE = 'http://127.0.0.1:8080'

const props = defineProps<{
  messages: Array<{
    role: string
    content: string
    raw?: string
    summary?: string
    description?: string
    changes?: Array<{type: string, file: string}>
    codeBlocks?: Array<{language: string, code: string}>
    error?: string
    timestamp?: number | string
  }>
  aiThinking: boolean
  supportsMultimodal?: boolean
}>()

const emit = defineEmits(['send-message', 'expand-thinking', 'upload-file', 'view-details', 'open-editor', 'modify', 'quote-message', 'open-file-in-editor', 'run-in-terminal'])

const inputMessage = ref('')
const globalTasks = ref<GlobalTask[]>([])

// 前端调试日志函数
const LogPrint = (msg: string) => {
  console.log('[ChatPanel] ' + msg)
}
const showThinking = ref(false)
const currentThinkingStep = ref(0)
const clarifyVisible = ref(false)
const clarifyQuestions = ref<Array<{ text: string; type: string; options?: any[] }>>([])
const clarifyFollowUp = ref(false)
const clarifyRef = ref<any>(null)
const messagesRef = ref<HTMLElement>()
const textareaRef = ref<HTMLTextAreaElement>()
const debugInfo = ref('')
const expandedMessages = ref(new Set<number>())
const inputFocused = ref(false)
const textareaHeight = ref(44)
const replyLanguage = ref('auto')

// 待发送消息队列
const pendingQueue = ref<string[]>([])

async function loadPendingQueue() {
  try {
    const res = await fetch(`${API_BASE}/api/v1/chat/pending`, {
      headers: { 'Authorization': 'Bearer local' }
    })
    const data = await res.json()
    pendingQueue.value = data.messages || []
  } catch (e) {
    console.error('Failed to load pending queue:', e)
  }
}

async function clearPendingQueue() {
  try {
    await fetch(`${API_BASE}/api/v1/chat/pending`, {
      method: 'DELETE',
      headers: { 'Authorization': 'Bearer local' }
    })
    pendingQueue.value = []
  } catch (e) {
    console.error('Failed to clear pending queue:', e)
  }
}

async function sendPendingMessage(_idx: number) {
  try {
    await fetch(`${API_BASE}/api/v1/chat/pending/send`, {
      method: 'POST',
      headers: { 'Authorization': 'Bearer local' }
    })
    setTimeout(loadPendingQueue, 500)
  } catch (e) {
    console.error('Failed to send pending message:', e)
  }
}

async function deletePendingMessage(idx: number) {
  pendingQueue.value.splice(idx, 1)
}

// #1 搜索相关
const showSearch = ref(false)
const searchQuery = ref('')
const searchInputRef = ref<HTMLInputElement>()

// #2 CLI相关
import CliPanel from './CliPanel.vue'
import GlobalTaskBar from './GlobalTaskBar.vue'
import TaskClarify from './TaskClarify.vue'
import SERichMessage from './chat/SERichMessage.vue'
import type { GlobalTask } from '../../types/task'
const showCli = ref(false)
const cliCommand = ref('')

function toggleCli() {
  showCli.value = !showCli.value
  if (showCli.value) {
    nextTick(() => {
      const cliInput = document.querySelector('.cli-input') as HTMLInputElement
      if (cliInput) cliInput.focus()
    })
  }
}

function handleCliCommand(cmd: string) {
  if (cmd.trim()) {
    emit('send-message', cmd)
    cliCommand.value = ''
    showCli.value = false
  }
}
const searchMatches = ref(0)
const searchCurrentIndex = ref(-1)
const matchedIndices = ref<number[]>([])

// #2 AI 思考进度
const currentThinkingText = ref('')
const currentStepIndex = ref(0)
const thinkingDots = ref('.')
let thinkingInterval: ReturnType<typeof setInterval> | null = null
let stepInterval: ReturnType<typeof setInterval> | null = null

// #9 右键菜单
const contextMenu = ref({ visible: false, x: 0, y: 0, msg: null as any, index: -1 })

const mentionMenu = ref({
  visible: false,
  x: 0,
  y: 0,
  atIndex: -1,
  items: [
    { role: 'SE', icon: '👨‍💻', desc: 'Software Engineer' },
    { role: 'PM', icon: '👨‍💼', desc: 'Project Manager' },
    { role: 'AP', icon: '🔍', desc: 'Approver' },
    { role: 'USR', icon: '👤', desc: 'User' },
  ],
  selectedIndex: 0,
})

function showMentionMenu() {
  const textarea = textareaRef.value
  if (!textarea) return
  const rect = textarea.getBoundingClientRect()
  mentionMenu.value.visible = true
  mentionMenu.value.selectedIndex = 0
  mentionMenu.value.x = rect.left + 10
  mentionMenu.value.y = rect.top - 130
  mentionMenu.value.atIndex = textarea.selectionStart
}

function hideMentionMenu() {
  mentionMenu.value.visible = false
}

function selectMentionItem(role: string) {
  const textarea = textareaRef.value
  if (!textarea) return
  const atPos = mentionMenu.value.atIndex
  const value = inputMessage.value
  if (atPos >= 0 && atPos <= value.length && value[atPos - 1] === '@') {
    inputMessage.value = value.slice(0, atPos - 1) + '@' + role + ' ' + value.slice(atPos)
    nextTick(() => {
      const newPos = atPos + role.length + 1
      textarea.setSelectionRange(newPos, newPos)
      textarea.focus()
    })
  }
  hideMentionMenu()
}

function handleMentionKeydown(e: KeyboardEvent) {
  if (!mentionMenu.value.visible) return false
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    mentionMenu.value.selectedIndex = (mentionMenu.value.selectedIndex + 1) % mentionMenu.value.items.length
    return true
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    mentionMenu.value.selectedIndex = (mentionMenu.value.selectedIndex - 1 + mentionMenu.value.items.length) % mentionMenu.value.items.length
    return true
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    const item = mentionMenu.value.items[mentionMenu.value.selectedIndex]
    selectMentionItem(item.role)
    return true
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    hideMentionMenu()
    return true
  }
  return false
}

function onTextareaInput(e: Event) {
  const target = e.target as HTMLTextAreaElement
  target.style.height = 'auto'
  const newHeight = Math.min(target.scrollHeight, 200)
  textareaHeight.value = Math.max(newHeight, 44)

  const cursorPos = target.selectionStart
  const value = target.value
  if (cursorPos > 0 && value[cursorPos - 1] === '@') {
    const before = value.slice(0, cursorPos - 1)
    if (before.length === 0 || /[\s\n]/.test(before[before.length - 1])) {
      showMentionMenu()
    }
  } else if (mentionMenu.value.visible) {
    const atIdx = mentionMenu.value.atIndex
    if (cursorPos < atIdx - 1 || cursorPos > atIdx + 5) {
      hideMentionMenu()
    }
  }
}

function onTextareaBlur() {
  setTimeout(() => {
    inputFocused.value = false
    hideMentionMenu()
  }, 200)
}

function insertMention() {
  const textarea = textareaRef.value
  if (textarea) {
    const start = textarea.selectionStart || 0
    const end = textarea.selectionEnd || 0
    const value = inputMessage.value
    inputMessage.value = value.slice(0, start) + '@' + value.slice(end)
    nextTick(() => {
      const newPos = start + 1
      textarea.setSelectionRange(newPos, newPos)
      textarea.focus()
      showMentionMenu()
      mentionMenu.value.atIndex = newPos
    })
  } else {
    inputMessage.value += '@'
  }
}

function onLanguageChange() {
  try {
    EventsEmit('set-reply-language', replyLanguage.value)
  } catch (e) {
    console.error('[ChatPanel] 语言切换失败:', e)
  }
}

onMounted(() => {
  EventsOn('ai-thinking-step', (data: { step: number, text: string }) => {
    currentStepIndex.value = data.step
    currentThinkingText.value = data.text
  })

  EventsOn('task_added', (data: any) => {
    LogPrint('[GLOBAL-TASK-BAR] 📥 task_added received: ' + JSON.stringify(data))
    globalTasks.value.push({ id: data.id, description: data.description, role: data.role || 'SE', status: 'doing', createdAt: new Date(), updatedAt: new Date() })
    LogPrint('[GLOBAL-TASK-BAR] 📋 globalTasks now: ' + globalTasks.value.length)
  })

  EventsOn('task_updated', (data: any) => {
    LogPrint('[GLOBAL-TASK-BAR] 📥 task_updated received: ' + JSON.stringify(data))
    const idx = globalTasks.value.findIndex(t => t.id === data.id)
    if (idx >= 0) {
      globalTasks.value[idx].status = data.status || 'done'
      globalTasks.value[idx].updatedAt = new Date()
    }
  })

  EventsOn('task-clarify', (data: any) => {
    LogPrint('[TASK-CLARIFY] 📋 收到需求澄清请求: ' + JSON.stringify(data))
    if (data && data.questions && data.questions.length > 0) {
      clarifyQuestions.value = data.questions
      clarifyFollowUp.value = !!data.isFollowUp
      clarifyVisible.value = true
      if (clarifyRef.value) clarifyRef.value.reset()
    }
  })

  EventsOn('debug-query', () => {
    const state = (window as any).__debugState || {}
    LogPrint('[DEBUG-QUERY] 📤 前端状态: ' + JSON.stringify(state))
    EventsEmit('debug-query-response', JSON.stringify(state))
  })

  EventsOn('reset', () => {
    globalTasks.value.length = 0
    LogPrint('[GLOBAL-TASK-BAR] 🗑️ reset事件: 任务列表已清空')
  })

  EventsOn('tasks_cleared', () => {
    globalTasks.value.length = 0
    LogPrint('[GLOBAL-TASK-BAR] 🗑️ tasks_cleared事件: 任务列表已清空')
  })

  EventsOn('messages-cleared', () => {
    globalTasks.value.length = 0
    LogPrint('[GLOBAL-TASK-BAR] 🗑️ messages-cleared事件: 任务列表已清空')
  })

  // 初始化时从后端拉取已有任务
  loadGlobalTasks()

  loadPendingQueue()

  document.addEventListener('keydown', handleGlobalKeydown)
  document.addEventListener('click', closeContextMenu)
})

// 💡 debug hook: 随时可查前端状态池
watch(globalTasks, (val) => {
  ;(window as any).__debugState = {
    globalTasks: JSON.parse(JSON.stringify(val)),
    globalTasksCount: val.length,
    updatedAt: new Date().toISOString(),
  }
}, { deep: true })

async function loadGlobalTasks() {
  try {
    const raw = await (window as any)['go']['main']['App']['GetGlobalTasks']()
    LogPrint('[GLOBAL-TASK-BAR] 🔍 GetGlobalTasks returned: ' + raw)
    if (raw && raw !== '[]') {
      const tasks = JSON.parse(raw)
      globalTasks.value.length = 0
      tasks.forEach((t: any) => globalTasks.value.push(t))
      LogPrint('[GLOBAL-TASK-BAR] 📋 loaded ' + globalTasks.value.length + ' tasks from backend')
    }
  } catch (e) {
    LogPrint('[GLOBAL-TASK-BAR] ❌ GetGlobalTasks error: ' + e)
  }
}

onUnmounted(() => {
  document.removeEventListener('keydown', handleGlobalKeydown)
  document.removeEventListener('click', closeContextMenu)
  if (thinkingInterval) clearInterval(thinkingInterval)
  if (stepInterval) clearInterval(stepInterval)
})

// 全局快捷键
function handleGlobalKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
    e.preventDefault()
    toggleSearch()
  }
}

// #2 AI 思考进度动画
watch(() => props.aiThinking, (val) => {
  if (val) {
    currentStepIndex.value = 0
    currentThinkingText.value = thinkingSteps.value[0]?.text || t('chatPanel.analyzing')
    
    thinkingInterval = setInterval(() => {
      thinkingDots.value = thinkingDots.value.length >= 3 ? '.' : thinkingDots.value + '.'
    }, 400)
    
    stepInterval = setInterval(() => {
      if (currentStepIndex.value < thinkingSteps.value.length - 1) {
        currentStepIndex.value++
        currentThinkingText.value = thinkingSteps.value[currentStepIndex.value].text
      }
    }, 3000)
  } else {
    if (thinkingInterval) { clearInterval(thinkingInterval); thinkingInterval = null }
    if (stepInterval) { clearInterval(stepInterval); stepInterval = null }
    thinkingDots.value = ''
    currentStepIndex.value = 0
  }
}, { immediate: true })

const thinkingSteps = computed(() => [
  { icon: '🧠', text: t('chatPanel.analyzing') },
  { icon: '📋', text: t('chatPanel.planning') },
  { icon: '✍️', text: t('chatPanel.generating') },
  { icon: '⚡', text: t('chatPanel.executing') },
  { icon: '✅', text: t('chatPanel.verifying') }
])

function handleTaskClick(task: GlobalTask) {
  console.log('[ChatPanel] 任务点击:', task)
}

function handleClarifySubmit(answers: Record<number, string | string[]>, custom?: string) {
  LogPrint('[CLARIFY-SUBMIT] 用户回答: ' + JSON.stringify({ answers, custom }))
  clarifyVisible.value = false
  emit('send-message', JSON.stringify({ type: 'clarify', answers, custom }))
}

function handleSend() {
  if (inputMessage.value.trim()) {
    emit('send-message', inputMessage.value)
    inputMessage.value = ''
    textareaHeight.value = 44
  } else {
    emit('send-message', '')
  }
}

function focusInput() {
  textareaRef.value?.focus()
}

function handleEnterKey(e: KeyboardEvent) {
  e.preventDefault()
  if (handleMentionKeydown(e)) return
  handleSend()
}

function toggleThinking() {
  showThinking.value = !showThinking.value
  emit('expand-thinking')
}

function getChangeIcon(type: string): string {
  const icons: Record<string, string> = { create: '✅', modify: '🔧', delete: '🗑️', install: '📦' }
  return icons[type] || '•'
}

function getRoleDisplayName(role: string): string {
  const roleMap: Record<string, string> = { user: 'USR', pm: 'PM', se: 'SE', ap: 'AP', mc: 'MC', error: 'ERR', system: 'SYS' }
  const lowerRole = (role || '').toLowerCase()
  if (lowerRole.startsWith('sys_')) {
    const sub = role.slice(4)
    return roleMap[sub] || sub.toUpperCase() || 'SYS'
  }
  return roleMap[lowerRole] || role || 'UNKNOWN'
}

function getRoleClass(role: string): string {
  const lower = (role || '').toLowerCase()
  if (lower === 'pm') return 'pm'
  if (lower === 'se') return 'se'
  if (lower === 'ap') return 'ap'
  if (lower === 'mc' || lower.startsWith('sys_')) return 'mc'
  if (lower === 'error') return 'err'
  if (lower === 'user') return 'usr'
  return 'default'
}

function formatContent(content: string): string {
  if (!content) return ''
  content = escapeHtml(content)
  return content.replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/\*([^*]+)\*/g, '<em>$1</em>')
    .replace(/\n/g, '<br>')
}

// #3 代码语法高亮（轻量级）
function highlightCode(code: string, language?: string): string {
  let escaped = escapeHtml(code)
  
  const lang = (language || '').toLowerCase()
  
  // 关键字
  const keywords = {
    go: ['func', 'var', 'const', 'type', 'struct', 'interface', 'map', 'chan', 'go', 'defer', 'return', 'if', 'else', 'for', 'range', 'switch', 'case', 'break', 'continue', 'package', 'import', 'fmt', 'error', 'string', 'int', 'bool', 'nil', 'make', 'append', 'len', 'cap'],
    python: ['def', 'class', 'import', 'from', 'return', 'if', 'elif', 'else', 'for', 'in', 'while', 'try', 'except', 'finally', 'with', 'as', 'yield', 'lambda', 'True', 'False', 'None', 'and', 'or', 'not', 'is', 'print', 'self', 'pass', 'raise', 'del', 'global', 'nonlocal'],
    js: ['function', 'var', 'let', 'const', 'return', 'if', 'else', 'for', 'while', 'class', 'import', 'export', 'from', 'async', 'await', 'new', 'this', 'true', 'false', 'null', 'undefined', 'typeof', 'instanceof'],
    ts: ['function', 'var', 'let', 'const', 'return', 'if', 'else', 'for', 'while', 'class', 'import', 'export', 'from', 'async', 'await', 'new', 'this', 'true', 'false', 'null', 'undefined', 'typeof', 'interface', 'type', 'enum', 'extends', 'implements']
  }

  const kw = keywords[lang as keyof typeof keywords] || keywords.go || []
  
  // 字符串
  escaped = escaped.replace(/("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`)/g, '<span class="hl-string">$1</span>')
  
  // 注释
  escaped = escaped.replace(/(\/\/.*$)/gm, '<span class="hl-comment">$1</span>')
  escaped = escaped.replace(/(\/\*[\s\S]*?\*\/)/g, '<span class="hl-comment">$1</span>')
  escaped = escaped.replace(/(#.*$)/gm, '<span class="hl-comment">$1</span>')
  
  // 数字
  escaped = escaped.replace(/\b(\d+\.?\d*)\b/g, '<span class="hl-number">$1</span>')
  
  // 关键字
  kw.forEach(k => {
    const re = new RegExp(`\\b(${k})\\b`, 'g')
    escaped = escaped.replace(re, '<span class="hl-keyword">$1</span>')
  })
  
  // 内置函数
  escaped = escaped.replace(/\b(fmt\.Print\w+|console\.log|print\w*)\b/g, '<span class="hl-func">$1</span>')
  
  return escaped
}

function copyCode(code: string) {
  navigator.clipboard.writeText(code).then(() => showToast('已复制')).catch(() => fallbackCopy(code))
}

function copyMessage(content: string) {
  navigator.clipboard.writeText(content).then(() => showToast('已复制')).catch(() => fallbackCopy(content))
}

function fallbackCopy(text: string) {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.cssText = 'position:fixed;opacity:0'
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  document.body.removeChild(ta)
  showToast('已复制')
}

// Toast 提示
let toastTimer: ReturnType<typeof setTimeout> | null = null
const toastMessage = ref('')
const toastVisible = ref(false)
function showToast(msg: string) {
  toastMessage.value = msg
  toastVisible.value = true
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toastVisible.value = false }, 1500)
}

function quoteMessage(content: string, index: number) {
  emit('quote-message', `> ${content.substring(0, 100)}${content.length > 100 ? '...' : ''}`, index)
}

function getFullMessageContent(msg: any): string {
  let content = ''
  if (msg.summary) content += msg.summary + '\n\n'
  if (msg.description) content += msg.description + '\n\n'
  if (msg.content) content += msg.content + '\n\n'
  if (msg.codeBlocks && msg.codeBlocks.length > 0) {
    msg.codeBlocks.forEach((block: any) => { content += '```' + (block.language || '') + '\n' + block.code + '\n```\n\n' })
  }
  return content.trim()
}

function getExecContent(msg: any): string {
  let text = ''
  const execData = msg._execData
  if (execData?.actions) {
    execData.actions.forEach((step: any) => { text += `${step.status === 'done' ? '✅' : '❌'} ${step.label}\n` })
  }
  if (execData?.outputs) {
    execData.outputs.forEach((out: any) => {
      if (out.command) text += `$ ${out.command}\n`
      text += out.output + '\n'
    })
  }
  return text.trim()
}

function viewDetails(index: number) { emit('view-details', index) }
function openInEditor(index: number) { emit('open-editor', index) }
function modify(index: number) { emit('modify', index) }

function toggleExpand(index: number) {
  if (expandedMessages.value.has(index)) expandedMessages.value.delete(index)
  else expandedMessages.value.add(index)
  expandedMessages.value = new Set(expandedMessages.value)
}

function getRichMessage(msg: any): RichMessageType | null {
  if (!msg || !window.__richMessages) return null
  
  // [G54调试] PM消息状态检查
  if (msg.role === 'pm') {
    console.log('[G54-PM] getRichMessage调用:', {
      content: msg.content,
      contentLen: msg.content?.length,
      _messageId: (msg as any)._messageId,
      _richTaskId: (msg as any)._richTaskId,
      _streaming: (msg as any)._streaming,
      richMessagesKeys: Object.keys(window.__richMessages)
    })
  }
  
  const keys = Object.keys(window.__richMessages)
  for (const k of keys) {
    const rm = window.__richMessages[k]
    if (rm && rm.role === msg.role && (rm.result?.text === msg.content || msg._richTaskId === k)) {
      return rm
    }
  }
  return null
}

function getMsgStatus(msg: any): { type: string, text: string } | null {
  const c = msg.content || ''
  if (c.includes('✅ 任务完成') || c.match(/✅\s*任务完成/)) return { type: 'success', text: '✅ 完成' }
  if (c.includes('📊 任务进行中')) return { type: 'progress', text: '📊 进行中' }
  if (c.includes('执行失败') || (c.includes('错误') && msg.role === 'se')) return { type: 'error', text: '❌ 失败' }
  if (msg.role === 'pm' && (c.includes('审核通过') || c.includes('代码审核通过'))) return { type: 'success', text: '✅ 审核通过' }
  if (msg.role === 'ap' && (c.includes('通过') || c.includes('✅') || c.includes('approved') || c.includes('"approval_result":"approve"'))) return { type: 'success', text: '✅ 审批通过' }
  if (msg.role === 'ap' && (c.includes('不通过') || c.includes('❌') || c.includes('未通过') || c.includes('reject') || c.includes('"approval_result":"reject"'))) return { type: 'error', text: '❌ 未通过' }
  if (c.includes('"review_result":"approve"')) return { type: 'success', text: '✅ PM审核通过' }
  if (c.includes('"review_result":"reject"')) return { type: 'error', text: '❌ PM要求返工' }
  if (c.includes('"task_status":"completed"')) return { type: 'success', text: '✅ SE完成' }
  return null
}

function getSummary(msg: any): string | null {
  const c = msg.content || ''
  if (!c || c.length < 100) return null
  const status = getMsgStatus(msg)
  let summary = ''
  if (status) summary += status.text + ' · '
  const taskMatch = c.match(/(?:当前任务|任务)[：:]?\s*([^\n{]+)/)
  if (taskMatch) summary += taskMatch[1].trim().substring(0, 50)
  const noteMatch = c.match(/"reason":"([^"]+)"/)
  if (noteMatch && !summary) summary = noteMatch[1].trim().substring(0, 60)
  if (msg.role === 'ap' && c.includes('@USR')) {
    const usrMatch = c.match(/@USR\s*([^\n{]+)/)
    if (usrMatch) summary = usrMatch[1].trim().substring(0, 60)
  }
  return summary || null
}

function getPreviewText(msg: any): string {
  const c = msg.content || ''
  const lines = c.split('\n').filter(l => l.trim() && !l.trim().startsWith('{') && !l.trim().startsWith('📝') && !l.trim().startsWith('📋'))
  return lines.slice(0, 2).join(' ').substring(0, 120) + (lines.length > 2 ? '...' : '')
}

function getSEActionCount(msg: any): number | null {
  const match = (msg.content || '').match(/已执行操作[:\s]*(\d+)/)
  return match ? parseInt(match[1]) : null
}

function getSEActions(msg: any): any[] | null {
  try {
    const execData = (msg as any)._execData
    if (execData?.actions && Array.isArray(execData.actions) && execData.actions.length > 0) {
      return execData.actions.map((a: any) => ({
        type: a.type,
        path: a.path || a.label || '',
        command: a.command || '',
        status: a.status || 'pending',
        content: a.content,
        output: a.output,
        duration: a.duration,
        size: a.content ? new Blob([a.content]).size : undefined
      }))
    }
    const content = msg.content || ''
    const jsonMatch = content.match(/\{[\s\S]*"actions"[\s]*:\s*\[[\s\S]*\][\s]*\}/)
    if (jsonMatch) {
      const parsed = JSON.parse(jsonMatch[0])
      if (parsed.actions && Array.isArray(parsed.actions) && parsed.actions.length > 0) {
        return parsed.actions
      }
    }

    const directMatch = content.match(/^\{[\s\S]*\}$/)
    if (directMatch) {
      const parsed = JSON.parse(directMatch[0])
      if (parsed.actions && Array.isArray(parsed.actions)) {
        return parsed.actions
      }
    }
    
    return null
  } catch (e) {
    return null
  }
}

function getSEShellOutput(msg: any): string {
  const execData = (msg as any)._execData
  if (!execData?.outputs) return ''
  return execData.outputs.map((o: any) => {
    let line = ''
    if (o.command) line += '$ ' + o.command + '\n'
    if (o.output) line += o.output
    return line
  }).filter(Boolean).join('\n')
}

function onOpenFileInEditor(data: { path: string }) {
  emit('open-file-in-editor', data)
}

function onRunInTerminal(data: { command: string }) {
  emit('run-in-terminal', data)
}

function renderSEText(msg: any): string {
  const c = msg.content || ''
  if (!c) return ''
  let text = c
  text = text.replace(/\{[\s\S]*"actions"[\s]*:\s*\[[\s\S]*\][\s\S]*\}/g, '')
  text = text.replace(/^\{[\s\S]*\}$/g, '')
  text = text.replace(/```json[\s\S]*?```/g, '')
  const lines = text.split('\n').filter(l => {
    const t = l.trim()
    if (!t) return false
    if (t.match(/^\[.*\]$/) && t.includes(':')) return false
    if (t.match(/^\{.*\}$/)) return false
    if (t.match(/"task_status"|"review_result"|"approval_result"/)) return false
    return true
  })
  return lines.map(l => `<div class="se-plain-line">${escapeHtml(l)}</div>`).join('')
}

function renderStructured(msg: any): string {
  const c = msg.content || ''
  if (!c) return ''
  let html = ''
  const parts = c.split(/\n(?=✅|📊|@PM|@USR|@SE|```|\{")/)
  for (const part of parts) {
    const trimmed = part.trim()
    if (!trimmed) continue
    
    const codeMatch = trimmed.match(/^```(\w*)\n([\s\S]*?)```$/)
    if (codeMatch) {
      html += `<div class="msg-code-block"><pre class="code-content"><code v-pre>${highlightCode(codeMatch[2], codeMatch[1])}</code></pre></div>`
      continue
    }
    
    if (trimmed.startsWith('@PM') || trimmed.startsWith('@USR') || trimmed.startsWith('@SE')) {
      html += `<div class="msg-main-text">${renderMarkdown(trimmed)}</div>`
    } else if (trimmed.match(/^\{[\s\S]*\}$/) && (trimmed.includes('"review_result"') || trimmed.includes('"approval_result"') || trimmed.includes('"task_status"'))) {
      html += `<div class="msg-json"><code class="inline-code">${escapeHtml(trimmed)}</code></div>`
    } else if (trimmed.includes('执行失败的原因是') || trimmed.includes('错误分析') || trimmed.includes('修复方案')) {
      html += `<div class="msg-section error-analysis"><div class="section-label">🔍 分析</div><div class="section-body">${renderMarkdown(trimmed)}</div></div>`
    } else {
      html += `<div class="msg-plain">${renderMarkdown(trimmed)}</div>`
    }
  }
  return html
}

function renderMarkdown(text: string): string {
  if (!text) return ''
  let html = escapeHtml(text)
  html = html.replace(/^>\s+/gm, '') 
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/\*([^*]+)\*/g, '<em>$1</em>')
  html = html.replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>')
  html = html.replace(/^[-*]\s+(.+)$/gm, '<li>$1</li>')
  html = html.replace(/(<li>.*<\/li>\n?)+/g, (m) => '<ul>' + m + '</ul>')
  html = html.replace(/^(\d+)\.\s+(.+)$/gm, '<li>$2</li>')
  html = html.replace(/\n/g, '<br>')
  return html
}

function escapeHtml(text: string): string {
  const div = document.createElement('div')
  div.textContent = text
  return div.innerHTML
}

// #1 时间戳格式化
function formatTime(timestamp?: number | string): string {
  if (!timestamp) return ''
  const ts = typeof timestamp === 'string' ? parseInt(timestamp) : timestamp
  if (isNaN(ts)) return ''
  const d = new Date(ts * 1000)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  return d.toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function formatFullTime(timestamp?: number | string): string {
  if (!timestamp) return ''
  const ts = typeof timestamp === 'string' ? parseInt(timestamp) : timestamp
  if (isNaN(ts)) return ''
  return new Date(ts * 1000).toLocaleString('zh-CN')
}

// #6 搜索功能
const filteredMessages = computed(() => props.messages)

function toggleSearch() {
  showSearch.value = !showSearch.value
  if (showSearch.value) {
    nextTick(() => searchInputRef.value?.focus())
  } else {
    searchQuery.value = ''
    searchMatches.value = 0
    matchedIndices.value = []
  }
}

function handleSearch() {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) {
    searchMatches.value = 0
    matchedIndices.value = []
    return
  }
  
  matchedIndices.value = []
  props.messages.forEach((msg, i) => {
    const content = (msg.content || '') + (msg.error || '')
    if (content.toLowerCase().includes(q)) matchedIndices.value.push(i)
  })
  searchMatches.value = matchedIndices.value.length
  searchCurrentIndex.value = matchedIndices.value.length > 0 ? 0 : -1
  
  if (matchedIndices.value.length > 0) scrollToMessage(matchedIndices.value[0])
}

function jumpToNextMatch() {
  if (matchedIndices.value.length === 0) return
  searchCurrentIndex.value = (searchCurrentIndex.value + 1) % matchedIndices.value.length
  scrollToMessage(matchedIndices.value[searchCurrentIndex.value])
}

function jumpToPrevMatch() {
  if (matchedIndices.value.length === 0) return
  searchCurrentIndex.value = (searchCurrentIndex.value - 1 + matchedIndices.value.length) % matchedIndices.value.length
  scrollToMessage(matchedIndices.value[searchCurrentIndex.value])
}

function isSearchMatch(index: number): boolean {
  return searchQuery.value && matchedIndices.value.includes(index)
}

function scrollToMessage(index: number) {
  nextTick(() => {
    const el = document.getElementById('msg-' + index)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  })
}

// #9 右键菜单
function showContextMenu(e: MouseEvent, msg: any, index: number) {
  contextMenu.value = { visible: true, x: e.clientX, y: e.clientY, msg, index }
}

function closeContextMenu() {
  contextMenu.value.visible = false
}

function contextAction(action: string) {
  const { msg, index } = contextMenu.value
  switch (action) {
    case 'copy': copyMessage(getFullMessageContent(msg)); break
    case 'quote': quoteMessage(getFullMessageContent(msg), index); break
    case 'delete': break
    case 'deleteAbove': break
  }
  closeContextMenu()
}

watch(() => props.messages.length, () => {
  nextTick(() => {
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  })
})

defineExpose({ toggleSearch })
</script>

<style scoped>
.chat-panel {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
  position: relative;
}

/* ===== 搜索栏 ===== */
.search-bar {
  padding: 8px 16px;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-secondary);
  animation: slideDown 0.15s ease-out;
}

@keyframes slideDown {
  from { opacity: 0; transform: translateY(-8px); }
  to { opacity: 1; transform: translateY(0); }
}

.search-input-wrapper {
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--bg-tertiary);
  border-radius: 6px;
  padding: 6px 10px;
}

.search-icon { font-size: 14px; }

.search-input {
  flex: 1;
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
}

.search-count {
  font-size: 11px;
  color: var(--accent);
  font-weight: 600;
  min-width: 36px;
  text-align: center;
}

.search-nav-btn {
  width: 22px;
  height: 22px;
  border: none;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  border-radius: 3px;
  cursor: pointer;
  font-size: 10px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.search-nav-btn:hover:not(:disabled) { background: var(--accent); color: white; }
.search-nav-btn:disabled { opacity: 0.3; cursor: not-allowed; }

.search-close-btn {
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  border-radius: 3px;
}
.search-close-btn:hover { background: rgba(255,59,48,0.2); color: #ff3b30; }

/* ===== 消息列表 ===== */
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px;
  scroll-behavior: smooth;
}

.message {
  margin-bottom: 12px;
  padding: 10px 14px;
  border-radius: 10px;
  position: relative;
  transition: background 0.15s, box-shadow 0.15s;
  animation: msgIn 0.2s ease-out;
}

@keyframes msgIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

.message:hover { background: rgba(255,255,255,0.02); }

.message.merged {
  margin-top: 2px;
  padding-top: 6px;
  border-radius: 8px;
}

.message.merged .message-header { display: none; }

.message.search-highlight {
  background: rgba(255,193,7,0.08);
  border: 1px solid rgba(255,193,7,0.25);
  box-shadow: 0 0 0 2px rgba(255,193,7,0.1);
}

/* #1 时间戳 */
.msg-timestamp {
  position: absolute;
  right: 8px;
  top: 6px;
  font-size: 10px;
  color: var(--text-secondary);
  opacity: 0.5;
  transition: opacity 0.15s;
  pointer-events: none;
  white-space: nowrap;
}

.message:hover .msg-timestamp,
.message.merged:hover .msg-timestamp { opacity: 0.8; }

.message.user { background: linear-gradient(135deg, rgba(74,158,255,0.1), rgba(74,158,255,0.04)); }
.message.pm { background: linear-gradient(135deg, rgba(139,92,246,0.08), rgba(139,92,246,0.03)); }
.message.se { background: linear-gradient(135deg, rgba(16,185,129,0.08), rgba(16,185,129,0.03)); }
.message.mc, .message[class*="Sys_"] { background: rgba(255,255,255,0.02); }
.message.error { background: rgba(239,68,68,0.06); }

.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.role-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 20px;
  padding: 0 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.role-badge.usr { background: var(--accent); color: white; }
.role-badge.pm { background: #8b5cf6; color: white; }
.role-badge.se { background: #10b981; color: white; }
.role-badge.ap { background: #f59e0b; color: white; }
.role-badge.mc { background: #6b7280; color: white; }
.role-badge.err { background: #ef4444; color: white; }
.role-badge.default { background: var(--bg-tertiary); color: var(--text-secondary); }

.status-tag {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 10px;
  margin-left: 6px;
}
.status-tag.success { background: #059669; color: #fff; }
.status-tag.progress { background: #d97706; color: #fff; }
.status-tag.error { background: #dc2626; color: #fff; }

.action-count {
  font-size: 10px;
  color: var(--text-secondary);
  margin-left: 6px;
  background: rgba(16,185,129,0.1);
  padding: 1px 6px;
  border-radius: 8px;
}

.message-content {
  font-size: 14px;
  line-height: 1.65;
  word-break: break-word;
}

.user-content { color: var(--text-primary); }
.pm-content { color: #e0d4ff; }
.se-content { color: #d1fae5; }
.se-simple-list { display: flex; flex-direction: column; gap: 2px; }
.se-line { display: flex; align-items: center; gap: 6px; font-size: 13px; line-height: 1.6; }
.se-dot { font-size: 12px; flex-shrink: 0; width: 14px; }
.se-line code { font-family: Consolas, monospace; font-size: 12px; background: rgba(255,255,255,0.1); padding: 1px 4px; border-radius: 2px; }
.se-plain { font-size: 13px; line-height: 1.6; }
.se-plain-line { padding: 2px 0; }
.ap-content { color: #fef3c7; }
.mc-content { color: var(--text-secondary); font-size: 12px; }
.error-content { color: #fca5a5; }

/* Trae风格 SE执行面板 */
.se-exec-panel {
  background: rgba(0,0,0,0.3);
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,0.08);
}
.se-steps { padding: 8px 12px; }
.se-step {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 13px;
  font-family: var(--font-mono, 'Cascadia Code', monospace);
}
.se-step .step-icon { flex-shrink: 0; width: 20px; }
.se-step .step-label { color: var(--text-primary); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.se-step .step-error { color: #fca5a5; font-size: 11px; }
.se-step.running .step-label { color: #60a5fa; }
.se-step.done .step-label { color: #86efac; }
.se-step.error .step-label, .step.blocked .step-label { color: #fca5a5; }

.se-terminal { border-top: 1px solid rgba(255,255,255,0.06); }
.se-terminal .terminal-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 6px 12px; cursor: pointer; font-size: 12px;
  background: rgba(0,0,0,0.2); color: var(--text-secondary);
  transition: background 0.15s;
}
.se-terminal .terminal-header:hover { background: rgba(0,0,0,0.3); }
.se-terminal .terminal-body { padding: 8px 12px; max-height: 200px; overflow-y: auto; }
.se-terminal .terminal-output {
  margin: 4px 0; padding: 8px; background: #1a1a2e; border-radius: 4px;
  font-size: 12px; font-family: var(--font-mono, 'Cascadia Code', monospace); color: #e2e8f0;
  white-space: pre-wrap; word-break: break-all;
}
.se-terminal .cmd-prompt { color: #60a5fa; font-weight: 600; }

/* 结构化消息样式 */
.structured-msg { position: relative; }

.msg-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  cursor: pointer;
  border-bottom: 1px solid rgba(255,255,255,0.05);
  border-radius: 6px;
  transition: background 0.15s;
}
.msg-summary:hover { background: rgba(255,255,255,0.04); }

.summary-text {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}

.expand-hint {
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.msg-full { padding: 8px 0; }

.msg-preview {
  margin-top: 6px;
  padding: 6px 10px;
  background: rgba(255,255,255,0.02);
  border-radius: 4px;
  font-size: 12px;
  color: var(--text-secondary);
  cursor: pointer;
  max-height: 60px;
  overflow: hidden;
  position: relative;
}
.msg-preview::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 20px;
  background: linear-gradient(transparent, var(--bg-primary));
}

.msg-section {
  margin: 10px 0;
  padding: 8px 10px;
  border-radius: 6px;
  border-left: 3px solid;
}
.msg-section.tech-notes { background: rgba(59,130,246,0.07); border-color: #3b82f6; }
.msg-section.changelog { background: rgba(16,185,129,0.07); border-color: #10b981; }
.msg-section.error-analysis { background: rgba(239,68,68,0.07); border-color: #ef4444; }

.section-label {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-secondary);
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.section-body { font-size: 13px; line-height: 1.6; }

.msg-json { margin: 8px 0; }
.msg-json .inline-code {
  display: block;
  padding: 8px 10px;
  background: rgba(255,255,255,0.03);
  border-radius: 4px;
  font-size: 11px;
  line-height: 1.5;
  overflow-x: auto;
}

.msg-main-text { margin: 6px 0; font-size: 14px; }
.msg-plain { margin: 4px 0; font-size: 13px; }

/* 代码块（带语法高亮） */
.msg-code-block {
  margin: 8px 0;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid var(--border-color);
}

.code-blocks { margin-top: 12px; }
.code-block {
  margin-bottom: 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  overflow: hidden;
}

.code-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 12px;
  background: var(--bg-tertiary);
  border-bottom: 1px solid var(--border-color);
}

.code-lang {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.copy-btn {
  padding: 2px 8px;
  border: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-secondary);
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
  transition: all 0.15s;
}
.copy-btn:hover { border-color: var(--accent); color: var(--accent); }

.code-content {
  margin: 0;
  padding: 12px;
  background: #0d1117;
  overflow-x: auto;
  font-size: 13px;
  line-height: 1.55;
}

.code-content code {
  font-family: 'Cascadia Code', 'Consolas', 'Monaco', 'Fira Code', monospace;
  color: #c9d1d9;
}

/* 语法高亮颜色 */
.hl-keyword { color: #ff7b72; font-weight: 600; }
.hl-string { color: #a5d6ff; }
.hl-comment { color: #8b949e; font-style: italic; }
.hl-number { color: #79c0ff; }
.hl-func { color: #d2a8ff; font-weight: 500; }

.inline-code {
  background: rgba(110,118,129,0.2);
  padding: 2px 6px;
  border-radius: 4px;
  font-family: 'Cascadia Code', 'Consolas', monospace;
  font-size: 12.5px;
  color: #f0883e;
}

.changes-list { margin-top: 8px; }
.change-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 0;
  font-size: 13px;
}
.change-icon { font-size: 12px; }
.change-file { color: var(--text-secondary); font-family: monospace; font-size: 12px; }

.description {
  margin-top: 8px;
  padding: 8px 10px;
  background: rgba(255,255,255,0.03);
  border-radius: 4px;
  font-size: 13px;
  color: var(--text-secondary);
}

.content { margin-top: 8px; }

.error-message {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 8px 12px;
  background: rgba(239,68,68,0.08);
  border: 1px solid rgba(239,68,68,0.2);
  border-radius: 6px;
  color: #fca5a5;
  font-size: 13px;
}
.error-icon { font-size: 14px; }

.message-actions { margin-top: 10px; display: flex; gap: 8px; }
.action-btn {
  padding: 5px 12px;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  color: var(--text-secondary);
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
}
.action-btn:hover { border-color: var(--accent); color: var(--accent); }

.message-actions-bar {
  display: flex;
  gap: 4px;
  margin-top: 6px;
  opacity: 0;
  transition: opacity 0.15s;
}
.message:hover .message-actions-bar { opacity: 1; }

.action-icon-btn {
  width: 26px;
  height: 26px;
  border: none;
  background: transparent;
  cursor: pointer;
  font-size: 13px;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}
.action-icon-btn:hover { background: rgba(255,255,255,0.08); }

/* #9 右键菜单 */
.context-menu {
  position: fixed;
  z-index: 9999;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.4);
  min-width: 180px;
  padding: 4px;
  animation: menuIn 0.1s ease-out;
}

@keyframes menuIn {
  from { opacity: 0; transform: scale(0.95); }
  to { opacity: 1; transform: scale(1); }
}

.context-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 13px;
  color: var(--text-primary);
  cursor: pointer;
  transition: background 0.1s;
}
.context-item:hover { background: rgba(255,255,255,0.06); }
.context-item.danger { color: #ef4444; }
.context-item.danger:hover { background: rgba(239,68,68,0.1); }
.context-divider { height: 1px; background: var(--border-color); margin: 4px 8px; }

/* 待发送消息队列 */
.pending-queue {
  margin: 8px 16px;
  background: rgba(255,193,7,0.05);
  border: 1px solid rgba(255,193,7,0.2);
  border-radius: 8px;
  overflow: hidden;
}
.pending-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: rgba(255,193,7,0.1);
  font-size: 12px;
  font-weight: 600;
  color: #ffc107;
}
.pending-clear-btn {
  background: none;
  border: none;
  color: #ffc107;
  cursor: pointer;
  font-size: 14px;
  padding: 2px 6px;
  border-radius: 4px;
}
.pending-clear-btn:hover { background: rgba(255,193,7,0.2); }
.pending-list {
  max-height: 120px;
  overflow-y: auto;
}
.pending-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  border-bottom: 1px solid rgba(255,255,255,0.05);
  font-size: 12px;
}
.pending-item:last-child { border-bottom: none; }
.pending-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
}
.pending-actions {
  display: flex;
  gap: 4px;
  margin-left: 8px;
}
.pending-send-btn, .pending-delete-btn {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 12px;
  padding: 2px 6px;
  border-radius: 4px;
}
.pending-send-btn { color: #10b981; }
.pending-send-btn:hover { background: rgba(16,185,129,0.2); }
.pending-delete-btn { color: #ef4444; }
.pending-delete-btn:hover { background: rgba(239,68,68,0.2); }

/* Toast 提示 */
.toast {
  position: fixed;
  top: 60px;
  left: 50%;
  transform: translateX(-50%);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  padding: 8px 20px;
  border-radius: 20px;
  font-size: 13px;
  z-index: 10000;
  box-shadow: 0 4px 16px rgba(0,0,0,0.3);
  animation: toastIn 0.2s ease-out;
  pointer-events: none;
}

@keyframes toastIn {
  from { opacity: 0; transform: translateX(-50%) translateY(-10px); }
  to { opacity: 1; transform: translateX(-50%) translateY(0); }
}

/* 输入区域 */
.input-area {
  padding: 10px 16px;
  border-top: 1px solid var(--border-color);
  background: var(--bg-secondary);
}

.target-select {
  height: 28px;
  padding: 0 6px;
  border: 1px solid transparent;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 600;
  border-radius: 6px;
  cursor: pointer;
  outline: none;
  min-width: 72px;
}
.target-select:hover { border-color: var(--accent); }
.target-select option { background: var(--bg-tertiary); color: var(--text-primary); }

.lang-select {
  height: 28px;
  padding: 0 4px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
  border-radius: 6px;
  cursor: pointer;
  outline: none;
  text-align: center;
  min-width: 72px;
}
.lang-select:hover { background: rgba(255,255,255,0.06); color: var(--text-primary); }
.lang-select option { background: var(--bg-tertiary); color: var(--text-primary); }

.mention-popup {
  position: fixed;
  z-index: 9999;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 4px 20px rgba(0,0,0,0.5);
  min-width: 240px;
  overflow: hidden;
  padding: 4px 0;
}
.mention-popup-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  cursor: pointer;
  transition: background 0.1s;
}
.mention-popup-item:hover,
.mention-popup-item.selected {
  background: rgba(255,255,255,0.08);
}
.mention-popup-icon { font-size: 18px; }
.mention-popup-name { font-weight: 600; color: var(--text-primary); min-width: 40px; }
.mention-popup-desc { font-size: 12px; color: var(--text-secondary); }

.input-wrapper {
  background: var(--bg-tertiary);
  border-radius: 10px;
  padding: 10px 12px;
  transition: border-color 0.2s, box-shadow 0.2s;
  border: 1px solid transparent;
  position: relative;
}
.input-wrapper.focused {
  border-color: rgba(74,158,255,0.3);
  box-shadow: 0 0 0 3px rgba(74,158,255,0.08);
}

.message-input {
  width: 100%;
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 14px;
  resize: none;
  outline: none;
  font-family: inherit;
  line-height: 1.5;
  min-height: 22px;
  max-height: 200px;
  overflow-y: auto;
}
.message-input::placeholder { color: var(--text-secondary); opacity: 0.6; }

.input-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
}

.input-left-actions, .input-right-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.input-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 14px;
  border-radius: 6px;
  transition: all 0.15s;
  display: flex;
  align-items: center;
  justify-content: center;
}
.input-btn:hover { background: rgba(255,255,255,0.06); color: var(--text-primary); }
.input-btn.active { background: var(--accent); color: white; }

.search-toggle-btn { font-size: 13px; }

.multimodal-badge {
  font-size: 13px;
  opacity: 0.7;
  cursor: default;
  user-select: none;
}
.multimodal-badge:hover { opacity: 1; }

.cli-toggle-btn {
  font-size: 13px;
  transition: all 0.2s;
}
.cli-toggle-btn.active {
  background: linear-gradient(135deg, #667eea, #764ba2);
  color: white;
  transform: scale(1.1);
  box-shadow: 0 0 10px rgba(102, 126, 234, 0.5);
}

.shortcut-hint {
  font-size: 11px;
  color: var(--text-secondary);
  opacity: 0.5;
  white-space: nowrap;
  transition: opacity 0.15s;
}
.input-wrapper:focus-within .shortcut-hint { opacity: 0.8; }

.send-btn {
  padding: 6px 16px;
  background: var(--accent);
  border: none;
  color: #fff;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  font-weight: 600;
  transition: all 0.15s;
}
.send-btn:hover { opacity: 0.88; filter: brightness(1.1); }
.send-btn:disabled { opacity: 0.35; cursor: not-allowed; }

/* 滚动条美化 */
.messages::-webkit-scrollbar { width: 5px; }
.messages::-webkit-scrollbar-track { background: transparent; }
.messages::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 3px; }
.messages::-webkit-scrollbar-thumb:hover { background: rgba(255,255,255,0.2); }

.message.streaming .message-content::after {
  content: '▊';
  animation: blink 1s step-end infinite;
  color: var(--accent);
  margin-left: 2px;
}

@keyframes blink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0; }
}

/* 响应式 */
@media (max-width: 700px) {
  .shortcut-hint { display: none; }
  .msg-timestamp { opacity: 0.4; }
}

.global-task-bar-wrapper {
  border-top: 1px solid rgba(128,128,128,0.1);
  background: transparent;
}

</style>

<!-- 全局样式：v-html 渲染的内容需要全局样式 -->
<style>
.thinking-body {
  font-size: 13px;
  font-style: italic;
  color: var(--text-secondary);
  line-height: 1.7;
  opacity: 0.85;
}
.result-body {
  font-size: 14px;
  font-weight: 500;
  line-height: 1.6;
  color: var(--text-primary);
}
</style>
