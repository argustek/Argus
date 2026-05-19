<template>
  <div class="side-bar">
    <div
      class="icon-btn"
      :class="{ active: activePanel === 'explorer' }"
      @click="$emit('panel-change', 'explorer')"
      title="资源管理器"
    >
      📁
    </div>
    <div
      class="icon-btn"
      :class="{ active: activePanel === 'search' }"
      @click="$emit('panel-change', 'search')"
      title="搜索"
    >
      🔍
    </div>
    <div
      class="icon-btn"
      :class="{ active: activePanel === 'git' }"
      @click="$emit('panel-change', 'git')"
      title="Git"
    >
      📊
    </div>
    <div
      class="icon-btn"
      :class="{ active: activePanel === 'ai' }"
      @click="$emit('panel-change', 'ai')"
      title="AI 助手"
    >
      🤖
    </div>
    <div
      class="icon-btn"
      :class="{ active: activePanel === 'monitor' }"
      @click="$emit('panel-change', 'monitor')"
      title="监控"
    >
      👁️
    </div>
    <div class="spacer"></div>
    <div class="icon-btn" title="账户">👤</div>
    <div class="icon-btn" title="设置">⚙️</div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  activePanel: string
}>()

defineEmits(['panel-change'])
</script>

<style scoped>
.side-bar {
  width: 48px;
  background: var(--bg-secondary);
  border-right: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 8px 0;
}

.icon-btn {
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  cursor: pointer;
  border-radius: 6px;
  margin-bottom: 4px;
}

.icon-btn:hover {
  background: var(--bg-tertiary);
}

.icon-btn.active {
  background: var(--bg-tertiary);
  border-left: 2px solid var(--accent-color);
}

.spacer {
  flex: 1;
}
</style>
