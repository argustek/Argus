<template>
  <div class="file-tree-item">
    <div
      class="item-row"
      :class="{ 'is-folder': item.type === 'folder', 'is-open': isOpen, 'is-selected': isSelected }"
      @click="onLeftClick"
      @contextmenu.prevent="showContextMenu($event)"
    >
      <span class="icon">{{ item.type === 'folder' ? (isOpen ? '📂' : '📁') : '📄' }}</span>
      <span class="name">{{ item.name }}</span>
    </div>
    <div v-if="item.type === 'folder' && isOpen && item.children" class="children">
      <FileTreeItem
        v-for="child in item.children"
        :key="child.name"
        :item="child"
        :selected-path="selectedPath"
        @select="$emit('select', $event)"
        @context="$emit('context', $event)"
      />
    </div>

    <!-- 右键菜单 -->
    <Teleport to="body">
      <div v-if="ctxMenu.visible" class="ctx-menu" :style="{ top: ctxMenu.y + 'px', left: ctxMenu.x + 'px' }" @click.stop>
        <template v-if="ctxMenu.item?.type === 'file'">
          <div class="ctx-item" @click="ctxAction('open')">📝 打开</div>
          <div class="ctx-item" @click="ctxAction('copy-path')">📋 复制路径</div>
          <div class="ctx-sep"></div>
          <div class="ctx-item danger" @click="ctxAction('delete')">🗑️ 删除</div>
        </template>
        <template v-else>
          <div class="ctx-item" @click="ctxAction('open-explorer')">📂 在资源管理器中打开</div>
          <div class="ctx-item" @click="ctxAction('copy-path')">📋 复制路径</div>
        </template>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onBeforeUnmount } from 'vue'

const props = defineProps<{
  item: any
  selectedPath?: string
}>()

const emit = defineEmits(['select', 'context'])

const isOpen = ref(props.item.expanded || false)
const isSelected = computed(() => props.selectedPath === props.item.path)

// 右键菜单状态
const ctxMenu = reactive({ visible: false, x: 0, y: 0, item: null as any })

function closeCtx() { ctxMenu.visible = false }

function onLeftClick() {
  if (props.item.type === 'folder') {
    isOpen.value = !isOpen.value
  } else {
    emit('select', props.item)
  }
}

function showContextMenu(e: MouseEvent) {
  e.stopPropagation()
  ctxMenu.visible = true
  ctxMenu.x = e.clientX
  ctxMenu.y = e.clientY
  ctxMenu.item = props.item
}

async function ctxAction(action: string) {
  const item = ctxMenu.item
  closeCtx()
  if (!item) return

  switch (action) {
    case 'open':
      emit('select', item)
      break
    case 'copy-path': {
      // @ts-ignore
      await navigator.clipboard.writeText(item.path)
      break
    }
    case 'delete':
      if (confirm(`确定删除 ${item.name}？`)) {
        emit('context', { action: 'delete', item })
      }
      break
    case 'open-explorer':
      emit('context', { action: 'open-explorer', item })
      break
  }
}

// 点击其他地方关闭菜单
document.addEventListener('click', closeCtx)
onBeforeUnmount(() => document.removeEventListener('click', closeCtx))
</script>

<style scoped>
.file-tree-item { user-select: none; position: relative; }

.item-row {
  display: flex; align-items: center; gap: 6px;
  padding: 3px 8px; cursor: pointer; border-radius: 4px;
}
.item-row:hover { background: var(--bg-tertiary); }
.item-row.is-folder { font-weight: 500; }
.item-row.is-selected { background: rgba(99, 102, 241, 0.15); }

.icon { font-size: 14px; flex-shrink: 0; }
.name { font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.children { padding-left: 16px; }

/* 右键菜单 */
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
.ctx-item.danger { color: #f87171; }
.ctx-item.danger:hover { background: rgba(248,113,113,0.1); }

.ctx-sep { height: 1px; background: var(--border-color); margin: 4px 0; }
</style>
