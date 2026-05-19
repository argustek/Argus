<template>
  <div class="file-tree-item">
    <div 
      class="item-row"
      :class="{ 'is-folder': item.type === 'folder', 'is-open': isOpen }"
      @click="toggle"
    >
      <span class="icon">{{ item.type === 'folder' ? (isOpen ? '📂' : '📁') : '📄' }}</span>
      <span class="name">{{ item.name }}</span>
    </div>
    <div v-if="item.type === 'folder' && isOpen && item.children" class="children">
      <FileTreeItem 
        v-for="child in item.children" 
        :key="child.name"
        :item="child"
        @select="$emit('select', $event)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{
  item: any
}>()

defineEmits(['select'])

const isOpen = ref(props.item.expanded || false)

function toggle() {
  if (props.item.type === 'folder') {
    isOpen.value = !isOpen.value
  } else {
    // 点击文件：触发选择事件
    emit('select', props.item)
  }
}
</script>

<style scoped>
.file-tree-item {
  user-select: none;
}

.item-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  cursor: pointer;
  border-radius: 4px;
}

.item-row:hover {
  background: var(--bg-tertiary);
}

.item-row.is-folder {
  font-weight: 500;
}

.icon {
  font-size: 14px;
}

.name {
  font-size: 13px;
}

.children {
  padding-left: 16px;
}
</style>
