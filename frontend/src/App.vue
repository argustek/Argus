<template>
  <div class="argus-app">
    <!-- 顶部栏 -->
    <TopBar 
      :active-windows="activeWindows"
      :ai-status="aiStatus"
      :ide-connected="ideConnected"
      :work-dir="workDir"
      :recent-projects="recentProjects"
      :c-monitor-enabled="cMonitorEnabled"
      :project-state="projectState"
      :project-level="projectLevel"
      :message-count="messages.length"
      :git-status-count="gitStatusCount"
      @toggle-window="toggleWindow"
      @reset="handleReset"
      @open-settings="showSettings = true"
      @select-project="handleSelectProject"
      @toggle-c-monitor="toggleCMonitor"
      @toggle-git="activeWindows.git = !activeWindows.git"
      @toggle-search="chatPanelRef?.toggleSearch()"
      @open-file-in-editor="openFileInEditor"
    />
    
    <!-- 未完成任务恢复对话框 -->
    <div v-if="showRecoveryDialog" class="recovery-overlay">
      <div class="recovery-dialog">
        <div class="recovery-icon">🔄</div>
        <h3 class="recovery-title">{{ t('app.recoveryTitle') }}</h3>
        <div class="recovery-content">
          <p class="recovery-label">{{ t('app.recoveryRequest') }}</p>
          <p class="recovery-request">{{ unfinishedTask.userRequest }}</p>
          <p class="recovery-label">{{ t('app.recoveryDescription') }}</p>
          <p class="recovery-description">{{ unfinishedTask.taskDescription }}</p>
        </div>
        <div class="recovery-actions">
          <button class="btn btn-primary" @click="recoverTask">
            ✅ {{ t('app.recoveryRecover') }}
          </button>
          <button class="btn btn-secondary" @click="dismissTask">
            ❌ {{ t('app.recoveryDismiss') }}
          </button>
        </div>
      </div>
    </div>
    
    <!-- 主内容区 -->
    <div class="main-content">
      <!-- 左侧：文件树 -->
      <FileTree
        v-if="activeWindows.fileTree"
        :work-dir="workDir"
        @select-file="openFile"
        @select-binary-file="handleSelectBinaryFile"
        @run-in-terminal="handleRunInTerminal"
        @add-to-chat="handleAddToChat"
        class="file-tree-panel"
        :style="{ width: treeWidth + 'px' }"
      />

      <!-- 文件树分隔条 -->
      <div class="resize-handle" @mousedown="startTreeResize">
        <div class="resize-line"></div>
      </div>

      <!-- 对话区 -->
      <ChatPanel
        ref="chatPanelRef"
        :messages="messages"
        :ai-thinking="aiThinking"
        :supports-multimodal="supportsMultimodal"
        :thought-events="thoughtEvents"
        :ide-connected="ideConnected"
        @send-message="handleSendMessage"
        @expand-thinking="toggleThinking"
        @upload-file="handleUploadFile"
        @open-file-in-editor="handleOpenFileInEditor"
        @run-in-terminal="handleRunInTerminal"
        class="chat-panel"
        :style="{ width: chatWidth + 'px' }"
      />

      <!-- 对话区分隔条 -->
      <div class="resize-handle" @mousedown="startChatResize">
        <div class="resize-line"></div>
      </div>

      <!-- 右侧面板：上编辑器 + 下终端 -->
      <div class="right-panel" v-if="activeWindows.editor || activeWindows.terminal">
        <div class="right-panel-top" v-if="activeWindows.editor" :style="{ flex: activeWindows.terminal ? rightSplitRatio : '1' }">
          <EditorWindow
            :file="currentFile"
            @close="activeWindows.editor = false"
            @open-file-in-editor="openFileInEditor"
          />
        </div>
        <div 
          v-if="activeWindows.editor && activeWindows.terminal" 
          class="right-panel-divider"
          @mousedown="startRightResize"
        ></div>
        <div class="right-panel-bottom" v-if="activeWindows.terminal" :style="{ flex: activeWindows.editor ? (1 - rightSplitRatio) : '1' }">
          <TerminalWindow
            :logs="terminalLogs"
            @close="activeWindows.terminal = false"
            @minimize="activeWindows.terminal = false"
          />
        </div>
      </div>
    </div>
    
    <!-- 底部：审核栏 -->
    <ReviewBar 
      v-if="pendingChanges.length > 0"
      :changes="pendingChanges"
      @accept="acceptChanges"
      @reject="rejectChanges"
      @adjust="handleAdjust"
    />
    
    <!-- (已迁移到右侧面板) -->
    
    <!-- 浮动窗口：改动摘要 -->
    <ChangesWindow
      v-if="activeWindows.changes"
      :history="changeHistory"
      @close="activeWindows.changes = false"
    />

    <!-- 浮动窗口：Git 版本控制 -->
    <GitWindow
      v-if="activeWindows.git"
      @close="activeWindows.git = false"
    />

    <!-- [v0.7.2] 浮动窗口：Debugger 调试器 -->
    <div v-if="activeWindows.debug" class="floating-panel floating-wide">
      <DebugPanel @close="activeWindows.debug = false" />
      <button class="panel-close" @click="activeWindows.debug = false">×</button>
    </div>

    <!-- [v0.7.2] 浮动窗口：MCP 工具协议 -->
    <div v-if="activeWindows.mcp" class="floating-panel">
      <MCPPanel />
      <button class="panel-close" @click="activeWindows.mcp = false">×</button>
    </div>

    <!-- [v0.7.2] 浮动窗口：Token 监控 -->
    <div v-if="activeWindows.token" class="floating-panel floating-narrow">
      <TokenMonitor />
      <button class="panel-close" @click="activeWindows.token = false">×</button>
    </div>
    
    <!-- 设置面板 -->
    <SettingsPanel
      v-if="showSettings"
      :config="config"
      @close="showSettings = false"
      @save="saveConfig"
      @api-config-changed="onAPIConfigChanged"
    />

    <!-- Diff 预览弹窗 -->
    <DiffPreviewDialog
      v-if="showDiffDialog && diffData"
      :data="diffData!"
      @approve="onDiffApprove"
      @reject="onDiffReject"
    />

  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetConfig, SaveConfig, SendMessage, StopCurrentTask, GetMessages, IsAIThinking, IsPMThinking, IsCRunning, IsSERunning, IsAPThinking, GetLogs, GetChangeHistory, GetWorkDir, GetRecentProjects, SetWorkDir, OpenFolderDialog, StartCMonitor, StopCMonitor, FixPosition, OpenFileDialog, ReadFile } from '../wailsjs/go/main/App'
