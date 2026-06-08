<template>
  <div class="debug-panel">
    <!-- Header -->
    <div class="panel-header">
      <span class="header-icon">🐛</span>
      <span>Debugger 调试器</span>
      <span v-if="currentSession" class="session-badge" :class="sessionStatusClass">
        {{ sessionStatusText }}
      </span>
    </div>

    <!-- Session 启动区 -->
    <div class="section">
      <div class="section-title">会话控制</div>
      <div class="form-row">
        <input
          class="input"
          placeholder="程序路径或测试包 (如 ./cmd/server 或 ./...)"
          v-model="startForm.program"
          @keydown.enter="handleStart"
        />
        <select class="input select-sm" v-model="startForm.mode">
          <option value="test">test</option>
          <option value="debug">debug</option>
        </select>
        <label class="checkbox-label">
          <input type="checkbox" v-model="startForm.stopOnEntry" />
          入口暂停
        </label>
      </div>
      <div class="btn-row">
        <button class="btn btn-primary btn-sm" @click="handleStart" :disabled="loading || !!currentSession">
          ▶ 启动
        </button>
        <button class="btn btn-danger btn-sm" @click="handleStop" :disabled="loading || !currentSession">
          🛑 停止
        </button>
        <button class="btn btn-sm" @click="refreshSessions" :disabled="loading">🔄 刷新</button>
      </div>
    </div>

    <!-- 执行控制 (仅在活跃会话时显示) -->
    <div v-if="currentSession && currentSession.status === 'stopped'" class="section">
      <div class="section-title">执行控制</div>
      <div class="control-grid">
        <button class="btn ctrl-btn" @click="handleContinue" :disabled="execLoading">▶ 继续</button>
        <button class="btn ctrl-btn" @click="handleStepOver" :disabled="execLoading">⤵ Over</button>
        <button class="btn ctrl-btn" @click="handleStepInto" :disabled="execLoading">⤵ Into</button>
        <button class="btn ctrl-btn" @click="handleStepOut" :disabled="execLoading">⤴ Out</button>
        <button class="btn ctrl-btn warn" @click="handlePause" :disabled="execLoading || currentSession.status !== 'running'">⏸ 暂停</button>
      </div>
    </div>

    <!-- 断点管理 -->
    <div class="section">
      <div class="section-title">断点</div>
      <div class="form-row compact">
        <input class="input flex-1" placeholder="文件路径" v-model="bpForm.file" />
        <input class="input input-xs" type="number" placeholder="行号" v-model.number="bpForm.line" min="1" />
        <button class="btn btn-primary btn-sm" @click="handleSetBreakpoint" :disabled="bpLoading">+ 添加</button>
      </div>
      <div class="breakpoint-list">
        <div v-if="!breakpoints.length" class="empty-hint">暂无断点</div>
        <div
          v-for="bp in breakpoints"
          :key="`${bp.file}:${bp.line}`"
          class="bp-item"
          :class="{ verified: bp.verified }"
        >
          <span class="bp-dot"></span>
          <span class="bp-file">{{ bp.file | shortPath }}</span>:{{ bp.line }}
          <span v-if="bp.verified" class="bp-ok">✓</span>
          <button class="btn-icon-x" @click="handleRemoveBreakpoint(bp)" title="删除断点">×</button>
        </div>
      </div>
    </div>

    <!-- 调用栈 -->
    <div class="section collapsible">
      <div class="section-title clickable" @click="stackExpanded = !stackExpanded">
        调用栈 <span class="count" v-if="stackFrames.length">{{ stackFrames.length }} 帧</span>
        <span class="arrow">{{ stackExpanded ? '▼' : '▶' }}</span>
      </div>
      <div v-if="stackExpanded" class="frame-list">
        <div v-if="!stackFrames.length && !stackLoading" class="empty-hint">无调用栈（需在断点处暂停）</div>
        <div v-if="stackLoading" class="loading-text">加载中...</div>
        <div
          v-for="(frame, idx) in stackFrames"
          :key="idx"
          class="frame-item"
          :class="{ active: idx === 0 }"
        >
          <span class="frame-num">#{{ idx }}</span>
          <span class="frame-name">{{ frame.name }}</span>
          <span class="frame-src" v-if="frame.source">{{ frame.source.path }}:{{ frame.line }}</span>
        </div>
        <button v-if="currentSession" class="btn btn-sm btn-link" @click="handleStacktrace" :disabled="stackLoading">🔄 刷新栈</button>
      </div>
    </div>

    <!-- 变量监视 -->
    <div class="section collapsible">
      <div class="section-title clickable" @click="varsExpanded = !varsExpanded">
        变量 <span class="count" v-if="Object.keys(variables).length">{{ totalVarCount }}</span>
        <span class="arrow">{{ varsExpanded ? '▼' : '▶' }}</span>
      </div>
      <div v-if="varsExpanded" class="var-list">
        <div v-if="!Object.keys(variables).length && !varsLoading" class="empty-hint">无变量数据</div>
        <div v-if="varsLoading" class="loading-text">加载中...</div>
        <template v-for="(vars, scope) in variables" :key="scope">
          <div class="var-scope">{{ scope }} ({{ vars.length }})</div>
          <div v-for="v in vars" :key="v.name" class="var-item">
            <span class="var-name">{{ v.name }}</span>
            <span class="var-type" v-if="v.type">[{{ v.type }}]</span>
            <span class="var-value">= {{ v.value }}</span>
            <span class="var-ref" v-if="v.variablesReference > 0">+{{ v.namedVariables + v.indexedVariables }}</span>
          </div>
        </template>
        <button v-if="currentSession" class="btn btn-sm btn-link" @click="handleVariables" :disabled="varsLoading">🔄 刷新变量</button>
      </div>
    </div>

    <!-- 表达式求值 -->
    <div class="section">
      <div class="section-title">表达式求值</div>
      <div class="form-row compact">
        <input class="input flex-1" placeholder="输入表达式 (如 len(arr), obj.field)" v-model="evalExpr" @keydown.enter="handleEvaluate" />
        <button class="btn btn-primary btn-sm" @click="handleEvaluate" :disabled="evalLoading || !evalExpr">求值</button>
      </div>
      <div v-if="evalResult" class="eval-result">
        <span class="eval-label">结果:</span>
        <span class="eval-value" v-if="evalResult.type">[{{ evalResult.type }}]</span>
        {{ evalResult.value }}
      </div>
      <div v-if="evalError" class="eval-error">{{ evalError }}</div>
    </div>

    <!-- 输出日志 -->
    <div class="section fill">
      <div class="section-title">调试输出</div>
      <div class="output-log" ref="logContainer">
        <div v-for="(line, i) in outputLog" :key="i" class="log-line" :class="line.level">{{ line.text }}</div>
        <div v-if="!outputLog.length" class="empty-hint">输出将在此显示</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick } from 'vue'
