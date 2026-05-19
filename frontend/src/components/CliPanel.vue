<template>
  <div class="cli-panel" @click.stop>
    <div class="cli-header">
      <span class="cli-title">⌨️ Argus CLI</span>
      <span class="cli-hint">输入命令 · Enter执行 · ↑↓历史 · Tab补全</span>
      <button class="cli-close" @click="$emit('close')">✕</button>
    </div>
    
    <div class="cli-output" ref="outputRef">
      <div v-for="(item, idx) in history" :key="idx" class="cli-history-item">
        <span class="cli-prompt">{{ item.prompt }}</span>
        <pre class="cli-output-text" :class="item.type">{{ item.output }}</pre>
      </div>
    </div>

    <div class="cli-input-wrapper">
      <span class="cli-prefix">❯</span>
      <input
        ref="inputRef"
        type="text"
        v-model="currentInput"
        class="cli-input"
        :placeholder="placeholder"
        @keydown.enter="executeCommand"
        @keydown.up.prevent="navigateHistory(-1)"
        @keydown.down.prevent="navigateHistory(1)"
        @keydown.tab.prevent="handleTabComplete"
        @keydown.escape="$emit('close')"
        spellcheck="false"
        autocomplete="off"
      />
      <div v-if="showSuggestions" class="cli-suggestions">
        <div 
          v-for="(sug, i) in suggestions" 
          :key="i"
          class="suggestion-item"
          :class="{ active: i === suggestionIndex }"
          @click="applySuggestion(sug)"
        >
          <span class="sug-cmd">{{ sug.cmd }}</span>
          <span class="sug-desc">{{ sug.desc }}</span>
        </div>
      </div>
    </div>

    <div class="cli-footer">
      <span class="cli-stats">{{ commandHistory.length }} 条历史</span>
      <span class="cli-commands">
        <kbd v-for="cmd in quickCommands" :key="cmd.name" @click="runQuick(cmd)" :title="cmd.desc">{{ cmd.name }}</kbd>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue'

const emit = defineEmits(['command', 'close'])

const inputRef = ref<HTMLInputElement>()
const outputRef = ref<HTMLElement>()
const currentInput = ref('')
const placeholder = ref('argus> help 查看所有命令')
const history = ref<Array<{ prompt: string, output: string, type: string }>>([])
const commandHistory = ref<string[]>([])
const historyIndex = ref(-1)
const showSuggestions = ref(false)
const suggestions = ref<Array<{ cmd: string, desc: string }>>([])
const suggestionIndex = ref(0)

interface QuickCommand {
  name: string
  cmd: string
  desc: string
}

const quickCommands: QuickCommand[] = [
  { name: 'status', cmd: 'status', desc: '查看状态' },
  { name: 'msg', cmd: 'messages', desc: '查看消息' },
  { name: 'log', cmd: 'log', desc: '查看日志' },
  { name: 'clear', cmd: 'clear', desc: '清屏' },
]

const cliCommands: Record<string, { desc: string, usage?: string }> = {
  'help': { desc: '显示帮助信息', usage: 'help [命令]' },
  'status': { desc: '查看系统状态', usage: 'status' },
  'messages': { desc: '查看最近消息', usage: 'messages [数量]' },
  'log': { desc: '查看日志', usage: 'log [行数]' },
  'clear': { desc: '清空屏幕', usage: 'clear' },
  'history': { desc: '显示命令历史', usage: 'history [数量]' },
  'send': { desc: '发送消息给AI', usage: 'send <消息内容>' },
  '@pm': { desc: '发送消息给PM', usage: '@pm <消息>' },
  '@se': { desc: '发送消息给SE', usage: '@se <任务描述>' },
  'stop': { desc: '停止当前任务', usage: 'stop' },
  'reset': { desc: '重置系统状态', usage: 'reset' },
  'config': { desc: '查看/设置配置', usage: 'config [key] [value]' },
  'version': { desc: '显示版本信息', usage: 'version' },
  'time': { desc: '显示当前时间', usage: 'time' },
  'whoami': { desc: '显示当前用户角色', usage: 'whoami' },
}

onMounted(() => {
  nextTick(() => inputRef.value?.focus())
  
  addOutput('╔══════════════════════════════════════╗\n║  ⌨️ Argus CLI v1.0                    ║\n║  输入 help 查看所有可用命令            ║\n╚══════════════════════════════════════╝', 'welcome')
})

