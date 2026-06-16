<template>
  <div class="top-bar" @mousedown="startDrag" @click.prevent @dblclick.prevent>
    <div class="left-section">
      <button class="icon-btn" :class="{ active: activeWindows.fileTree }" @click.stop="openFileExplorer" :title="t('topBar.fileExplorer')">📂</button>
      <button class="icon-btn" :class="{ active: activeWindows.editor }" @click.stop="$emit('toggle-window', 'editor')" :title="t('topBar.codeEditor')">📝</button>
      <button class="icon-btn" :class="{ active: activeWindows.terminal }" @click.stop="$emit('toggle-window', 'terminal')" :title="t('topBar.terminal')">🖥️</button>
      <div class="divider"></div>
      
      <!-- 即时贴按钮 -->
      <button class="icon-btn" :class="{ active: stickyNoteVisible }" @click.stop="toggleStickyNote" :title="stickyNoteVisible ? '隐藏即时贴' : '显示即时贴'">📌</button>

      <div class="divider"></div>
      <button 
        class="icon-btn monitor-btn" 
        :class="{ active: cMonitorEnabled, error: projectState === 'error' }" 
        @click.stop="toggleCMonitor"
        :title="cMonitorEnabled ? t('topBar.monitorRunning') : t('topBar.monitorStopped')"
      >
        {{ cMonitorEnabled ? '🟢' : '🔴' }}
      </button>
      <div class="divider"></div>
      <button class="icon-btn" @click.stop="$emit('reset')" :title="t('common.reset')">↻</button>
      <button class="icon-btn search-btn" @click.stop="$emit('toggle-search')" :title="t('topBar.searchChat') + ' (Ctrl+F)'">🔍</button>
      <button class="icon-btn git-btn" @click.stop="$emit('toggle-git')" :title="t('topBar.gitVersionControl')">
        🌿 <span v-if="gitStatusCount > 0" class="git-badge">{{ gitStatusCount }}</span>
      </button>
      <!-- [v0.7.2] 新增工具面板入口 -->
      <button class="icon-btn" @click.stop="$emit('toggle-window', 'debug')" :class="{ active: activeWindows.debug }" title="Debugger 调试器">🐛</button>
      <button class="icon-btn" @click.stop="$emit('toggle-window', 'mcp')" :class="{ active: activeWindows.mcp }" title="MCP 工具协议">🔌</button>
      <button class="icon-btn" @click.stop="$emit('toggle-window', 'token')" :class="{ active: activeWindows.token }" title="Token 监控">📊</button>
      <div class="divider"></div>
      
      <div class="workdir-selector" @mousedown.stop>
        <span v-if="!workDir || workDir.length === 0" class="workdir-text">{{ t('topBar.workDir') }}</span>
        <span v-else class="workdir-text">{{ workDir?.split?.(/[\\/]/).pop() || workDir }}</span>
        <button class="arrow-btn" @click.prevent="toggleMenu">▼</button>
        
        <div v-if="showMenu" class="menu-dropdown">
          <div class="menu-item" @click="openFolder">
            <span class="menu-icon">📁</span>
            <span>{{ t('topBar.setWorkDir') }}</span>
          </div>
          <div class="menu-item" @click="connectRemote">
            <span class="menu-icon">🖥️</span>
            <span>{{ t('topBar.connectRemote') }}</span>
          </div>
          
          <template v-if="workDir">
            <div class="menu-divider"></div>
            <div class="menu-item" @click="openWorkDirInExplorer">
              <span class="menu-icon">📂</span>
              <span>{{ t('topBar.openInExplorer') }}</span>
            </div>
            <div class="menu-item" @click="clearWorkDir">
              <span class="menu-icon">🗑️</span>
              <span>{{ t('topBar.clearWorkDir') }}</span>
            </div>
          </template>
          
          <template v-if="recentProjects && recentProjects.length > 0">
            <div class="menu-divider"></div>
            <div class="menu-header">{{ t('topBar.recent') }}</div>
            <div v-for="(project, index) in recentProjects" :key="index" class="menu-item" :class="{ active: project === workDir }" @click="selectProject(project)">
              <div class="project-badge" :style="{ background: getColor(project) }">{{ getInitials(project) }}</div>
              <div class="project-info">
                <div class="project-name">{{ getName(project) }}</div>
                <div class="project-path">{{ project }}</div>
              </div>
              <span v-if="project === workDir" class="check">✓</span>
            </div>
          </template>
        </div>
      </div>
    </div>
    
    <div class="right-section">
      <span v-if="projectLevel" class="project-level-badge" :class="'level-' + projectLevel" :title="'Project Level: ' + projectLevel">[{{ projectLevel }}]</span>
      <div class="project-status-indicator" :class="projectStatusClass || 'idle'" :title="projectStatusText">
        <span class="status-light"></span>
        <span class="status-label">{{ projectStatusLabel }}</span>
      </div>
      <div class="divider"></div>
      <div class="ai-status">
        <span class="status-item pm" :class="{ busy: aiStatus.pmStatus === 'busy' }" title="PM">PM</span>
        <span class="status-item mc" :class="{ 'mc-running': aiStatus.cRunning }" title="MC">MC</span>
        <span class="status-item se" :class="{ busy: aiStatus.seStatus === 'busy' }" title="SE">SE</span>
        <span class="status-item ap" :class="{ busy: aiStatus.apStatus === 'busy' }" title="AP 审批者">AP</span>
      </div>
      <div class="divider"></div>
      <div class="ide-status">
        <span v-for="(_, name) in ideConnected" :key="name" class="ide-indicator connected" :title="name">{{ name.slice(-1) }}</span>
      </div>
      <button class="icon-btn" @click.stop="$emit('open-settings')" :title="t('topBar.settings')">⚙️</button>
      <div class="lang-selector" @mousedown.stop>
        <button class="icon-btn lang-btn" @click.prevent="toggleLangMenu" :title="t('topBar.switchLanguage')">
          {{ currentLocaleLabel }}
        </button>
        <div v-if="showLangMenu" class="lang-dropdown">
          <div v-for="loc in availableLocales" :key="loc.code"
            :class="['lang-item', { active: loc.code === locale }]"
            @click="changeLang(loc.code)">
            {{ loc.label }}
          </div>
        </div>
      </div>
      <div class="divider"></div>
      <button class="window-btn" @click.stop="minimizeWindow" :title="t('topBar.minimize')">─</button>
      <button class="window-btn" @click.stop="maximizeWindow" :title="t('topBar.maximize')">□</button>
      <button class="window-btn close" @click.stop="closeWindow" :title="t('common.close')">×</button>
    </div>

    <Teleport to="body">
    <div v-if="showRemoteDialog" class="dialog-overlay" @click.self="showRemoteDialog = false">
      <div class="quick-dialog">
        <h3>🔗 {{ t('topBar.remoteDialogTitle') }}</h3>
        <input v-model="remoteUrl" :placeholder="t('topBar.remoteUrlPlaceholder')" @keyup.enter="doAddRemote" />
        <div v-if="remoteStatus" class="remote-status" :class="remoteStatus.type">{{ remoteStatus.msg }}</div>
        <div class="dialog-actions">
          <button @click="doAddRemote" :disabled="!remoteUrl.trim() || remoteLoading">{{ remoteLoading ? t('topBar.connecting') : t('common.confirm') }}</button>
          <button class="secondary" @click="showRemoteDialog = false; remoteStatus = null">{{ t('common.cancel') }}</button>
        </div>
      </div>
    </div>
    
    <!-- 即时贴弹窗 -->
    <div v-if="stickyNoteVisible" class="sticky-note" :style="{ left: stickyNotePos.x + 'px', top: stickyNotePos.y + 'px', width: stickyNoteSize.width + 'px', height: stickyNoteSize.height + 'px' }" @mousedown="startStickyDrag">
      <div class="sticky-header">
        <span>📌 即时贴</span>
        <button class="sticky-close" @click.stop="stickyNoteVisible = false">×</button>
      </div>
      <textarea 
        v-model="stickyNoteContent" 
        class="sticky-content" 
        placeholder="记录临时想法、待办事项..."
        @mousedown.stop
      ></textarea>
      <div class="sticky-resize-handle" @mousedown.stop="startStickyResize"></div>
    </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { availableLocales } from '../i18n'
