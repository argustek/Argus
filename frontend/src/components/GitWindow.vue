<template>
  <div 
    class="floating-window git-window"
    :style="{ left: windowPos.x + 'px', top: windowPos.y + 'px' }"
  >
    <div class="window-header" @mousedown="startDrag">
      <span class="window-title">📦 Git</span>
      <div class="header-info" v-if="repoInfo.is_repo">
        <span class="branch-badge">{{ repoInfo.current_branch }}</span>
        <span v-if="repoInfo.ahead > 0" class="ahead-badge">↑{{ repoInfo.ahead }}</span>
        <span v-if="repoInfo.behind > 0" class="behind-badge">↓{{ repoInfo.behind }}</span>
      </div>
      <div class="header-actions">
        <button class="refresh-btn" @click.stop="refreshAll" :title="t('common.refresh')" :disabled="loading">🔄</button>
        <button class="close-btn" @click.stop="$emit('close')">×</button>
      </div>
    </div>

    <div class="tab-bar">
      <button 
        v-for="tab in tabs" 
        :key="tab.key"
        class="tab-btn"
        :class="{ active: currentTab === tab.key }"
        @click="currentTab = tab.key"
      >{{ tab.label }}</button>
    </div>

    <div class="window-content">
      <!-- 非Git仓库提示 -->
      <div v-if="!repoInfo.is_repo && !cloning" class="no-repo">
        <div class="no-repo-icon">📂</div>
        <p>{{ t('git.notARepo') }}</p>
        <div class="no-repo-actions">
          <button class="action primary" @click="showInitDialog = true">{{ t('git.initRepo') }}</button>
          <button class="action secondary" @click="showCloneDialog = true">{{ t('git.cloneRepo') }}</button>
        </div>
      </div>

      <!-- 克隆对话框 -->
      <Teleport to="body">
      <div v-if="showCloneDialog" class="dialog-overlay" @click.self="showCloneDialog = false">
        <div class="dialog clone-dialog">
          <h3>{{ t('git.cloneRepo') }}</h3>
          <div class="form-group">
            <label>{{ t('git.cloneUrl') }}</label>
            <input v-model="cloneUrl" placeholder="https://github.com/user/repo.git" />
          </div>
          <div class="form-group">
            <label>{{ t('git.cloneDir') }}</label>
            <input v-model="cloneDir" placeholder="E:/projects/my-repo" />
          </div>
          <div class="form-group">
            <label>{{ t('git.cloneBranch') }}</label>
            <input v-model="cloneBranch" placeholder="main / master / develop" />
          </div>
          <div class="dialog-actions">
            <button @click="doClone" :disabled="!cloneUrl || cloning">
              {{ cloning ? t('git.cloning') : t('git.cloneRepo') }}
            </button>
            <button class="secondary" @click="showCloneDialog = false">{{ t('common.cancel') }}</button>
          </div>
          <div v-if="cloneOutput" class="clone-output">
            <pre>{{ cloneOutput }}</pre>
          </div>
        </div>
      </div>

      <div v-if="showInitDialog" class="dialog-overlay" @click.self="showInitDialog = false">
        <div class="dialog init-dialog">
          <h3>{{ t('git.initRepo') }}</h3>
          <p>{{ t('git.initFirst') }}</p>
          <div class="dialog-actions">
            <button @click="doInit">{{ t('common.confirm') }}</button>
            <button class="secondary" @click="showInitDialog = false">{{ t('common.cancel') }}</button>
          </div>
        </div>
      </div>
      </Teleport>

      <!-- Tab 1: 工作区 -->
      <div v-if="currentTab === 'workspace' && repoInfo.is_repo" class="tab-content">
        <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
        <div v-else-if="error" class="error">{{ error }}</div>
        <template v-else>
          <div v-if="entries.length === 0" class="empty-state">
            ✅ {{ t('git.noChanges') }}
          </div>
          <div v-else class="file-list">
            <div
              v-for="(entry, idx) in entries"
              :key="idx"
              class="file-entry"
              :class="{ expanded: expandedFile === entry.path }"
            >
              <span class="status-badge" :class="getStatusClass(entry.status)">{{ statusLabel(entry.status) }}</span>
              <span class="file-path" @click="toggleDiff(entry.path)">{{ entry.path || '?' }}</span>
              <div class="file-actions">
                <button v-if="canStage(entry.status)" class="btn-xs stage" @click.stop="stage(entry.path)" :title="t('git.stage')">+S</button>
                <button v-if="canUnstage(entry.status)" class="btn-xs unstage" @click.stop="unstage(entry.path)" :title="t('git.unstage')">-S</button>
                <button class="btn-xs restore" @click.stop="restore(entry.path)" :title="t('git.restore')">↩</button>
              </div>
            </div>
            <div v-if="expandedFile && fileDiff" class="diff-viewer">
              <div class="diff-header">
                <span>{{ fileDiff.path }}</span>
                <span class="diff-stats">
                  <span class="stat-add">+{{ fileDiff.additions || 0 }}</span>
                  <span class="stat-del">-{{ fileDiff.deletions || 0 }}</span>
                </span>
              </div>
              <pre class="diff-content"><code v-for="(line, i) in diffLines" :key="i" :class="getDiffLineClass(line)">{{ line }}</code></pre>
            </div>
          </div>

          <div v-if="entries.length > 0" class="commit-bar">
            <input 
              v-model="commitMessage" 
              class="commit-input" 
              :placeholder="t('git.commitMessage')" 
              @keyup.enter="doCommit"
            />
            <button class="commit-btn" @click="doCommit" :disabled="!commitMessage.trim()">{{ t('git.commit') }}</button>
            <button class="discard-btn" @click="confirmDiscard" :title="t('git.discardAll')">🗑</button>
          </div>
        </template>
      </div>

      <!-- Tab 2: 历史记录 -->
      <div v-if="currentTab === 'history' && repoInfo.is_repo" class="tab-content">
        <div v-if="loadingHistory" class="loading">{{ t('git.loadingHistory') }}</div>
        <div v-else class="log-list">
          <div v-for="log in commitLog" :key="log.hash" class="log-entry" :class="{ expanded: expandedCommit === log.hash }">
            <span class="log-hash" :title="log.hash" @click="toggleCommitDiff(log.hash)">{{ log.short_hash }}</span>
            <span class="log-msg">{{ log.message }}</span>
            <span class="log-author">{{ log.author }}</span>
            <span class="log-date">{{ log.date }}</span>
          </div>
          <div v-if="expandedCommit && commitDiff" class="diff-viewer">
            <div class="diff-header">
              <span>{{ expandedCommit.substring(0, 8) }}</span>
              <span class="diff-stats">
                <span class="stat-add">+{{ commitDiff.additions || 0 }}</span>
                <span class="stat-del">-{{ commitDiff.deletions || 0 }}</span>
              </span>
            </div>
            <pre class="diff-content"><code v-for="(line, i) in commitDiffLines" :key="i" :class="getDiffLineClass(line)">{{ line }}</code></pre>
          </div>
          <div v-if="commitLog.length === 0 && !loadingHistory" class="empty-state">{{ t('git.noHistory') }}</div>
        </div>
      </div>

      <!-- Tab 3: 分支 -->
      <div v-if="currentTab === 'branches' && repoInfo.is_repo" class="tab-content">
        <div class="branch-toolbar">
          <input v-model="newBranchName" :placeholder="t('git.newBranch')" class="branch-input" @keyup.enter="createBranch" />
          <button class="btn-sm create" @click="createBranch" :disabled="!newBranchName.trim()">{{ t('git.createBranch') }}</button>
        </div>
        <div class="branch-list">
          <div v-for="b in branches" :key="b.name + b.is_remote" class="branch-entry" :class="{ current: b.current, remote: b.is_remote }">
            <span class="branch-name">{{ b.name }}</span>
            <span v-if="b.current" class="current-tag">{{ t('git.currentBranch') }}</span>
            <span v-if="b.is_remote" class="remote-tag">{{ t('git.remote') }}</span>
            <button v-if="!b.current && !b.is_remote" class="btn-sm switch" @click="switchBranch(b.name)">{{ t('git.switchBranch') }}</button>
          </div>
          <div v-if="branches.length === 0" class="empty-state">{{ t('git.noBranches') }}</div>
        </div>
      </div>

      <!-- Tab 4: 远程 -->
      <div v-if="currentTab === 'remote' && repoInfo.is_repo" class="tab-content">
        <div class="remote-info">
          <div class="info-row">
            <label>{{ t('git.remoteUrl') }}:</label>
            <span>{{ repoInfo.remote_url || '(' + t('git.notARepo') + ')' }}</span>
          </div>
          <div class="info-row">
            <label>{{ t('git.remoteName') }}:</label>
            <span>{{ repoInfo.remote_name || '-' }}</span>
          </div>
        </div>

        <div class="remote-actions">
          <button class="action-btn auth" @click="showCredDialog = true" :title="credSet ? t('git.credentialsSet') : t('git.setCredentials')">🔑 {{ credSet ? t('git.credentialsSet') : t('git.credentials') }}</button>
          <button class="action-btn push" @click="doPush" :disabled="pushingPulling">⬆ {{ t('git.push') }}</button>
          <button class="action-btn pull" @click="doPull" :disabled="pushingPulling">⬇ {{ t('git.pull') }}</button>
        </div>

        <div class="remote-list-section">
          <h4>{{ t('git.remoteList') }}</h4>
          <div v-for="r in remotes" :key="r.name" class="remote-entry" :class="{ current: r.name === repoInfo.remote_name }">
            <span class="remote-name">{{ r.name }}</span>
            <span v-if="r.name === repoInfo.remote_name" class="current-tag">{{ t('git.currentRemote') }}</span>
            <span class="remote-url">{{ r.url }}</span>
            <button class="btn-sm delete" @click="removeRemote(r.name)" :title="t('git.removeRemote')">✕</button>
          </div>
          <div v-if="remotes.length === 0" class="empty-state">{{ t('git.noRemotes') }}</div>
          
          <div class="add-remote-form">
            <input v-model="newRemoteName" :placeholder="t('git.remoteName')" class="remote-input" />
            <input v-model="newRemoteUrl" :placeholder="t('git.remoteUrl')" class="remote-input url" />
            <button class="btn-sm add" @click="addRemote" :disabled="!newRemoteName.trim() || !newRemoteUrl.trim()">{{ t('git.addRemote') }}</button>
          </div>
        </div>

        <div v-if="remoteOutput" class="remote-output">
          <pre>{{ remoteOutput }}</pre>
        </div>
      </div>
    </div>

    <!-- 认证弹窗 -->
    <Teleport to="body">
    <div v-if="showCredDialog" class="cred-overlay" @click.self="showCredDialog = false">
      <div class="cred-dialog">
        <h3>🔑 {{ t('git.setCredentials') }}</h3>
        <p class="cred-hint">{{ t('git.credentials') }}</p>
        <input v-model="credUser" :placeholder="t('git.username')" />
        <input v-model="credPass" type="password" :placeholder="t('git.password')" />
        <div class="cred-actions">
          <button @click="doSetCred">{{ t('common.save') }}</button>
          <button class="secondary" @click="showCredDialog = false">{{ t('common.cancel') }}</button>
        </div>
      </div>
    </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch, onErrorCaptured } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDraggable } from '../composables/useDraggable'
