<template>
  <div class="status-bar">
    <div class="status-left">
      <span class="status-item clickable" title="Git 版本控制" @click="$emit('toggle-git')">
        <span class="icon">🌿</span>
        {{ status.gitBranch }}
      </span>
      <span class="status-item" title="行号">
        Ln {{ status.line }}, Col {{ status.column }}
      </span>
      <span class="status-item" title="编码">
        {{ status.encoding }}
      </span>
      <span class="status-item" title="语言">
        {{ status.language }}
      </span>
    </div>
    <div class="status-right">
      <span class="status-item">
        <span class="status-dot online"></span>
        AI 监控运行中
      </span>
      <span class="status-item">
        Argus v1.0.0
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
defineEmits(['toggle-git'])

defineProps<{
  status: {
    line: number
    column: number
    encoding: string
    language: string
    gitBranch: string
  }
}>()
</script>

<style scoped>
.status-bar {
  height: 24px;
  background: var(--accent-color);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  font-size: 12px;
}

.status-left,
.status-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.status-item {
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: 3px;
}

.status-item:hover {
  background: rgba(255, 255, 255, 0.1);
}

.status-item.clickable:hover {
  background: rgba(255, 255, 255, 0.15);
  cursor: pointer;
}

.icon {
  font-size: 12px;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.status-dot.online {
  background: #fff;
  box-shadow: 0 0 4px #fff;
}
</style>
