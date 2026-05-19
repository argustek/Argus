<template>
  <div class="review-bar">
    <div class="review-header">
      <div class="review-title">
        <span class="ai-icon">🤖</span>
        <span>{{ t('review.changesDone', { count: changes.length }) }}</span>
      </div>
      <button class="collapse-btn" @click="collapsed = !collapsed">
        {{ collapsed ? t('review.expand') : t('review.collapse') }}
      </button>
    </div>
    
    <div v-if="!collapsed" class="review-content">
      <div class="changes-list">
        <div v-for="(change, index) in changes" :key="index" class="change-item">
          <span class="change-type">{{ getChangeLabel(change.type) }}</span>
          <span class="change-file">{{ change.file }}</span>
        </div>
      </div>
      
      <div class="review-actions">
        <button class="review-btn accept" @click="$emit('accept')">
          👍 {{ t('review.acceptAll') }}
        </button>
        <button class="review-btn reject" @click="$emit('reject')">
          👎 {{ t('review.rejectAll') }}
        </button>
        <button class="review-btn adjust" @click="$emit('adjust')">
          📝 {{ t('review.partialAdjust') }}
        </button>
      </div>
    </div>
    
    <div v-else class="review-collapsed">
      <span>{{ t('review.waitingConfirm') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  changes: Array<{type: string, file: string}>
}>()

defineEmits(['accept', 'reject', 'adjust'])

const collapsed = ref(false)

function getChangeLabel(type: string): string {
  const keyMap: Record<string, string> = {
    create: 'review.create',
    modify: 'review.modify',
    delete: 'review.delete',
    install: 'review.install'
  }
  return keyMap[type] ? t(keyMap[type]) : t('review.change')
}
</script>

<style scoped>
.review-bar {
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
}

.review-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: var(--bg-tertiary);
}

.review-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 500;
}

.ai-icon {
  font-size: 14px;
}

.collapse-btn {
  background: transparent;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
}

.collapse-btn:hover {
  color: var(--text-primary);
}

.review-content {
  padding: 12px 16px;
}

.changes-list {
  margin-bottom: 12px;
}

.change-item {
  display: flex;
  gap: 8px;
  padding: 4px 0;
  font-size: 13px;
}

.change-type {
  color: var(--text-secondary);
}

.change-file {
  font-family: monospace;
  color: var(--text-primary);
}

.review-actions {
  display: flex;
  gap: 8px;
}

.review-btn {
  padding: 6px 16px;
  border: 1px solid var(--border-color);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  border-radius: 4px;
  font-size: 13px;
  cursor: pointer;
}

.review-btn:hover {
  opacity: 0.9;
}

.review-btn.accept {
  background: var(--success);
  border-color: var(--success);
}

.review-btn.reject {
  background: var(--error);
  border-color: var(--error);
}

.review-btn.adjust {
  background: var(--warning);
  border-color: var(--warning);
}

.review-collapsed {
  padding: 8px 16px;
  text-align: center;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
