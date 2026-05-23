<template>
  <div class="ai-monitor-float">
    <div class="monitor-header" @click="$emit('toggle')">
      <span class="title">{{ t('aiMonitor.aiStatus') }}</span>
      <button class="toggle-btn">−</button>
    </div>
    <div class="monitor-content">
      <div class="ai-status-item">
        <span class="label">PM</span>
        <span class="status" :class="status.pmStatus">
          {{ status.pmStatus === 'idle' ? t('aiMonitor.idle') : status.pmStatus === 'busy' ? t('aiMonitor.busy') : t('aiMonitor.error') }}
        </span>
      </div>
      <div class="ai-status-item">
        <span class="label">SE</span>
        <span class="status" :class="status.seStatus">
          {{ status.seStatus === 'idle' ? t('aiMonitor.idle') : status.seStatus === 'busy' ? t('aiMonitor.busy') : t('aiMonitor.error') }}
        </span>
      </div>
      <div v-if="status.currentTask" class="current-task">
        <div class="task-label">Current Task</div>
        <div class="task-name">{{ status.currentTask }}</div>
        <div class="progress-bar">
          <div class="progress-fill" :style="{ width: status.progress }"></div>
        </div>
        <div class="progress-text">{{ status.progress }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  status: {
    pmStatus: string
    seStatus: string
    apStatus: string
    cRunning: boolean
    currentTask: string
    progress: string
  }
}>()

defineEmits(['toggle'])

const { t } = useI18n()
</script>

<style scoped>
.ai-monitor-float {
  position: fixed;
  right: 20px;
  bottom: 60px;
  width: 240px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.5);
  z-index: 100;
}

.monitor-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  background: var(--bg-tertiary);
  border-radius: 8px 8px 0 0;
  cursor: pointer;
}

.title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
}

.toggle-btn {
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.monitor-content {
  padding: 12px;
}

.ai-status-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  font-size: 12px;
}

.ai-status-item .label {
  color: var(--text-secondary);
}

.ai-status-item .status {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
}

.ai-status-item .status.idle {
  background: rgba(125, 133, 144, 0.2);
  color: var(--text-secondary);
}

.ai-status-item .status.busy {
  background: rgba(47, 129, 247, 0.2);
  color: var(--accent-color);
}

.ai-status-item .status.error {
  background: rgba(248, 81, 73, 0.2);
  color: var(--error-color);
}

.current-task {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border-color);
}

.task-label {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.task-name {
  font-size: 12px;
  margin-bottom: 8px;
}

.progress-bar {
  height: 4px;
  background: var(--bg-tertiary);
  border-radius: 2px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background: var(--accent-color);
  transition: width 0.3s;
}

.progress-text {
  font-size: 11px;
  color: var(--text-secondary);
  text-align: right;
  margin-top: 4px;
}
</style>