import { EventsEmit, EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'

const { t } = useI18n()

const emit = defineEmits(['close'])

const { windowPos, startDrag } = useDraggable(420, 60)

const tabs = computed(() => [
  { key: 'workspace', label: t('git.workspace') },
  { key: 'history', label: t('git.history') },
  { key: 'branches', label: t('git.branches') },
  { key: 'remote', label: t('git.remote') }
])

const currentTab = ref('workspace')
const loading = ref(false)
const error = ref('')
const entries = ref<Array<{status: string, path: string}>>([])
const commitMessage = ref('')
const expandedFile = ref('')
const fileDiff = ref<any>(null)
const cloning = ref(false)

const repoInfo = reactive({
  is_repo: false,
  current_branch: '',
  remote_url: '',
  remote_name: '',
  ahead: 0,
  behind: 0,
  is_clean: true
})

const commitLog = ref<any[]>([])
const loadingHistory = ref(false)
const expandedCommit = ref('')
const commitDiff = ref<any>(null)
const commitDiffLines = computed(() => {
  if (!commitDiff.value?.content) return []
  return commitDiff.value.content.split('\n')
})
const branches = ref<any[]>([])
const remotes = ref<any[]>([])

// 防抖：避免频繁调用 Git IPC 导致前端主线程阻塞（触发消息丢失误报）
let gitRefreshTimer: ReturnType<typeof setTimeout> | null = null
const GIT_DEBOUNCE_MS = 800

const showCloneDialog = ref(false)
const showInitDialog = ref(false)
const cloneUrl = ref('')
const cloneDir = ref('')
const cloneBranch = ref('')
const cloneOutput = ref('')

const newBranchName = ref('')
const newRemoteName = ref('')
const newRemoteUrl = ref('')
const pushingPulling = ref(false)
const remoteOutput = ref('')
const showCredDialog = ref(false)
const credUser = ref('')
const credPass = ref('')
const credSet = ref(false)

const diffLines = computed(() => {
  if (!fileDiff.value?.content) return []
  return fileDiff.value.content.split('\n')
})

function getDiffLineClass(line: string) {
  if (line.startsWith('+')) return 'diff-add'
  if (line.startsWith('-')) return 'diff-del'
  return 'diff-ctx'
}

let refreshTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  // 注册 Git 结果监听器（后端 goroutine 执行完后推送结果，不阻塞前端）
  EventsOn('git:repo-info', (raw: string) => {
    try { const info = JSON.parse(raw); if (info) Object.assign(repoInfo, info) } catch {}
  })
  EventsOn('git:status', (raw: string) => {
    try { entries.value = JSON.parse(raw) || [] } catch {}
    loading.value = false
  })
  refreshAll()
  refreshTimer = setInterval(() => {
    refreshAll()
  }, 5000)
})

