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
      <!-- [3.4] AI 思考链区域（嵌入对话区内部） -->
      <div v-if="thoughtEvents.length > 0" class="thinking-section" :class="{ collapsed: thinkingCollapsed }">
        <div class="thinking-header" @click="thinkingCollapsed = !thinkingCollapsed">
          <span class="thinking-icon">🧠</span>
          <span class="thinking-title">AI Thinking</span>
          <span class="thinking-count">{{ thoughtEvents.length }}</span>
          <span class="thinking-toggle">{{ thinkingCollapsed ? '▶' : '▼' }}</span>
        </div>
        <div v-if="!thinkingCollapsed" class="thinking-body" ref="thinkingBodyRef">
          <div
            v-for="(evt, idx) in displayThoughts"
            :key="'t-' + idx"
            class="thinking-event"
            :class="[evt.type, 'role-' + (evt.role || 'unknown')]"
          >
            <span class="t-role">{{ (evt.role || '').toUpperCase() }}</span>
            <span class="t-content" :title="evt.content">{{ thinkingPreview(evt) }}</span>
            <span class="t-time">{{ formatTime(evt.timestamp) }}</span>
          </div>
        </div>
      </div>

      <!-- IDE连接状态指示器 -->
      <div v-if="ideConnected && Object.keys(ideConnected).length > 0" class="ide-connected-bar">
        <span class="ide-bar-label">🔗 IDE</span>
        <span v-for="(_, name) in ideConnected" :key="name" class="ide-dot" :title="name">{{ name.slice(-1) }}</span>
      </div>

      <div 
        v-for="(msg, index) in filteredMessages" 
        :key="'msg-' + index"
        :id="'msg-' + index"
        :class="['message', msg.role, { 'search-highlight': isSearchMatch(index), streaming: (msg as any)._streaming }]"
        @contextmenu.prevent="showContextMenu($event, msg, index)"
      >
        <div class="msg-timestamp" :title="formatFullTime(msg.timestamp)">
          {{ formatTime(msg.timestamp) }}
        </div>

        <!-- user -->
        <template v-if="msg.role === 'user'">
          <div class="message-header">
            <span class="role-badge usr">USR</span>
          </div>
          <div class="message-content user-content">{{ msg.raw || msg.content }}</div>
          <div class="message-actions-bar">
            <button class="action-icon-btn" @click="copyMessage(msg.raw || msg.content)" title="📋">📋</button>
            <button class="action-icon-btn" @click="quoteMessage(msg.raw || msg.content, index)" title="💬">💬</button>
          </div>
        </template>

        <!-- mc -->
        <template v-else-if="msg.role === 'mc' || msg.role?.startsWith('Sys_')">
          <div class="message-header">
            <span class="role-badge mc">{{ msg.role === 'mc' ? 'Argus-MC' : 'SYS' }}</span>
          </div>
          <div class="message-content mc-content">{{ msg.content }}</div>
        </template>

        <!-- PM / SE / AP — unified text block -->
        <template v-else-if="['pm','se','ap'].includes(msg.role || '')">
          <div class="message-header">
            <span class="role-badge" :class="msg.role">{{ msg.role.toUpperCase() }}</span>
            <span v-if="(msg as any)._streaming" class="status-tag running">🔄</span>
          </div>
          <div class="message-content ai-content">
            <div class="msg-body-text">{{ msg.content }}</div>
            <!-- sections -->
            <div v-if="(msg as any).sections?.length" class="msg-sections">
              <div
                v-for="(sec, si) in (msg as any).sections"
                :key="si"
                class="msg-section"
                :class="sec.type"
              >
                <div class="section-header" @click="toggleSection(index, si)">
                  <span class="section-toggle">{{ sectionCollapsed(index, si) ? '▶' : '▼' }}</span>
                  <span class="section-label">{{ sec.label || sec.type }}</span>
                </div>
                <div v-show="!sectionCollapsed(index, si)" class="section-body">
                  <pre v-if="sec.type === 'terminal'" class="terminal-output">{{ sec.content }}</pre>
                  <div v-else class="section-text">{{ sec.content }}</div>
                </div>
              </div>
            </div>
          </div>
          <div class="message-actions-bar">
            <button class="action-icon-btn" @click="copyMessage(msg.content)" title="📋">📋</button>
            <button class="action-icon-btn" @click="quoteMessage(msg.content, index)" title="💬">💬</button>
          </div>
        </template>

        <!-- error -->
        <template v-else-if="msg.role === 'error'">
          <div class="message-header">
            <span class="role-badge err">ERR</span>
          </div>
          <div class="message-content error-content">{{ msg.error || msg.content }}</div>
        </template>

        <!-- system / other -->
        <template v-else>
          <div class="message-header">
            <span class="role-badge sys">SYS</span>
          </div>
          <div class="message-content ai-content">{{ msg.content }}</div>
          <div class="message-actions-bar">
            <button class="action-icon-btn" @click="copyMessage(msg.content)" title="📋">📋</button>
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
            <button type="button" class="send-btn" @click="handleSend" :title="inputMessage.trim() ? t('chatPanel.sendMessage') : t('chatPanel.stopAI')">
              <template v-if="inputMessage.trim()">➤</template>
              <template v-else>⏹</template>
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
  thoughtEvents?: Array<{
    type: string
    role: string
    content: string
    timestamp: number
    meta?: Record<string, any>
  }>
  ideConnected?: Record<string, boolean>
}>()

