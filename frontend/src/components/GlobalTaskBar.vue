<template>
  <div class="task-bar">
    <div class="task-header" @click="toggle">
      <span v-if="tasks.length > 0" class="summary">{{ doneCount }}/{{ tasks.length }} 已完成</span>
      <span v-else class="summary">任务追踪</span>
      <span class="toggle">{{ isExpanded ? '▼' : '▲' }}</span>
    </div>
    <div v-show="isExpanded" class="task-list">
      <div v-if="tasks.length === 0" class="empty-hint">暂无任务</div>
      <div v-for="task in tasks" :key="task.id" class="task-line" :class="{ done: task.status === 'done', failed: task.status === 'failed' }">
        <span class="dot">{{ task.status === 'done' ? '✓' : task.status === 'failed' ? '✗' : '○' }}</span>
        <span class="desc">{{ task.description }}</span>
        <span class="role-tag">{{ '[' + task.role + ']' }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { GlobalTask } from '../types/task'

const props = defineProps<{ tasks: GlobalTask[] }>()

const isExpanded = ref(true)
const userToggled = ref(false)

const doneCount = computed(() => props.tasks.filter(t => t.status === 'done').length)
const allDone = computed(() => props.tasks.length > 0 && doneCount.value === props.tasks.length)

watch([() => props.tasks.length, doneCount], () => {
  if (allDone.value && !userToggled.value) {
    isExpanded.value = false
  } else if (!allDone.value && !userToggled.value) {
    isExpanded.value = true
  }
}, { deep: true })

function toggle() {
  userToggled.value = true
  isExpanded.value = !isExpanded.value
  // 如果手动折叠后又全部完成了，重置flag让下次自动折叠能生效
  if (allDone.value && isExpanded.value) {
    userToggled.value = false
  }
}
</script>

<style scoped>
.task-bar {
  background: transparent;
  font-size: 12px;
}
.task-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 16px;
  cursor: pointer;
  user-select: none;
}
.summary {
  color: #aaa;
  font-size: 11px;
}
.toggle {
  font-size: 9px;
  color: #ccc;
}
.task-list {
  padding: 0 16px 10px;
}
.task-line {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 0;
  line-height: 1.5;
}
.dot {
  font-size: 11px;
  flex-shrink: 0;
  width: 14px;
  color: #999;
}
.desc {
  color: #888;
  word-break: break-word;
  font-size: 12px;
}
.task-line.done .dot { color: #52c41a; }
.task-line.done .desc {
  text-decoration: line-through !important;
  color: #bbb !important;
}
.task-line.failed .dot { color: #ff4d4f; }
.role-tag {
  margin-left: auto;
  font-size: 10px;
  color: #999;
  flex-shrink: 0;
  opacity: 0.7;
}
.empty-hint {
  padding: 6px 0;
  color: #bbb;
  font-size: 11px;
  text-align: center;
}
</style>
