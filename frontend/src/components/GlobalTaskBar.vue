<template>
  <div class="task-bar">
    <div class="task-header" @click="isExpanded = !isExpanded">
      <span v-if="tasks.length > 0">{{ doneCount }}/{{ tasks.length }} 已完成</span>
      <span v-else>任务追踪</span>
      <span class="toggle">{{ isExpanded ? '▼' : '▲' }}</span>
    </div>
    <div v-show="isExpanded" class="task-list">
      <div v-if="tasks.length === 0" class="empty-hint">暂无任务</div>
      <div v-for="task in tasks" :key="task.id" class="task-line">
        <span class="dot">{{ task.status === 'done' ? '✓' : task.status === 'failed' ? '✗' : '○' }}</span>
        <span class="desc">{{ task.description }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { GlobalTask } from '../types/task'

const props = defineProps<{ tasks: GlobalTask[] }>()

const isExpanded = ref(true)

const doneCount = computed(() => props.tasks.filter(t => t.status === 'done').length)
</script>

<style scoped>
.task-bar {
  border-top: 1px solid #e5e5e5;
  font-size: 13px;
}
.task-header {
  display: flex;
  justify-content: space-between;
  padding: 8px 12px;
  cursor: pointer;
  user-select: none;
  color: #666;
}
.task-header:hover { color: #333; }
.toggle { font-size: 10px; }
.task-list { padding: 0 12px 8px; }
.task-line {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 0;
  line-height: 1.5;
}
.dot {
  font-size: 12px;
  flex-shrink: 0;
  width: 14px;
}
.desc {
  color: #333;
  word-break: break-word;
}
.empty-hint {
  padding: 8px 0;
  color: #999;
  font-size: 12px;
  text-align: center;
}
</style>