const emit = defineEmits(['send-message', 'upload-file', 'quote-message'])

const inputMessage = ref('')

// 从消息中提取所有角色的任务，驱动底部任务追踪栏
const globalTasks = computed<GlobalTask[]>(() => {
  const tasks: GlobalTask[] = []
  for (const msg of props.messages) {
    const role = msg.role.toUpperCase() as 'PM' | 'SE' | 'AP' | 'USR'
    const ts = new Date(msg.timestamp as number)

    if (role === 'PM' && msg.content) {
      const summary = msg.content.substring(0, 80)
      if (summary.length > 3) {
        tasks.push({
          id: `pm-${msg.timestamp}`,
          description: summary.replace(/\n/g, ' ').substring(0, 60),
          role: 'PM',
          status: 'done',
          createdAt: ts,
          updatedAt: ts,
        })
      }
      continue
    }

    if (role === 'AP') {
      const apText = msg.content.substring(0, 60)
      if (apText.length > 3) {
        const isPass = /PASSED|APPROVED|通过/i.test(msg.content)
        tasks.push({
          id: `ap-${msg.timestamp}`,
          description: apText.replace(/\n/g, ' '),
          role: 'AP',
          status: isPass ? 'done' : 'doing',
          createdAt: ts,
          updatedAt: ts,
        })
      }
      continue
    }

    if (role === 'USR') {
      const usrText = msg.content.substring(0, 60)
      if (usrText.length > 3) {
        tasks.push({
          id: `usr-${msg.timestamp}`,
          description: usrText.replace(/\n/g, ' '),
          role: 'USR',
          status: 'pending',
          createdAt: ts,
          updatedAt: ts,
        })
      }
    }
  }
  return tasks
})

// [3.4] AI 思考链状态
const thinkingCollapsed = ref(true) // 默认折叠，不占空间

const clarifyVisible = ref(false)
const clarifyQuestions = ref<Array<{ text: string; type: string; options?: any[] }>>([])
const clarifyFollowUp = ref(false)
const clarifyRef = ref<any>(null)
const messagesRef = ref<HTMLElement>()
const thinkingBodyRef = ref<HTMLElement>()
const textareaRef = ref<HTMLTextAreaElement>()
const inputFocused = ref(false)
const textareaHeight = ref(44)
const replyLanguage = ref('auto')



// #1 搜索相关
const showSearch = ref(false)
const searchQuery = ref('')
const searchInputRef = ref<HTMLInputElement>()

// #2 CLI相关
import CliPanel from './CliPanel.vue'
import GlobalTaskBar from './GlobalTaskBar.vue'
import TaskClarify from './TaskClarify.vue'
import type { GlobalTask } from '../../types/task'