import { WindowMinimise, WindowToggleMaximise, Quit, WindowGetPosition, WindowSetPosition } from '../../wailsjs/runtime'
import { OpenFolderDialog, ClearWorkDir, ForceQuit, ShowWindow, OpenWorkDir, OpenFileDialog, SetLang } from '../../wailsjs/go/main/App'

const props = defineProps<{
  activeWindows: { fileTree: boolean; editor: boolean; terminal: boolean; changes: boolean; debug?: boolean; mcp?: boolean; token?: boolean }
  aiStatus: { pmStatus: string; seStatus: string; apStatus: string; cRunning: boolean; currentTask: string }
  ideConnected: Record<string, boolean>
  workDir: string
  recentProjects: string[]
  cMonitorEnabled: boolean
  projectState: string
  projectLevel: string // [v0.8.1] short / normal / full
  messageCount: number
  gitStatusCount: number
}>()

const emit = defineEmits(['toggle-window', 'reset', 'open-settings', 'select-project', 'toggle-c-monitor', 'toggle-git', 'toggle-search', 'open-file-in-editor'])

const showMenu = ref(false)
const showLangMenu = ref(false)

const { t, locale } = useI18n()

const currentLocaleLabel = computed(() => {
  const found = availableLocales.find(l => l.code === locale.value)
  return found ? found.label : 'EN'
})

