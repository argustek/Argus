<template>
  <div class="mcp-panel">
    <div class="panel-header">
      <span class="header-icon">🔌</span>
      <span>MCP 工具协议</span>
    </div>

    <!-- Server 管理 -->
    <div class="section">
      <div class="section-title">MCP Servers</div>
      <div class="server-list">
        <div v-if="!servers.length" class="empty-hint">暂无已连接的 Server</div>
        <div
          v-for="srv in servers"
          :key="srv.name"
          :class="['server-card', { connected: srv.status === 'connected' }]"
        >
          <div class="server-info">
            <span class="server-status-dot" :class="srv.status"></span>
            <span class="server-name">{{ srv.name }}</span>
            <span class="server-type">{{ srv.transport }}</span>
          </div>
          <div class="server-meta" v-if="srv.tool_count">
            {{ srv.tool_count }} 个工具可用
          </div>
          <button class="btn-icon-x" @click="handleRemoveServer(srv.name)" title="移除">×</button>
        </div>
      </div>

      <!-- 添加 Server -->
      <div class="form-row compact">
        <input class="input flex-1" placeholder="Server 名称 (如 filesystem)" v-model="addForm.name" />
        <select class="input select-sm" v-model="addForm.transport">
          <option value="stdio">stdio</option>
          <option value="sse">sse</option>
        </select>
        <input class="input input-xs" placeholder="命令/URL" v-model="addForm.command" />
        <button class="btn btn-primary btn-sm" @click="handleAddServer" :disabled="loading">+ 添加</button>
      </div>
    </div>

    <!-- 工具列表 -->
    <div class="section fill collapsible">
      <div class="section-title clickable" @click="toolsExpanded = !toolsExpanded">
        可用工具 <span class="count" v-if="tools.length">{{ tools.length }}</span>
        <span class="arrow">{{ toolsExpanded ? '▼' : '▶' }}</span>
      </div>
      <div v-if="toolsExpanded" class="tool-list">
        <div v-if="!tools.length && !toolsLoading" class="empty-hint">请先添加并连接 Server</div>
        <div v-if="toolsLoading" class="loading-text">加载中...</div>
        <div
          v-for="(tool, idx) in tools"
          :key="idx"
          class="tool-item"
          @click="selectedTool = tool"
          :class="{ active: selectedTool?.name === tool.name }"
        >
          <span class="tool-server">{{ tool.server_name || 'unknown' }}</span>
          <span class="tool-name">{{ tool.name }}</span>
          <span class="tool-desc">{{ tool.description | truncate(60) }}</span>
        </div>
        <button v-if="servers.some(s => s.status === 'connected')" class="btn btn-sm btn-link" @click="handleListTools" :disabled="toolsLoading">🔄 刷新工具列表</button>
      </div>
    </div>

    <!-- 工具调用 -->
    <div class="section" v-if="selectedTool">
      <div class="section-title">调用: {{ selectedTool.name }}</div>
      <div class="tool-detail">
        <div class="detail-row">
          <label>描述:</label>
          <span>{{ selectedTool.description }}</span>
        </div>
        <div class="detail-row" v-if="selectedTool.input_schema">
          <label>参数:</label>
          <pre class="schema-preview">{{ formatSchema(selectedTool.input_schema) }}</pre>
        </div>
      </div>
      <div class="form-area">
        <textarea
          class="input textarea"
          placeholder='输入 JSON 参数，如 {"path": "/tmp/test"}'
          v-model="callArgs"
          rows="3"
        ></textarea>
        <div class="btn-row">
          <button class="btn btn-primary btn-sm" @click="handleCallTool" :disabled="callLoading">执行调用</button>
          <button class="btn btn-sm" @click="selectedTool = null; callArgs = ''; callResult.value = ''">清空</button>
        </div>
      </div>
      <!-- 调用结果 -->
      <div v-if="callResult" class="call-result">
        <div class="result-header">返回结果</div>
        <pre class="result-body">{{ callResult }}</pre>
      </div>
      <div v-if="callError" class="call-error">{{ callError }}</div>
    </div>

    <!-- 操作日志 -->
    <div class="section fill">
      <div class="section-title">操作日志</div>
      <div class="log-area" ref="mcpLogContainer">
        <div v-for="(entry, i) in logEntries" :key="i" class="log-entry" :class="entry.level">
          <span class="log-time">{{ entry.time }}</span>
          <span class="log-msg">{{ entry.msg }}</span>
        </div>
        <div v-if="!logEntries.length" class="empty-hint">日志将在此显示</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, nextTick, onMounted } from 'vue'
import { MCPServers, MCPAddServer, MCPRemoveServer, MCPTools, MCPCallTool } from '../../wailsjs/go/main/App'