import {
  DebugStart, DebugStop, DebugSessions, DebugStatus,
  DebugSetBreakpoint, DebugRemoveBreakpoint, DebugBreakpoints,
  DebugContinue, DebugStepOver, DebugStepInto, DebugStepOut, DebugPause,
  DebugStacktrace, DebugVariables, DebugEvaluate
} from '../../wailsjs/go/main/App'

// ========== Types ==========
interface Breakpoint { file: string; line: number; verified: boolean; id?: number }
interface StackFrame { id: number; name: string; source?: { path: string }; line: number }
interface Variable { name: string; value: string; type?: string; variablesReference: number; namedVariables: number; indexedVariables: number }
interface DebugSession {
  id: string; program: string; mode: string; status: string;
}

// ========== State ==========
const loading = ref(false)
const execLoading = ref(false)
const bpLoading = ref(false)
const stackLoading = ref(false)
const varsLoading = ref(false)
const evalLoading = ref(false)

const currentSession = ref<DebugSession | null>(null)
const sessions = ref<DebugSession[]>([])
const breakpoints = ref<Breakpoint[]>([])
const stackFrames = ref<StackFrame[]>([])
const variables = ref<Record<string, Variable[]>>({})
const evalResult = ref<{ value: string; type: string } | null>(null)
const evalError = ref('')
const evalExpr = ref('')
const outputLog = ref<{ text: string; level: string }[]>([])
const logContainer = ref<HTMLDivElement>()