function toggleLangMenu() { showLangMenu.value = !showLangMenu.value }
function closeLangMenu() { showLangMenu.value = false }

function changeLang(code: string) {
  locale.value = code
  localStorage.setItem('argus-locale', code)
  SetLang(code).catch((e: Error) => console.error('Backend language set failed:', e))
  showLangMenu.value = false
}

const showRemoteDialog = ref(false)
const remoteName = ref('origin')
const remoteUrl = ref('')
const remoteStatus = ref<{ type: string; msg: string } | null>(null)
const remoteLoading = ref(false)
const credUser = ref('')
const credPass = ref('')

// ========== 工作目录菜单 ==========
function toggleMenu() { showMenu.value = !showMenu.value }
function closeMenu() { showMenu.value = false }

function openFileExplorer() {
  emit('toggle-window', 'fileTree')
}

function openFolder() {
  closeMenu()
  emit('select-project', 'browse')
}

function connectRemote() {
  closeMenu()
  showRemoteDialog.value = true
}

async function openWorkDirInExplorer() {
  closeMenu()
  try {
    // @ts-ignore
    await window.go.main.App.OpenWorkDir()
  } catch (e) {
    console.error('打开工作目录失败:', e)
  }
}

function clearWorkDir() {
  closeMenu()
  emit('select-project', '')
}

function selectProject(project: string) {
  closeMenu()
  emit('select-project', project)
}

// 项目徽章辅助函数
function getInitials(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/')
  return parts[parts.length - 1]?.substring(0, 2).toUpperCase() || '??'
}
function getName(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/')
  return parts[parts.length - 1] || path
}

const colors = ['#6366f1','#8b5cf6','#a855f7','#d946ef','#ec4899','#f43f5e','#ef4444','#f97316','#eab308','#84cc16','#22c55e','#10b981','#14b8a6','#06b6d4','#0ea5e9','#3b82f6']

// 即时贴相关
const STICKY_STORAGE_KEY = 'argus_sticky_note'

