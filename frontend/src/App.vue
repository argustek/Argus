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
      @clear-messages="handleClearMessages"
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
        @send-message="handleSendMessage"
        @expand-thinking="toggleThinking"
        @upload-file="handleUploadFile"
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
    
    <!-- 设置面板 -->
    <SettingsPanel
      v-if="showSettings"
      :config="config"
      @close="showSettings = false"
      @save="saveConfig"
      @api-config-changed="onAPIConfigChanged"
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
  git: savedWindowStates.git ?? false
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
  pmBusy: false,
  cBusy: false,
  seBusy: false,
  apBusy: false,
  currentTask: ''
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

const seenMsgIds = new Set<number>()
const streamingRole = ref('')
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
  if (newVal.length > (oldVal?.length || 0)) {
    const lastMsg = newVal[newVal.length - 1]
    LogPrint(`[FRONTEND-DISPLAY] 👁️ 实际显示: role="${lastMsg.role}" content="${(lastMsg.content||'').substring(0,80)}" 总消息数=${newVal.length}`)
  } else if (newVal.length < (oldVal?.length || 0)) {
    LogPrint(`[FRONTEND-DISPLAY] 🗑️ 消息删除: 从${oldVal?.length||0}条变为${newVal.length}条`)
  }
}, { deep: true })