const stackExpanded = ref(true)
const varsExpanded = ref(true)

const startForm = reactive({ program: '', mode: 'test', stopOnEntry: false })
const bpForm = reactive({ file: '', line: 0 })

// ========== Computed ==========
const sessionStatusClass = computed(() => ({
  running: currentSession.value?.status === 'running',
  stopped: currentSession.value?.status === 'stopped',
  error: currentSession.value?.status === 'error',
  exited: currentSession.value?.status === 'exited',
}))

const sessionStatusText = computed(() => {
  const s = currentSession.value
  if (!s) return ''
  const map: Record<string, string> = { running: '运行中', stopped: '已暂停', starting: '启动中', exited: '已退出', error: '错误' }
  return map[s.status] || s.status
})

const totalVarCount = computed(() =>
  Object.values(variables.value).reduce((sum, v) => sum + v.length, 0)
)

// ========== Helpers ==========
function log(msg: string, level: 'info' | 'error' | 'warn' | 'out' = 'info') {
  const time = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  outputLog.value.push({ text: `[${time}] ${msg}`, level })
  nextTick(() => {
    if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
  })
  if (outputLog.value.length > 500) outputLog.value.shift()
}

function sessionId(): string {
  return currentSession.value?.id || ''
}

// ========== Actions (Wails Bindings) ==========
async function handleStart() {
  if (!startForm.program) { log('请输入程序路径', 'error'); return }
  loading.value = true
  try {
    const session = await DebugStart(startForm.program, startForm.mode, [], startForm.stopOnEntry)
    currentSession.value = session as DebugSession
    log(`调试会话已启动 [${session.id}] ${startForm.mode} 模式`, 'info')
    await refreshBreakpoints()
  } catch (e: any) { log(`启动失败: ${e.message}`, 'error') }
  finally { loading.value = false }
}

async function handleStop() {
  loading.value = true
  try {
    await DebugStop(sessionId())
    log('调试会话已停止', 'info')
    currentSession.value = null
    stackFrames.value = []
    variables.value = {}
  } catch (e: any) { log(`停止失败: ${e.message}`, 'error') }
  finally { loading.value = false }
}

async function refreshSessions() {
  try {
    const data = await DebugSessions()
    sessions.value = (data.sessions as DebugSession[]) || []
    if (sessions.value.length > 0 && !currentSession.value) {
      currentSession.value = sessions.value[sessions.value.length - 1]
    } else if (sessions.value.length === 0) {
      currentSession.value = null
    }
  } catch {}
}

async function refreshBreakpoints() {
  try {
    const data = await DebugBreakpoints(sessionId())
    breakpoints.value = ((data as any[]) || []).map((bp: any) => ({
      file: bp.source?.path || '',
      line: bp.line,
      verified: bp.verified,
      id: bp.id,
    }))
  } catch {}
}

// --- 断点 ---
async function handleSetBreakpoint() {
  if (!bpForm.file || !bpForm.line) { log('请填写文件路径和行号', 'error'); return }
  bpLoading.value = true
  try {
    const bp = await DebugSetBreakpoint(sessionId(), bpForm.file, bpForm.line, '')
    if ((bp as any).verified) {
      log(`断点 #${(bp as any).id} 已验证 ✓ ${bpForm.file}:${bpForm.line}`, 'info')
    } else {
      log(`断点已设置但未验证 ${bpForm.file}:${bpForm.line}`, 'warn')
    }
    bpForm.file = ''; bpForm.line = 0
    await refreshBreakpoints()
  } catch (e: any) { log(`设置断点失败: ${e.message}`, 'error') }
  finally { bpLoading.value = false }
}