function loadStickyData() {
  try {
    const raw = localStorage.getItem(STICKY_STORAGE_KEY)
    if (raw) {
      const data = JSON.parse(raw)
      const pos = data.pos || { x: window.innerWidth - 320, y: 60 }
      // [FIX] 确保即时贴在可视区域内（防止跨显示器/分辨率时丢出屏幕外）
      if (pos.x > window.innerWidth - 100) pos.x = window.innerWidth - 320
      if (pos.x < 0) pos.x = 20
      if (pos.y > window.innerHeight - 100) pos.y = 60
      if (pos.y < 0) pos.y = 20
      return {
        content: data.content || '',
        pos,
        size: data.size || { width: 280, height: 200 },
        visible: data.visible || false,
      }
    }
  } catch (_) { /* ignore */ }
  return null
}

function saveStickyData() {
  try {
    localStorage.setItem(STICKY_STORAGE_KEY, JSON.stringify({
      content: stickyNoteContent.value,
      pos: stickyNotePos.value,
      size: stickyNoteSize.value,
      visible: stickyNoteVisible.value,
    }))
  } catch (_) { /* ignore */ }
}

const savedSticky = loadStickyData()
const stickyNoteVisible = ref(savedSticky?.visible || false)
const stickyNoteContent = ref(savedSticky?.content || '')
const stickyNotePos = ref(savedSticky?.pos || { x: window.innerWidth - 320, y: 60 })
const stickyNoteSize = ref(savedSticky?.size || { width: 280, height: 200 })

// 自动保存：内容变化、可见性变化时保存
watch(stickyNoteContent, saveStickyData, { deep: false })
watch(stickyNoteVisible, saveStickyData)

let isStickyDragging = false
let stickyDragStartX = 0
let stickyDragStartY = 0
let stickyStartX = 0
let stickyStartY = 0

// 拖拽调整大小相关
let isStickyResizing = false
let resizeStartX = 0
let resizeStartY = 0
let resizeStartW = 0
let resizeStartH = 0

function toggleStickyNote() {
  stickyNoteVisible.value = !stickyNoteVisible.value
  if (stickyNoteVisible.value && !stickyNoteContent.value) {
    setTimeout(() => {
      const textarea = document.querySelector('.sticky-content') as HTMLTextAreaElement
      textarea?.focus()
    }, 100)
  }
}

function startStickyDrag(e: MouseEvent) {
  if ((e.target as HTMLElement).closest('.sticky-content')) return
  if ((e.target as HTMLElement).closest('.sticky-close')) return
  if ((e.target as HTMLElement).closest('.sticky-resize-handle')) return
  
  isStickyDragging = true
  stickyDragStartX = e.screenX
  stickyDragStartY = e.screenY
  stickyStartX = stickyNotePos.value.x
  stickyStartY = stickyNotePos.value.y
  
  document.addEventListener('mousemove', onStickyDrag)
  document.addEventListener('mouseup', stopStickyDrag)
}

const onStickyDrag = (e: MouseEvent) => {
  if (!isStickyDragging) return
  const deltaX = e.screenX - stickyDragStartX
  const deltaY = e.screenY - stickyDragStartY
  stickyNotePos.value.x = stickyStartX + deltaX
  stickyNotePos.value.y = stickyStartY + deltaY
}

const stopStickyDrag = () => {
  isStickyDragging = false
  // [FIX] 确保拖拽结束后即时贴不超出屏幕
  if (stickyNotePos.value.x > window.innerWidth - 100) stickyNotePos.value.x = window.innerWidth - 320
  if (stickyNotePos.value.x < 0) stickyNotePos.value.x = 20
  if (stickyNotePos.value.y > window.innerHeight - 100) stickyNotePos.value.y = 60
  if (stickyNotePos.value.y < 0) stickyNotePos.value.y = 20
  document.removeEventListener('mousemove', onStickyDrag)
  document.removeEventListener('mouseup', stopStickyDrag)
  saveStickyData()
}

function startStickyResize(e: MouseEvent) {
  isStickyResizing = true
  resizeStartX = e.screenX
  resizeStartY = e.screenY
  resizeStartW = stickyNoteSize.value.width
  resizeStartH = stickyNoteSize.value.height
  
  document.addEventListener('mousemove', onStickyResize)
  document.addEventListener('mouseup', stopStickyResize)
}

