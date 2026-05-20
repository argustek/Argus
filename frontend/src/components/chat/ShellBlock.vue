<template>
  <div class="shell-block" :class="shell.status">
    <div class="sb-header" @click="toggleExpand">
      <span class="sb-icon">{{ typeIcon }}</span>
      <span class="sb-cmd" :title="shell.command">{{ displayCmd }}</span>
      <span v-if="shell.exitCode !== 0 && shell.status === 'done'" class="sb-exit">exit {{ shell.exitCode }}</span>
      <span v-if="shell.duration && shell.status !== 'running'" class="sb-duration">{{ shell.duration }}</span>
      <span v-if="shell.status === 'running'" class="sb-status running"><span class="dot"></span> 运行中</span>
    </div>

    <transition name="slide-down">
      <div v-show="expanded || shell.status === 'running'" class="sb-output-wrap">
        <pre class="terminal-output" ref="outputEl"><code>{{ shell.output || (shell.status === 'running' ? '⏳ 等待输出...' : '') }}</code></pre>
        <div v-if="shell.extra?.filePath" class="sb-extra">📄 {{ shell.extra.filePath }}</div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick } from 'vue'
import type { ShellBlock } from '../../types/rich-message'

const props = defineProps<{ shell: ShellBlock; autoScroll: boolean }>()

const expanded = ref(true)
const outputEl = ref<HTMLPreElement | null>(null)

const typeIcon = computed(() => {
  switch (props.shell.type) {
    case 'exec': return '▶️'
    case 'write_file': return '✏️'
    case 'read_file': return '📖'
    case 'list_files': return '📁'
    default: return '⚙️'
  }
})

const displayCmd = computed(() => {
  const cmd = props.shell.command
  if (cmd.length > 60) return cmd.slice(0, 57) + '...'
  return cmd
})

function toggleExpand() { expanded.value = !expanded.value }

function scrollToBottom() {
  if (!props.autoScroll) return
  nextTick(() => {
    if (outputEl.value) outputEl.value.scrollTop = outputEl.value.scrollHeight
  })
}
</script>

<style scoped>
.shell-block {
  border-radius: 6px;
  overflow: hidden;
  font-size: 12px;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace;
  margin-top: 4px;
}
.sb-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  cursor: pointer;
  background: rgba(0,0,0,0.04);
  border-radius: 4px;
  transition: background 0.15s;
}
.sb-header:hover { background: rgba(0,0,0,0.07); }
.sb-icon { font-size: 13px; flex-shrink: 0; }
.sb-cmd { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #333; }
.sb-exit { color: #ef4444; font-weight: 600; font-size: 11px; }
.sb-duration { color: #888; font-size: 11px; }
.sb-status.running { color: #3b82f6; font-size: 11px; display: flex; align-items: center; gap: 3px; }
.dot {
  width: 6px; height: 6px; border-radius: 50%;
  background: #3b82f6; animation: blink 1s infinite;
}
@keyframes blink { 50% { opacity: 0.2; } }

.sb-output-wrap { padding: 8px; background: #1e1e1e; border-radius: 4px; margin-top: 1px; max-height: 300px; overflow-y: auto; }
.terminal-output { margin: 0; white-space: pre-wrap; word-break: break-all; line-height: 1.45; }
.terminal-output code {
  color: #d4d4d4; font-family: inherit; font-size: inherit;
}
.shell-block.error .sb-output-wrap { border-left: 3px solid #ef4444; }
.shell-block.done .sb-output-wrap { border-left: 3px solid #22c55e; }
.shell-block.running .sb-output-wrap { border-left: 3px solid #3b82f6; }
.sb-extra { font-size: 11px; color: #888; padding: 2px 10px 4px; }

.slide-down-enter-active, .slide-down-leave-active { transition: all 0.2s ease; }
.slide-down-enter-from, .slide-down-leave-to { opacity: 0; transform: translateY(-4px); height: 0; overflow: hidden; }
</style>