async function handleRemoveBreakpoint(bp: Breakpoint) {
  try {
    await DebugRemoveBreakpoint(sessionId(), bp.file, bp.line)
    log(`断点已删除 ${bp.file}:${bp.line}`, 'info')
    await refreshBreakpoints()
  } catch (e: any) { log(`删除失败: ${e.message}`, 'error') }
}

// --- 执行控制 ---
async function handleContinue() { await execAction(() => DebugContinue(sessionId()), '继续执行') }
async function handleStepOver() { await execAction(() => DebugStepOver(sessionId()), 'Step Over') }
async function handleStepInto() { await execAction(() => DebugStepInto(sessionId()), 'Step Into') }
async function handleStepOut() { await execAction(() => DebugStepOut(sessionId()), 'Step Out') }
async function handlePause() { await execAction(async () => { await DebugPause(sessionId()) }, '暂停') }

async function execAction(fn: () => Promise<any>, label: string) {
  execLoading.value = true
  try {
    await fn()
    log(label + ' 完成', 'info')
    await Promise.all([handleStacktrace(), handleVariables()])
  } catch (e: any) { log(`${label} 失败: ${e.message}`, 'error') }
  finally { execLoading.value = false }
}

// --- 调用栈 ---
async function handleStacktrace() {
  stackLoading.value = true
  try {
    const data = await DebugStacktrace(sessionId(), 20)
    stackFrames.value = ((data as any)?.frames || []) as StackFrame[]
  } catch (e: any) { log(`获取调用栈失败: ${e.message}`, 'error') }
  finally { stackLoading.value = false }
}

// --- 变量 ---
async function handleVariables() {
  varsLoading.value = true
  try {
    variables.value = (await DebugVariables(sessionId(), '')) as Record<string, Variable[]>
  } catch (e: any) { log(`获取变量失败: ${e.message}`, 'error') }
  finally { varsLoading.value = false }
}

// --- 表达式 ---
async function handleEvaluate() {
  if (!evalExpr.value) return
  evalLoading.value = true
  evalError.value = ''
  evalResult.value = null
  try {
    const result = await DebugEvaluate(sessionId(), evalExpr.value)
    evalResult.value = result as { value: string; type: string }
    log(`eval(${evalExpr.value}) = ${(result as any).value}`, 'info')
  } catch (e: any) {
    evalError.value = e.message
    log(`求值失败: ${e.message}`, 'error')
  }
  finally { evalLoading.value = false }
}
</script>

<style scoped>
.debug-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-secondary);
  font-size: 12px;
  overflow: hidden;
}

.panel-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color);
  font-size: 13px;
  font-weight: 600;
  background: var(--bg-tertiary);
  flex-shrink: 0;
}

.header-icon { font-size: 16px; }

.session-badge {
  margin-left: auto;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
}
.session-badge.running { background: rgba(63,185,80,.2); color: var(--success-color); }
.session-badge.stopped { background: rgba(210,153,34,.2); color: var(--warning-color); }
.session-badge.exited { background: rgba(125,133,144,.2); color: var(--text-secondary); }
.session-badge.error { background: rgba(248,81,73,.2); color: var(--error-color); }