function executeCommand() {
  const cmd = currentInput.value.trim()
  if (!cmd) return

  addOutput(`❯ ${cmd}`, 'input')
  commandHistory.value.push(cmd)
  if (commandHistory.value.length > 200) {
    commandHistory.value = commandHistory.value.slice(-200)
  }
  historyIndex.value = -1

  const result = processCommand(cmd)
  if (result) {
    addOutput(result.output, result.type || 'output')
  }

  if (result?.sendToChat) {
    emit('command', result.sendToChat)
  }

  currentInput.value = ''
  showSuggestions.value = false
  
  nextTick(() => {
    if (outputRef.value) {
      outputRef.value.scrollTop = outputRef.value.scrollHeight
    }
  })
}

function processCommand(cmd: string): { output: string, type?: string, sendToChat?: string } | null {
  const parts = cmd.split(/\s+/)
  const command = parts[0].toLowerCase()
  const args = parts.slice(1).join(' ')

  switch (command) {
    case 'help':
      return { output: generateHelp(args), type: 'info' }
    
    case 'status':
      return { output: '📊 系统运行中...\n   状态: 🟢 正常\n   时间: ' + new Date().toLocaleString(), type: 'success' }
    
    case 'messages':
      return { output: `📨 最近 ${args ? args : '10'} 条消息\n   (功能开发中...)`, type: 'info' }
    
    case 'log':
      return { output: `📋 日志功能开发中...`, type: 'warn' }
    
    case 'clear':
      history.value = []
      return null
    
    case 'history':
      const count = parseInt(args) || 20
      const h = commandHistory.value.slice(-count)
      return { 
        output: h.length > 0 ? h.map((c, i) => `  ${String(i + 1).padStart(3)}. ${c}`).join('\n') : '  (无历史记录)', 
        type: 'info' 
      }
    
    case 'send':
      if (!args) return { output: '❌ 用法: send <消息内容>', type: 'error' }
      return { output: `✅ 已发送: ${args}`, type: 'success', sendToChat: args }
    
    case '@pm':
      if (!args) return { output: '❌ 用法: @pm <消息>', type: 'error' }
      return { output: `✅ 发送给PM: ${args}`, type: 'success', sendToChat: `@PM ${args}` }
    
    case '@se':
      if (!args) return { output: '❌ 用法: @se <任务>', type: 'error' }
      return { output: `✅ 发送给SE: ${args}`, type: 'success', sendToChat: `@SE ${args}` }
    
    case 'stop':
      return { output: '⏹️ 停止信号已发送', type: 'warn', sendToChat: '' }
    
    case 'reset':
      return { output: '🔄 重置请求已发送', type: 'warn', sendToChat: 'reset' }
    
    case 'version':
      return { output: 'Argus Desktop v2.0\nBuild: ' + new Date().toISOString().split('T')[0] + '\nEngine: GLM-5 Turbo', type: 'info' }
    
    case 'time':
      return { output: '🕐 ' + new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }), type: 'success' }
    
    case 'whoami':
      return { output: '👤 USR (用户)\n   权限: 超级管理员\n   角色: 最终决策者', type: 'info' }
    
    default:
      if (cmd.startsWith('@')) {
        return { output: `✅ 直接发送: ${cmd}`, type: 'success', sendToChat: cmd }
      }
      return { 
        output: `❓ 未知命令: ${command}\n   输入 help 查看可用命令`, 
        type: 'error',
        sendToChat: cmd
      }
  }
}

function generateHelp(filter?: string): string {
  let output = '📖 Argus CLI 命令列表:\n\n'
  
  const commands = Object.entries(cliCommands)
    .filter(([name]) => !filter || name.includes(filter.toLowerCase()))
  
  if (commands.length === 0) {
    return `❌ 未找到匹配 "${filter}" 的命令`
  }

  for (const [name, info] of commands) {
    output += `  ${name.padEnd(12)} ${info.desc}\n`
    if (info.usage) {
      output += `              用法: ${info.usage}\n`
    }
  }

  output += '\n💡 提示: 直接输入文字会作为消息发送给AI'
  return output
}

function navigateHistory(direction: number) {
  if (commandHistory.value.length === 0) return

  historyIndex.value += direction

  if (historyIndex.value < 0) {
    historyIndex.value = -1
    currentInput.value = ''
    return
  }

  if (historyIndex.value >= commandHistory.value.length) {
    historyIndex.value = commandHistory.value.length - 1
  }

  currentInput.value = commandHistory.value[commandHistory.value.length - 1 - historyIndex.value]
}

