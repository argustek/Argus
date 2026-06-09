<template>
  <div class="token-panel">
    <div class="panel-header">
      <span class="header-icon">📊</span>
      <span>Token 监控</span>
      <span v-if="stats" class="ratio-badge" :class="ratioClass">
        {{ stats.ratio }}
      </span>
    </div>

    <!-- 概览卡片 -->
    <div class="overview" v-if="stats">
      <div class="stat-card">
        <div class="stat-value">{{ formatNum(stats.total_tokens) }}</div>
        <div class="stat-label">总 Token</div>
      </div>
      <div class="stat-card">
        <div class="stat-value used">{{ formatNum(stats.used) }}</div>
        <div class="stat-label">已用</div>
      </div>
      <div class="stat-card">
        <div class="stat-value avail">{{ formatNum(stats.available) }}</div>
        <div class="stat-label">可用</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{ stats.message_count }}</div>
        <div class="stat-label">消息数</div>
      </div>
    </div>

    <!-- 使用率条 -->
    <div class="section" v-if="stats">
      <div class="usage-bar-track">
        <div
          class="usage-bar-fill"
          :style="{ width: usagePercent + '%' }"
          :class="usageBarColor"
        ></div>
      </div>
      <div class="usage-detail">
        <span>上限: {{ formatNum(stats.max_tokens) }}</span>
        <span>输出预留: {{ formatNum(16000) }}</span>
      </div>
    </div>

    <!-- 操作按钮 -->
    <div class="btn-row section">
      <button class="btn btn-primary btn-sm" @click="handleRefresh" :disabled="loading">🔄 刷新</button>
      <button class="btn btn-sm warn-btn" @click="handleManage" :disabled="loading">🧹 自动管理</button>
      <button class="btn btn-sm danger-btn" @click="handleClear" :disabled="loading">🗑 清空上下文</button>
      <button class="btn btn-sm" @click="showPrune = !showPrune">✂️ 裁剪</button>
    </div>

    <!-- 裁剪控制 -->
    <div class="section" v-if="showPrune">
      <div class="form-row compact">
        <input
          class="input flex-1"
          type="number"
          placeholder="目标 token 上限 (如 80000)"
          v-model.number="pruneTarget"
          min="1000"
          max="128000"
        />
        <button class="btn btn-danger btn-sm" @click="handlePrune" :disabled="loading || !pruneTarget">执行裁剪</button>
      </div>
    </div>

    <!-- 分类统计 -->
    <div class="section fill collapsible" v-if="stats && stats.by_role">
      <div class="section-title clickable" @click="detailExpanded = !detailExpanded">
        详细分布 <span class="arrow">{{ detailExpanded ? '▼' : '▶' }}</span>
      </div>
      <div v-if="detailExpanded" class="detail-list">
        <!-- By Role -->
        <div class="detail-group">
          <div class="group-label">按角色</div>
          <div v-for="(count, role) in stats.by_role" :key="'r-'+role" class="detail-item">
            <span class="detail-key">{{ role }}</span>
            <div class="detail-bar-wrap">
              <div class="detail-bar" :style="{ width: barWidth(count, stats.total_tokens) + '%' }"></div>
            </div>
            <span class="detail-val">{{ formatNum(count) }}</span>
          </div>
        </div>
        <!-- By Tag -->
        <div class="detail-group" v-if="stats.by_tag">
          <div class="group-label">按标签</div>
          <div v-for="(count, tag) in stats.by_tag" :key="'t-'+tag" class="detail-item">
            <span class="detail-key">{{ tag }}</span>
            <div class="detail-bar-wrap">
              <div class="detail-bar tag-bar" :style="{ width: barWidth(count, stats.total_tokens) + '%' }"></div>
            </div>
            <span class="detail-val">{{ formatNum(count) }}</span>
          </div>
        </div>
        <!-- 统计 -->
        <div class="detail-group">
          <div class="group-label">操作统计</div>
          <div class="stat-row"><span>总添加消息</span><span>{{ stats.total_added }}</span></div>
          <div class="stat-row"><span>总裁剪数</span><span>{{ stats.total_pruned }}</span></div>
          <div class="stat-row"><span>总压缩数</span><span>{{ stats.total_compressed }}</span></div>
        </div>
      </div>
    </div>

    <!-- Token 计算器 -->
    <div class="section">
      <div class="section-title">Token 计算器</div>
      <div class="calc-area">
        <textarea
          class="input textarea"
          placeholder="粘贴文本估算 token 数..."
          v-model="calcText"
          rows="3"
        ></textarea>
        <div class="calc-result" v-if="calcResult !== null">
          <span class="calc-num">{{ calcResult }}</span> tokens
          <span class="calc-meta">({{ calcChars }} 字符 / {{ calcRunes }} 字符)</span>
        </div>
        <button class="btn btn-primary btn-sm" @click="handleCountTokens" :disabled="!calcText.trim() || counting">计算</button>
      </div>
    </div>

    <!-- 管理结果 -->
    <div class="action-log" v-if="lastAction">
      <div class="log-entry" :class="lastAction.success ? 'ok' : 'err'">
        {{ lastAction.detail }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { TokenStats, TokenManage, TokenClear, TokenCount, TokenPrune } from '../../wailsjs/go/main/App'

interface TokenStatsData {
  total_tokens: number; used: number; available: number; ratio: string;
  max_tokens: number; message_count: number;
  by_role?: Record<string, number>; by_tag?: Record<string, number>;
  total_added: number; total_pruned: number; total_compressed: number;
}

const loading = ref(false)
const counting = ref(false)
const stats = ref<TokenStatsData | null>(null)
const showPrune = ref(false)
const pruneTarget = ref(80000)
const detailExpanded = ref(true)

const calcText = ref('')
const calcResult = ref<number | null>(null)
const calcChars = ref(0)
const calcRunes = ref(0)

const lastAction = ref<{ success: boolean; detail: string } | null>(null)

// ========== Computed ==========
const usagePercent = computed(() => {
  if (!stats.value) return 0
  return Math.min(100, Math.round((stats.value.used / stats.value.max_tokens) * 100))
})

const ratioClass = computed(() => {
  if (!stats.value) return ''
  const r = parseFloat(stats.value.ratio)
  if (r >= 90) return 'danger'
  if (r >= 70) return 'warn'
  return 'ok'
})

const usageBarColor = computed(() => {
  const p = usagePercent.value
  if (p >= 90) return 'bar-danger'
  if (p >= 70) return 'bar-warn'
  return 'bar-ok'
})

// ========== Lifecycle ==========
// [v0.7.3] IMPORTANT: Wails EventsOff(eventName, callback) does NOT support callback param!
// The 2nd arg is ...additionalEventNames (string[]), so calling EventsOff on shared events
// kills ALL listeners including App.vue's global ACK listener.
// Solution: use mounted flag to silently ignore events after unmount, never call EventsOff.
const _mounted = ref(false)

onMounted(() => {
  _mounted.value = true
  handleRefresh()
  // [v0.7.2] Token stats: receive via MessageBus event, ACK to prevent LOST alert
  EventsOn('token_stats', (data: any) => {
    if (!_mounted.value) return // ignore after unmount
    if (data && typeof data === 'object') {
      stats.value = data as TokenStatsData
      if (data._msgId) { ;(window as any).__argusAck?.(data._msgId) }
    }
  })

  // [v0.7.2] ContextBuilder: task context build result
  EventsOn('context_built', (data: any) => {
    if (!_mounted.value) return
    if (data && typeof data === 'object') {
      console.log('[TokenMonitor] context_built received:', data.task_id, 'len=', data.context?.length)
      if (data._msgId) { ;(window as any).__argusAck?.(data._msgId) }
    }
  })

  // [v0.7.2] Compressor: compression complete notification
  EventsOn('compress_done', (data: any) => {
    if (!_mounted.value) return
    if (data && typeof data === 'object') {
      console.log('[TokenMonitor] compress_done received:', data.task_id, 'compressed=', data.compressed_count)
      lastAction.value = { success: true, detail: `对话已压缩 ${data.compressed_count} 条旧消息` }
      if (data._msgId) { ;(window as any).__argusAck?.(data._msgId) }
    }
  })
})

onUnmounted(() => {
  _mounted.value = false
  // DO NOT call EventsOff on shared events — it would kill App.vue global ACK listeners!
  // Callbacks become no-ops via _mounted flag check above.
})

// ========== Helpers ==========
function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

function barWidth(part: number, total: number): number {
  if (!total) return 0
  return Math.max(2, Math.round((part / total) * 100))
}

// ========== Actions (Wails Bindings) ==========
async function handleRefresh() {
  loading.value = true
  try {
    const data = await TokenStats()
    stats.value = data as TokenStatsData
  } catch (e: any) { console.error('[TokenMonitor] refresh error:', e.message) }
  finally { loading.value = false }
}

async function handleManage() {
  loading.value = true
  try {
    const res = await TokenManage()
    lastAction.value = { success: res.action_taken as boolean, detail: res.detail as string }
    await handleRefresh()
  } catch (e: any) { lastAction.value = { success: false, detail: e.message } }
  finally { loading.value = false }
}

async function handleClear() {
  loading.value = true
  try {
    await TokenClear()
    lastAction.value = { success: true, detail: '上下文已清空（保留 system prompt）' }
    await handleRefresh()
  } catch (e: any) { lastAction.value = { success: false, detail: e.message } }
  finally { loading.value = false }
}

async function handlePrune() {
  if (!pruneTarget.value) return
  loading.value = true
  try {
    const res = await TokenPrune(pruneTarget.value)
    lastAction.value = { success: true, detail: `裁剪了 ${res.pruned} 条消息` }
    await handleRefresh()
  } catch (e: any) { lastAction.value = { success: false, detail: e.message } }
  finally { loading.value = false }
}

async function handleCountTokens() {
  counting.value = true
  try {
    const res = await TokenCount(calcText.value)
    calcResult.value = res.token_count as number
    calcChars.value = res.char_count as number
    calcRunes.value = res.rune_count as number
  } catch (e: any) { lastAction.value = { success: false, detail: e.message } }
  finally { counting.value = false }
}
</script>

<style scoped>
.token-panel {
  display: flex; flex-direction: column; height: 100%;
  background: var(--bg-secondary); font-size: 12px; overflow: hidden;
}
.panel-header {
  display: flex; align-items: center; gap: 6px; padding: 10px 12px;
  border-bottom: 1px solid var(--border-color); font-size: 13px; font-weight: 600;
  background: var(--bg-tertiary); flex-shrink: 0;
}
.header-icon { font-size: 16px; }

.ratio-badge {
  margin-left: auto; padding: 2px 8px; border-radius: 10px;
  font-size: 11px; font-weight: 700; font-family: monospace;
}
.ratio-badge.ok { background: rgba(63,185,80,.2); color: var(--success-color); }
.ratio-badge.warn { background: rgba(210,153,34,.2); color: var(--warning-color); }
.ratio-badge.danger { background: rgba(248,81,73,.2); color: var(--error-color); }

.overview { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; padding: 8px 12px; border-bottom: 1px solid var(--border-color); }

.stat-card {
  background: var(--bg-tertiary); border-radius: 6px; padding: 8px; text-align: center;
}
.stat-value { font-size: 18px; font-weight: 700; font-family: monospace; color: var(--accent-color); }
.stat-value.used { color: #f0883e; }
.stat-value.avail { color: var(--success-color); }
.stat-label { font-size: 10px; color: var(--text-secondary); margin-top: 2px; text-transform: uppercase; }

.section { padding: 8px 12px; border-bottom: 1px solid var(--border-color); flex-shrink: 0; }
.section.fill { flex: 1; min-height: 0; overflow: hidden; display: flex; flex-direction: column; }
.section.collapsible { padding: 4px 12px; }

.section-title {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  color: var(--text-secondary); margin-bottom: 6px; letter-spacing: .5px;
}
.section-title.clickable { cursor: pointer; user-select: none; display: flex; align-items: center; gap: 4px; }
.arrow { margin-left: auto; font-size: 10px; }

/* Usage Bar */
.usage-bar-track {
  height: 6px; background: var(--bg-tertiary); border-radius: 3px; overflow: hidden;
}
.usage-bar-fill { height: 100%; border-radius: 3px; transition: width .4s ease; }
.usage-bar-fill.bar-ok { background: var(--success-color); }
.usage-bar-fill.bar-warn { background: var(--warning-color); }
.usage-bar-fill.bar-danger { background: var(--error-color); }

.usage-detail {
  display: flex; justify-content: space-between; margin-top: 4px;
  font-size: 10px; color: var(--text-secondary);
}

.btn-row { display: flex; gap: 6px; flex-wrap: wrap; }
.btn-sm { padding: 4px 10px; font-size: 11px; }
.warn-btn { background: rgba(210,153,34,.15); border-color: var(--warning-color); color: var(--warning-color); }
.danger-btn { background: rgba(248,81,73,.1); border-color: var(--error-color); color: var(--error-color); }

.form-row { display: flex; gap: 6px; align-items: center; }
.form-row.compact { gap: 4px; }
.input {
  background: var(--bg-primary); border: 1px solid var(--border-color);
  border-radius: 4px; color: var(--text-primary); padding: 5px 8px;
  font-size: 12px; outline: none; transition: border-color .15s;
}
.input:focus { border-color: var(--accent-color); }
.input.flex-1 { flex: 1; min-width: 0; }
.textarea { resize: vertical; font-family: monospace; }

/* Detail */
.detail-list { overflow-y: auto; }
.detail-group { margin-bottom: 10px; }
.group-label { font-size: 10px; font-weight: 600; color: var(--accent-color); text-transform: uppercase; margin-bottom: 4px; }
.detail-item { display: flex; align-items: center; gap: 6px; padding: 2px 0; font-size: 11px; }
.detail-key { width: 70px; color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.detail-bar-wrap { flex: 1; height: 5px; background: var(--bg-tertiary); border-radius: 2px; overflow: hidden; }
.detail-bar { height: 100%; background: var(--accent-color); border-radius: 2px; transition: width .3s; }
.tag-bar { background: #a371f7; }
.detail-val { width: 50px; text-align: right; font-family: monospace; font-size: 10px; color: var(--text-secondary); }

.stat-row { display: flex; justify-content: space-between; padding: 2px 0; font-size: 11px; color: var(--text-secondary); }

/* Calculator */
.calc-area { display: flex; flex-direction: column; gap: 4px; }
.calc-result { display: flex; align-items: baseline; gap: 6px; font-size: 13px; }
.calc-num { font-size: 20px; font-weight: 700; font-family: monospace; color: var(--accent-color); }
.calc-meta { font-size: 10px; color: var(--text-secondary); }

/* Action Log */
.action-log { padding: 4px 12px; }
.log-entry { padding: 4px 8px; border-radius: 4px; font-size: 11px; }
.log-entry.ok { background: rgba(63,185,80,.1); color: var(--success-color); }
.log-entry.err { background: rgba(248,81,73,.1); color: var(--error-color); }

:deep(button:disabled) { opacity: .45; cursor: not-allowed; }
</style>
