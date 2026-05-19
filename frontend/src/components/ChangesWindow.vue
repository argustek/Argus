<template>
  <div 
    class="floating-window changes-window"
    :style="{ left: windowPos.x + 'px', top: windowPos.y + 'px' }"
  >
    <div class="window-header" @mousedown="startDrag">
      <span class="window-title">📋 {{ t('changes.title') }}</span>
      <button class="close-btn" @click.stop="$emit('close')">×</button>
    </div>
    <div class="window-content">
      <div v-if="history.length === 0" class="empty-state">
        <div class="empty-text">{{ t('changes.noHistory') }}</div>
      </div>
      <div v-else class="history-list">
        <div v-for="(item, index) in history" :key="index" class="history-item">
          <div class="history-time">{{ item.time }}</div>
          <div class="history-title">{{ item.title }}</div>
          <div class="history-changes">
            <div v-for="(change, idx) in item.changes" :key="idx" class="history-change">
              {{ getChangeIcon(change.type) }} {{ change.file }}
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useDraggable } from '../composables/useDraggable'

const { t } = useI18n()

defineProps<{
  history: Array<any>
}>()

defineEmits(['close'])

const { windowPos, startDrag } = useDraggable(700, 60)

function getChangeIcon(type: string): string {
  const icons: Record<string, string> = {
    create: '✅',
    modify: '🔧',
    delete: '️'
  }
  return icons[type] || '•'
}
</script>

<style scoped>
.floating-window {
  position: fixed;
  width: 300px;
  height: 400px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.5);
  z-index: 100;
}

.window-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  background: var(--bg-tertiary);
  border-radius: 8px 8px 0 0;
  border-bottom: 1px solid var(--border-color);
  cursor: move;
  user-select: none;
}

.window-title {
  font-size: 13px;
  font-weight: 500;
}

.close-btn {
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.close-btn:hover {
  color: var(--text-primary);
}

.window-content {
  flex: 1;
  overflow: auto;
  padding: 12px;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-secondary);
}

.history-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.history-item {
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border-color);
}

.history-time {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.history-title {
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 8px;
}

.history-changes {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-left: 12px;
}

.history-change {
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