import { EventsOn, EventsOff, EventsEmit, LogPrint } from '../wailsjs/runtime/runtime'

const { t } = useI18n()
import TopBar from './components/TopBar.vue'
import ChatPanel from './components/ChatPanel.vue'
import ReviewBar from './components/ReviewBar.vue'
import EditorWindow from './components/EditorWindow.vue'
import TerminalWindow from './components/TerminalWindow.vue'
import ChangesWindow from './components/ChangesWindow.vue'
import GitWindow from './components/GitWindow.vue'
import FileTree from './components/FileTree.vue'
import SettingsPanel from './components/SettingsPanel.vue'
import DiffPreviewDialog from './components/DiffPreviewDialog.vue'
import DebugPanel from './components/DebugPanel.vue'
import MCPPanel from './components/MCPPanel.vue'
import TokenMonitor from './components/TokenMonitor.vue'

// DiffPreviewDialog 数据类型
interface DiffData {
  type: string
  path: string
  diff: string
  action?: string
}

const WINDOW_STATES_KEY = 'argus_window_states'

const savedWindowStates = (() => {
  try {
    const saved = localStorage.getItem(WINDOW_STATES_KEY)
    return saved ? JSON.parse(saved) : {}
  } catch {
    return {}
  }
})()

const activeWindows = reactive({
  fileTree: savedWindowStates.fileTree ?? false,
  editor: savedWindowStates.editor ?? false,
  terminal: true,
  changes: savedWindowStates.changes ?? false,
  git: savedWindowStates.git ?? false,
  debug: savedWindowStates.debug ?? false,
  mcp: savedWindowStates.mcp ?? false,
  token: savedWindowStates.token ?? false
})

const chatPanelRef = ref<InstanceType<typeof ChatPanel> | null>(null)

  const treeWidth = ref(Number(localStorage.getItem('treeWidth')) || 240)
  const chatWidth = ref(Number(localStorage.getItem('chatWidth')) || 640)
  const leftPanelWidth = ref(Number(localStorage.getItem('leftPanelWidth')) || 340)

  function startTreeResize(e: MouseEvent) {
    const startW = treeWidth.value
    const startX = e.screenX
    function onMove(e: MouseEvent) {
      const delta = startX - e.screenX
      treeWidth.value = Math.max(150, Math.min(500, startW + delta))
    }
    const handler = onMove
    const cleanup = () => {
      document.removeEventListener('mousemove', handler)
      document.removeEventListener('mouseup', cleanup)
      localStorage.setItem('treeWidth', String(treeWidth.value))
    }
    document.addEventListener('mousemove', handler)
    document.addEventListener('mouseup', cleanup)
  }

  function startChatResize(e: MouseEvent) {
    const startW = chatWidth.value
    const startX = e.screenX
    function onMove(e: MouseEvent) {
      const delta = startX - e.screenX
      chatWidth.value = Math.max(350, Math.min(1000, startW + delta))
    }
    const handler = onMove
    const cleanup = () => {
      document.removeEventListener('mousemove', handler)
      document.removeEventListener('mouseup', cleanup)
      localStorage.setItem('chatWidth', String(chatWidth.value))
    }
    document.addEventListener('mousemove', handler)
    document.addEventListener('mouseup', cleanup)
  }

  // 右侧面板上下分隔
  const rightSplitRatio = ref(Number(localStorage.getItem('rightSplitRatio')) || 0.5)
  const isRightResizing = ref(false)

  function startRightResize(e: MouseEvent) {
    isRightResizing.value = true
    const startY = e.clientY
    const startRatio = rightSplitRatio.value
    const rightPanel = (e.target as HTMLElement).closest('.right-panel') as HTMLElement
    if (!rightPanel) return
    const panelHeight = rightPanel.offsetHeight
    const handler = (ev: MouseEvent) => {
      if (!isRightResizing.value) return
      const delta = ev.clientY - startY
      rightSplitRatio.value = Math.max(0.2, Math.min(0.8, startRatio + delta / panelHeight))
    }
    const cleanup = () => {
      isRightResizing.value = false
      localStorage.setItem('rightSplitRatio', String(rightSplitRatio.value))
      document.removeEventListener('mousemove', handler)
      document.removeEventListener('mouseup', cleanup)
    }
    document.addEventListener('mousemove', handler)
    document.addEventListener('mouseup', cleanup)
  }

  watch(activeWindows, (newVal) => {
  localStorage.setItem(WINDOW_STATES_KEY, JSON.stringify(newVal))
}, { deep: true })

// AI 状态
const aiStatus = reactive({
  pmStatus: 'idle',
  seStatus: 'idle',
  apStatus: 'idle',
  cRunning: false,
  currentTask: '',
  progress: ''
})

// IDE 连接状态（动态：{ "IDE-A": true, "IDE-B": true, ... }）
const ideConnected = reactive<Record<string, boolean>>({})

// C监控状态
const cMonitorEnabled = ref(true) // 默认开启
const projectState = ref('idle') // idle, running, error, done
const projectLevel = ref('') // short-process / normal-process / full-process (项目重量)

// Git 状态计数
const gitStatusCount = ref(0)

// 聊天消息
const messages = ref<Array<{
  role: string
  content: string
  summary?: string
  description?: string
  changes?: Array<{type: string, file: string}>
  codeBlocks?: Array<{language: string, code: string}>
  error?: string
  sections?: Array<{type: string, label: string, content: string}>
}>>([])

const aiThinking = ref(false)
const supportsMultimodal = ref(false)

// [3.4] Agent思考链事件（Dashboard可视化）
const thoughtEvents = ref<Array<{
  type: string
  role: string
  content: string
  timestamp: number
  meta?: Record<string, any>
}>>([])

const seenMsgIds = new Set<number>()
const streamingRole = ref('')

// [v1.0.24] 缓冲 terminal:output 事件，等 PM/SE 消息到达时附加为 sections
const pendingSections = ref<Array<{type: string, label: string, content: string}>>([])