onUnmounted(() => {
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
  EventsOff('git:repo-info')
  EventsOff('git:status')
})

onErrorCaptured((err: any) => {
  console.error('[GitWindow] 组件错误:', err)
  error.value = '组件错误: ' + (err?.message || String(err))
  return false
})

async function refreshAll() {
  // 防抖：合并短时间内多次调用，避免阻塞前端主线程
  if (gitRefreshTimer) { clearTimeout(gitRefreshTimer) }
  gitRefreshTimer = setTimeout(async () => {
    await doRefreshAll()
  }, GIT_DEBOUNCE_MS)
}

async function doRefreshAll() {
  const hadExpanded = expandedFile.value
  if (!hadExpanded) {
    fileDiff.value = null
  }
  try {
    await Promise.all([loadRepoInfo(), loadStatus()])
    if (hadExpanded) {
      try {
        const { GetFileDiff } = await import('../../wailsjs/go/main/App')
        fileDiff.value = await GetFileDiff(hadExpanded)
      } catch {
        fileDiff.value = null
      }
    }
  } catch (err: any) {
    console.error('[GitWindow] refreshAll 失败:', err)
  }
}

async function loadRepoInfo() {
  // 非阻塞：通过事件总线请求，后端 goroutine 执行后推送结果
  try { EventsEmit('git:request-repo-info') } catch {}
}