function handleTabComplete() {
  const input = currentInput.value.toLowerCase()
  if (!input) {
    showSuggestions.value = true
    suggestions.value = Object.entries(cliCommands).map(([cmd, info]) => ({ cmd, desc: info.desc }))
    suggestionIndex.value = 0
    return
  }

  const matches = Object.keys(cliCommands).filter(c => c.startsWith(input))
  if (matches.length === 1) {
    currentInput.value = matches[0] + ' '
    showSuggestions.value = false
  } else if (matches.length > 1) {
    suggestions.value = matches.map(m => ({ cmd: m, desc: cliCommands[m]?.desc || '' }))
    showSuggestions.value = true
    suggestionIndex.value = 0
  }
}

function applySuggestion(sug: { cmd: string }) {
  currentInput.value = sug.cmd + ' '
  showSuggestions.value = false
  inputRef.value?.focus()
}

function runQuick(cmd: QuickCommand) {
  currentInput.value = cmd.cmd
  executeCommand()
}

function addOutput(text: string, type: string = 'output') {
  history.value.push({ prompt: '', output: text, type })
}
</script>

<style scoped>
.cli-panel {
  background: #1a1a2e;
  border-top: 2px solid #667eea;
  border-radius: 8px;
  margin: 4px 8px;
  max-height: 300px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 4px 20px rgba(102, 126, 234, 0.3);
  animation: slideUp 0.2s ease;
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

.cli-header {
  display: flex;
  align-items: center;
  padding: 6px 12px;
  background: linear-gradient(135deg, #667eea20, #764ba220);
  border-bottom: 1px solid #667eea40;
}

.cli-title {
  font-weight: bold;
  color: #667eea;
  font-size: 13px;
  margin-right: 12px;
}

.cli-hint {
  flex: 1;
  font-size: 11px;
  color: #888;
}

.cli-close {
  background: none;
  border: none;
  color: #888;
  cursor: pointer;
  font-size: 14px;
  padding: 2px 6px;
}
.cli-close:hover { color: #ff6b6b; }

.cli-output {
  flex: 1;
  overflow-y: auto;
  padding: 8px 12px;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  line-height: 1.5;
  min-height: 100px;
  max-height: 180px;
}

.cli-history-item {
  margin-bottom: 4px;
}

.cli-prompt {
  color: #667eea;
  font-weight: bold;
}

.cli-output-text {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  color: #d4d4d4;
}
.cli-output-text.welcome { color: #667eea; }
.cli-output-text.input { color: #ffd93d; }
.cli-output-text.success { color: #6bcb77; }
.cli-output-text.error { color: #ff6b6b; }
.cli-output-text.info { color: #4ecdc4; }
.cli-output-text.warn { color: #ffd93d; }

.cli-input-wrapper {
  position: relative;
  padding: 8px 12px;
  border-top: 1px solid #333;
  display: flex;
  align-items: center;
  background: #16213e;
}

.cli-prefix {
  color: #667eea;
  font-weight: bold;
  margin-right: 8px;
  font-size: 16px;
}

.cli-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  color: #fff;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 13px;
  caret-color: #667eea;
}
.cli-input::placeholder { color: #555; }

.cli-suggestions {
  position: absolute;
  bottom: 100%;
  left: 30px;
  right: 12px;
  background: #1a1a2e;
  border: 1px solid #667eea50;
  border-radius: 6px;
  max-height: 150px;
  overflow-y: auto;
  z-index: 100;
  box-shadow: 0 -4px 15px rgba(0,0,0,0.5);
}

.suggestion-item {
  padding: 6px 12px;
  cursor: pointer;
  display: flex;
  justify-content: space-between;
  font-size: 12px;
}
.suggestion-item:hover,
.suggestion-item.active {
  background: #667eea30;
}
.sug-cmd { color: #667eea; font-weight: bold; }
.sug-desc { color: #888; }

.cli-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 4px 12px;
  background: #0f0f23;
  border-top: 1px solid #333;
  font-size: 10px;
  color: #666;
}

.cli-commands kbd {
  background: #333;
  color: #aaa;
  padding: 2px 6px;
  border-radius: 3px;
  margin-left: 4px;
  cursor: pointer;
  font-size: 10px;
}
.cli-commands kbd:hover {
  background: #667eea;
  color: white;
}
</style>
