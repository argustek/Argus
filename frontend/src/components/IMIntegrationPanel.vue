<template>
  <div class="im-integration-panel">
    <div class="panel-header">
      <h2 class="panel-title">IM 集成配置</h2>
      <p class="panel-desc">配置钉钉、企业微信、飞书等 IM 平台机器人接入</p>
    </div>

    <!-- 顶部横向标签页 -->
    <div class="platform-tabs">
      <button 
        v-for="platform in platforms" 
        :key="platform.id"
        class="tab-btn"
        :class="{ active: selectedPlatform === platform.id }"
        @click="selectPlatform(platform.id)"
      >
        <span class="tab-icon">{{ platform.icon }}</span>
        <span class="tab-name">{{ platform.name }}</span>
        <span v-if="platform.enabled" class="tab-status enabled">●</span>
        <span v-else class="tab-status disabled">○</span>
      </button>
    </div>

    <!-- 配置内容区域 -->
    <div class="config-content">
      <!-- 钉钉配置 -->
      <div v-if="selectedPlatform === 'dingtalk'" class="config-form">
        <div class="form-header">
          <h3>钉钉机器人配置</h3>
          <label class="switch-label">
            <span>启用</span>
            <input type="checkbox" v-model="dingtalkConfig.enabled" />
            <span class="switch-slider"></span>
          </label>
        </div>

        <div class="form-body">
          <div class="form-row">
            <div class="form-group">
              <label>机器人名称</label>
              <input 
                type="text" 
                v-model="dingtalkConfig.name" 
                class="form-input"
                placeholder="如：Argus助手"
              />
            </div>
            <div class="form-group">
              <label>Client ID (AppKey)</label>
              <input 
                type="text" 
                v-model="dingtalkConfig.clientId" 
                class="form-input"
                placeholder="钉钉应用 Client ID"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label>Client Secret</label>
              <div class="input-with-toggle">
                <input 
                  :type="showSecret ? 'text' : 'password'" 
                  v-model="dingtalkConfig.clientSecret" 
                  class="form-input"
                  placeholder="钉钉应用 Client Secret"
                />
                <button class="toggle-btn" @click="showSecret = !showSecret">
                  {{ showSecret ? '🙈' : '👁️' }}
                </button>
              </div>
            </div>
            <div class="form-group">
              <label>Robot Code</label>
              <input 
                type="text" 
                v-model="dingtalkConfig.robotCode" 
                class="form-input"
                placeholder="机器人编码"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label>接入方式</label>
              <div class="radio-group">
                <label class="radio-item">
                  <input type="radio" v-model="dingtalkConfig.mode" value="stream" />
                  <span class="radio-text">Stream 模式（实时推送）</span>
                </label>
                <label class="radio-item">
                  <input type="radio" v-model="dingtalkConfig.mode" value="webhook" />
                  <span class="radio-text">Webhook 模式（需公网地址）</span>
                </label>
              </div>
            </div>
            <div class="form-group">
              <label>默认回复</label>
              <input 
                type="text" 
                v-model="dingtalkConfig.defaultReply" 
                class="form-input"
                placeholder="收到，正在处理..."
              />
            </div>
          </div>
        </div>

        <div class="form-footer">
          <button class="btn-secondary" @click="testConnection">测试连接</button>
          <button class="btn-primary" @click="saveConfig">保存配置</button>
        </div>
      </div>

      <!-- 企业微信配置 -->
      <div v-else-if="selectedPlatform === 'wecom'" class="config-form">
        <div class="form-header">
          <h3>企业微信配置</h3>
          <label class="switch-label">
            <span>启用</span>
            <input type="checkbox" v-model="wecomConfig.enabled" />
            <span class="switch-slider"></span>
          </label>
        </div>
        <div class="form-body">
          <div class="placeholder-msg">
            <span class="msg-icon">🚧</span>
            <span class="msg-text">企业微信接入开发中...</span>
          </div>
        </div>
      </div>

      <!-- 飞书配置 -->
      <div v-else-if="selectedPlatform === 'feishu'" class="config-form">
        <div class="form-header">
          <h3>飞书配置</h3>
          <label class="switch-label">
            <span>启用</span>
            <input type="checkbox" v-model="feishuConfig.enabled" />
            <span class="switch-slider"></span>
          </label>
        </div>
        <div class="form-body">
          <div class="placeholder-msg">
            <span class="msg-icon">🚧</span>
            <span class="msg-text">飞书接入开发中...</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { GetDingTalkConfig, SaveDingTalkConfig } from '../../wailsjs/go/main/App'

const selectedPlatform = ref('dingtalk')
const showSecret = ref(false)

const platforms = ref([
  { id: 'dingtalk', name: '钉钉', icon: '📱', enabled: false },
  { id: 'wecom', name: '企业微信', icon: '💼', enabled: false },
  { id: 'feishu', name: '飞书', icon: '🚀', enabled: false },
])

const dingtalkConfig = reactive({
  enabled: false,
  name: '',
  clientId: '',
  clientSecret: '',
  robotCode: '',
  mode: 'stream',
  defaultReply: '收到，正在处理...',
})

const wecomConfig = reactive({
  enabled: false,
})

const feishuConfig = reactive({
  enabled: false,
})

const selectPlatform = (id: string) => {
  selectedPlatform.value = id
}