async function loadStatus() {
  if (!repoInfo.is_repo) return
  loading.value = true
  error.value = ''
  // 非阻塞：通过事件总线请求，后端 goroutine 执行后推送结果
  try { EventsEmit('git:request-status') } catch {}
}

async function loadHistory() {
  if (!repoInfo.is_repo) return
  loadingHistory.value = true
  try {
    const { GetCommitLog } = await import('../../wailsjs/go/main/App')
    commitLog.value = await GetCommitLog(20) || []
  } catch {}
  finally {
    loadingHistory.value = false
  }
}

async function loadBranches() {
  if (!repoInfo.is_repo) return
  try {
    const { GetBranches } = await import('../../wailsjs/go/main/App')
    branches.value = await GetBranches() || []
  } catch (err: any) {
    console.error('[GitWindow] loadBranches 失败:', err)
    branches.value = []
  }
}

async function loadRemotes() {
  if (!repoInfo.is_repo) return
  try {
    const { GetRemotes } = await import('../../wailsjs/go/main/App')
    remotes.value = await GetRemotes() || []
  } catch (err: any) {
    console.error('[GitWindow] loadRemotes 失败:', err)
    remotes.value = []
  }
}

watch(currentTab, async (tab) => {
  if (tab === 'history') await loadHistory()
  if (tab === 'branches') await loadBranches()
  if (tab === 'remote') { await loadRemotes(); await loadRepoInfo() }
})

function getStatusClass(status: string): string {
  if (!status || status.length < 2) return ''
  const staging = status[0]
  const worktree = status[1]
  if (worktree === '?' && staging === '?') return 'untracked'
  if (worktree === 'M' || worktree === 'D' || worktree === 'R') return 'modified'
  if (staging === 'M' || staging === 'A' || staging === 'D' || staging === 'R') return 'added'
  return ''
}

function statusLabel(status: string): string {
  if (!status || status.length < 2) return status || ''
  const staging = status[0]
  const worktree = status[1]
  if (staging === '?' && worktree === '?') return 'U'
  const parts: string[] = []
  if (staging !== ' ') parts.push(staging)
  if (worktree !== ' ' && worktree !== '?') parts.push(worktree)
  return parts.length > 0 ? parts.join('') : status.replace(/\s/g, '')
}

function canStage(status: string): boolean {
  if (!status || status.length < 2) return false
  return status[1] !== ' '
}

function canUnstage(status: string): boolean {
  if (!status || status.length < 2) return false
  return status[0] !== ' ' && status[0] !== '?'
}

async function toggleDiff(path: string) {
  if (expandedFile.value === path) {
    expandedFile.value = ''
    fileDiff.value = null
    return
  }
  expandedFile.value = path
  try {
    const { GetFileDiff } = await import('../../wailsjs/go/main/App')
    fileDiff.value = await GetFileDiff(path)
  } catch {
    fileDiff.value = null
  }
}

async function toggleCommitDiff(hash: string) {
  if (expandedCommit.value === hash) {
    expandedCommit.value = ''
    commitDiff.value = null
    return
  }
  expandedCommit.value = hash
  try {
    const { GetCommitDiff } = await import('../../wailsjs/go/main/App')
    commitDiff.value = await GetCommitDiff(hash)
  } catch {
    commitDiff.value = null
  }
}