const onStickyResize = (e: MouseEvent) => {
  if (!isStickyResizing) return
  const deltaX = e.screenX - resizeStartX
  const deltaY = e.screenY - resizeStartY
  stickyNoteSize.value.width = Math.max(200, resizeStartW + deltaX)
  stickyNoteSize.value.height = Math.max(150, resizeStartH + deltaY)
}

const stopStickyResize = () => {
  isStickyResizing = false
  document.removeEventListener('mousemove', onStickyResize)
  document.removeEventListener('mouseup', stopStickyResize)
  saveStickyData()
}

function getColor(path: string): string {
  let hash = 0
  for (let i = 0; i < path.length; i++) {
    hash = path.charCodeAt(i) + ((hash << 5) - hash)
  }
  return colors[Math.abs(hash) % colors.length]
}

function handleClickOutside(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (!target.closest('.workdir-selector')) {
    closeMenu()
  }
  if (!target.closest('.lang-selector')) {
    closeLangMenu()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})
onUnmounted(() => document.removeEventListener('click', handleClickOutside))

let isDragging = false
let dragStarted = false
let dragStartX = 0
let dragStartY = 0
let windowStartX = 0
let windowStartY = 0

const DRAG_THRESHOLD = 5

function startDrag(e: MouseEvent) {
  if ((e.target as HTMLElement).closest('.workdir-selector')) return
  if ((e.target as HTMLElement).closest('button')) return
  
  // 彻底阻止默认行为，防止 Windows 系统标题栏点击最小化
  e.preventDefault()
  e.stopPropagation()
  e.stopImmediatePropagation()
  
  isDragging = true
  dragStarted = false
  dragStartX = e.screenX
  dragStartY = e.screenY
  
  document.addEventListener('mousemove', onDrag)
  document.addEventListener('mouseup', stopDrag)
}

const onDrag = (e: MouseEvent) => {
  if (!isDragging) return
  
  const deltaX = e.screenX - dragStartX
  const deltaY = e.screenY - dragStartY
  
  if (!dragStarted) {
    if (Math.abs(deltaX) < DRAG_THRESHOLD && Math.abs(deltaY) < DRAG_THRESHOLD) {
      return
    }
    dragStarted = true
    
    WindowGetPosition().then(pos => {
      windowStartX = pos.x
      windowStartY = pos.y
      
      const newX = windowStartX + deltaX
      const newY = windowStartY + deltaY
      WindowSetPosition(newX, newY)
    }).catch(() => {
      windowStartX = 100
      windowStartY = 100
      
      const newX = windowStartX + deltaX
      const newY = windowStartY + deltaY
      WindowSetPosition(newX, newY)
    })
    return
  }
  
  const newX = windowStartX + deltaX
  const newY = windowStartY + deltaY
  WindowSetPosition(newX, newY)
}

const stopDrag = () => {
  isDragging = false
  dragStarted = false
  document.removeEventListener('mousemove', onDrag)
  document.removeEventListener('mouseup', stopDrag)
}

function toggleCMonitor() {
  emit('toggle-c-monitor')
}

function minimizeWindow() { WindowMinimise() }
function maximizeWindow() { WindowToggleMaximise() }
function closeWindow() {
	if (confirm(t('common.quitConfirm'))) {
		ForceQuit()
	}
}

const projectStatusClass = computed(() => props.projectState || 'idle')

const projectStatusLabel = computed(() => {
  switch (props.projectState) {
    case 'running': return 'RUNNING'
    case 'error': return 'ERROR'
    case 'done': return 'DONE'
    case 'approved': return 'APPROVED'
    case 'idle': return 'IDLE'
    default: return 'IDLE'
  }
})

const projectStatusText = computed(() => {
  switch (props.projectState) {
    case 'running': return t('topBar.projectRunning')
    case 'error': return t('topBar.projectError')
    case 'done': return t('topBar.projectDone')
    case 'approved': return t('topBar.projectApproved') || 'AP已批准 ✅✅'
    case 'idle': return t('topBar.projectIdle')
    default: return t('topBar.projectIdle')
  }
})
</script>