.section {
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.section.fill { flex: 1; display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
.section.collapsible { padding: 4px 12px; }

.section-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-secondary);
  margin-bottom: 6px;
  letter-spacing: 0.5px;
}
.section-title.clickable { cursor: pointer; user-select: none; display: flex; align-items: center; gap: 4px; }
.arrow { margin-left: auto; font-size: 10px; }
.count { background: var(--accent-color); color: #fff; padding: 0 6px; border-radius: 8px; font-size: 10px; }

.form-row { display: flex; gap: 6px; margin-bottom: 6px; align-items: center; }
.form-row.compact { gap: 4px; }

.input {
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  color: var(--text-primary);
  padding: 5px 8px;
  font-size: 12px;
  outline: none;
  transition: border-color .15s;
}
.input:focus { border-color: var(--accent-color); }
.input.flex-1 { flex: 1; min-width: 0; }
.input-xs { width: 56px; }
.select-sm { width: 70px; flex-shrink: 0; }

.checkbox-label {
  display: flex; align-items: center; gap: 3px;
  font-size: 11px; color: var(--text-secondary);
  white-space: nowrap;
  cursor: pointer;
}

.btn-row { display: flex; gap: 6px; }

.btn-sm { padding: 4px 10px; font-size: 11px; }
.btn-link { background: transparent; border: none; color: var(--accent-color); padding: 2px 6px; }
.btn-link:hover { text-decoration: underline; }
.btn-icon-x {
  background: transparent; border: none; color: var(--text-secondary);
  cursor: pointer; font-size: 14px; padding: 0 4px;
}
.btn-icon-x:hover { color: var(--error-color); }

.control-grid { display: flex; gap: 4px; flex-wrap: wrap; }
.ctrl-btn { flex: 1; min-width: 60px; padding: 6px 4px; font-size: 11px; text-align: center; }
.ctrl-btn.warn { background: rgba(210,153,34,.15); border-color: var(--warning-color); color: var(--warning-color); }
.ctrl-btn.warn:hover { background: rgba(210,153,34,.25); }

/* Breakpoints */
.breakpoint-list { max-height: 120px; overflow-y: auto; }
.bp-item {
  display: flex; align-items: center; gap: 4px;
  padding: 3px 6px; border-radius: 3px; font-size: 11px;
  transition: background .1s;
}
.bp-item:hover { background: var(--bg-tertiary); }
.bp-dot { width: 8px; height: 14px; border-radius: 2px; background: var(--error-color); flex-shrink: 0; }
.bp-item.verified .bp-dot { background: var(--success-color); }
.bp-file { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 160px; color: var(--text-secondary); }
.bp-ok { color: var(--success-color); font-weight: bold; margin-left: auto; }

/* Call Stack */
.frame-list { max-height: 150px; overflow-y: auto; }
.frame-item {
  display: flex; gap: 6px; padding: 3px 6px; font-size: 11px;
  border-left: 2px solid transparent;
}
.frame-item.active { border-left-color: var(--accent-color); background: rgba(47,129,247,.08); }
.frame-num { color: var(--text-secondary); min-width: 22px; }
.frame-name { font-weight: 500; }
.frame-src { color: var(--text-secondary); margin-left: auto; font-size: 10px; }

/* Variables */
.var-list { max-height: 180px; overflow-y: auto; }
.var-scope {
  font-size: 10px; font-weight: 600; color: var(--accent-color);
  padding: 4px 6px 2px; text-transform: uppercase; letter-spacing: .5px;
}
.var-item {
  display: flex; gap: 4px; padding: 2px 6px 2px 14px; font-size: 11px; flex-wrap: wrap;
}
.var-name { color: #79c0ff; font-family: monospace; }
.var-type { color: var(--text-secondary); font-size: 10px; }
.var-value { color: #a5d6ff; font-family: monospace; word-break: break-all; }
.var-ref { color: var(--warning-color); font-size: 10px; }

/* Eval */
.eval-result {
  background: var(--bg-primary); border-radius: 4px; padding: 6px 8px;
  margin-top: 4px; font-family: monospace; font-size: 12px;
}
.eval-label { color: var(--text-secondary); }
.eval-value { color: var(--success-color); margin-left: 4px; }
.eval-error { color: var(--error-color); margin-top: 4px; font-size: 11px; }

/* Output Log */
.output-log {
  flex: 1; overflow-y: auto; background: var(--bg-primary);
  border-radius: 4px; padding: 6px; font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace;
  font-size: 11px; line-height: 1.5;
}
.log-line { white-space: pre-wrap; word-break: break-all; }
.log-line.info { color: var(--text-secondary); }
.log-line.error { color: var(--error-color); }
.log-line.warn { color: var(--warning-color); }
.log-line.out { color: var(--text-primary); }

.empty-hint { color: var(--text-secondary); font-style: italic; padding: 8px 0; text-align: center; }
.loading-text { color: var(--text-secondary); padding: 8px 0; text-align: center; }

:deep(button:disabled) { opacity: .45; cursor: not-allowed; }
</style>