async function stage(path: string) {
  try {
    const { TrackFile } = await import('../../wailsjs/go/main/App')
    await TrackFile(path)
    await loadStatus()
  } catch (err: any) { error.value = String(err) }
}

async function unstage(path: string) {
  try {
    const { UnTrackFile } = await import('../../wailsjs/go/main/App')
    await UnTrackFile(path)
    await loadStatus()
  } catch (err: any) { error.value = String(err) }
}

async function restore(path: string) {
  try {
    const { RestoreFile } = await import('../../wailsjs/go/main/App')
    await RestoreFile(path)
    expandedFile.value = ''
    await loadStatus()
  } catch (err: any) { error.value = String(err) }
}

async function doCommit() {
  if (!commitMessage.value.trim()) return
  try {
    const { SaveVersion } = await import('../../wailsjs/go/main/App')
    await SaveVersion(commitMessage.value.trim())
    commitMessage.value = ''
    await Promise.all([loadStatus(), loadRepoInfo(), loadHistory()])
  } catch (err: any) { error.value = String(err) }
}

async function confirmDiscard() {
  if (!confirm(t('git.discardConfirm'))) return
  try {
    const { DiscardAllChanges } = await import('../../wailsjs/go/main/App')
    await DiscardAllChanges()
    await loadStatus()
  } catch (err: any) { error.value = String(err) }
}

async function doClone() {
  if (!cloneUrl.value) return
  cloning.value = true
  cloneOutput.value = ''
  try {
    const { GitClone, SetWorkDir } = await import('../../wailsjs/go/main/App')
    const result = await GitClone(cloneUrl.value, cloneDir.value, cloneBranch.value)
    cloneOutput.value = result.success ? result.output : (t('common.error') + ': ' + result.error)
    if (result.success) {
      const targetDir = cloneDir.value || cloneUrl.value.split('/').pop()?.replace('.git', '') || ''
      if (targetDir) {
        await SetWorkDir(targetDir)
        showCloneDialog.value = false
        cloneUrl.value = ''
        cloneDir.value = ''
        cloneBranch.value = ''
        await refreshAll()
      }
    }
  } catch (err: any) {
    cloneOutput.value = '错误: ' + err
  } finally {
    cloning.value = false
  }
}

async function doInit() {
  try {
    const { GitInit } = await import('../../wailsjs/go/main/App')
    await GitInit()
    showInitDialog.value = false
    await refreshAll()
  } catch (err: any) { error.value = String(err) }
}

async function doPush() {
  pushingPulling.value = true
  remoteOutput.value = ''
  try {
    const { GitPush } = await import('../../wailsjs/go/main/App')
    const out = await GitPush(repoInfo.remote_name, repoInfo.current_branch)
    remoteOutput.value = out || t('git.pushSuccess')
    await loadRepoInfo()
  } catch (err: any) {
    remoteOutput.value = String(err)
  } finally {
    pushingPulling.value = false
  }
}

async function doPull() {
  pushingPulling.value = true
  remoteOutput.value = ''
  try {
    const { GitPull } = await import('../../wailsjs/go/main/App')
    const out = await GitPull(repoInfo.remote_name, repoInfo.current_branch)
    remoteOutput.value = out || t('git.pullSuccess')
    await loadRepoInfo()
    await loadStatus()
  } catch (err: any) {
    remoteOutput.value = String(err)
  } finally {
    pushingPulling.value = false
  }
}

async function doSetCred() {
  if (!credUser.value || !credPass.value) return
  try {
    const { SetGitCredentials } = await import('../../wailsjs/go/main/App')
    await SetGitCredentials(credUser.value, credPass.value)
    credSet.value = true
    showCredDialog.value = false
    remoteOutput.value = '✅ ' + t('git.credentialsSet') + ': ' + credUser.value
  } catch (err: any) {
    remoteOutput.value = t('git.credentials') + ' ' + t('common.error') + ': ' + err
  }
}

async function switchBranch(name: string) {
  try {
    const { SwitchBranch } = await import('../../wailsjs/go/main/App')
    await SwitchBranch(name)
    await loadRepoInfo()
    await loadBranches()
    await loadStatus()
  } catch (err: any) {
    error.value = String(err)
  }
}

async function createBranch() {
  if (!newBranchName.value.trim()) return
  try {
    const { CreateBranch } = await import('../../wailsjs/go/main/App')
    await CreateBranch(newBranchName.value.trim())
    newBranchName.value = ''
    await loadBranches()
  } catch (err: any) { error.value = String(err) }
}

