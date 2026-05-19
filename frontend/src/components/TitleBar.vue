<template>
  <div class="title-bar">
    <div class="menu-bar">
      <div class="logo">
        <span class="logo-icon">👁️</span>
        <span>Argus</span>
      </div>
      <div class="menus">
        <div class="menu-item" @click="$emit('menu-click', 'file')">文件</div>
        <div class="menu-item" @click="$emit('menu-click', 'edit')">编辑</div>
        <div class="menu-item" @click="$emit('menu-click', 'view')">视图</div>
        <div class="menu-item" @click="$emit('menu-click', 'ai')">AI</div>
        <div class="menu-item" @click="$emit('menu-click', 'settings')">设置</div>
        <div class="menu-item" @click="$emit('menu-click', 'help')">帮助</div>
      </div>
    </div>
    <div class="window-controls">
      <button class="win-btn minimize">−</button>
      <button class="win-btn maximize">□</button>
      <button class="win-btn close">×</button>
    </div>
  </div>
</template>

<script setup lang="ts">
defineEmits(['menu-click'])
</script>

<style scoped>
.title-bar {
  height: 40px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  -webkit-app-region: drag;
}

.menu-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  -webkit-app-region: no-drag;
}

.logo {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 14px;
}

.logo-icon {
  font-size: 18px;
}

.menus {
  display: flex;
  gap: 4px;
}

.menu-item {
  padding: 4px 12px;
  font-size: 13px;
  cursor: pointer;
  border-radius: 4px;
}

.menu-item:hover {
  background: var(--bg-tertiary);
}

.window-controls {
  display: flex;
  gap: 8px;
  -webkit-app-region: no-drag;
}

.win-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 14px;
  cursor: pointer;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.win-btn:hover {
  background: var(--bg-tertiary);
}

.win-btn.close:hover {
  background: var(--error-color);
}
</style>