const loadConfig = async () => {
  try {
    const config = await GetDingTalkConfig()
    if (config) {
      dingtalkConfig.enabled = config.enabled
      dingtalkConfig.name = config.name
      dingtalkConfig.clientId = config.client_id
      dingtalkConfig.clientSecret = config.client_secret
      dingtalkConfig.robotCode = config.robot_code
      dingtalkConfig.mode = config.mode
      dingtalkConfig.defaultReply = config.default_reply
      
      // 更新平台状态
      const dingtalk = platforms.value.find(p => p.id === 'dingtalk')
      if (dingtalk) {
        dingtalk.enabled = config.enabled
      }
    }
  } catch (error) {
    console.error('加载配置失败:', error)
  }
}

const saveConfig = async () => {
  try {
    await SaveDingTalkConfig({
      enabled: dingtalkConfig.enabled,
      name: dingtalkConfig.name,
      client_id: dingtalkConfig.clientId,
      client_secret: dingtalkConfig.clientSecret,
      robot_code: dingtalkConfig.robotCode,
      mode: dingtalkConfig.mode,
      default_reply: dingtalkConfig.defaultReply,
    })
    
    // 更新平台状态
    const dingtalk = platforms.value.find(p => p.id === 'dingtalk')
    if (dingtalk) {
      dingtalk.enabled = dingtalkConfig.enabled
    }
    
    alert('配置已保存')
  } catch (error) {
    console.error('保存配置失败:', error)
    alert('保存失败: ' + error)
  }
}

const testConnection = async () => {
  alert('测试连接功能开发中...')
}

onMounted(() => {
  loadConfig()
})
</script>

<style scoped>
.im-integration-panel {
  padding: 20px;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.panel-header {
  margin-bottom: 20px;
}

.panel-title {
  font-size: 18px;
  font-weight: 600;
  margin: 0 0 8px 0;
  color: #e0e0e0;
}

.panel-desc {
  font-size: 13px;
  color: #888;
  margin: 0;
}

/* 顶部横向标签页 */
.platform-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 0;
  border-bottom: 1px solid #333;
  padding-bottom: 0;
}

.tab-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  color: #888;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s;
  position: relative;
  top: 1px;
}

.tab-btn:hover {
  color: #ccc;
  background: #1a1a1a;
}

.tab-btn.active {
  color: #4a9eff;
  border-bottom-color: #4a9eff;
  background: #1a1a1a;
}

.tab-icon {
  font-size: 16px;
}

.tab-status {
  font-size: 10px;
  margin-left: 4px;
}

.tab-status.enabled {
  color: #4caf50;
}

.tab-status.disabled {
  color: #666;
}

/* 配置内容区域 */
.config-content {
  flex: 1;
  background: #1a1a1a;
  border-radius: 0 0 8px 8px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.config-form {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.form-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid #333;
}

.form-header h3 {
  margin: 0;
  font-size: 16px;
  color: #e0e0e0;
}

.switch-label {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
  color: #aaa;
}

.switch-label input {
  display: none;
}

.switch-slider {
  width: 40px;
  height: 20px;
  background: #444;
  border-radius: 10px;
  position: relative;
  transition: background 0.3s;
}

.switch-slider::after {
  content: '';
  position: absolute;
  width: 16px;
  height: 16px;
  background: #fff;
  border-radius: 50%;
  top: 2px;
  left: 2px;
  transition: transform 0.3s;
}

.switch-label input:checked + .switch-slider {
  background: #4a9eff;
}

.switch-label input:checked + .switch-slider::after {
  transform: translateX(20px);
}

.form-body {
  flex: 1;
  padding: 20px;
  overflow-y: auto;
}

.form-row {
  display: flex;
  gap: 16px;
  margin-bottom: 16px;
}

.form-row .form-group {
  flex: 1;
  margin-bottom: 0;
}

.form-group {
  margin-bottom: 16px;
}

.form-group label {
  display: block;
  font-size: 13px;
  color: #aaa;
  margin-bottom: 6px;
}

.form-input {
  width: 100%;
  padding: 10px 12px;
  background: #252525;
  border: 1px solid #333;
  border-radius: 6px;
  color: #e0e0e0;
  font-size: 13px;
  box-sizing: border-box;
}

.form-input:focus {
  outline: none;
  border-color: #4a9eff;
}

.input-with-toggle {
  display: flex;
  gap: 8px;
}

.input-with-toggle .form-input {
  flex: 1;
}

.toggle-btn {
  padding: 0 12px;
  background: #333;
  border: 1px solid #444;
  border-radius: 6px;
  color: #aaa;
  cursor: pointer;
}

.radio-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px 0;
}

.radio-item {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
  color: #ccc;
}

.radio-item input {
  margin: 0;
}

.form-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 20px;
  border-top: 1px solid #333;
}

.btn-secondary {
  padding: 8px 16px;
  background: #333;
  border: 1px solid #444;
  border-radius: 6px;
  color: #ccc;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-secondary:hover {
  background: #3a3a3a;
}

.btn-primary {
  padding: 8px 16px;
  background: #4a9eff;
  border: none;
  border-radius: 6px;
  color: #fff;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-primary:hover {
  background: #5aaaff;
}

.placeholder-msg {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  color: #666;
  gap: 12px;
}

.msg-icon {
  font-size: 48px;
}

.msg-text {
  font-size: 14px;
}
</style>
