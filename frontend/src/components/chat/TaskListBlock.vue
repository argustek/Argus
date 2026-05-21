<template>
  <div class="tasklist-block" :class="taskList.status">
    <div class="tl-header" @click="$emit('toggle')">
      <span class="tl-icon">📋</span>
      <span class="tl-title">{{ taskList.title }}</span>
      <span class="tl-progress">{{ doneCount }}/{{ total }}</span>
      <div class="tl-bar-wrap">
        <div class="tl-bar-fill" :style="{ width: progressPercent + '%' }"></div>
      </div>
      <span class="expand-hint">{{ expanded ? '收起 ▲' : '展开 ▼' }}</span>
    </div>

    <transition name="slide">
      <div v-show="expanded" class="tl-steps">
        <div
          v-for="(task, idx) in taskList.tasks"
          :key="task.id"
          class="tl-step"
          :class="[task.status, { active: task.status === 'running' }]"
        >
          <span class="step-icon">
            <template v-if="task.status === 'pending'">○</template>
            <template v-else-if="task.status === 'running'"><span class="spinner"></span></template>
            <template v-else-if="task.status === 'done'">✅</template>
            <template v-else-if="task.status === 'error'">❌</template>
            <template v-else-if="task.status === 'skipped'">⏭</template>
          </span>
          <div class="step-content">
            <span class="step-text">{{ task.text }}</span>
            <span v-if="task.detail" class="step-detail">{{ task.detail }}</span>
          </div>
          <span v-if="task.duration" class="step-duration">{{ task.duration }}</span>
          <span v-if="task.error" class="step-error">{{ task.error }}</span>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { TaskList, TaskItem } from '../../types/rich-message'

const props = defineProps<{
  taskList: TaskList
  expanded: boolean
}>()

defineEmits<{
  toggle: []
}>()

const doneCount = computed(() => props.taskList.tasks.filter((t: TaskItem) => t.status === 'done').length)
const total = computed(() => props.taskList.tasks.length)
const progressPercent = computed(() => (total.value > 0 ? Math.round(doneCount.value / total.value * 100) : 0))
</script>

<style scoped>
.tasklist-block {
  border-radius: 8px;
  overflow: hidden;
  font-size: 13px;
}
.tl-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  cursor: pointer;
  user-select: none;
  background: rgba(0,0,0,0.03);
  border-radius: 6px;
  transition: background 0.2s;
}
.tl-header:hover { background: rgba(0,0,0,0.06); }
.tl-icon { font-size: 14px; }
.tl-title { flex: 1; font-weight: 600; }
.tl-progress { font-size: 11px; color: #666; background: #eee; padding: 1px 6px; border-radius: 10px; }
.tl-bar-wrap { width: 60px; height: 4px; background: #e0e0e0; border-radius: 2px; overflow: hidden; }
.tl-bar-fill { height: 100%; background: #3b82f6; border-radius: 2px; transition: width 0.5s ease; }
.expand-hint { font-size: 11px; color: #999; }
.tl-steps { padding: 4px 0; }
.tl-step {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 12px 5px 16px;
  transition: all 0.2s;
  border-left: 3px solid transparent;
}
.tl-step.pending { opacity: 0.45; }
.tl-step.running {
  background: rgba(59,130,246,0.08);
  border-left-color: #3b82f6;
  font-weight: 500;
}
.tl-step.done { opacity: 0.85; }
.tl-step.error {
  background: rgba(239,68,68,0.08);
  border-left-color: #ef4444;
}
.step-icon { font-size: 13px; min-width: 18px; text-align: center; }
.step-content { flex: 1; display: flex; flex-direction: column; gap: 2px; }
.step-text { font-weight: 500; }
.step-detail { font-size: 11.5px; color: #666; font-family: 'Cascadia Code', 'Fira Code', monospace; background: rgba(0,0,0,0.04); padding: 2px 6px; border-radius: 3px; word-break: break-all; }
.step-duration { font-size: 11px; color: #888; }
.step-error { font-size: 11px; color: #ef4444; }

.spinner {
  display: inline-block; width: 12px; height: 12px;
  border: 2px solid transparent; border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

.slide-enter-active { transition: all 0.25s ease; }
.slide-enter-from { opacity: 0; transform: translateY(-6px); }
</style>
