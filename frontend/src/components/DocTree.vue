<template>
  <div class="doc-tree">
    <div class="panel-header">
      <span>{{ t('docTree.title') }}</span>
      <button class="refresh-btn" @click="refresh" :title="t('common.refresh')">↻</button>
    </div>
    <div class="tree-body">
      <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
      <div v-else-if="error" class="error">❌ {{ error }}</div>
      <div v-else-if="!workDir" class="empty">⚠️ 未设置工作目录</div>
      <div v-else-if="!treeData || treeData.length === 0" class="empty">📭 {{ t('docTree.noDocuments') }} (workDir: {{ workDir }})</div>
      <DocTreeNode
        v-for="node in treeData"
        :key="node.id"
        :node="node"
        :depth="0"
        @select="(path: string) => emit('open-doc', path)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { EventsOn, EventsOff } from '../../wailsjs/runtime'
import DocTreeNode from './DocTreeNode.vue'

const { t } = useI18n()

const props = defineProps<{
  workDir: string
}>()

const emit = defineEmits(['open-doc'])

const treeData = ref<any[]>([])
const loading = ref(false)
const error = ref('')

async function refresh() {
  if (!props.workDir) { treeData.value = []; return }
  loading.value = true
  error.value = ''
  try {
    // @ts-ignore Wails binding — returns JSON string now
    const jsonStr: string = await window.go.main.App.GetDocTree()
    if (!jsonStr || jsonStr === '[]') {
      treeData.value = []
    } else {
      treeData.value = JSON.parse(jsonStr) || []
    }
  } catch (e: any) {
    error.value = e?.message || String(e)
    treeData.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (props.workDir) refresh()
  EventsOn('doc-tree-dirty', () => {
    if (props.workDir) refresh()
  })
})

onUnmounted(() => {
  EventsOff('doc-tree-dirty')
})

watch(() => props.workDir, (newDir) => {
  if (newDir) refresh()
})
</script>

<style scoped>
.doc-tree {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  font-size: 12px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-color);
  user-select: none;
  gap: 4px;
  min-height: 30px;
}

.refresh-btn {
  background: none; border: none; cursor: pointer;
  font-size: 14px; color: var(--text-tertiary);
  padding: 2px 4px; border-radius: 4px;
}
.refresh-btn:hover { color: var(--text-primary); background: var(--bg-tertiary); }

.tree-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}

.loading, .error, .empty {
  padding: 20px; text-align: center; font-size: 13px; color: var(--text-tertiary);
}
.error { color: #f87171; }
</style>