// ========== Types ==========
interface MCPServer {
  name: string; status: string; transport: string;
  command?: string; tool_count?: number;
}
interface MCPTool {
  name: string; description: string; server_name?: string;
  input_schema?: Record<string, any>;
}

// ========== State ==========
const loading = ref(false)
const toolsLoading = ref(false)
const callLoading = ref(false)
const servers = ref<MCPServer[]>([])
const tools = ref<MCPTool[]>([])
const selectedTool = ref<MCPTool | null>(null)
const callArgs = ref('')
const callResult = ref('')
const callError = ref('')
const logContainer = ref<HTMLDivElement>()
const mcpLogContainer = ref<HTMLDivElement>()
const toolsExpanded = ref(true)

const addForm = reactive({ name: '', transport: 'stdio', command: '' })
const logEntries = ref<{ time: string; msg: string; level: string }[]>([])

// ========== Lifecycle ==========
onMounted(() => {
  handleListServers()
})

// ========== Helpers ==========
function addLog(msg: string, level: 'info' | 'error' | 'warn' = 'info') {
  const time = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  logEntries.value.push({ time, msg, level })
  nextTick(() => {
    const el = mcpLogContainer.value
    if (el) el.scrollTop = el.scrollHeight
  })
  if (logEntries.value.length > 300) logEntries.value.shift()
}

function truncate(s: string, n: number) {
  return s.length > n ? s.slice(0, n) + '...' : s
}

function formatSchema(schema: Record<string, any>): string {
  try { return JSON.stringify(schema, null, 2) } catch { return String(schema) }
}

// ========== Actions (Wails Bindings) ==========
async function handleListServers() {
  try {
    const data = await MCPServers()
    servers.value = (data.servers as MCPServer[]) || []
  } catch (e: any) { addLog(`获取 Server 列表失败: ${e.message}`, 'error') }
}

async function handleAddServer() {
  if (!addForm.name) { addLog('请输入 Server 名称', 'warn'); return }
  loading.value = true
  try {
    await MCPAddServer(addForm.name, addForm.command, [], {})
    addLog(`Server [${addForm.name}] 已添加 (${addForm.transport})`, 'info')
    addForm.name = ''; addForm.command = ''
    await handleListServers()
  } catch (e: any) { addLog(`添加失败: ${e.message}`, 'error') }
  finally { loading.value = false }
}

async function handleRemoveServer(name: string) {
  try {
    await MCPRemoveServer(name)
    addLog(`Server [${name}] 已移除`, 'info')
    await handleListServers()
  } catch (e: any) { addLog(`移除失败: ${e.message}`, 'error') }
}

async function handleListTools() {
  toolsLoading.value = true
  try {
    const data = await MCPTools()
    tools.value = (data as MCPTool[]) || []
    addLog(`获取到 ${tools.value.length} 个工具`, 'info')
  } catch (e: any) { addLog(`获取工具失败: ${e.message}`, 'error') }
  finally { toolsLoading.value = false }
}

async function handleCallTool() {
  if (!selectedTool.value) return
  let args: any = {}
  try { if (callArgs.value.trim()) args = JSON.parse(callArgs.value) }
  catch { addLog('参数 JSON 格式错误', 'error'); return }

  callLoading.value = true
  callResult.value = ''
  callError.value = ''
  try {
    const res = await MCPCallTool(selectedTool.value.name, args)
    callResult.value = typeof res === 'string' ? res : JSON.stringify(res, null, 2)
    addLog(`${selectedTool.value.name} 调用成功`, 'info')
  } catch (e: any) {
    callError.value = e.message
    addLog(`调用失败: ${e.message}`, 'error')
  }
  finally { callLoading.value = false }
}
</script>

<style scoped>
.mcp-panel {
  display: flex; flex-direction: column; height: 100%;
  background: var(--bg-secondary); font-size: 12px; overflow: hidden;
}
.panel-header {
  display: flex; align-items: center; gap: 6px; padding: 10px 12px;
  border-bottom: 1px solid var(--border-color); font-size: 13px; font-weight: 600;
  background: var(--bg-tertiary); flex-shrink: 0;
}
.header-icon { font-size: 16px; }

