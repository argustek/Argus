<template>
  <div class="argus-app">
    <!-- 顶部栏 -->
    <TopBar 
      :active-windows="activeWindows"
      :ai-status="aiStatus"
      :work-dir="workDir"
      :recent-projects="recentProjects"
      :c-monitor-enabled="cMonitorEnabled"
      :project-state="projectState"
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
      <!-- 左侧：对话区 -->
      <ChatPanel
        ref="chatPanelRef"
        :messages="messages"
        :ai-thinking="aiThinking"
        :supports-multimodal="supportsMultimodal"
        :thought-events="thoughtEvents"
        @send-message="handleSendMessage"
        @expand-thinking="toggleThinking"
        @upload-file="handleUploadFile"
        @open-file-in-editor="handleOpenFileInEditor"
        @run-in-terminal="handleRunInTerminal"
        class="chat-panel"
        :style="{ width: chatWidth + 'px' }"
      />

      <!-- 拖拽分隔条 -->
      <div
        class="resize-handle"
        @mousedown="startResize"
      >
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

  const chatWidth = ref(Number(localStorage.getItem('chatWidth')) || 640)
  const isResizing = ref(false)

  function startResize(e: MouseEvent) {
    isResizing.value = true
    const startWidth = chatWidth.value
    const handler = (ev: MouseEvent) => {
      if (!isResizing.value) return
      const delta = ev.clientX - e.clientX
      chatWidth.value = Math.max(350, Math.min(1000, startWidth + delta))
    }
    const cleanup = () => {
      isResizing.value = false
      localStorage.setItem('chatWidth', String(chatWidth.value))
      document.removeEventListener('mousemove', handler)
      document.removeEventListener('mouseup', cleanup)
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

// C监控状态
const cMonitorEnabled = ref(true) // 默认开启
const projectState = ref('idle') // idle, running, error, done

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
const receivedMessageIds = new Set<string>() // [G49] 已收到的消息ID（防止重复）

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
  // [DEBUG-20260529] 测试消息：验证前端渲染是否工作
  messages.value.push({
    id: Date.now(),
    role: 'system',
    content: '🧪 **前端渲染测试** - 如果您看到此消息，说明Vue渲染系统正常工作！\n\n时间: ' + new Date().toLocaleString(),
    timestamp: Math.floor(Date.now() / 1000)
  })
  console.log('[DEBUG] Test message added to messages array, count:', messages.value.length)

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

  // [v0.7.2] Context Management: 全局监听三个组件事件（防止TokenMonitor未打开时丢车）
  EventsOn('token_stats', (data: any) => {
    if (data?._msgId) ackMessage(data._msgId)
  })
  EventsOn('context_built', (data: any) => {
    if (data?._msgId) ackMessage(data._msgId)
  })
  EventsOn('compress_done', (data: any) => {
    if (data?._msgId) ackMessage(data._msgId)
  })

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

    // 第2层去重：同角色最后一条消息内容比对
    if (msg.role !== 'se' && msg.role !== 'ap') {
      const lastSameRole = [...messages.value].reverse().find(m => m.role === msg.role)
      if (lastSameRole && (lastSameRole.content || '').trim() === (msg.content || '').trim()) {
        return
      }
    }

    // PM消息由专用pm_message通道处理，new-message不再处理PM
    if (msg.role === 'pm') {
      return
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
      } else {
        messages.value.push({ role: msg.role, content: msg.content, raw: msg.raw, timestamp: msg.timestamp })
        streamingRole.value = msg.role
      }
    } else {
      const lastSame = messages.value.findLast(m => m.role === msg.role)
      if (lastSame && (lastSame.content || '').trim() === (msg.content || '').trim()) {
        return
      }
      messages.value.push({
        role: msg.role,
        content: msg.content,
        raw: msg.raw,
        timestamp: msg.timestamp
      })
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

  // [G57] 监听AI流式输出事件
  // ⚠️ 统一方案：PM/AP不再使用ai-stream-chunk（各自有专用通道）
  // - PM → pm_message (单一通道，像AP一样)
  // - SE → ai-stream-chunk + exec_start/exec_completed (无专用通道)
  // - AP → ap_message (单一通道)
  EventsOff('ai-stream-chunk')
  EventsOn('ai-stream-chunk', (data: { role: string; delta: string; messageId?: string; _msgId?: string }) => {
    const msgId = data._msgId || data.messageId || data.role + '_unknown'
    
    // [G63] 自动ACK
    ackMessage(data._msgId || '')
    
    // [G57+G63] PM/AP/SE忽略ai-stream-chunk
    // PM → pm_message专用通道
    // AP → ap_message专用通道  
    // SE → exec_start/exec_done/exec_completed卡片机制（流式JSON是噪音，不应显示）
    if (data.role === 'pm' || data.role === 'ap' || data.role === 'se') {
      return
    }
    
    if (!receivedMessageIds.has(msgId)) {
      receivedMessageIds.add(msgId)
      streamingRole.value = data.role
      messages.value.push({
        role: data.role,
        content: data.delta,
        _streaming: true,
        _messageId: msgId
      } as any)
      // [G60] 记录收水
      recordReceive(data.role, msgId, data.delta, 'ai-stream-chunk')
    } else {
      const lastMsg = [...messages.value].reverse().find(m => (m as any)._messageId === msgId)
      if (lastMsg) {
        lastMsg.content += data.delta
      }
    }
  })

  // [G57] 监听PM消息事件 - 统一为AP模式（单一通道，直接push）
  EventsOff('pm_message')
  EventsOn('pm_message', (data: { delta: string; _msgId?: string }) => {
    console.log('[PROBE] 📨 pm_message received:', data.delta?.substring(0, 50), 'msgId:', data._msgId)
    if (!data.delta) return

    // [G63] 自动ACK
    ackMessage(data._msgId || '')

    // [G57] 简单可靠：像AP一样直接push新消息
    const pmMsg = {
      role: 'pm',
      content: data.delta,
      timestamp: Date.now()
    } as any
    messages.value.push(pmMsg)
    console.log('[PROBE] ✅ PM pushed! total msgs:', messages.value.length)
    // [G60] 记录收水
    recordReceive('pm', data._msgId || 'pm_' + Date.now(), data.delta, 'pm_message')
  })

  // 监听AP消息事件（AP审批结果）
  EventsOff('ap_message')
  EventsOn('ap_message', (data: { delta: string; _msgId?: string }) => {
    if (!data.delta) return
    // [G64] AP消息去重：检查是否已存在相同内容的AP消息
    const lastApMsg = [...messages.value].reverse().find(m => m.role === 'ap')
    if (lastApMsg && (lastApMsg.content || '').trim() === (data.delta || '').trim()) {
      console.log('[G64] AP消息去重: 跳过重复内容')
      return
    }
    // [G63] 自动ACK
    ackMessage(data._msgId || '')
    messages.value.push({
      role: 'ap',
      content: data.delta,
      timestamp: Date.now()
    } as any)
    // [G60] 记录收水
    recordReceive('ap', data._msgId || 'ap_' + Date.now(), data.delta, 'ap_message')
  })

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

  // 监听SE执行事件（Trae风格：步骤列表+终端输出）
  let currentExecMsg: any = null
  interface ExecActionStep {
    index: number
    type: string
    label: string
    status: 'running' | 'done' | 'error' | 'blocked'
    error?: string
  }

  EventsOn('exec_start', (data: { executor: string; index: number; total: number; type: string; label: string; _msgId?: string }) => {
    // [G63] 自动ACK
    ackMessage(data._msgId || '')
    if (!currentExecMsg) {
      currentExecMsg = {
        role: data.executor === 'pm' ? 'pm' : 'se',
        content: '',  // 初始为空，后续填充
        timestamp: Date.now(),
        _streaming: true,
        _execData: {
          executor: data.executor,
          totalActions: data.total,
          actions: [] as ExecActionStep[],
          outputs: [] as { command: string; output: string; exitCode: number }[],
        },
      }
      messages.value.push(currentExecMsg)
    }
    currentExecMsg._execData.actions.push({
      index: data.index,
      type: data.type,
      label: data.label,
      status: 'running',
    })
  })

  EventsOn('exec_done', (data: { executor: string; index: number; type: string; label: string; status: string; error?: string; _msgId?: string }) => {
    // [G63] 自动ACK
    ackMessage(data._msgId || '')
    if (!currentExecMsg) return
    const step = currentExecMsg._execData.actions.find((a: ExecActionStep) => a.index === data.index)
    if (step) {
      step.status = data.status as ExecActionStep['status']
      if (data.error) step.error = data.error
    }
  })

  EventsOn('exec_output', (data: { executor: string; command: string; output: string; exit_code: number; blocked?: boolean; _msgId?: string }) => {
    // [G63] 自动ACK
    ackMessage(data._msgId || '')
    if (currentExecMsg) {
      currentExecMsg._execData.outputs.push({
        command: data.command || '',
        output: data.output,
        exitCode: data.exit_code,
      })
    }
  })

  EventsOn('exec_completed', (data: { _msgId?: string }) => {
    // [G63] 自动ACK
    ackMessage((data as any)._msgId || '')
    if (currentExecMsg) {
      currentExecMsg._streaming = false
      currentExecMsg = null
    }
    const seMsgs = messages.value.filter(m => m.role === 'se')
    for (const msg of seMsgs) {
      if ((msg as any)._streaming) {
        ;(msg as any)._streaming = false
      }
    }
  })

  EventsOn('pm_review_completed', (data: { taskId: string; status: string; result: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    const pmMsgs = messages.value.filter(m => m.role === 'pm')
    for (const msg of pmMsgs) {
      if ((msg as any)._streaming) {
        ;(msg as any)._streaming = false
      }
    }
    streamingRole.value = ''
    aiThinking.value = false
    // [G60] 任务阶段完成，输出一致性报告
    try {
      ;(window as any).go.main.App.GetConsistencyReport()
    } catch(e) { /* 静默 */ }
  })

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

  EventsOn('pm_started', (data: { _msgId?: string }) => {
    ackMessage(data._msgId || '')
    aiThinking.value = true
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

  // [Legacy] 兼容旧版字符串状态（逐步废弃）
  EventsOn('role-status', (data: { _msgId?: string; [key: string]: any }) => {
    ackMessage(data._msgId || '')
    const statusStr = typeof data === 'string' ? data : (data.delta || data.content || '')

    if (typeof statusStr === 'string' && statusStr.includes('role:')) {
      const roleMatch = statusStr.match(/role:(\w+)/)
      const statusMatch = statusStr.match(/status:(\w+)/)
      if (roleMatch && statusMatch) {
        const role = roleMatch[1]
        const status = statusMatch[1]
        if (role === 'pm') aiStatus.pmStatus = status === 'busy' ? 'busy' : 'idle'
        else if (role === 'se') aiStatus.seStatus = status === 'busy' ? 'busy' : 'idle'
        else if (role === 'ap') aiStatus.apStatus = status === 'busy' ? 'busy' : 'idle'
        else if (role === 'none' || status === 'error') { aiStatus.pmStatus = 'idle'; aiStatus.seStatus = 'idle'; aiStatus.apStatus = 'idle' }
      }
    }
  })

  EventsOn('se_task_assigned', (data: { task?: string; steps?: number; _msgId?: string }) => {
    ackMessage(data._msgId || '')
  })

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

  EventsOn('pm_streaming_done', (data: { content?: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')

    const pmMsgs = messages.value.filter(m => m.role === 'pm')
    for (const msg of pmMsgs) {
      if ((msg as any)._streaming) {
        ;(msg as any)._streaming = false
        
        // [G49] 清理已完成的messageId
        const msgId = (msg as any)._messageId
        if (msgId && receivedMessageIds.has(msgId)) {
          receivedMessageIds.delete(msgId)
        }
      }
    }
    streamingRole.value = ''
  })

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

  EventsOn('se-file-written', async (data: string | { _msgId?: string; path?: string }) => {
    const msgId = (data as any)?._msgId
    if (msgId) ackMessage(msgId)
    const relPath = typeof data === 'string' ? data : data?.path || ''
    try {
      const { OpenFileLocation } = await import('../wailsjs/go/main/App')
      await OpenFileLocation(relPath)
    } catch (e) {
      console.error('[SEFileWritten] 打开失败:', e)
    }
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

  // === 三层模型事件监听 ===
  EventsOn('tasklist_start', (data: { roleId: string; taskId: string; title: string; tasks: Array<{id:string;text:string;status:string}>; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    console.log('[RichMessage] tasklist_start:', data)
    window.__richMessages = window.__richMessages || {}
    window.__richMessages[data.taskId] = {
      id: data.taskId,
      role: data.roleId,
      title: data.title,
      taskList: { id: data.taskId, role: data.roleId, title: data.title, tasks: data.tasks, status: 'running', startedAt: Date.now() },
      shells: [],
      result: undefined
    }
    EventsEmit('rich-message-update', data.taskId)
  })
  
  EventsOn('tasklist_update', (data: { taskId: string; taskIndex: number; status: string; error?: string; detail?: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    const rm = window.__richMessages?.[data.taskId]
    if (rm?.taskList) {
      const t = rm.taskList.tasks[data.taskIndex]
      if (t) {
        t.status = data.status
        if (data.error) t.error = data.error
        if (data.detail) t.detail = data.detail
        if (data.status === 'running') t.startedAt = Date.now()
        if (data.status === 'done' || data.status === 'error') t.completedAt = Date.now()
      }
      EventsEmit('rich-message-update', data.taskId)
    }
  })

  EventsOn('tasklist_replace', (data: { taskId: string; tasks: Array<{id:string;text:string;status:string}>; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    const rm = window.__richMessages?.[data.taskId]
    if (rm?.taskList) {
      rm.taskList.tasks = data.tasks.map(t => ({ ...t }))
      EventsEmit('rich-message-update', data.taskId)
    }
  })

  EventsOn('tasklist_complete', (data: { taskId: string; status: string; result?: { text?: string }; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    const rm = window.__richMessages?.[data.taskId]
    if (rm) {
      rm.taskList.status = data.status
      rm.taskList.endedAt = Date.now()
      if (data.result) rm.result = data.result
      const lastRoleMsg = [...messages.value].reverse().find(m => m.role === rm.role && !(m as any)._richTaskId)
      if (lastRoleMsg) (lastRoleMsg as any)._richTaskId = data.taskId
      EventsEmit('rich-message-complete', data.taskId)
    }
  })

  EventsOn('shell_start', (data: { roleId: string; taskId: string; taskIndex: number; type: string; command: string; extra?: Record<string,string>; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    const rm = window.__richMessages?.[data.taskId]
    if (rm) {
      rm.shells.push({
        taskId: data.taskId, type: data.type as any, command: data.command,
        output: '', exitCode: 0, duration: '', status: 'running',
        timestamp: Date.now(), extra: data.extra
      })
      EventsEmit('rich-message-update', data.taskId)
    }
  })

  EventsOn('shell_output', (data: { taskId: string; output: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    const rm = window.__richMessages?.[data.taskId]
    if (rm && rm.shells.length > 0) {
      const lastShell = rm.shells[rm.shells.length - 1]
      lastShell.output += data.output
      EventsEmit('rich-message-update', data.taskId)
    }
  })

  EventsOn('shell_done', (data: { roleId: string; taskId: string; exitCode: number; duration: string; status: string; _msgId?: string }) => {
    ackMessage(data._msgId || '')
    console.log('[shell_done] 收到事件:', JSON.stringify(data))
    const rm = window.__richMessages?.[data.taskId]
    console.log('[shell_done] rm存在:', !!rm, 'shells数量:', rm?.shells?.length)
    if (rm && rm.shells.length > 0) {
      const lastShell = rm.shells[rm.shells.length - 1]
      lastShell.exitCode = data.exitCode
      lastShell.duration = data.duration
      lastShell.status = data.status as any
      EventsEmit('rich-message-update', data.taskId)
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
    if (dir === '') {
      // 清除工作目录
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

// 处理来自 SERichMessage 的文件打开请求（事件通信机制）
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

// 处理来自 SERichMessage 的命令执行请求（事件通信机制）
function handleRunInTerminal(data: { command: string }) {
  console.log('[App] 在终端执行:', data.command)
  
  activeWindows.terminal = true
  
  EventsEmit('terminal:execute', {
    command: data.command,
    timestamp: Math.floor(Date.now() / 1000)
  })
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
      pmConfigId: newConfig.pmConfigId ?? currentConfig.pmConfigID,
      seConfigId: newConfig.seConfigId ?? currentConfig.seConfigID,
      apConfigId: newConfig.apConfigId ?? currentConfig.apConfigID,
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
