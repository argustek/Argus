<script setup lang="ts">
import { ref, computed } from 'vue'

export interface DockPanelItem {
  id: string
  title: string
  component: any
  props?: Record<string, any>
  docked: boolean
}

const props = defineProps<{
  panels: DockPanelItem[]
}>()

const emit = defineEmits<{
  (e: 'undock', id: string): void
  (e: 'redock', id: string): void
  (e: 'close', id: string): void
}>()

const dockedPanels = computed(() => props.panels.filter(p => p.docked))

// --- Drag to undock ---
const draggingId = ref<string | null>(null)
const dockRef = ref<HTMLElement | null>(null)

function startDragUndock(e: MouseEvent, id: string) {
  draggingId.value = id
  document.addEventListener('mousemove', onDragMove)
  document.addEventListener('mouseup', onDragEnd)
}

function onDragMove(e: MouseEvent) {
  if (!draggingId.value || !dockRef.value) return
  const r = dockRef.value.getBoundingClientRect()
  const t = 30
  if (e.clientX < r.left - t || e.clientY < r.top - t || e.clientY > r.bottom + t) {
    emit('undock', draggingId.value)
    stopDrag()
  }
}

function onDragEnd() { stopDrag() }
function stopDrag() {
  draggingId.value = null
  document.removeEventListener('mousemove', onDragMove)
  document.removeEventListener('mouseup', onDragEnd)
}

// --- Resize between panels ---
const resizingIndex = ref(-1)
const resizeStartY = ref(0)
const panelHeights = ref<number[]>([])

function startResize(e: MouseEvent, index: number) {
  e.preventDefault()
  resizingIndex.value = index
  resizeStartY.value = e.clientY
  const el = dockRef.value
  if (!el) return
  const items = el.querySelectorAll('.dock-panel-item')
  panelHeights.value = Array.from(items).map((el: Element) => (el as HTMLElement).offsetHeight)

  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
}

function onResizeMove(e: MouseEvent) {
  if (resizingIndex.value < 0 || !dockRef.value) return
  const delta = e.clientY - resizeStartY.value
  const items = dockRef.value.querySelectorAll('.dock-panel-item')
  const i = resizingIndex.value
  if (!items[i] || !items[i + 1]) return

  const above = items[i] as HTMLElement
  const below = items[i + 1] as HTMLElement
  above.style.flex = 'none'
  above.style.height = Math.max(80, panelHeights.value[i] + delta) + 'px'
  below.style.flex = 'none'
  below.style.height = Math.max(80, panelHeights.value[i + 1] - delta) + 'px'
}

function onResizeEnd() {
  resizingIndex.value = -1
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
}
</script>

<template>
  <div v-if="dockedPanels.length > 0" class="dock-area" ref="dockRef">
    <div
      v-for="(panel, index) in dockedPanels"
      :key="panel.id"
      class="dock-panel-item"
      :class="{ dragging: draggingId === panel.id }"
    >
      <!-- Header -->
      <div class="dock-header" @mousedown="startDragUndock($event, panel.id)">
        <span class="dock-title">{{ panel.title }}</span>
        <button class="dock-btn" title="Float" @click.stop="emit('undock', panel.id)">⊞</button>
        <button class="dock-btn close" title="Close" @click.stop="emit('close', panel.id)">×</button>
      </div>

      <!-- Content -->
      <div class="dock-body">
        <component :is="panel.component" v-bind="panel.props" />
      </div>

      <!-- Resize handle at bottom of each panel (except last) -->
      <div
        v-if="index < dockedPanels.length - 1"
        class="dock-splitter"
        :class="{ active: resizingIndex === index }"
        @mousedown="startResize($event, index)"
      ></div>
    </div>
  </div>
</template>

<style scoped>
.dock-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 280px;
  overflow: hidden;
  border-left: 1px solid var(--border-color);
  background: var(--bg-secondary);
}

.dock-panel-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 80px;
  position: relative;
}

.dock-panel-item.dragging { opacity: 0.5; }

.dock-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  background: var(--bg-tertiary);
  border-bottom: 1px solid var(--border-color);
  cursor: grab;
  user-select: none;
  font-size: 12px;
  color: var(--text-secondary);
  flex-shrink: 0;
}
.dock-header:active { cursor: grabbing; }

.dock-title {
  flex: 1;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dock-btn {
  background: transparent;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  padding: 2px 4px;
  border-radius: 3px;
  transition: all 0.15s;
}
.dock-btn:hover {
  background: rgba(99, 102, 241, 0.15);
  color: #6366f1;
}
.dock-btn.close:hover { color: #ef4444; }

.dock-body {
  flex: 1;
  overflow: auto;
}

/* Splitter between panels */
.dock-splitter {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 4px;
  cursor: row-resize;
  z-index: 5;
  transition: background 0.12s;
  margin-bottom: -2px; /* overlap border */
}
.dock-splitter:hover,
.dock-splitter.active {
  background: rgba(99, 102, 241, 0.35);
}
</style>