// [G60] 前端收水记录（用于前后端一致性校验）
let recordReceiveCounter = 0
function recordReceive(role: string, messageId: string, content: string, source: string) {
  recordReceiveCounter++
  if (recordReceiveCounter <= 500) {
    try {
      ;(window as any).go.main.App.RecordReceive(role, messageId, content, source)
    } catch(e) { /* 静默失败 */ }
  }
}

// [G63] MessageBus: 自动ACK确认收到消息
function ackMessage(msgId: string) {
  if (!msgId) return
  try {
    ;(window as any).go.main.App.AckMessage(msgId)
  } catch(e) { /* 静默失败 */ }
}
;(window as any).__argusAck = ackMessage
const pendingChanges = ref<Array<{type: string, file: string}>>([])
const showSettings = ref(false)

// 未完成任务恢复相关
const unfinishedTask = ref<{
  hasUnfinished: boolean
  userRequest: string
  taskDescription: string
}>({
  hasUnfinished: false,
  userRequest: '',
  taskDescription: ''
})
const showRecoveryDialog = ref(false)

// Diff 预览
const showDiffDialog = ref(false)
const diffData = ref<DiffData | null>(null)

// 项目目录
const workDir = ref('')
const recentProjects = ref<string[]>([])

const currentFile = ref(null)
const terminalLogs = ref(['Argus 已启动'])
const changeHistory = ref([])
const config = ref({
  apiConfigs: [],
  showCodeBlocks: true,
  showThinking: true,
  pmDecisionAlert: false
})

watch(messages, (newVal, oldVal) => {
  // 消息变化监听（静默）
}, { deep: true })

