<template>
  <div class="se-msg">
    <div class="se-actions">
      <div v-for="(action, i) in actions" :key="i" class="se-action-line">
        <span class="se-dot">{{ action.status === 'error' ? '✗' : '✓' }}</span>
        <span v-if="action.type === 'write_file'">创建 <code>{{ action.path }}</code></span>
        <span v-else-if="action.type === 'edit_file'">修改 <code>{{ action.path }}</code></span>
        <span v-else-if="action.type === 'exec'">执行 <code>{{ action.command }}</code></span>
        <span v-else-if="action.type === 'read_file'">读取 <code>{{ action.path }}</code></span>
        <span v-else>{{ action.type }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
interface Action {
  type: string
  path?: string
  command?: string
  status?: string
}

interface Props {
  message: { id: string; role: string; timestamp: Date | string }
  actions?: Action[]
}

withDefaults(defineProps<Props>(), { actions: () => [] })

defineEmits<{
  'open-file-in-editor': [{ path: string }]
  'run-in-terminal': [{ command: string }]
}>()
</script>

<style scoped>
.se-msg { font-size: 13px; line-height: 1.6; }
.se-actions { display: flex; flex-direction: column; gap: 2px; }
.se-action-line { display: flex; align-items: center; gap: 6px; }
.se-dot { font-size: 12px; flex-shrink: 0; width: 14px; }
.se-action-line code {
  font-family: Consolas, monospace;
  font-size: 12px;
  background: #f0f0f0;
  padding: 1px 4px;
  border-radius: 2px;
}
</style>
