<template>
  <div class="doc-tree-node">
    <!-- Node group header -->
    <div v-if="node.node_id" class="node-row group-row" :class="colorClass" :style="{ paddingLeft: (8 + depth * 20) + 'px' }" @click="toggle">
      <span class="folder-icon">{{ expanded ? '📂' : '📁' }}</span>
      <span class="node-title">{{ node.node_title }}</span>
      <span v-if="fileCount > 0" class="file-count">{{ fileCount }}</span>
    </div>

    <!-- File row (leaf) -->
    <div v-else class="node-row file-row" :class="[colorClass, { selected: isSelected }]" :style="{ paddingLeft: (8 + depth * 20) + 'px' }" @click="openFile" @contextmenu.prevent="showContextMenu($event)">
      <span class="file-icon">📄</span>
      <span class="node-title" :title="node.summary || node.id">{{ node.id }}</span>
      <span v-if="node.dirty" class="dirty-dot" title="已修改">🟡</span>
    </div>

    <!-- Expanded content for node groups -->
    <div v-if="expanded && node.node_id" class="node-children">
      <div v-for="file in (node.files || [])" :key="file.id" class="file-item">
        <DocTreeNode
          :node="file"
          :depth="depth + 1"
          :color-index="colorIndex"
          @select="(p: string) => emit('select', p)"
          @context="(e: any) => emit('context', e)"
        />
      </div>
      <div v-for="child in (node.children || [])" :key="child.node_id || child.id" class="child-group">
        <DocTreeNode
          :node="child"
          :depth="depth + 1"
          :color-index="colorIndex"
          @select="(p: string) => emit('select', p)"
          @context="(e: any) => emit('context', e)"
        />
      </div>
      <div v-if="(!node.files || node.files.length === 0) && (!node.children || node.children.length === 0)" class="empty-files">
        (空节点)
      </div>
    </div>

    <!-- 右键菜单 -->
    <Teleport to="body">
      <div v-if="ctxMenu.visible" class="ctx-menu" :style="{ top: ctxMenu.y + 'px', left: ctxMenu.x + 'px' }" @click.stop>
        <div class="ctx-item" @click="ctxAction('open')">📝 打开</div>
        <div class="ctx-item" @click="ctxAction('add-to-chat')">💬 添加到对话</div>
        <div class="ctx-item" @click="ctxAction('copy-path')">📋 复制路径</div>
        <div class="ctx-item" @click="ctxAction('open-explorer')">📂 在资源管理器中打开</div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onBeforeUnmount } from 'vue'

const props = defineProps<{
  node: any
  depth: number
  colorIndex?: number
}>()

const emit = defineEmits(['select', 'context'])

const expanded = ref(props.depth < 1)
const isSelected = ref(false)

function toggle() {
  expanded.value = !expanded.value
}

function openFile() {
  const path = props.node.file_path || `.argus/${props.node.id}`
  isSelected.value = true
  emit('select', path)
}

const colorClass = computed(() => {
  const idx = props.colorIndex ?? 0
  return idx % 2 === 0 ? 'color-a' : 'color-b'
})

function countFiles(n: any): number {
  let c = (n.files || []).length
  for (const child of (n.children || [])) {
    c += countFiles(child)
  }
  return c
}

const fileCount = computed(() => countFiles(props.node))

// 右键菜单
const ctxMenu = reactive({ visible: false, x: 0, y: 0 })

function closeCtx() { ctxMenu.visible = false }

function showContextMenu(e: MouseEvent) {
  ctxMenu.visible = true
  ctxMenu.x = e.clientX
  ctxMenu.y = e.clientY
}

function ctxAction(action: string) {
  closeCtx()
  const item = props.node
  const filePath = item.file_path || `.argus/${item.id}`
  emit('context', { action, item: { ...item, path: filePath, name: item.id } })
}

document.addEventListener('click', closeCtx)
onBeforeUnmount(() => document.removeEventListener('click', closeCtx))
</script>

<style scoped>
.node-row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  font-size: 13px;
  cursor: pointer;
  border-radius: 0;
  transition: background 0.1s;
  user-select: none;
}
.node-row:hover {
  background: var(--bg-tertiary);
}
.node-row.selected {
  background: var(--bg-tertiary);
}

.color-a {
  color: #64b5f6;
}
.color-b {
  color: #81c784;
}
.file-row {
  font-weight: 400;
}
.file-row .node-title {
  color: var(--text-primary);
}

.folder-icon {
  font-size: 14px;
  flex-shrink: 0;
  width: 18px;
  text-align: center;
}
.file-icon {
  font-size: 13px;
  flex-shrink: 0;
  width: 18px;
  text-align: center;
}

.node-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-count {
  font-size: 11px;
  opacity: 0.6;
  margin-left: auto;
  padding-left: 8px;
  flex-shrink: 0;
}

.dirty-dot {
  font-size: 10px;
  flex-shrink: 0;
}

.empty-files {
  padding: 4px 8px;
  font-size: 11px;
  color: var(--text-tertiary);
  text-align: center;
}

.ctx-menu {
  position: fixed; z-index: 9999; min-width: 160px;
  background: var(--bg-secondary); border: 1px solid var(--border-color);
  border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.3);
  padding: 4px 0; animation: ctxIn 0.1s ease;
}
@keyframes ctxIn { from { opacity: 0; transform: scale(0.95); } to { opacity: 1; transform: scale(1); } }

.ctx-item {
  padding: 7px 14px; cursor: pointer; font-size: 12.5px;
  color: var(--text-primary); display: flex; align-items: center; gap: 8px;
}
.ctx-item:hover { background: var(--bg-tertiary); }
</style>
