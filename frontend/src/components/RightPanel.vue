<template>
  <div class="right-panel">
    <div class="panel-header">
      <span>AI 监控面板</span>
    </div>
    
    <!-- 监控状态 -->
    <div class="monitor-card">
      <div class="card-title">
        <span class="status-dot" :class="status.isRunning ? 'online' : 'offline'"></span>
        监控状态
      </div>
      <div class="status-grid">
        <div class="status-item">
          <span class="label">看板 A</span>
          <span class="value" :class="status.board1Status">{{ status.board1Status }}</span>
        </div>
        <div class="status-item">
          <span class="label">看板 B</span>
          <span class="value" :class="status.board2Status">{{ status.board2Status }}</span>
        </div>
        <div class="status-item">
          <span class="label">Worker B</span>
          <span class="value" :class="status.bStatus">{{ status.bStatus }}</span>
        </div>
      </div>
      <div class="last-check" v-if="status.lastCheckTime">
        上次检查: {{ formatTime(status.lastCheckTime) }}
      </div>
    </div>
    
    <!-- 控制按钮 -->
    <div class="control-buttons">
      <button 
        v-if="!status.isRunning"
        class="btn btn-primary"
        @click="$emit('start-monitor')"
      >
        ▶ 启动监控
      </button>
      <button 
        v-else
        class="btn btn-danger"
        @click="$emit('stop-monitor')"
      >
        ⏹ 停止监控
      </button>
      <button class="btn" @click="$emit('open-settings')">
        ⚙ 设置
      </button>
    </div>
    
    <!-- 告警信息 -->
    <div v-if="status.alertMessage" class="alert-box">
      <div class="alert-title">⚠️ 告警</div>
      <div class="alert-message">{{ status.alertMessage }}</div>
    </div>
    
    <!-- 系统日志 -->
    <div class="logs-section">
      <div class="section-title">系统日志</div>
      <div class="logs-list">
        <div 
          v-for="(log, index) in logs.slice(0, 10)" 
          :key="index"
          class="log-item"
          :class="log.level"
        >
          <span class="log-time">{{ formatTime(log.time) }}</span>
          <span class="log-msg">{{ log.message }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  status: {
    isRunning: boolean
    board1Status: string
    board2Status: string
    bStatus: string
    lastCheckTime: Date | null
    alertMessage: string
  }
  logs: Array<{time: Date, level: string, message: string}>
}>()

defineEmits(['start-monitor', 'stop-monitor', 'open-settings'])

function formatTime(time: Date): string {
  if (!time) return '-'
  return new Date(time).toLocaleTimeString('zh-CN', { hour12: false })
}
</script>

<style scoped>
.right-panel {
  width: 280px;
  background: var(--bg-secondary);
  border-left: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
  padding: 12px;
}

.panel-header {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-secondary);
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border-color);
}

.monitor-card {
  background: var(--bg-tertiary);
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 12px;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 12px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.status-dot.online {
  background: var(--success-color);
  box-shadow: 0 0 6px var(--success-color);
}

.status-dot.offline {
  background: var(--error-color);
}

.status-grid {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.status-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
}

.status-item .label {
  color: var(--text-secondary);
}

.status-item .value {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  text-transform: uppercase;
}

.status-item .value.ok,
.status-item .value.alive {
  background: rgba(63, 185, 80, 0.2);
  color: var(--success-color);
}

.status-item .value.error,
.status-item .value.dead {
  background: rgba(248, 81, 73, 0.2);
  color: var(--error-color);
}

.status-item .value.unknown {
  background: rgba(125, 133, 144, 0.2);
  color: var(--text-secondary);
}

.last-check {
  margin-top: 12px;
  font-size: 11px;
  color: var(--text-secondary);
  text-align: center;
}

.control-buttons {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.control-buttons .btn {
  flex: 1;
  padding: 8px;
  font-size: 12px;
}

.alert-box {
  background: rgba(248, 81, 73, 0.1);
  border: 1px solid var(--error-color);
  border-radius: 6px;
  padding: 10px;
  margin-bottom: 12px;
}

.alert-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--error-color);
  margin-bottom: 4px;
}

.alert-message {
  font-size: 11px;
  color: var(--text-secondary);
}

.logs-section {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.section-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 8px;
}

.logs-list {
  flex: 1;
  overflow: auto;
  font-size: 11px;
}

.log-item {
  display: flex;
  gap: 8px;
  padding: 4px 0;
  border-bottom: 1px solid var(--border-color);
}

.log-item .log-time {
  color: var(--text-secondary);
  min-width: 60px;
}

.log-item .log-msg {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-item.info .log-msg {
  color: var(--accent-color);
}

.log-item.warning .log-msg {
  color: var(--warning-color);
}

.log-item.error .log-msg {
  color: var(--error-color);
}
</style>