<style scoped>
.top-bar {
  height: 36px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 8px;
  user-select: none;
  cursor: default;
}

.left-section {
  display: flex;
  align-items: center;
  gap: 2px;
}

.icon-btn {
  width: 32px;
  height: 32px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 16px;
  cursor: pointer;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  -webkit-app-region: no-drag;
}

.icon-btn:hover { background: var(--bg-tertiary); color: var(--text-primary); }
.icon-btn.active { background: var(--accent-color); color: #fff; }

.icon-btn.clear-btn .msg-badge {
  font-size: 10px;
  background: var(--accent-color);
  color: #fff;
  border-radius: 8px;
  padding: 1px 5px;
  margin-left: 2px;
  line-height: 1;
}

.icon-btn.git-btn .git-badge {
  font-size: 10px;
  background: #d29922;
  color: #fff;
  border-radius: 8px;
  padding: 1px 5px;
  margin-left: 2px;
  line-height: 14px;
}

/* 监控按钮样式 */
.monitor-btn {
  font-size: 14px;
  transition: all 0.2s;
  outline: none;
  border: none;
  box-shadow: none;
  background: transparent !important;
}

.monitor-btn:focus,
.monitor-btn:active,
.monitor-btn:hover {
  outline: none;
  border: none;
  box-shadow: none;
  background: transparent !important;
}

.monitor-btn.active {
  background: transparent !important;
}

.monitor-btn.error {
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.divider {
  width: 1px;
  height: 20px;
  background: var(--border-color);
  margin: 0 8px;
}

/* 语言选择器 */
.lang-selector {
  position: relative;
  display: flex;
  align-items: center;
  -webkit-app-region: no-drag;
}

.lang-btn {
  font-size: 11px !important;
  font-weight: 700;
  letter-spacing: 0.3px;
  min-width: 32px;
}

.lang-dropdown {
  position: absolute;
  top: 100%;
  right: 0;
  margin-top: 4px;
  min-width: 100px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  z-index: 1000;
  padding: 4px 0;
}

.lang-item {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px 16px;
  cursor: pointer;
  font-size: 12px;
  color: var(--text-primary);
}

.lang-item:hover { background: var(--bg-tertiary); }
.lang-item.active {
  background: rgba(47, 129, 247, 0.15);
  color: var(--accent-color);
  font-weight: 600;
}

/* 工作目录选择器 */
.workdir-selector {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 8px;
  height: 28px;
  border-radius: 6px;
  -webkit-app-region: no-drag;
}

.workdir-text {
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 600;
  white-space: nowrap;
  user-select: none;
}

.arrow-btn {
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 10px;
  cursor: pointer;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  -webkit-app-region: no-drag;
}

.arrow-btn:hover { background: var(--bg-tertiary); }

/* 下拉菜单 */
.menu-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  margin-top: 4px;
  min-width: 300px;
  max-width: 400px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  z-index: 1000;
  padding: 4px 0;
}

.menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text-primary);
}

.menu-item:hover { background: var(--bg-tertiary); }
.menu-item.active { background: var(--bg-tertiary); }

.menu-icon {
  font-size: 16px;
  width: 20px;
  text-align: center;
}

.menu-divider {
  height: 1px;
  background: var(--border-color);
  margin: 4px 0;
}

.menu-header {
  padding: 4px 12px;
  font-size: 11px;
  color: var(--text-secondary);
  font-weight: 600;
  text-transform: uppercase;
}

.project-badge {
  width: 24px;
  height: 24px;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  font-weight: 700;
  color: #fff;
  flex-shrink: 0;
}

