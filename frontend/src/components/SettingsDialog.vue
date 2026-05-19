<template>
  <div v-if="show" class="dialog-overlay" @click.self="close">
    <div class="dialog">
      <div class="dialog-header">
        <h3>设置</h3>
        <button class="close-btn" @click="close">×</button>
      </div>
      
      <div class="dialog-body">
        <div class="settings-tabs">
          <div 
            class="settings-tab"
            :class="{ active: activeTab === 'general' }"
            @click="activeTab = 'general'"
          >
            常规
          </div>
          <div 
            class="settings-tab"
            :class="{ active: activeTab === 'monitor' }"
            @click="activeTab = 'monitor'"
          >
            监控
          </div>
          <div 
            class="settings-tab"
            :class="{ active: activeTab === 'ai' }"
            @click="activeTab = 'ai'"
          >
            AI 配置
          </div>
        </div>
        
        <div class="settings-content">
          <!-- 常规设置 -->
          <div v-if="activeTab === 'general'" class="settings-section">
            <div class="form-group">
              <label>主题</label>
              <select class="input">
                <option value="dark">深色主题</option>
                <option value="light">浅色主题</option>
              </select>
            </div>
            <div class="form-group">
              <label>字体大小</label>
              <input type="number" class="input" value="14" />
            </div>
          </div>
          
          <!-- 监控设置 -->
          <div v-if="activeTab === 'monitor'" class="settings-section">
            <div class="form-group">
              <label>检查间隔 (秒)</label>
              <input type="number" class="input" v-model.number="localConfig.checkInterval" />
            </div>
            <div class="form-group">
              <label>心跳超时 (秒)</label>
              <input type="number" class="input" v-model.number="localConfig.heartbeatTimeout" />
            </div>
            <div class="form-group">
              <label>看板 A 路径</label>
              <input type="text" class="input" v-model="localConfig.board1Path" />
            </div>
            <div class="form-group">
              <label>看板 B 路径</label>
              <input type="text" class="input" v-model="localConfig.board2Path" />
            </div>
            <div class="form-group">
              <label>最大重试次数</label>
              <input type="number" class="input" v-model.number="localConfig.maxRetryCount" />
            </div>
            <div class="form-group checkbox">
              <label>
                <input type="checkbox" v-model="localConfig.autoRecovery" />
                启用自动恢复
              </label>
            </div>
          </div>
          
          <!-- AI 配置 -->
          <div v-if="activeTab === 'ai'" class="settings-section">
            <div class="form-group">
              <label>管理 AI API</label>
              <input type="text" class="input" v-model="localConfig.managerAPI" />
            </div>
            <div class="form-group">
              <label>Worker API</label>
              <input type="text" class="input" v-model="localConfig.workerAPI" />
            </div>
          </div>
        </div>
      </div>
      
      <div class="dialog-footer">
        <button class="btn" @click="close">取消</button>
        <button class="btn btn-primary" @click="save">保存</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  show: boolean
  config: any
}>()

const emit = defineEmits(['update:show', 'save'])

const activeTab = ref('monitor')
const localConfig = ref({ ...props.config })

watch(() => props.show, (newVal) => {
  if (newVal) {
    localConfig.value = { ...props.config }
  }
})

function close() {
  emit('update:show', false)
}

function save() {
  emit('save', localConfig.value)
  close()
}
</script>

<style scoped>
.dialog-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.dialog {
  width: 600px;
  max-height: 80vh;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-color);
}

.dialog-header h3 {
  font-size: 16px;
  font-weight: 600;
}

.close-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 20px;
  cursor: pointer;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.close-btn:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.dialog-body {
  flex: 1;
  overflow: auto;
  display: flex;
  padding: 20px;
  gap: 20px;
}

.settings-tabs {
  width: 120px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.settings-tab {
  padding: 10px 12px;
  font-size: 13px;
  cursor: pointer;
  border-radius: 6px;
}

.settings-tab:hover {
  background: var(--bg-tertiary);
}

.settings-tab.active {
  background: var(--accent-color);
  color: #fff;
}

.settings-content {
  flex: 1;
}

.settings-section {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-group label {
  font-size: 13px;
  color: var(--text-secondary);
}

.form-group.checkbox label {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.form-group.checkbox input {
  width: auto;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 20px;
  border-top: 1px solid var(--border-color);
}
</style>