.section { padding: 8px 12px; border-bottom: 1px solid var(--border-color); flex-shrink: 0; }
.section.fill { flex: 1; display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
.section.collapsible { padding: 4px 12px; }

.section-title {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  color: var(--text-secondary); margin-bottom: 6px; letter-spacing: .5px;
}
.section-title.clickable { cursor: pointer; user-select: none; display: flex; align-items: center; gap: 4px; }
.arrow { margin-left: auto; font-size: 10px; }
.count { background: #a371f7; color: #fff; padding: 0 6px; border-radius: 8px; font-size: 10px; }

.form-row { display: flex; gap: 6px; margin-bottom: 6px; align-items: center; }
.form-row.compact { gap: 4px; }

.input {
  background: var(--bg-primary); border: 1px solid var(--border-color);
  border-radius: 4px; color: var(--text-primary); padding: 5px 8px;
  font-size: 12px; outline: none; transition: border-color .15s;
}
.input:focus { border-color: #a371f7; }
.input.flex-1 { flex: 1; min-width: 0; }
.input-xs { width: 70px; }
.select-sm { width: 65px; flex-shrink: 0; }
.textarea { resize: vertical; font-family: monospace; min-height: 50px; }

.btn-row { display: flex; gap: 6px; margin-top: 4px; }
.btn-sm { padding: 4px 10px; font-size: 11px; }
.btn-link { background: transparent; border: none; color: #a371f7; padding: 2px 6px; }
.btn-link:hover { text-decoration: underline; }
.btn-icon-x {
  background: transparent; border: none; color: var(--text-secondary);
  cursor: pointer; font-size: 14px; padding: 0 4px;
}
.btn-icon-x:hover { color: var(--error-color); }

/* Servers */
.server-list { max-height: 140px; overflow-y: auto; margin-bottom: 6px; }
.server-card {
  display: flex; align-items: center; gap: 6px; padding: 6px 8px;
  margin-bottom: 3px; border-radius: 4px; background: var(--bg-tertiary);
  transition: background .15s;
}
.server-card:hover { background: var(--border-color); }
.server-card.connected { border-left: 3px solid #a371f7; }

.server-info { display: flex; align-items: center; gap: 6px; flex: 1; min-width: 0; }
.server-status-dot {
  width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0;
}
.server-status-dot.connected { background: var(--success-color); box-shadow: 0 0 4px var(--success-color); }
.server-status_dot.disconnected { background: var(--text-secondary); }
.server-status-dot.error { background: var(--error-color); }
.server-name { font-weight: 500; font-size: 12px; }
.server-type {
  font-size: 10px; color: var(--text-secondary); background: var(--bg-primary);
  padding: 1px 5px; border-radius: 3px;
}
.server-meta { font-size: 10px; color: var(--text-secondary); }

/* Tools */
.tool-list { max-height: 200px; overflow-y: auto; }
.tool-item {
  display: flex; flex-direction: column; gap: 1px; padding: 5px 8px;
  cursor: pointer; border-radius: 4px; transition: background .1s;
}
.tool-item:hover { background: var(--bg-tertiary); }
.tool-item.active { background: rgba(163,113,247,.12); border-left: 2px solid #a371f7; }
.tool-server { font-size: 9px; color: #a371f7; text-transform: uppercase; }
.tool-name { font-weight: 500; font-size: 11px; }
.tool-desc { font-size: 10px; color: var(--text-secondary); }

/* Tool Detail */
.tool-detail { margin-bottom: 6px; }
.detail-row { margin-bottom: 4px; }
.detail-row label { color: var(--text-secondary); font-size: 11px; display: block; margin-bottom: 2px; }
.schema-preview {
  background: var(--bg-primary); border-radius: 4px; padding: 6px;
  font-family: monospace; font-size: 10px; max-height: 100px; overflow-y: auto;
  white-space: pre-wrap; word-break: break-all;
}

.form-area { margin-top: 4px; }

.call-result { margin-top: 6px; }
.result-header { font-size: 11px; font-weight: 600; color: var(--success-color); margin-bottom: 2px; }
.result-body {
  background: var(--bg-primary); border-radius: 4px; padding: 6px;
  font-family: monospace; font-size: 11px; max-height: 150px; overflow-y: auto;
  white-space: pre-wrap; word-break: break-all;
}
.call-error { color: var(--error-color); margin-top: 4px; font-size: 11px; }

/* Log */
.log-area {
  flex: 1; overflow-y: auto; background: var(--bg-primary);
  border-radius: 4px; padding: 6px; font-size: 11px;
}
.log-entry { display: flex; gap: 6px; padding: 2px 0; }
.log-time { color: var(--text-secondary); min-width: 55px; font-size: 10px; }
.log-entry.info .log-msg { color: var(--text-secondary); }
.log-entry.error .log-msg { color: var(--error-color); }
.log-entry.warn .log-msg { color: var(--warning-color); }

.empty-hint { color: var(--text-secondary); font-style: italic; padding: 8px 0; text-align: center; }
.loading-text { color: var(--text-secondary); padding: 8px 0; text-align: center; }

:deep(button:disabled) { opacity: .45; cursor: not-allowed; }
</style>