.project-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.project-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-path {
  font-size: 11px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.check {
  color: var(--accent-color);
  font-weight: bold;
}

/* 右侧 */
.right-section {
  display: flex;
  align-items: center;
  gap: 4px;
  -webkit-app-region: no-drag;
}

.ai-status {
  display: flex;
  gap: 8px;
}

.status-item {
  width: 24px;
  height: 24px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: 11px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 地铁线路风格 - 与聊天角色颜色一致 */
.status-item.pm.busy {
  background: #8b5cf6;
  color: #fff;
  box-shadow: 0 0 6px rgba(139,92,246,0.4);
}

.status-item.se.busy {
  background: #10b981;
  color: #fff;
  box-shadow: 0 0 6px rgba(16,185,129,0.4);
}

.status-item.ap.busy {
  background: #f59e0b;
  color: #fff;
  box-shadow: 0 0 6px rgba(245,158,11,0.4);
}

/* MC 监控运行中 - 灰色（与ChatPanel Sys_ 一致） */
.status-item.mc.mc-running {
  background: #6b7280;
  color: #fff;
  animation: mcPulse 1.5s ease-in-out infinite;
}

@keyframes mcPulse {
  0%, 100% {
    background: #6b7280;
    box-shadow: 0 0 8px #6b7280;
  }
  50% {
    background: #9ca3af;
    box-shadow: 0 0 12px #9ca3af;
  }
}

.window-btn {
  width: 36px;
  height: 36px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 16px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  -webkit-app-region: no-drag;
}

.window-btn:hover { background: var(--bg-tertiary); color: var(--text-primary); }
.window-btn.close:hover { background: #e81123; color: #fff; }

/* 项目状态指示灯 */
.project-status-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.5px;
  transition: all 0.3s ease;
  background: var(--bg-tertiary);
}

.project-status-indicator .status-light {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  transition: all 0.3s ease;
}

.project-status-indicator .status-label {
  font-family: 'Consolas', monospace;
}

/* 运行中 - 蓝色（每秒4次闪烁） */
.project-status-indicator.running {
  background: rgba(59, 130, 246, 0.15) !important;
  color: #3b82f6 !important;
  animation: runningBlinkBg 0.25s ease-in-out infinite;
}
.project-status-indicator.running .status-light {
  background: #3b82f6 !important;
  animation: runningBlink 0.25s ease-in-out infinite;
}

@keyframes runningBlink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.1; }
}