async function addRemote() {
  try {
    const { AddRemote } = await import('../../wailsjs/go/main/App')
    await AddRemote(newRemoteName.value.trim(), newRemoteUrl.value.trim())
    newRemoteName.value = ''
    newRemoteUrl.value = ''
    await loadRemotes()
    await loadRepoInfo()
  } catch (err: any) { error.value = String(err) }
}

async function removeRemote(name: string) {
  if (!confirm(t('git.deleteRemoteConfirm') + ' "' + name + '"？')) return
  try {
    const { RemoveRemote } = await import('../../wailsjs/go/main/App')
    await RemoveRemote(name)
    await loadRemotes()
    await loadRepoInfo()
  } catch (err: any) { error.value = String(err) }
}
</script>

<style scoped>
.floating-window {
  position: fixed;
  width: 580px;
  height: 520px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.6);
  z-index: 100;
  -webkit-app-region: no-drag;
}
.window-header {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px; background: var(--bg-tertiary);
  border-radius: 8px 8px 0 0; border-bottom: 1px solid var(--border-color);
  cursor: move; user-select: none; flex-shrink: 0;
}
.window-title { font-size: 13px; font-weight: 600; white-space: nowrap; }
.header-info { display: flex; align-items: center; gap: 4px; flex: 1; justify-content: center; }
.branch-badge {
  font-family: monospace; font-size: 11px; padding: 1px 6px;
  background: rgba(47, 129, 247, 0.15); color: #2f81f7; border-radius: 3px;
}
.ahead-badge { font-size: 10px; color: #3fb950; }
.behind-badge { font-size: 10px; color: #d29922; }
.header-actions { display: flex; gap: 2px; }

.tab-bar {
  display: flex; background: var(--bg-primary); border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.tab-btn {
  flex: 1; padding: 6px 0; border: none; background: transparent; color: var(--text-secondary);
  font-size: 12px; cursor: pointer; transition: all 0.15s; border-bottom: 2px solid transparent;
}
.tab-btn:hover { color: var(--text-primary); background: rgba(255,255,255,0.03); }
.tab-btn.active { color: var(--accent-color); border-bottom-color: var(--accent-color); font-weight: 500; }

.window-content { flex: 1; overflow-y: auto; position: relative; -webkit-app-region: no-drag; }

.no-repo {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  height: 100%; gap: 16px; color: var(--text-secondary);
}
.no-repo-icon { font-size: 48px; opacity: 0.3; }
.no-repo-actions { display: flex; gap: 8px; }

.dialog-overlay {
  position: absolute; inset: 0; background: rgba(0,0,0,0.6);
  display: flex; align-items: center; justify-content: center; z-index: 200;
  -webkit-app-region: no-drag;
}
.dialog {
  background: var(--bg-secondary); border: 1px solid var(--border-color);
  border-radius: 8px; padding: 20px; width: 85%; max-width: 450px;
  -webkit-app-region: no-drag;
}
.dialog h3 { margin: 0 0 14px; font-size: 14px; color: var(--text-primary); }
.form-group { margin-bottom: 12px; }
.form-group label { display: block; font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; }
.form-group input {
  width: 100%; padding: 7px 10px; border: 1px solid var(--border-color);
  border-radius: 4px; background: var(--bg-primary); color: var(--text-primary); font-size: 12px;
  box-sizing: border-box;
}
.form-group input:focus { outline: none; border-color: var(--accent-color); }
.dialog-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
.dialog-actions button {
  padding: 7px 18px; border: none; border-radius: 4px; cursor: pointer; font-size: 12px;
  background: var(--accent-color); color: #fff;
}
.dialog-actions button.secondary { background: var(--bg-tertiary); color: var(--text-primary); }
.clone-output { margin-top: 12px; }
.clone-output pre {
  background: var(--bg-primary); padding: 10px; border-radius: 4px; font-size: 11px;
  overflow: auto; max-height: 150px; color: var(--text-secondary); white-space: pre-wrap;
}

.loading, .error, .empty-state {
  text-align: center; padding: 30px; color: var(--text-secondary); font-size: 13px;
}
.error { color: #ff6b6b; }

.file-list { display: flex; flex-direction: column; gap: 3px; padding: 8px; }
.file-entry {
  display: flex; align-items: center; gap: 6px; padding: 5px 8px;
  background: var(--bg-tertiary); border-radius: 4px; cursor: pointer; font-size: 12px;
  transition: background 0.1s;
}
.file-entry:hover { background: rgba(255,255,255,0.06); }
.file-entry.expanded { background: rgba(47, 129, 247, 0.08); }

.status-badge {
  font-family: monospace; font-weight: bold; min-width: 22px; text-align: center;
  padding: 1px 4px; border-radius: 2px; font-size: 11px;
}
.status-badge.untracked { color: #d29922; background: rgba(210,153,34,0.12); }
.status-badge.modified { color: #2f81f7; background: rgba(47,129,247,0.12); }
.status-badge.added { color: #3fb950; background: rgba(63,185,80,0.12); }
.status-badge.deleted { color: #f85149; background: rgba(248,81,73,0.12); }
.status-badge.renamed { color: #a371f7; background: rgba(163,113,247,0.12); }

.file-path { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-primary); }
.entry-buttons { display: flex; gap: 3px; }

.btn-sm {
  width: 20px; height: 20px; border: 1px solid var(--border-color); border-radius: 3px;
  background: var(--bg-secondary); color: var(--text-primary); cursor: pointer;
  font-size: 12px; display: flex; align-items: center; justify-content: center;
  padding: 0;
}
.btn-sm:hover { background: var(--border-color); }
.btn-sm.stage { border-color: #2f81f7; color: #2f81f7; }
.btn-sm.unstage { border-color: #d29922; color: #d29922; }
.btn-sm.restore { border-color: #3fb950; color: #3fb950; }
.btn-sm.switch { border-color: #a371f7; color: #a371f7; padding: 2px 8px; width: auto; font-size: 11px; }
.btn-sm.create { border-color: #3fb950; color: #3fb950; padding: 2px 10px; width: auto; font-size: 11px; }
.btn-sm.add { border-color: #2f81f7; color: #2f81f7; padding: 2px 10px; width: auto; font-size: 11px; }

.diff-viewer {
  margin-left: 36px; margin-top: 3px; padding: 8px;
  background: var(--bg-primary); border-radius: 4px; border: 1px solid var(--border-color);
}
.diff-viewer pre { margin: 0; font-size: 11px; overflow-x: auto; white-space: pre; line-height: 1.5; }
.diff-stats { display: flex; gap: 12px; margin-bottom: 6px; font-size: 11px; }
.diff-stats .additions { color: #3fb950; }
.diff-stats .deletions { color: #f85149; }
.diff-content { margin: 0; padding: 4px 0; max-height: 300px; overflow-y: auto; }
.diff-add { display: block; background: rgba(63,185,80,0.15); color: #3fb950; }
.diff-del { display: block; background: rgba(248,81,73,0.15); color: #f85149; }
.diff-ctx { display: block; color: var(--text-secondary); }

.commit-bar {
  position: absolute; bottom: 0; left: 0; right: 0;
  padding: 10px 12px; background: var(--bg-secondary);
  border-top: 1px solid var(--border-color); display: flex; gap: 8px;
}
.commit-input {
  flex: 1; padding: 6px 10px; border: 1px solid var(--border-color);
  border-radius: 4px; background: var(--bg-primary); color: var(--text-primary); font-size: 12px;
}
.commit-input:focus { outline: none; border-color: var(--accent-color); }
.commit-btn {
  padding: 6px 16px; border: none; border-radius: 4px;
  background: var(--accent-color); color: #fff; cursor: pointer; font-size: 12px; font-weight: 500;
}
.discard-btn {
  padding: 6px 8px; border: 1px solid var(--border-color); border-radius: 4px;
  background: transparent; cursor: pointer; font-size: 14px;
}

.action { padding: 8px 18px; border: none; border-radius: 4px; cursor: pointer; font-size: 12px; }
.action.primary { background: var(--accent-color); color: #fff; }
.action.secondary { background: var(--bg-tertiary); color: var(--text-primary); border: 1px solid var(--border-color); }

.log-list { padding: 8px; }
.log-entry {
  display: flex; align-items: center; gap: 8px; padding: 6px 8px;
  border-bottom: 1px solid rgba(255,255,255,0.04); font-size: 12px;
}
.log-entry:hover { background: rgba(255,255,255,0.03); }
.log-hash { font-family: monospace; color: var(--accent-color); font-size: 11px; min-width: 56px; cursor: pointer; }
.log-hash:hover { text-decoration: underline; }
.log-msg { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-primary); }
.log-author { color: var(--text-secondary); font-size: 11px; min-width: 60px; }
.log-date { color: var(--text-secondary); font-size: 11px; min-width: 70px; text-align: right; }

.branch-toolbar { display: flex; gap: 6px; padding: 8px 8px 4px; }
.branch-input {
  flex: 1; padding: 5px 8px; border: 1px solid var(--border-color);
  border-radius: 4px; background: var(--bg-primary); color: var(--text-primary); font-size: 12px;
}
.branch-input:focus { outline: none; border-color: var(--accent-color); }

.branch-list { padding: 4px 8px; }
.branch-entry {
  display: flex; align-items: center; gap: 6px; padding: 5px 8px;
  border-radius: 4px; font-size: 12px;
}
.branch-entry:hover { background: rgba(255,255,255,0.04); }
.branch-entry.current { background: rgba(63,185,80,0.08); }
.branch-entry.remote { opacity: 0.75; }
.branch-name { flex: 1; font-family: monospace; color: var(--text-primary); }
.current-tag { font-size: 10px; padding: 1px 5px; background: rgba(63,185,80,0.15); color: #3fb950; border-radius: 2px; }
.remote-tag { font-size: 10px; padding: 1px 5px; background: rgba(210,153,34,0.15); color: #d29922; border-radius: 2px; }

.remote-info { padding: 12px; }
.info-row { display: flex; gap: 8px; margin-bottom: 6px; font-size: 12px; }
.info-row label { color: var(--text-secondary); min-width: 70px; }
.info-row span { color: var(--text-primary); word-break: break-all; }

.remote-actions { display: flex; gap: 8px; padding: 0 12px 10px; }
.action-btn {
  flex: 1; padding: 8px; border: 1px solid var(--border-color); border-radius: 4px;
  background: var(--bg-tertiary); color: var(--text-primary); cursor: pointer; font-size: 12px; font-weight: 500;
}
.action-btn.push:hover { border-color: #3fb950; color: #3fb950; }
.action-btn.pull:hover { border-color: #2f81f7; color: #2f81f7; }
.action-btn.auth { border-color: #d29922; color: #d29922; }
.action-btn.auth:hover, .action-btn.auth.active { border-color: #e3b341; background: rgba(210, 153, 34, 0.15); color: #e3b341; }
.action-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.remote-list-section { padding: 0 12px; }
.remote-list-section h4 { font-size: 12px; color: var(--text-secondary); margin: 8px 0 6px; }
.remote-entry {
  display: flex; gap: 8px; padding: 5px 8px; font-size: 11px; border-radius: 4px;
}
.remote-entry:hover { background: rgba(255,255,255,0.03); }
.remote-entry .remote-name { font-weight: 600; color: var(--accent-color); min-width: 50px; }
.remote-entry .remote-url { color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.remote-entry.current { background: rgba(47, 129, 247, 0.08); border: 1px solid rgba(47, 129, 247, 0.2); }
.remote-entry .btn-sm.delete {
  padding: 2px 6px; border: 1px solid transparent; border-radius: 3px;
  background: transparent; color: var(--text-secondary); cursor: pointer; font-size: 11px;
}
.remote-entry .btn-sm.delete:hover { color: #dc2626; border-color: rgba(220, 38, 38, 0.3); background: rgba(220, 38, 38, 0.1); }

.add-remote-form { display: flex; gap: 4px; margin-top: 10px; }
.remote-input {
  padding: 4px 6px; border: 1px solid var(--border-color); border-radius: 3px;
  background: var(--bg-primary); color: var(--text-primary); font-size: 11px;
}
.remote-input.url { flex: 1; }
.remote-output { margin: 10px 12px; }
.remote-output pre {
  background: var(--bg-primary); padding: 8px; border-radius: 4px; font-size: 11px;
  overflow: auto; max-height: 120px; color: var(--text-secondary); white-space: pre-wrap;
}

.refresh-btn, .close-btn {
  width: 22px; height: 22px; border: none; background: transparent;
  color: var(--text-secondary); cursor: pointer; font-size: 14px;
  display: flex; align-items: center; justify-content: center; border-radius: 3px;
}
.refresh-btn:hover, .close-btn:hover { color: var(--text-primary); background: rgba(255,255,255,0.08); }
.refresh-btn:disabled { opacity: 0.3; cursor: not-allowed; }

.cred-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 9999; }
.cred-dialog { background: var(--bg-secondary); border: 1px solid var(--border-color); border-radius: 10px; padding: 20px; width: 360px; }
.cred-dialog h3 { margin: 0 0 8px; font-size: 15px; }
.cred-hint { font-size: 12px; color: var(--text-secondary); margin-bottom: 12px; }
.cred-dialog input { width: 100%; padding: 8px 10px; margin-bottom: 8px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-tertiary); color: var(--text-primary); font-size: 13px; box-sizing: border-box; outline: none; }
.cred-dialog input:focus { border-color: #d29922; }
.cred-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 4px; }
.cred-actions button { padding: 6px 16px; border: 1px solid #d29922; border-radius: 6px; background: transparent; color: #d29922; cursor: pointer; font-size: 13px; }
.cred-actions button:hover { background: rgba(210,153,34,0.15); }
.cred-actions button.secondary { border-color: var(--border-color); color: var(--text-secondary); }
.cred-actions button.secondary:hover { background: rgba(255,255,255,0.06); }
</style>
