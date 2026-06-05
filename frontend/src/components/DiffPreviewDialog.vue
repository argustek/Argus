<template>
  <div class="diff-overlay" @click.self="onAutoApply">
    <div class="diff-dialog">
      <div class="diff-header">
        <div class="diff-title">
          <span class="diff-icon">{{ actionIcon }}</span>
          <span class="diff-filename">{{ data.path }}</span>
          <span class="diff-action-badge">{{ actionLabel }}</span>
        </div>
        <button class="diff-close" @click="onReject" :title="t('diff.reject')">&#10005;</button>
      </div>

      <div class="diff-body">
        <pre class="diff-content"><code v-html="renderedDiff"></code></pre>
      </div>

      <div class="diff-footer">
        <button class="btn btn-reject" @click="onReject">
          {{ t('diff.reject') }}
        </button>
        <label class="auto-apply-label">
          <input type="checkbox" v-model="autoApply" />
          {{ t('diff.autoApply') }}
        </label>
        <button class="btn btn-approve" @click="onApprove">
          {{ t('diff.approve') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

export interface DiffData {
  type: string       // write_file | edit_file | show_diff | new_file
  path: string
  diff: string
  action?: string    // before_write | before_edit
}

const props = defineProps<{
  data: DiffData
}>()

const emit = defineEmits<{
  (e: 'approve', data: DiffData): void
  (e: 'reject', data: DiffData): void
}>()

const autoApply = ref(false)

const actionIcon = computed(() => {
  switch (props.data.type) {
    case 'write_file': return '\u{1F4C4}'   // 📄
    case 'edit_file': return '\u{270F}\uFE0F' // ✏️
    case 'new_file': return '\u{1F5BC}\uFE0F'  // 🖼️ (new file)
    case 'show_diff': return '\u{1F50D}'       // 🔍
    default: return '\u{1F4AC}'                 // 💬
  }
})

const actionLabel = computed(() => {
  switch (props.data.action) {
    case 'before_write': return t('diff.beforeWrite')
    case 'before_edit': return t('diff.beforeEdit')
    default:
      switch (props.data.type) {
        case 'write_file': return t('diff.writeFile')
        case 'edit_file': return t('diff.editFile')
        case 'new_file': return t('diff.newFile')
        case 'show_diff': return t('diff.showDiff')
        default: return props.data.type
      }
  }
})

// 将 unified diff 渲染为带颜色的 HTML
const renderedDiff = computed(() => {
  const diff = props.data.diff
  if (!diff) return ''
  const lines = diff.split('\n')
  return lines.map(line => {
    let cls = 'diff-line'
    let escaped = escapeHtml(line)
    if (line.startsWith('+')) {
      cls += ' diff-add'
    } else if (line.startsWith('-')) {
      cls += ' diff-del'
    } else if (line.startsWith('@')) {
      cls += ' diff-hunk'
    } else if (line.startsWith('+++') || line.startsWith('---')) {
      cls += ' diff-header'
    }
    return `<span class="${cls}">${escaped || '&nbsp;'}</span>`
  }).join('\n')
})

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

function onApprove() {
  emit('approve', props.data)
}

function onReject() {
  emit('reject', props.data)
}

function onAutoApply() {
  if (autoApply.value) {
    onApprove()
  }
}
</script>

<style scoped>
.diff-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
  animation: fadeIn 0.15s ease;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.diff-dialog {
  width: 720px;
  max-width: 90vw;
  height: 480px;
  max-height: 80vh;
  background: var(--bg-primary, #1e1e2e);
  border: 1px solid var(--border-color, #313244);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  animation: slideUp 0.2s ease;
}

@keyframes slideUp {
  from { transform: translateY(20px); opacity: 0; }
  to { transform: translateY(0); opacity: 1; }
}

.diff-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-color, #313244);
  flex-shrink: 0;
}

.diff-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.diff-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.diff-filename {
  font-weight: 600;
  font-size: 13px;
  color: var(--text-primary, #cdd6f4);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.diff-action-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--accent-color, #89b4fa);
  color: #1e1e2e;
  font-weight: 600;
  flex-shrink: 0;
}

.diff-close {
  background: none;
  border: none;
  color: var(--text-secondary, #6c7086);
  font-size: 18px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  transition: all 0.15s;
  flex-shrink: 0;
}

.diff-close:hover {
  background: rgba(243, 139, 168, 0.15);
  color: #f38ba8;
}

.diff-body {
  flex: 1;
  overflow: auto;
  padding: 0;
}

.diff-content {
  margin: 0;
  padding: 8px 0;
  font-family: 'JetBrains Mono', 'Cascadia Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.5;
  background: var(--bg-secondary, #181825);
  color: var(--text-secondary, #6c7086);
  tab-size: 4;
  white-space: pre;
  overflow-x: auto;
}

.diff-line {
  display: block;
  padding: 0 16px;
  min-height: 19px;
}

.diff-add {
  background: rgba(166, 227, 161, 0.12);
  color: #a6e3a1;
}

.diff-del {
  background: rgba(243, 139, 168, 0.12);
  color: #f38ba8;
}

.diff-hunk {
  background: rgba(137, 180, 250, 0.08);
  color: #89b4fa;
  font-style: italic;
}

.diff-header-line {
  color: #6c7086;
}

.diff-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-top: 1px solid var(--border-color, #313244);
  flex-shrink: 0;
  gap: 12px;
}

.btn {
  padding: 6px 20px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border: none;
  transition: all 0.15s;
}

.btn-reject {
  background: rgba(243, 139, 168, 0.15);
  color: #f38ba8;
}

.btn-reject:hover {
  background: rgba(243, 139, 168, 0.25);
}

.btn-approve {
  background: rgba(166, 227, 161, 0.15);
  color: #a6e3a1;
}

.btn-approve:hover {
  background: rgba(166, 227, 161, 0.25);
}

.auto-apply-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary, #6c7086);
  cursor: pointer;
  user-select: none;
}

.auto-apply-label input[type="checkbox"] {
  accent-color: var(--accent-color, #89b4fa);
}
</style>