@keyframes runningBlinkBg {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* 错误 - 红色闪烁 */
.project-status-indicator.error {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
  animation: errorPulse 1s ease-in-out infinite;
}
.project-status-indicator.error .status-light {
  background: #ef4444;
  animation: errorBlink 1s ease-in-out infinite;
}

@keyframes errorPulse {
  0%, 100% { background: rgba(239, 68, 68, 0.15); }
  50% { background: rgba(239, 68, 68, 0.3); }
}

@keyframes errorBlink {
  0%, 100% { opacity: 1; box-shadow: 0 0 8px #ef4444; }
  50% { opacity: 0.3; box-shadow: none; }
}

/* 完成 - 绿色 */
.project-status-indicator.done {
  background: rgba(34, 197, 94, 0.15);
  color: #22c55e;
}
.project-status-indicator.done .status-light {
  background: #22c55e;
  box-shadow: 0 0 8px #22c55e;
}

/* AP已批准 - 金色双✅ */
.project-status-indicator.approved {
  background: rgba(16, 185, 129, 0.2);
  color: #10b981;
  font-weight: 600;
}
.project-status-indicator.approved .status-light {
  background: linear-gradient(45deg, #10b981, #34d399);
  box-shadow: 0 0 12px #10b981;
  animation: approved-pulse 2s ease-in-out infinite;
}
@keyframes approved-pulse {
  0%, 100% { box-shadow: 0 0 8px #10b981; }
  50% { box-shadow: 0 0 16px #34d399, 0 0 24px rgba(52, 211, 153, 0.3); }
}

/* [v0.8.1] 项目级别指示器 */
.project-level-badge {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 4px;
  margin-right: 6px;
  letter-spacing: 0.5px;
  white-space: nowrap;
}
.level-short-process {
  color: #a78bfa;
  background: rgba(167, 139, 250, 0.12);
  border: 1px solid rgba(167, 139, 250, 0.25);
}
.level-normal-process {
  color: #60a5fa;
  background: rgba(96, 165, 250, 0.12);
  border: 1px solid rgba(96, 165, 250, 0.25);
}
.level-full-process {
  color: #f97316;
  background: rgba(249, 115, 22, 0.12);
  border: 1px solid rgba(249, 115, 22, 0.25);
}

/* IDE 连接状态指示器 */
.ide-status {
  display: flex;
  gap: 4px;
}

.ide-indicator {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  font-size: 11px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
  transition: all 0.3s ease;
}

.ide-indicator.connected {
  background: #22c55e;
  color: #fff;
  border-color: #22c55e;
  box-shadow: 0 0 6px rgba(34, 197, 94, 0.5);
}

/* 空闲/就绪 - 黄色 */
.project-status-indicator.idle {
  background: rgba(234, 179, 8, 0.15);
  color: #eab308;
}
.project-status-indicator.idle .status-light {
  background: #eab308;
  box-shadow: 0 0 8px #eab308;
}

/* 快捷弹窗 */
.dialog-overlay {
  position: fixed;
  top: 36px;
  left: 0;
  right: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: center;
  padding-top: 40px;
  z-index: 2000;
  -webkit-app-region: no-drag;
}

.quick-dialog {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 20px;
  min-width: 400px;
  max-width: 500px;
  -webkit-app-region: no-drag;
}

.quick-dialog h3 {
  margin: 0 0 14px;
  font-size: 15px;
  color: var(--text-primary);
}

.quick-dialog input {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: 13px;
  margin-bottom: 10px;
  outline: none;
  box-sizing: border-box;
}

.quick-dialog input:focus {
  border-color: var(--accent-color);
}

.dialog-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 4px;
}

.dialog-actions button {
  padding: 7px 18px;
  border: none;
  border-radius: 5px;
  cursor: pointer;
  font-size: 13px;
  background: var(--accent-color);
  color: #fff;
}

.dialog-actions button.secondary {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.dialog-actions button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.remote-status {
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
  text-align: center;
  margin-top: 10px;
}
.remote-status.success {
  background: rgba(52, 199, 89, 0.15);
  color: #34c759;
}
.remote-status.error {
  background: rgba(255, 59, 48, 0.15);
  color: #ff3b30;
}

.cred-row {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.cred-row input {
  flex: 1;
}

/* 即时贴样式 */
.sticky-note {
  position: fixed;
  width: 280px;
  min-height: 200px;
  background: linear-gradient(135deg, #fef3c7 0%, #fde68a 100%);
  border: 1px solid #f59e0b;
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(251, 191, 36, 0.2);
  z-index: 3000;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  user-select: none;
}

.sticky-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: rgba(251, 191, 36, 0.3);
  border-bottom: 1px solid rgba(245, 158, 11, 0.3);
  font-size: 13px;
  font-weight: 600;
  color: #92400e;
  cursor: move;
}

.sticky-close {
  width: 20px;
  height: 20px;
  border: none;
  background: rgba(146, 64, 14, 0.1);
  color: #92400e;
  font-size: 16px;
  cursor: pointer;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
}

.sticky-close:hover {
  background: rgba(146, 64, 14, 0.2);
}

.sticky-content {
  flex: 1;
  min-height: 160px;
  padding: 12px;
  border: none;
  background: transparent;
  color: #78350f;
  font-size: 13px;
  font-family: inherit;
  line-height: 1.6;
  resize: vertical;
  outline: none;
}

.sticky-content::placeholder {
  color: rgba(120, 53, 15, 0.4);
}

.sticky-resize-handle {
  position: absolute;
  bottom: 0;
  right: 0;
  width: 16px;
  height: 16px;
  cursor: nwse-resize;
  background: linear-gradient(135deg, transparent 50%, rgba(146, 64, 14, 0.3) 50%);
  border-bottom-right-radius: 8px;
}

.sticky-resize-handle:hover {
  background: linear-gradient(135deg, transparent 50%, rgba(146, 64, 14, 0.5) 50%);
}
</style>