// 加载配置和消息
onMounted(async () => {
  // 监听项目状态变更事件
  EventsOn('project-state-changed', (state: string) => {
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
      EventsEmit('pm-waiting-decision')
    }
  })

  // 监听新消息事件（来自后端）
  EventsOff('new-message')
  EventsOn('new-message', (msg: { id?: number; role: string; content: string; raw?: string; timestamp?: number | string }) => {

    // 第1层去重：按消息ID（精确匹配）
    if (msg.id != null) {
      if (seenMsgIds.has(msg.id)) {
        LogPrint(`[FRONTEND-SKIP] ❌ ID去重: id=${msg.id} role="${msg.role}"`)
        console.log(`[NEW-MSG] SKIP duplicate id=${msg.id}`)
        return
      }
      seenMsgIds.add(msg.id)
    }

    // 第2层去重：同角色最后一条消息内容比对（防止后端不同ID但同内容双发）
    const lastSameRole = [...messages.value].reverse().find(m => m.role === msg.role)
    if (lastSameRole && (lastSameRole.content || '').trim() === (msg.content || '').trim()) {
      LogPrint(`[FRONTEND-SKIP] ❌ 内容去重: role="${msg.role}"`)
      console.log(`[NEW-MSG] SKIP content-duplicate role=${msg.role}`)
      return
    }

    // ✅ 增强版流式消息替换逻辑：检查最近3条消息
    if (streamingRole.value === msg.role || msg.role === 'pm' || msg.role === 'se' || msg.role === 'ap') {
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
        LogPrint(`[FRONTEND-SKIP] ❌ 内容去重: role="${msg.role}"`)
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
          EventsEmit('pm-waiting-decision')
        }
      }
    }
  })

  // 监听AI流式输出事件
  EventsOff('ai-stream-chunk')
  EventsOn('ai-stream-chunk', (data: { role: string; delta: string }) => {
    if (streamingRole.value !== data.role) {
      streamingRole.value = data.role
      messages.value.push({
        role: data.role,
        content: data.delta,
        _streaming: true
      } as any)
    } else if (messages.value.length > 0) {
      const lastMsg = messages.value[messages.value.length - 1]
      if (lastMsg.role === data.role && (lastMsg as any)._streaming) {
        lastMsg.content += data.delta
      }
    }
  })

  // 监听PM消息事件（PM回复用户时触发）
  EventsOff('pm_message')
  EventsOn('pm_message', (data: { delta: string }) => {
    if (!data.delta) return
    if (streamingRole.value !== 'pm') {
      streamingRole.value = 'pm'
      messages.value.push({
        role: 'pm',
        content: data.delta,
        _streaming: true
      } as any)
    } else if (messages.value.length > 0) {
      const lastMsg = messages.value[messages.value.length - 1]
      if (lastMsg.role === 'pm' && (lastMsg as any)._streaming) {
        lastMsg.content += data.delta
      }
    }
  })

  // 监听消息清空事件（来自后端ClearMessages/ResetRoleStatus）
  EventsOn('messages-cleared', () => {
    messages.value = []
    seenMsgIds.clear()
    streamingRole.value = ''
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

  EventsOn('exec_start', (data: { executor: string; index: number; total: number; type: string; label: string }) => {
    if (!currentExecMsg) {
      currentExecMsg = {
        role: data.executor === 'pm' ? 'pm' : 'se',
        content: '',
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
      probeLogPush(4, 'PUSH', `exec role=${currentExecMsg.role} len=${messages.value.length}`)
    }
    currentExecMsg._execData.actions.push({
      index: data.index,
      type: data.type,
      label: data.label,
      status: 'running',
    })
  })

  EventsOn('exec_done', (data: { executor: string; index: number; type: string; label: string; status: string; error?: string }) => {
    if (!currentExecMsg) return
    const step = currentExecMsg._execData.actions.find((a: ExecActionStep) => a.index === data.index)
    if (step) {
      step.status = data.status as ExecActionStep['status']
      if (data.error) step.error = data.error
    }
  })

  EventsOn('exec_output', (data: { executor: string; command: string; output: string; exit_code: number; blocked?: boolean }) => {
    if (currentExecMsg) {
      currentExecMsg._execData.outputs.push({
        command: data.command || '',
        output: data.output,
        exitCode: data.exit_code,
      })
    }
  })

  EventsOn('exec_completed', () => {
    if (currentExecMsg) {
      currentExecMsg._streaming = false
      currentExecMsg = null
    }
  })

  EventsOn('se-file-written', async (relPath: string) => {
    try {
      const { OpenFileLocation } = await import('../wailsjs/go/main/App')
      await OpenFileLocation(relPath)
    } catch (e) {
      console.error('[SEFileWritten] 打开失败:', e)
    }
  })

  // 监听任务恢复事件（记忆持久化功能）
  EventsOn('task-recovered', async (data: { userRequest: string; taskDescription: string; messageCount: number }) => {
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
      apConfig: loadedConfig.apConfig || null
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
  
  // 定期检查 AI 状态和消息（不再使用事件推送，避免重复）
  setInterval(async () => {
    try {
      const pmThinking = await IsPMThinking()
      const cRunning = await IsCRunning()
      const seRunning = await IsSERunning()
      const apThinking = await IsAPThinking()

      aiStatus.pmBusy = pmThinking
      aiStatus.cBusy = cRunning
      aiStatus.seBusy = seRunning
      aiStatus.apBusy = apThinking

      // 同步 aiThinking 状态，确保发送按钮状态正确
      aiThinking.value = pmThinking || apThinking

      // 根据 AI 状态更新项目状态指示灯
      updateProjectState(pmThinking, cRunning, seBusy)

      // ✅ 移除定时器中的 loadMessages()
      // 原因：消息已经通过 new-message 事件实时推送，定时刷新会导致竞态条件和消息重复
      // 如需强制刷新，用户可以手动操作或切换工作目录
    } catch (e) {
      console.error(t('app.checkAIStatusFailed'), e)
    }
  }, 1000)

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

async function handleClearMessages() {
  try {
    const { ClearMessages } = await import('../wailsjs/go/main/App')
    await ClearMessages()
  } catch (err) {
    console.error(t('app.clearMessagesFailed'), err)
  }
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

// 恢复未完成任务
async function recoverTask() {
  try {
    const { RecoverTask } = await import('../wailsjs/go/main/App')
    const recoveredMessages = await RecoverTask()
    
    if (recoveredMessages && recoveredMessages.length > 0) {
      messages.value = recoveredMessages.map((msg: any) => ({
        role: msg.role,
        content: msg.content,
        timestamp: msg.timestamp
      }))
      if (config.value.pmDecisionAlert) {
        showSystemTrayNotification('✅ 任务已恢复', `恢复了 ${recoveredMessages.length} 条消息`)
      }
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
      apConfig: newConfig.apConfig ?? null
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
</style>