// section collapse state
const collapsedSections = ref(new Set<string>())
function toggleSection(msgIdx: number, secIdx: number) {
  const key = `${msgIdx}-${secIdx}`
  if (collapsedSections.value.has(key)) {
    collapsedSections.value.delete(key)
  } else {
    collapsedSections.value.add(key)
  }
}
function sectionCollapsed(msgIdx: number, secIdx: number): boolean {
  return !collapsedSections.value.has(`${msgIdx}-${secIdx}`)
}
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
  EventsOn('task-clarify', (data: any) => {
    if (data?._msgId) (window as any).__argusAck?.(data._msgId)
    if (data && data.questions && data.questions.length > 0) {
      clarifyQuestions.value = data.questions
      clarifyFollowUp.value = !!data.isFollowUp
      clarifyVisible.value = true
      if (clarifyRef.value) clarifyRef.value.reset()
    }
  })

  EventsOn('reset', () => {
    globalTasks.value.length = 0
  })

  EventsOn('tasks_cleared', () => {
    globalTasks.value.length = 0
  })

  EventsOn('messages-cleared', () => {
    globalTasks.value.length = 0
  })

  // 初始化时从后端拉取已有任务
  loadGlobalTasks()

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
    if (raw && raw !== '[]') {
      const tasks = JSON.parse(raw)
      globalTasks.value.length = 0
      tasks.forEach((t: any) => globalTasks.value.push(t))
    }
  } catch (e) {
    // 静默失败
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

// [3.4] 思考链：只显示最近20条，合并同角色连续 thinking
const displayThoughts = computed(() => {
  const events = props.thoughtEvents || []
  if (events.length === 0) return []
  // 取最近20条
  const recent = events.slice(-20)
  // 合并连续的 thinking（同一角色的相邻 thinking 合为一条）
  const merged: typeof recent = []
  for (const evt of recent) {
    if (evt.type === 'thinking' && merged.length > 0) {
      const last = merged[merged.length - 1]
      if (last.type === 'thinking' && last.role === evt.role) {
        last.content += evt.content
        continue
      }
    }
    merged.push({ ...evt })
  }
  return merged
})

function thinkingPreview(evt: { type: string; content: string }): string {
  if (!evt.content) return ''
  if (evt.type === 'step') return evt.content
  // thinking 类型截断显示
  const text = evt.content.replace(/\s+/g, ' ').trim()
  return text.length > 80 ? text.slice(0, 80) + '...' : text
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
  const content = msg.content || msg.raw || ''
  switch (action) {
    case 'copy': copyMessage(content); break
    case 'quote': quoteMessage(content, index); break
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

// 思考链自动滚动到底部
watch(() => props.thoughtEvents?.length, () => {
  if (thinkingCollapsed.value) return
  nextTick(() => {
    if (thinkingBodyRef.value) {
      thinkingBodyRef.value.scrollTop = thinkingBodyRef.value.scrollHeight
    }
  })
})

defineExpose({ toggleSearch, appendToInput })

function appendToInput(text: string) {
  if (!text) return
  if (inputMessage.value) {
    inputMessage.value += '\n' + text
  } else {
    inputMessage.value = text
  }
  nextTick(() => textareaRef.value?.focus())
}
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

/* ===== [3.4] AI 思考链区域 ===== */
.thinking-section {
  background: rgb(25, 20, 38);
  border: 1px solid rgba(100, 100, 255, 0.2);
  border-radius: 8px;
  margin-bottom: 10px;
  overflow: hidden;
  font-size: 11px;
  /* 固定在消息列表顶部，不随消息滚动 */
  position: sticky;
  top: 0;
  z-index: 10;
}
.thinking-section.collapsed .thinking-body { display: none; }
.thinking-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  cursor: pointer;
  user-select: none;
  background: rgba(60, 50, 90, 0.4);
  transition: background 0.15s;
}
.thinking-header:hover { background: rgba(80, 70, 120, 0.5); }
.thinking-icon { font-size: 13px; }
.thinking-title { font-weight: 600; opacity: 0.7; text-transform: uppercase; letter-spacing: 0.5px; }
.thinking-count {
  background: #4a9eff;
  color: #000;
  font-size: 9px;
  padding: 0 5px;
  border-radius: 8px;
  font-weight: 700;
}
.thinking-toggle { margin-left: auto; font-size: 9px; opacity: 0.5; }
.thinking-body {
  padding: 6px 10px;
  max-height: 150px;
  overflow-y: auto;
}
.thinking-body::-webkit-scrollbar { width: 2px; }
.thinking-body::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 1px; }
.thinking-event {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 0;
  line-height: 1.5;
  border-left: 2px solid transparent;
}
.thinking-event.role-pm { border-left-color: #f59e0b; }
.thinking-event.role-se { border-left-color: #3b82f6; }
.thinking-event.role-ap { border-left-color: #10b981; }
.t-role {
  font-weight: 700;
  font-size: 9px;
  min-width: 24px;
  padding: 0 3px;
  border-radius: 3px;
  text-align: center;
}
.role-pm .t-role { background: rgba(245,158,11,0.15); color: #f59e0b; }
.role-se .t-role { background: rgba(59,130,246,0.15); color: #3b82f6; }
.role-ap .t-role { background: rgba(16,185,129,0.15); color: #10b981; }
.t-content { flex: 1; min-width: 0; word-break: break-all; white-space: pre-wrap; opacity: 0.75; font-style: italic; }
.thinking-event.step .t-content { font-style: normal; font-weight: 600; opacity: 1; }
.t-time { color: rgba(255,255,255,0.2); font-size: 9px; min-width: 42px; text-align: right; }

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
.section-body .terminal-output {
  margin: 4px 0; padding: 10px 12px;
  background: #1a1a2e; border-radius: 4px;
  font-size: 13px; font-family: var(--font-mono, 'Cascadia Code', monospace);
  color: #e2e8f0; white-space: pre-wrap; word-break: break-all;
  line-height: 1.5; max-height: 300px; overflow-y: auto;
}

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

/* IDE连接状态指示器 */
.ide-connected-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  margin: 0 12px 6px;
  background: rgba(34, 197, 94, 0.08);
  border: 1px solid rgba(34, 197, 94, 0.15);
  border-radius: 6px;
  font-size: 11px;
}

.ide-bar-label {
  color: var(--text-secondary);
  font-weight: 600;
  margin-right: 2px;
}

.ide-dot {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: #22c55e;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 0 4px rgba(34, 197, 94, 0.4);
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
