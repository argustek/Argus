<template>
  <div class="se-msg">
    <div class="se-header">
      <span class="se-count">{{ actions.length }} 个操作</span>
      <span v-if="allDone" class="se-all-done">✅ 全部完成 ({{ doneCount }}/{{ actions.length }})</span>
      <span v-else-if="hasError" class="se-error">❌ 有失败</span>
    </div>
    <div class="se-actions">
      <div v-for="(action, i) in actions" :key="i" class="se-action-line">
        <span class="se-dot" :class="action.status">{{ statusIcon(action) }}</span>
        <template v-if="action.type === 'write_file' || action.type === 'edit_file'">
          <span class="se-label">{{ action.type === 'write_file' ? '创建' : '修改' }}</span>
          <code class="se-path" :class="{ done: action.status === 'done' }">{{ action.path }}</code>
          <span class="se-badge ok">已{{ action.type === 'write_file' ? '创建' : '修改' }}</span>
          <span v-if="action.size" class="se-size">{{ action.size }}B</span>
        </template>
        <template v-else-if="action.type === 'exec'">
          <span class="se-label">执行</span>
          <code class="se-cmd" :class="{ done: action.status === 'done' }">{{ action.command }}</code>
          <button v-if="action.status !== 'done' && action.status !== 'error'" class="se-run-btn" @click="$emit('run-in-terminal', { command: action.command })">▶ 运行</button>
          <span v-else class="se-badge" :class="action.status === 'done' ? 'ok' : 'err'">{{ action.status === 'done' ? '成功' : '失败' }}</span>
          <span v-if="action.duration" class="se-duration">{{ action.duration }}</span>
        </template>
        <template v-else-if="action.type === 'read_file'">
          <span class="se-label">读取</span>
          <code class="se-path">{{ action.path }}</code>
          <span class="se-badge ok">已读取</span>
        </template>
        <template v-else>
          <span class="se-label">{{ action.type }}</span>
          <span>{{ action.path || action.command || '' }}</span>
        </template>
      </div>
    </div>
    <div v-if="shellOutput" class="se-output">
      <pre><code>{{ shellOutput }}</code></pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Action {
  type: string
  path?: string
  command?: string
  content?: string
  status?: string
  output?: string
  duration?: string
  size?: number
}

interface Props {
  message: { id: string; role: string; timestamp: Date | string }
  actions?: Action[]
  shellOutput?: string
}

const props = withDefaults(defineProps<Props>(), { actions: () => [], shellOutput: '' })

defineEmits<{
  'open-file-in-editor': [{ path: string }]
  'run-in-terminal': [{ command: string }]
}>()

const doneCount = computed(() => props.actions.filter(a => a.status === 'done').length)
const allDone = computed(() => props.actions.length > 0 && doneCount.value === props.actions.length)
const hasError = computed(() => props.actions.some(a => a.status === 'error'))

function statusIcon(action: Action): string {
  if (action.status === 'error') return '✗'
  if (action.status === 'done') return '✓'
  return '○'
}
</script>

<style scoped>
.se-msg { font-size: 13px; line-height: 1.6; }
.se-header {
  display: flex; align-items: center; gap: 8px;
  padding-bottom: 4px; color: #666;
}
.se-count { font-weight: 600; }
.se-all-done { color: #16a34a; }
.se-error { color: #dc2626; }
.se-actions { display: flex; flex-direction: column; gap: 3px; }
.se-action-line {
  display: flex; align-items: center; gap: 5px;
  padding: 2px 0;
}
.se-dot { font-size: 12px; flex-shrink: 0; width: 14px; text-align: center; }
.se-dot.done { color: #16a34a; }
.se-dot.error { color: #dc2626; }
.se-label { color: #666; font-size: 12px; flex-shrink: 0; }
.se-path, .se-cmd {
  font-family: Consolas, monospace; font-size: 12px;
  background: #e8e8e8; padding: 1px 5px; border-radius: 3px;
  color: #1a1a1a;
}
.se-path.done, .se-cmd.done {
  text-decoration: line-through; opacity: 0.6; color: #888;
}
.se-badge {
  font-size: 11px; padding: 0 4px; border-radius: 2px; flex-shrink: 0;
}
.se-badge.ok { background: #dcfce7; color: #16a34a; }
.se-badge.err { background: #fee2e2; color: #dc2626; }
.se-size { color: #9ca3af; font-size: 11px; }
.se-duration { color: #9ca3af; font-size: 11px; }
.se-run-btn {
  font-size: 11px; padding: 1px 6px; border: 1px solid #d1d5db;
  border-radius: 3px; background: #fff; cursor: pointer; flex-shrink: 0;
}
.se-run-btn:hover { background: #f3f4f6; }
.se-output {
  margin-top: 6px; padding: 8px; background: #1e1e1e; border-radius: 4px;
  max-height: 150px; overflow-y: auto;
}
.se-output pre { margin: 0; }
.se-output code {
  font-family: Consolas, monospace; font-size: 12px;
  color: #d4d4d4; white-space: pre-wrap; word-break: break-all;
}
</style>