// 加载配置和消息
onMounted(async () => {
  // [v0.7.3] Context events handled by TokenMonitor child component

  // 监听项目状态变更事件（后端产生 → 必须ACK）
  EventsOn('project-state-changed', (data: { state?: string; _msgId?: string; data?: any }) => {
    // [🔴追踪] 确认收到后端状态更新
    ackMessage(data._msgId || '')

    // 兼容后端发送的两种格式：对象 {state:"done"} 或原始值被包装为 {data:"done"}
    const state = data.state || data.data || (typeof data === 'string' ? data : null)
    projectState.value = state
    if (state === 'done') {
      aiThinking.value = false
      if (config.value.pmDecisionAlert) {
        showSystemTrayNotification(t('app.notificationTaskDoneTitle'), t('app.notificationTaskDoneBody'))
        playNotificationSound()
      }
    } else if (state === 'error') {
      aiThinking.value = false
      if (config.value.pmDecisionAlert) {
        playUrgentSound()
        showSystemTrayNotification(t('app.notificationTaskErrorTitle'), t('app.notificationTaskErrorBody'))
      }
    }
  })

  // [v0.8.1] 监听项目级别变更事件（后端 Bridge 推送）
  EventsOn('project-level', (data: string | { data?: string; _msgId?: string }) => {
    const raw = typeof data === 'string' ? { _msgId: undefined, data } : data
    if (raw?._msgId) ackMessage(raw._msgId)
    const level = typeof data === 'string' ? data : (data.data || '')
    if (level) {
      projectLevel.value = level
    }
  })

  // [v0.8.1] 全局 ACK git 事件 — GitWindow 关闭时没人监听会导致 message_lost 报警
  EventsOff('git:repo-info')
  EventsOn('git:repo-info', (raw: any) => {
    if (raw?._msgId) ackMessage(raw._msgId)
  })
  EventsOff('git:status')
  EventsOn('git:status', (raw: any) => {
    if (raw?._msgId) ackMessage(raw._msgId)
  })
  // [v0.9.0] 全局 ACK PathStatus 事件 — 组件未挂载时无人 ACK 导致 message_lost
  EventsOff('token_stats')
  EventsOn('token_stats', (raw: any) => { if (raw?._msgId) ackMessage(raw._msgId) })
  EventsOff('role-status')
  EventsOn('role-status', (raw: any) => { if (raw?._msgId) ackMessage(raw._msgId) })
  EventsOff('context_built')
  EventsOn('context_built', (raw: any) => { if (raw?._msgId) ackMessage(raw._msgId) })
  EventsOff('compress_done')
  EventsOn('compress_done', (raw: any) => { if (raw?._msgId) ackMessage(raw._msgId) })

  // [v1.0.24] 缓冲 terminal:output 事件 + ACK
  // 同时缓冲内容到 pendingSections，等 PM/SE 消息到达时附加为 sections
  // ⚠️ NO EventsOff here! Child component (TerminalWindow) onMounted runs before parent App.vue,
  // so EventsOff would remove TerminalWindow's handleOutput handler, causing terminal to go silent.
  EventsOn('terminal:output', (raw: any) => {
    const content = typeof raw === 'string' ? raw : (raw?.data || raw?.delta || '')
    console.log('[DEBUG-terminal:output] raw=', raw, 'content=', content)
    if (content) {
      pendingSections.value.push({
        type: 'terminal',
        label: '▶ Exec',
        content
      })
      console.log('[DEBUG-pendingSections] now=', pendingSections.value.length)
    }
    if (raw?._msgId) ackMessage(raw._msgId)
  })

  // [v0.7.3] Context Management events moved into onMounted (Wails runtime must be ready)
  // Registered below in onMounted block

  // 监听新消息事件（来自后端）
  EventsOff('new-message')
  EventsOn('new-message', (msg: { id?: number; role: string; content: string; raw?: string; timestamp?: number | string; _msgId?: string }) => {

    // [G63] 自动ACK
    ackMessage((msg as any)._msgId || '')

    // 第1层去重：按消息ID（精确匹配）
    if (msg.id != null) {
      if (seenMsgIds.has(msg.id)) {
        return
      }
      seenMsgIds.add(msg.id)
    }

    // 第2层去重：同角色最后一条消息内容比对（跳过 user，用户可能重复输入相同内容）
    if (msg.role !== 'user' && msg.role !== 'se' && msg.role !== 'ap') {
      const lastSameRole = [...messages.value].reverse().find(m => m.role === msg.role)
      if (lastSameRole && (lastSameRole.content || '').trim() === (msg.content || '').trim()) {
        return
      }
    }

    if (streamingRole.value === msg.role || msg.role === 'se' || msg.role === 'ap') {
      // [G59] SE特殊处理：优先查找已有_execData的操作卡片
      if (msg.role === 'se') {
        const existingExecCard = [...messages.value].reverse().find(m =>
          m.role === 'se' && (m as any)._execData
        )
        if (existingExecCard) {
          existingExecCard.content = msg.content
          existingExecCard.raw = msg.raw
          if (msg.timestamp) existingExecCard.timestamp = msg.timestamp
          delete (existingExecCard as any)._streaming
          return
        }
        // [v0.9.2] SE消息无_execData时静默丢弃（SSE事件不应变chat气泡）
        return
      }

      const lastMsgs = messages.value.slice(-3).reverse()
      const streamingIdx = lastMsgs.findIndex(m =>
        m.role === msg.role && ((m as any)._streaming || (m as any).content === msg.content)
      )
      if (streamingIdx !== -1) {
        const actualIdx = messages.value.length - 1 - streamingIdx
        messages.value[actualIdx].content = msg.content
        messages.value[actualIdx].raw = msg.raw
        if (msg.timestamp) messages.value[actualIdx].timestamp = msg.timestamp
        delete (messages.value[actualIdx] as any)._streaming
        streamingRole.value = ''
        aiThinking.value = false
      }
    } else if (msg.role !== 'user') {
      const lastSame = messages.value.findLast(m => m.role === msg.role)
      if (lastSame && (lastSame.content || '').trim() === (msg.content || '').trim()) {
        return
      }
      const pending = pendingSections.value.length
      const sections = pending > 0 ? pendingSections.value.splice(0) : undefined
      console.log('[DEBUG-new-message] role=', msg.role, 'pending=', pending, 'attached=', sections?.length)
      messages.value.push({
        role: msg.role,
        content: msg.content,
        raw: msg.raw,
        timestamp: msg.timestamp,
        sections
      })
    } else {
      // 用户消息不按内容去重 — 相同的"再运行一次"可能输入多次
      messages.value.push({
        role: msg.role,
        content: msg.content,
        raw: msg.raw,
        timestamp: msg.timestamp
      })
      // 新对话轮次，清空上一轮的终端输出缓存
      pendingSections.value = []
    }

    if (msg.role !== 'user') {
      playNotificationSound()
      if ((msg.role === 'pm' || msg.role === 'ap') && msg.content.includes('@USR') && config.value.pmDecisionAlert) {
        const cleanContent = msg.content.replace(/@USR\s*/, '')
        if (isPMNeedsDecision(cleanContent)) {
          playUrgentSound()
          showSystemTrayNotification(msg.role === 'ap' ? '✅ AP 审批结果' : '🚨 PM 需要您决策', cleanContent.substring(0, 80))
        }
      }
    }
    // [G60] 记录收水
    recordReceive(msg.role, (msg.id || 'newmsg_') + '_' + Date.now(), msg.content, 'new-message')
  })

  // PM/AP/SE messages all arrive via new-message event — no separate handlers needed

  // 监听消息清空事件（来自后端ClearMessages/ResetRoleStatus）- 后端产生 → 必须ACK
  EventsOn('messages-cleared', (data?: { _msgId?: string }) => {
    console.log('[PROBE] 🗑️ messages-cleared! before:', messages.value.length, 'msgId:', data?._msgId)
    // [🔴追踪] 确认收到清空指令
    ackMessage(data?._msgId || '')

    messages.value = []
    seenMsgIds.clear()
    streamingRole.value = ''
    thoughtEvents.value = []
    console.log('[PROBE] 🗑️ cleared done! after: 0')
  })

  // 📋 [TODO-SYNC] 监听Todo更新事件（Message Bus驱动）- 后端产生 → 必须ACK
  EventsOn('todo_update', (data: any) => {
    // [🔴追踪] 确认收到Todo更新
    ackMessage(data._msgId || '')

    console.log('[App.vue📋TODO] Received raw data:', data)
    console.log('[App.vue📋TODO] Data type:', typeof data, Array.isArray(data))

    // 转发给TopBar（通过全局事件或直接操作）
    if (window.__argusTodoUpdate) {
      window.__argusTodoUpdate(data)
    }
  })

  // Execution events (exec_start/done/output/completed) removed — no longer needed

  const seenLostMsgIds = new Set<string>()

  // [G63] MessageBus: 监听消息丢失事件（后端检测到超时未ACK）
  EventsOn('message_lost', (data: { msgId: string; role: string; event: string; path: string; source: string; elapsedSec: number; isNewLoss?: boolean; direction?: string; contentPreview?: string; contentLen?: number }) => {
    console.error(`[🚨MSG] 消息丢失! id=${data.msgId} role=${data.role} path=${data.path} source=${data.source} 等待${data.elapsedSec?.toFixed(1)}s`)

    if (seenLostMsgIds.has(data.msgId)) return
    seenLostMsgIds.add(data.msgId)

    const errorMsg = `🚨 **消息丢失** (${data.event?.toUpperCase()}/${data.event})
方向: ${data.direction || '未知'}
发送者: ${data.source}
路径: ${data.path}
等待: ${data.elapsedSec?.toFixed(1)}s
大小: ${data.contentLen || 0} bytes
预览: ${data.contentPreview?.substring(0, 60) || '(无)'}`

    messages.value.push({
      id: Date.now(),
      role: 'system',
      content: errorMsg,
      timestamp: Math.floor(Date.now() / 1000),
      _isError: true
    })

    if (config.value.pmDecisionAlert) {
      showSystemTrayNotification('🚨 消息丢失', `${data.role}消息可能未送达 (${data.elapsedSec?.toFixed(1)}s超时)`)
      playNotificationSound()
    }
  })

  // [FIX-20260529] SE互斥警告：当SE正在执行时拒绝新调用
  EventsOn('warning', (data: { from?: string; blockedTask?: string; _msgId?: string; [key: string]: any }) => {
    console.warn('[⚠️WARNING]', data)
    
    // [G63] 自动ACK（重要！否则MessageBus会标记为丢失）
    ackMessage(data._msgId || '')
    
    const warningMsg = data.blockedTask || data.message || 'SE正在执行任务中'
    const from = data.from || 'system'
    
    messages.value.push({
      id: Date.now(),
      role: 'system',
      content: `⚠️ **SE正在执行任务中，请等待完成后再发送新指令。**\n\n当前任务: ${warningMsg}\n来源: ${from}`,
      timestamp: Math.floor(Date.now() / 1000),
      _isWarning: true
    })
    
    if (config.value.pmDecisionAlert) {
      showSystemTrayNotification('⚠️ SE忙', 'SE正在执行任务中，请等待完成')
      playNotificationSound()
    }
  })

  // [V2-LabVIEW] 结构化角色状态（MessageBus RoleState → 前面板投影）
  EventsOn('role-state', (data: { _msgId?: string; phase?: string; pm?: string; se?: string; ap?: string; mc?: boolean; task?: string; [key: string]: any }) => {
    ackMessage(data._msgId || '')
    console.log('[LabVIEW-State]', JSON.stringify(data))

    if (data.pm) aiStatus.pmStatus = data.pm
    if (data.se) aiStatus.seStatus = data.se
    if (data.ap) aiStatus.apStatus = data.ap
    if (data.mc !== undefined) aiStatus.cRunning = data.mc
    if (data.task) aiStatus.currentTask = data.task
    if (data.phase) {
      aiStatus.progress = data.phase
      aiThinking.value = ['pm', 'se', 'ap', 'review', 'approve'].includes(data.phase)
    }
    ;(window as any).__stateUpdated?.()
  })

  // IDE连接状态（动态列表，来自SSEBridge的变更通知）
  EventsOn('ide_status', (data: { _msgId?: string; ides?: string[]; [key: string]: any }) => {
    ackMessage(data._msgId || '')
    const active = new Set(data.ides || [])
    // 保留已连接的，移除已断开的
    for (const key of Object.keys(ideConnected)) {
      if (!active.has(key)) delete ideConnected[key]
    }
    for (const name of active) {
      ideConnected[name] = true
    }
  })

  // role-status deprecated (role-state covers it)

  EventsOn('error', (data: { error?: string; stage?: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    console.error('[ERROR]', data)
    messages.value.push({
      id: Date.now(),
      role: 'system',
      content: `❌ **错误** (${data.stage || 'unknown'})\n${data.error || '未知错误'}`,
      timestamp: Math.floor(Date.now() / 1000),
      _isError: true
    })
  })

  // pm_streaming_done removed — covered by new-message handler

  EventsOn('ai-thinking', (data: boolean | { _msgId?: string }) => {
    const msgId = (data as any)?._msgId
    if (msgId) ackMessage(msgId)
    aiThinking.value = typeof data === 'boolean' ? data : !!data
  })

  // [3.4] Agent思考链 → Dashboard
  EventsOn('agent-thought', (raw: any) => {
    const msgId = raw?._msgId
    if (msgId) ackMessage(msgId)
    // MessageBus 可能传 JSON 字符串或已解析对象
    const data = typeof raw === 'string' ? JSON.parse(raw) : raw
    if (data && (data.type || data.content)) {
      thoughtEvents.value.push({
        type: data.type || 'unknown',
        role: data.role || 'unknown',
        content: data.content || '',
        timestamp: data.timestamp || Math.floor(Date.now() / 1000),
        meta: data.meta,
      })
      // 保留最近200条，防止内存泄漏
      if (thoughtEvents.value.length > 200) {
        thoughtEvents.value = thoughtEvents.value.slice(-150)
      }
    }
  })

  EventsOn('reset-completed', (data: { _msgId?: string }) => {
    ackMessage(data._msgId || '')
  })

  // [v0.7.2] 全局ACK后端事件（对话框=log，两车站一致）
  EventsOn('reset', (data: { _msgId?: string }) => {
    ackMessage(data._msgId || '')
  })
  EventsOn('tasks_cleared', (data: { _msgId?: string }) => {
    ackMessage(data._msgId || '')
  })
  EventsOn('done', (data: { _msgId?: string }) => {
    ackMessage(data._msgId || '')
  })

  // 监听 AP approved 事件，清空消息防止旧任务显示
  EventsOn('project_approved', (data: { timestamp: number; action: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    messages.value = []
    seenMsgIds.clear()
    streamingRole.value = ''
    thoughtEvents.value = []
  })

  EventsOn('se-file-written', (data: string | { _msgId?: string; path?: string }) => {
    const msgId = (data as any)?._msgId
    if (msgId) ackMessage(msgId)
  })

  // 监听任务恢复事件（记忆持久化功能）
  EventsOn('task-recovered', async (data: { userRequest: string; taskDescription: string; messageCount: number; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    if (config.value.pmDecisionAlert) {
      showSystemTrayNotification(
        '🔄 发现未完成任务',
        `已自动恢复：${data.taskDescription.substring(0, 50)}... (${data.messageCount}条消息)`
      )
      playNotificationSound()
    }

    // 加载恢复的消息到界面
    await loadMessages()

    if (confirm(`检测到未完成任务：\n\n${data.userRequest}\n\n已恢复 ${data.messageCount} 条消息。\n\n是否继续执行此任务？`)) {
      try {
        await SendMessage('继续执行之前的任务')
      } catch (err) {
        console.error('[TaskRecovered] ❌ 发送继续指令失败:', err)
      }
    } else {
      // 用户选择不继续
    }
  })

  
  try {
    const loadedConfig = await GetConfig()
    config.value = {
      apiConfigs: loadedConfig.apiConfigs || [],
      imConfigs: loadedConfig.imConfigs || [],
      showCodeBlocks: loadedConfig.showCodeBlocks ?? true,
      showThinking: loadedConfig.showThinking ?? true,
      pmDecisionAlert: loadedConfig.pmDecisionAlert ?? false,
      apEnabled: loadedConfig.apEnabled ?? false,
      apConfig: loadedConfig.apConfig || null,
      http: loadedConfig.http || { enabled: false, port: 8080, apiToken: '', allowRemote: false }
    }
    
    // 加载工作目录
    workDir.value = await GetWorkDir()
    recentProjects.value = await GetRecentProjects()
    
    // 加载多模态状态
    updateMultimodalStatus(loadedConfig)
    
    // 加载历史消息
    await loadMessages()
    
    // ✅ 检查是否有未完成任务（记忆恢复功能）
    try {
      const { CheckUnfinishedTask } = await import('../wailsjs/go/main/App')
      const [hasTask, userRequest, taskDescription] = await CheckUnfinishedTask()
      if (hasTask) {
        unfinishedTask.value = {
          hasUnfinished: true,
          userRequest: userRequest,
          taskDescription: taskDescription
        }
        showRecoveryDialog.value = true
      }
    } catch (e) {
      console.error('[Recovery] 检查未完成任务失败:', e)
    }
    
    // 同步后端 C监控 状态（后端可能还在初始化）
    try {
      const isCRunning = await IsCRunning()
      cMonitorEnabled.value = isCRunning
    } catch (e) {
      console.error(t('app.syncCMonitorFailed'), e)
    }
    
    // 加载日志
    try {
      const logs = await GetLogs()
      if (logs && logs.length > 0) {
        terminalLogs.value = logs
      }
    } catch (e) {
      console.error(t('app.loadLogsFailed'), e)
    }
    
    // 加载改动历史
    try {
      const history = await GetChangeHistory()
      if (history && history.length > 0) {
        changeHistory.value = history
      }
    } catch (e) {
      console.error(t('app.loadHistoryFailed'), e)
    }
  } catch (e) {
    console.error(t('app.loadConfigFailed'), e)
  }
  
  // LabVIEW式状态同步：统一使用MessageBus事件驱动（单线程模式可靠）
  // 移除定时轮询：避免与role-state事件冲突导致状态闪烁/多灯同亮
  // 后端通过emitStatus保证每个角色状态变更都会推送事件

  // 暴露给 role-state 事件更新时间戳（保留用于调试）
  ;(window as any).__stateUpdated = () => { /* 事件驱动模式，无需stale检测 */ }

  // 定期刷新 Git 状态计数
  setInterval(async () => {
    try {
      const { GetGitStatus } = await import('../wailsjs/go/main/App')
      const entries = await GetGitStatus()
      gitStatusCount.value = Array.isArray(entries) ? entries.length : 0
    } catch {
      gitStatusCount.value = 0
    }
  }, 3000)

  // 监听窗口可见性变化（最小化恢复时修正位置）
  const handleVisibilityChange = () => {
    if (!document.hidden) {
      FixPosition()
    }
  }
  document.addEventListener('visibilitychange', handleVisibilityChange)

  // 监听 Diff 预览事件（后端 write/edit 前推送）
  EventsOn('diff_preview', (data: DiffData & { _msgId?: string }) => {
    ackMessage(data._msgId || '')
    console.log('[DiffPreview] 收到预览:', data.type, data.path)
    diffData.value = { type: data.type, path: data.path, diff: data.diff, action: data.action }
    showDiffDialog.value = true
  })
})

// 更新项目状态指示灯
function updateProjectState(pmBusy: boolean, cRunning: boolean, seBusy: boolean) {
  // 监控始终开着，根据是否有任务判断状态
  // 优先级：error > running > done > idle
  
  // 检查最近的消息判断状态
  const lastMessages = messages.value.slice(-5)
  // 错误检测：只检测 PM/SE 的消息，排除 MC 的系统消息
  const hasError = lastMessages.some(m => {
    // 只检查 PM 和 SE 的消息
    if (m.role !== 'pm' && m.role !== 'se') return false
    if (m.error) return true
    if (!m.content) return false
    // 检测明确错误词
    const content = m.content
    return content.includes('出错') || content.includes('执行失败') || 
           content.includes('报错') || content.includes('异常') ||
           content.includes('失败')
  })
  // 完成检测：只检测 PM 的消息
  const hasDone = lastMessages.some(m => {
    if (m.role !== 'pm') return false
    return m.content?.includes('任务完成') || m.content?.includes('已完成')
  })
  const isWorking = pmBusy || seBusy
  
  if (hasError) {
    projectState.value = 'error'
  } else if (isWorking) {
    projectState.value = 'running'
  } else if (hasDone) {
    projectState.value = 'done'  // 任务完成
  } else {
    projectState.value = 'idle'  // 没任务，初始状态
  }
}

async function loadMessages() {
  try {
    const msgs = await GetMessages()
    messages.value = msgs
    msgs.forEach((m: any) => { if (m.id != null) seenMsgIds.add(m.id) })
  } catch (e) {
    console.error(t('app.loadMessagesFailed'), e)
  }
}

function toggleWindow(windowName: string) {
  activeWindows[windowName] = !activeWindows[windowName]
}

// 切换C监控
async function toggleCMonitor() {
  try {
    if (cMonitorEnabled.value) {
      // 停止监控
      try {
        await StopCMonitor()
      } catch (e: any) {
      }
      cMonitorEnabled.value = false
    } else {
      // 启动监控，检查状态
      await StartCMonitor()
      cMonitorEnabled.value = true
      // 如果状态是error，弹窗提示
      if (projectState.value === 'error') {
        alert(t('app.monitorStartedError'))
      }
    }
  } catch (e: any) {
    console.error(t('app.switchMonitorFailed'), e)
    const errorMsg = e?.message || e?.toString() || '未知错误'
    alert(t('app.switchMonitorFailed') + errorMsg)
  }
}

async function handleUploadFile() {
  try {
    const filePath = await OpenFileDialog()
    if (!filePath) return
    const content = await ReadFile(filePath)
    const fileName = filePath.split(/[\\/]/).pop() || filePath
    const msg = `[文件] ${fileName}\n\`\`\`\n${content}\n\`\`\``
    await SendMessage(msg)
  } catch (e: any) {
    console.error(t('app.uploadFileFailed'), e)
    alert(t('app.uploadFileFailed') + e.message)
  }
}

function updateMultimodalStatus(cfg: any) {
  const apiConfigs = cfg?.apiConfigs || []
  const defaultCfg = apiConfigs.find((c: any) => c.isDefault) || apiConfigs[0]
  supportsMultimodal.value = defaultCfg?.supportsMultimodal || false
}

function onAPIConfigChanged(newConfig: any) {
  if (newConfig) {
    supportsMultimodal.value = newConfig.supportsMultimodal || false
  }
}

async function handleSendMessage(msg: string) {
  try {
    if (msg === '') {
      await StopCurrentTask()
    } else {
      await SendMessage(msg)
    }
  } catch (e: any) {
    console.error(t('app.sendFailed'), e)
    alert(t('app.sendFailed'))
  }
}

async function handleSelectProject(dir: string) {
  try {
    if (dir === 'browse') {
      const selected = await OpenFolderDialog()
      if (selected) {
        await SetWorkDir(selected)
        workDir.value = selected
        recentProjects.value = await GetRecentProjects()
      }
    } else if (dir === '') {
      workDir.value = ''
    } else {
      await SetWorkDir(dir)
      workDir.value = dir
      recentProjects.value = await GetRecentProjects()
    }
  } catch (e) {
    console.error(t('app.switchProjectFailed'), e)
    alert(t('app.switchProjectFailed') + e)
  }
}

function toggleThinking() {
  // 展开/折叠思考过程
}

function acceptChanges() {
  pendingChanges.value = []
}

function rejectChanges() {
  pendingChanges.value = []
}

// Diff 预览处理
function onDiffApprove(data: DiffData) {
  console.log('[DiffPreview] 用户批准:', data.path)
  showDiffDialog.value = false
  // 后端已推送diff时继续执行（write/edit已在diff之后执行）
  // 如果未来需要后端等待确认，这里发确认事件
  EventsEmit('diff_confirmed', { path: data.path, approved: true })
}

function onDiffReject(data: DiffData) {
  console.log('[DiffPreview] 用户拒绝:', data.path)
  showDiffDialog.value = false
  EventsEmit('diff_confirmed', { path: data.path, approved: false })
}

function handleAdjust() {
  // 弹出调整输入
}

function handleSaveConfig(newConfig: any) {
  config.value = newConfig
  SaveConfig(newConfig)
}

async function handleReset() {
  if (!confirm(t('app.confirmReset') || '确定要复位吗？当前任务和历史消息将被清空。')) {
    return
  }
  try {
    const { ExecuteReset } = await import('../wailsjs/go/main/App')
    await ExecuteReset('用户手动复位')
  } catch (err) {
    console.error('复位失败', err)
  }
  aiThinking.value = false
  projectState.value = 'idle'
  chatPanelRef.value?.resetState()
}

function openFile(file: any) {
  currentFile.value = file
  nextTick(() => {
    activeWindows.editor = true
  })
}

function openFileInEditor(filePath: string) {
  const fileName = filePath.split(/[/\\]/).pop() || 'untitled'
  openFile({ name: fileName, path: filePath })
}

// 全局任务追踪器
const globalTasks = ref<GlobalTask[]>([])

// 打开文件到编辑器
async function handleOpenFileInEditor(data: { path: string }) {
  console.log('[App] 打开文件到编辑器:', data.path)
  
  activeWindows.editor = true
  
  try {
    const content = await ReadFile(data.path)
    EventsEmit('editor:open-file', {
      path: data.path,
      content: content,
      timestamp: Math.floor(Date.now() / 1000)
    })
  } catch (error) {
    console.error('[App] 读取文件失败:', error)
  }
}

// 在终端执行命令
function handleRunInTerminal(data: { command: string }) {
  console.log('[App] 在终端执行:', data.command)
  
  activeWindows.terminal = true
  
  EventsEmit('terminal:execute', {
    command: data.command,
    timestamp: Math.floor(Date.now() / 1000)
  })
}

// 将文件路径添加到对话输入框
function handleAddToChat(path: string) {
  const sep = '\\'
  const dir = (workDir.value || '').replace(/\/+$/, '').replace(/\\+$/, '')
  const absPath = dir ? dir + sep + path.replace(/\//g, sep) : path
  chatPanelRef.value?.appendToInput(absPath)
}

// 处理二进制文件单击：在编辑器中显示提示
function handleSelectBinaryFile(item: any) {
  activeWindows.editor = true
  currentFile.value = {
    name: item.name || item.path?.split(/[\\/]/).pop(),
    path: item.path,
    _binary: true,
    _binaryError: `无法显示 "${item.name || item.path}"，因为它是二进制文件`
  }
}

// 恢复未完成任务
async function recoverTask() {
  try {
    const { RecoverTask } = await import('../wailsjs/go/main/App')
    const recoveredMessages = await RecoverTask()

    if (recoveredMessages && recoveredMessages.length > 0) {
      // ⚠️ G点35修复：恢复消息时必须重置 _streaming 状态，防止蓝块闪烁
      messages.value = recoveredMessages.map((msg: any) => ({
        role: msg.role,
        content: msg.content,
        timestamp: msg.timestamp,
        _streaming: false  // ← 强制重置流式状态
      }))
      if (config.value.pmDecisionAlert) {
        showSystemTrayNotification('✅ 任务已恢复', `恢复了 ${recoveredMessages.length} 条消息`)
      }
      LogPrint(`[RECOVERY] ✅ 恢复 ${recoveredMessages.length} 条消息，所有 _streaming 已重置为 false`)
    }

    showRecoveryDialog.value = false
    unfinishedTask.value.hasUnfinished = false
  } catch (e) {
    console.error('[Recovery] 恢复任务失败:', e)
    if (config.value.pmDecisionAlert) {
      showSystemTrayNotification('❌ 恢复失败', '无法恢复未完成任务')
    }
  }
}

// 忽略未完成任务（开始新任务）
function dismissTask() {
  showRecoveryDialog.value = false
  unfinishedTask.value.hasUnfinished = false
}

function playNotificationSound() {
  try {
    const ctx = new AudioContext()
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.connect(gain)
    gain.connect(ctx.destination)
    osc.frequency.value = 880
    osc.type = 'sine'
    gain.gain.setValueAtTime(0.15, ctx.currentTime)
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.2)
    osc.start(ctx.currentTime)
    osc.stop(ctx.currentTime + 0.2)
    setTimeout(() => ctx.close(), 300)
  } catch {}
}

const DECISION_KEYWORDS = [
  '无法', '不能', '无法继续', '无法完成',
  '失败', '错误', '出错',
  '请选择', '请决定', '请确认',
  '需要您', '需要用户', '请您',
  '方案', '选项', '建议',
  '超时', '中断', '阻塞',
  '权限', '不足', '冲突',
  '如何处理', '是否继续'
]

function isPMNeedsDecision(content: string): boolean {
  const lower = content.toLowerCase()
  return DECISION_KEYWORDS.some(keyword => lower.includes(keyword.toLowerCase()))
}

function playUrgentSound() {
  try {
    const ctx = new AudioContext()
    const t = ctx.currentTime
    const playBeep = (freq: number, start: number, dur: number) => {
      const o = ctx.createOscillator()
      const g = ctx.createGain()
      o.connect(g)
      g.connect(ctx.destination)
      o.frequency.value = freq
      o.type = 'sine'
      g.gain.setValueAtTime(0.2, t + start)
      g.gain.exponentialRampToValueAtTime(0.001, t + start + dur)
      o.start(t + start)
      o.stop(t + start + dur)
    }
    playBeep(880, 0, 0.15)
    playBeep(1100, 0.2, 0.25)
    setTimeout(() => ctx.close(), 600)
  } catch {}
}

function showSystemTrayNotification(title: string, body: string) {
  if ('Notification' in window && Notification.permission === 'granted') {
    new Notification(title, { body, icon: '' })
  } else if ('Notification' in window && Notification.permission !== 'denied') {
    Notification.requestPermission().then(perm => {
      if (perm === 'granted') new Notification(title, { body, icon: '' })
    })
  }
}

async function saveConfig(newConfig: any) {
  config.value = newConfig
  try {
    // 获取完整配置并保存
    const currentConfig = await GetConfig()
    await SaveConfig({
      ...currentConfig,
      apiConfigs: newConfig.apiConfigs || [],
      imConfigs: newConfig.imConfigs || [],
      showCodeBlocks: newConfig.showCodeBlocks ?? true,
      showThinking: newConfig.showThinking ?? true,
      pmDecisionAlert: newConfig.pmDecisionAlert ?? false,
      http: newConfig.http ?? currentConfig.http,
      apEnabled: newConfig.apEnabled ?? false,
      apConfig: newConfig.apConfig ?? null,
      useSeparateModels: newConfig.useSeparateModels ?? currentConfig.useSeparateModels,
      pmConfigId: newConfig.pmConfigId ?? currentConfig.pmConfigId,
      seConfigId: newConfig.seConfigId ?? currentConfig.seConfigId,
      apConfigId: newConfig.apConfigId ?? currentConfig.apConfigId,
    })
    alert('配置保存成功！')
    updateMultimodalStatus(newConfig)
  } catch (e) {
    console.error(t('app.saveConfigFailed'), e)
    alert(t('app.saveConfigFailed') + e)
  }
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

:root {
  --bg-primary: #1a1a1a;
  --bg-secondary: #252525;
  --bg-tertiary: #2d2d2d;
  --border-color: #3a3a3a;
  --text-primary: #e0e0e0;
  --text-secondary: #888;
  --accent: #4a9eff;
  --success: #4caf50;
  --warning: #ff9800;
  --error: #f44336;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: 14px;
  line-height: 1.5;
}

.argus-app {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 未完成任务恢复对话框 */
.recovery-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.8);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.recovery-dialog {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 32px;
  max-width: 560px;
  width: 90%;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
  animation: slideUp 0.3s ease;
}

@keyframes slideUp {
  from { 
    opacity: 0;
    transform: translateY(30px);
  }
  to { 
    opacity: 1;
    transform: translateY(0);
  }
}

.recovery-icon {
  font-size: 48px;
  text-align: center;
  margin-bottom: 16px;
  animation: spin 2s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.recovery-title {
  font-size: 24px;
  font-weight: 600;
  text-align: center;
  color: var(--text-primary);
  margin-bottom: 24px;
}

.recovery-content {
  background: var(--bg-tertiary);
  border-radius: 8px;
  padding: 20px;
  margin-bottom: 24px;
}

.recovery-label {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.recovery-request {
  font-size: 15px;
  color: var(--accent);
  margin-bottom: 16px;
  line-height: 1.6;
  word-break: break-word;
}

.recovery-description {
  font-size: 14px;
  color: var(--text-primary);
  line-height: 1.6;
  word-break: break-word;
}

.recovery-actions {
  display: flex;
  gap: 12px;
  justify-content: center;
}

.recovery-actions .btn {
  padding: 12px 28px;
  font-size: 15px;
  font-weight: 500;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s ease;
  border: none;
  flex: 1;
}

.btn-primary {
  background: var(--accent);
  color: white;
}

.btn-primary:hover {
  background: #3a8eef;
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(74, 158, 255, 0.4);
}

.btn-secondary {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
}

.btn-secondary:hover {
  background: var(--border-color);
  color: var(--text-primary);
}

.main-content {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.file-tree-panel {
  flex-shrink: 0;
  border-right: 1px solid var(--border-color);
  overflow: hidden;
}

.chat-panel {
  flex-shrink: 0;
  min-width: 350px;
  max-width: 1000px;
}

.resize-handle {
  width: 6px;
  cursor: col-resize;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  transition: background 0.15s;
  flex-shrink: 0;
  z-index: 10;
}

.resize-handle:hover,
.resize-handle:active {
  background: rgba(99, 102, 241, 0.15);
}

.resize-line {
  width: 2px;
  height: 40px;
  background: var(--border-color);
  border-radius: 2px;
  transition: all 0.15s;
}

.resize-handle:hover .resize-line {
  background: #6366f1;
  height: 50px;
  box-shadow: 0 0 6px rgba(99, 102, 241, 0.3);
}

/* 右侧面板 */
.right-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-width: 300px;
}

.right-panel-top {
  overflow: hidden;
  min-height: 80px;
}

.right-panel-bottom {
  overflow: hidden;
  min-height: 80px;
}

.right-panel-divider {
  height: 4px;
  cursor: row-resize;
  background: transparent;
  flex-shrink: 0;
  transition: background 0.15s;
  z-index: 10;
}

.right-panel-divider:hover,
.right-panel-divider:active {
  background: rgba(99, 102, 241, 0.15);
}

.right-panel .editor-window,
.right-panel .terminal-window {
  position: relative !important;
  left: 0 !important;
  top: 0 !important;
  width: 100% !important;
  height: 100% !important;
  border: none !important;
  border-radius: 0 !important;
}

.right-panel .editor-window .window-header,
.right-panel .terminal-window .top-bar {
  cursor: default !important;
}

.right-panel .editor-window .win-btn,
.right-panel .terminal-window .win-btn {
  display: none !important;
}

/* [v0.7.2] 浮动面板 (Debug/MCP/Token) */
.floating-panel {
  position: fixed;
  top: 40px; /* TopBar height */
  right: 12px;
  width: 360px;
  max-height: calc(100vh - 60px);
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.4);
  z-index: 100;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.floating-panel.floating-wide {
  width: 480px;
}

.floating-panel.floating-narrow {
  width: 300px;
}

.panel-close {
  position: absolute;
  top: 8px; right: 8px;
  background: transparent; border: none; color: var(--text-secondary);
  font-size: 18px; cursor: pointer; z-index: 10;
  line-height: 1; padding: 2px 6px; border-radius: 4px;
}
.panel-close:hover { color: var(--error-color); background: var(--bg-tertiary); }
</style>
