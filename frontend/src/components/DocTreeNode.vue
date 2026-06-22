<template>
  <div class="doc-tree-node">
    <div class="node-row" :class="{ selected: isSelected }" @click="handleClick">
      <span class="toggle-icon" v-if="node.children?.length" @click.stop="toggle">
        {{ expanded ? '▼' : '▶' }}
      </span>
      <span class="toggle-icon spacer" v-else></span>
      <span class="role-tag" :class="roleClass">{{ roleLabel }}</span>
      <span class="node-title" :title="node.summary || node.id">{{ node.title || node.id }}</span>
      <span v-if="node.dirty" class="dirty-dot" title="已修改">🟡</span>
      <span v-if="node.exports?.length" class="export-badge" :title="exportsTitle">{{ node.exports.length }}</span>
    </div>
    <div v-if="expanded && node.children?.length" class="node-children">
      <DocTreeNode
        v-for="child in node.children"
        :key="child.id"
        :node="child"
        :depth="depth + 1"
        @select="(path: string) => emit('select', path)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  node: any
  depth: number
}>()

const emit = defineEmits(['select'])

const expanded = ref(props.depth < 1)
const isSelected = ref(false)

function toggle() {
  expanded.value = !expanded.value
}

function handleClick() {
  // If leaf node (no children), treat as file open
  if (!props.node.children?.length) {
    const path = `.argus/tree/${props.node.id}.md`
    isSelected.value = true
    emit('select', path)
  } else {
    toggle()
  }
}

const roleClass = computed(() => (props.node.owner_role || 'pm').toLowerCase())
const roleLabel = computed(() => t(`docTree.roles.${props.node.owner_role || 'PM'}`))
const exportsTitle = computed(() => {
  const names = props.node.exports?.map((e: any) => e.name || e) || []
  return names.join(', ')
})
</script>

<style scoped>
.node-row {
  display: flex;
  align-items: center;
  gap: 3px;
  padding: 3px 8px;
  font-size: 13px;
  cursor: pointer;
  border-radius: 0;
  transition: background 0.1s;
}
.node-row:hover {
  background: var(--bg-tertiary);
}
.node-row.selected {
  background: var(--bg-tertiary);
  color: var(--accent-color);
}

.toggle-icon {
  width: 14px;
  font-size: 10px;
  color: var(--text-tertiary);
  flex-shrink: 0;
  text-align: center;
}
.toggle-icon.spacer {
  visibility: hidden;
}

.role-tag {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 3px;
  flex-shrink: 0;
  text-transform: uppercase;
}
.role-tag.pm {
  background: #1d4ed8;
  color: #fff;
}
.role-tag.se {
  background: #059669;
  color: #fff;
}
.role-tag.ap {
  background: #d97706;
  color: #fff;
}

.node-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
}

.dirty-dot {
  font-size: 10px;
  flex-shrink: 0;
}

.export-badge {
  font-size: 10px;
  background: var(--border-color);
  color: var(--text-secondary);
  padding: 0 5px;
  border-radius: 8px;
  flex-shrink: 0;
}

.node-children {
  /* children inherit padding from parent via depth */
}
</style>
