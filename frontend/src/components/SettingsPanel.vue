<template>
  <div class="settings-overlay" @click.self="$emit('close')">
    <div class="settings-panel">
      <div class="settings-header">
        <h3>{{ t('settings.title') }}</h3>
        <button class="close-btn" @click="$emit('close')">×</button>
      </div>
      
      <div class="settings-layout">
        <!-- 顶部 Tab 栏 -->
        <div class="settings-tabs">
          <button 
            class="tab-item"
            :class="{ active: currentTab === 'api' }"
            @click="currentTab = 'api'"
          >{{ t('settings.apiConfig') }}</button>
          <button 
            class="tab-item"
            :class="{ active: currentTab === 'im' }"
            @click="currentTab = 'im'"
          >{{ t('settings.imIntegration') }}</button>
          <button 
            class="tab-item"
            :class="{ active: currentTab === 'decision' }"
            @click="currentTab = 'decision'"
          >{{ t('settings.decisionConfig') }}</button>
          <button 
            class="tab-item"
            :class="{ active: currentTab === 'permission' }"
            @click="currentTab = 'permission'"
          >{{ t('settings.permissionConfig') }}</button>
          <button 
            class="tab-item"
            :class="{ active: currentTab === 'ui' }"
            @click="currentTab = 'ui'"
          >{{ t('settings.ui') }}</button>
          <button 
            class="tab-item"
            :class="{ active: currentTab === 'about' }"
            @click="currentTab = 'about'"
          >{{ t('settings.about') }}</button>
        </div>
        
        <div class="settings-content">
        <!-- API 配置 -->
        <div v-if="currentTab === 'api'" class="tab-panel">
        <div class="settings-section">
          <div class="section-header">
            <h4>{{ t('settings.apiConfig') }}</h4>
            <button class="btn-small" @click="addApiConfig">+ {{ t('settings.addConfig') }}</button>
          </div>
          
          <!-- API 配置列表 -->
          <div class="api-config-list">
            <div 
              v-for="(config, index) in apiConfigs" 
              :key="config.id"
              class="api-config-item"
              :class="{ 
                active: selectedConfigIndex === index, 
                default: config.isDefault,
                'current-use': currentApiConfigId === config.id
              }"
              @click="selectConfig(index)"
            >
              <div class="config-info">
                <span class="config-name">{{ config.name }}</span>
                <span class="config-provider">{{ getProviderName(config.provider) }} · {{ config.modelName }}</span>
                <span v-if="currentApiConfigId === config.id" class="current-badge">🟢 {{ t('settings.inUse') }}</span>
                <span v-else-if="config.isDefault" class="default-badge">{{ t('common.default') }}</span>
              </div>
              <div class="config-item-actions">
                <button 
                  v-if="currentApiConfigId !== config.id" 
                  class="switch-btn" 
                  @click.stop="switchToConfig(config.id)"
                  :title="t('common.switch')"
                >{{ t('common.switch') }}</button>
                <button class="delete-btn" @click.stop="deleteConfig(index)">×</button>
              </div>
            </div>
          </div>
          
          <!-- 选中的配置详情 -->
          <div v-if="selectedConfig" class="config-detail">
            <div class="form-group">
              <label>{{ t('settings.configName') }}</label>
              <input type="text" class="input" v-model="selectedConfig.name" :placeholder="t('settings.configNamePlaceholder')" />
            </div>
            
            <div class="form-group">
              <label>{{ t('settings.provider') }}</label>
              <select class="input" v-model="selectedConfig.provider" @change="onProviderChange">
                <option value="qwen">{{ t('settings.providerQwen') }}</option>
                <option value="zhipu">{{ t('settings.providerZhipu') }}</option>
                <option value="openai">{{ t('settings.providerOpenAI') }}</option>
                <option value="claude">{{ t('settings.providerClaude') }}</option>
                <option value="deepseek">{{ t('settings.providerDeepSeek') }}</option>
                <option value="nvidia">{{ t('settings.providerNvidia') }}</option>
                <option value="custom">{{ t('settings.providerCustom') }}</option>
              </select>
            </div>
            
            <div class="form-group">
              <label>{{ t('settings.baseUrl') }}</label>
              <input type="text" class="input" v-model="selectedConfig.baseUrl" :placeholder="t('settings.baseUrlPlaceholder')" />
            </div>
            
            <div class="form-group">
              <label>{{ t('settings.apiKey') }}</label>
              <input type="password" class="input" v-model="selectedConfig.apiKey" :placeholder="t('settings.apiKeyPlaceholder')" />
            </div>
            
            <div class="form-group">
              <label>{{ t('settings.modelName') }}</label>
              <input type="text" class="input" v-model="selectedConfig.modelName" :placeholder="t('settings.modelNamePlaceholder')" />
            </div>
            
            <div class="form-group checkbox">
              <label>
                <input type="checkbox" v-model="selectedConfig.supportsMultimodal" />
                {{ t('settings.supportsMultimodal') }}
              </label>
            </div>
            
            <div class="form-group checkbox">
              <label>
                <input type="checkbox" v-model="selectedConfig.isDefault" @change="onDefaultChange" />
                {{ t('settings.setAsDefault') }}
              </label>
            </div>
            
            <div class="config-actions">
              <button class="btn" @click="testConfig">{{ t('settings.testConnection') }}</button>
              <span v-if="testStatus" :class="['test-status', testStatus.type]">{{ testStatus.message }}</span>
            </div>
          </div>

          <!-- AP 审批者配置（独立模型选择） -->
          <div class="settings-section" style="margin-top: 20px; border-top: 1px solid var(--border-color); padding-top: 16px;">
            <div class="section-header">
              <h4>🛡️ AP 审批者配置</h4>
            </div>

            <div class="form-group checkbox">
              <label>
                <input type="checkbox" v-model="useIndependentModel" @change="onUseIndependentModelChange" />
                AP 使用独立模型
              </label>
            </div>

            <template v-if="useIndependentModel">
              <div class="form-group">
                <label>选择 API 配置</label>
                <select class="input" v-model="apConfigId" @change="onAPConfigChange">
                  <option v-for="config in apiConfigs" :key="config.id" :value="config.id">
                    {{ config.name }} - {{ getProviderName(config.provider) }} · {{ config.modelName }}
                  </option>
                </select>
              </div>
              <div class="config-actions">
                <button class="btn" @click="apTestConfig" :disabled="!apConfig">🧪 测试连接</button>
                <span v-if="apTestStatus" :class="['test-status', apTestStatus.type]">{{ apTestStatus.message }}</span>
              </div>
            </template>
          </div>
          </div>
        </div>
        </div>

        <!-- IM 集成 -->
        <div v-if="currentTab === 'im'" class="tab-panel">
          <div class="settings-section">
            <div class="section-header">
              <h4>{{ t('settings.imIntegration') }}</h4>
              <button class="btn-small" @click="addImConfig">+ {{ t('settings.addConfig') }}</button>
            </div>

            <div class="api-config-list">
              <div
                v-for="(config, index) in imConfigs"
                :key="config.id"
                class="api-config-item"
                :class="{ active: selectedImConfigIndex === index, enabled: config.enabled }"
                @click="selectImConfig(index)"
              >
                <div class="config-info">
                  <span class="config-name">{{ config.name || t('settings.unnamedConfig') }}</span>
                  <span class="config-provider">{{ getImProviderName(config.provider) }}</span>
                  <span v-if="config.enabled" class="default-badge">{{ t('common.enabled') }}</span>
                </div>
                <button class="delete-btn" @click.stop="deleteImConfig(index)">×</button>
              </div>
            </div>

            <div v-if="selectedImConfig" class="config-detail">
              <div class="form-group">
                <label>{{ t('settings.configName') }}</label>
                <input type="text" class="input" v-model="selectedImConfig.name" :placeholder="t('settings.imConfigNamePlaceholder')" />
              </div>

              <div class="form-group">
                <label>{{ t('settings.platformType') }}</label>
                <select class="input" v-model="selectedImConfig.provider">
                  <option value="dingtalk">{{ t('settings.providerDingTalk') }}</option>
                  <option value="wecom">{{ t('settings.providerWeCom') }}</option>
                  <option value="feishu">{{ t('settings.providerFeishu') }}</option>
                </select>
              </div>

              <template v-if="selectedImConfig.provider === 'dingtalk'">
                <div class="form-group">
                  <label>{{ t('settings.clientId') }}</label>
                  <input type="text" class="input" v-model="selectedImConfig.clientId" placeholder="DingTalk App Client ID" />
                </div>

                <div class="form-group">
                  <label>{{ t('settings.clientSecret') }}</label>
                  <input type="password" class="input" v-model="selectedImConfig.clientSecret" placeholder="DingTalk App Client Secret" />
                </div>

                <div class="form-group">
                  <label>{{ t('settings.robotCode') }}</label>
                  <input type="text" class="input" v-model="selectedImConfig.robotCode" placeholder="Robot Code" />
                </div>

                <div class="form-group">
                  <label>{{ t('settings.accessMode') }}</label>
                  <select class="input" v-model="selectedImConfig.mode">
                    <option value="stream">{{ t('settings.streamMode') }}</option>
                    <option value="webhook">{{ t('settings.webhookMode') }}</option>
                  </select>
                </div>
              </template>

              <template v-if="selectedImConfig.provider === 'wecom'">
                <div class="placeholder-msg">
                  <span class="msg-icon">🚧</span>
                  <span class="msg-text">{{ t('settings.wecomDeveloping') }}</span>
                </div>
              </template>

              <template v-if="selectedImConfig.provider === 'feishu'">
                <div class="placeholder-msg">
                  <span class="msg-icon">🚧</span>
                  <span class="msg-text">{{ t('settings.feishuDeveloping') }}</span>
                </div>
              </template>

              <div class="form-group checkbox" v-if="selectedImConfig.provider === 'dingtalk'">
                <label>
                  <input type="checkbox" v-model="selectedImConfig.enabled" />
                  {{ t('settings.enableConfig') }}
                </label>
              </div>

              <div class="config-actions" v-if="selectedImConfig.provider === 'dingtalk'">
                <button class="btn" @click="testImConnection">{{ t('settings.testConnection') }}</button>
                <span v-if="imTestStatus" :class="['test-status', imTestStatus.type]">{{ imTestStatus.message }}</span>
              </div>
            </div>
          </div>
        </div>
        </div>

        <!-- 决策配置 & 权限配置 -->
        <div v-if="currentTab === 'decision' || currentTab === 'permission'" class="tab-panel">
          <ConfigSettings :currentSubTab="currentTab" />
        </div>

        <!-- 界面 -->
        <div v-if="currentTab === 'ui'" class="tab-panel">
        <div class="settings-section">
          <h4>{{ t('settings.ui') }}</h4>
          <div class="form-group">
            <label>{{ t('settings.language') }}</label>
            <select class="input" v-model="currentLocale" @change="changeLocale">
              <option v-for="loc in availableLocales" :key="loc.code" :value="loc.code">{{ loc.label }}</option>
            </select>
          </div>
          <div class="form-group checkbox">
            <label>
              <input type="checkbox" v-model="localConfig.showCodeBlocks" />
              {{ t('settings.showCodeBlocks') }}
            </label>
          </div>
          <div class="form-group checkbox">
            <label>
              <input type="checkbox" v-model="localConfig.showThinking" />
              {{ t('settings.showThinking') }}
            </label>
          </div>
          <div class="form-group checkbox">
            <label>
              <input type="checkbox" v-model="localConfig.pmDecisionAlert" />
              {{ t('settings.pmDecisionAlert') }}
            </label>
          </div>
        </div>

        <!-- HTTP 服务设置 -->
        <div class="settings-section">
          <h4>{{ t('settings.httpService') || 'HTTP 服务' }}</h4>
          <div class="form-group checkbox">
            <label>
              <input type="checkbox" v-model="localHttpConfig.enabled" />
              {{ t('settings.httpEnabled') || '启用 HTTP API 服务' }}
            </label>
          </div>
          <template v-if="localHttpConfig.enabled">
            <div class="form-group">
              <label>{{ t('settings.httpPort') || '端口' }}</label>
              <input type="number" class="input" v-model.number="localHttpConfig.port" min="1024" max="65535" />
            </div>
            <div class="form-group">
              <label>{{ t('settings.httpApiToken') || 'API Token (可选)' }}</label>
              <input type="text" class="input" v-model="localHttpConfig.apiToken" :placeholder="'留空则不需要验证'" />
            </div>
            <div class="form-group checkbox">
              <label>
                <input type="checkbox" v-model="localHttpConfig.allowRemote" />
                {{ t('settings.httpAllowRemote') || '允许远程访问' }}
              </label>
            </div>
          </template>
        </div>
        
        <div v-if="currentTab === 'about'" class="tab-panel">
          <div class="settings-section version-section">
            <h4>{{ t('settings.about') }}</h4>
            <div class="version-info">
              <div class="version-item">
                <span class="version-label">{{ t('settings.argusDescription') }}</span>
              </div>
              <div class="version-item">
              <span class="version-label">{{ t('settings.version') }}</span>
              <span class="version-value">{{ version }}</span>
            </div>
            <div class="version-item">
              <span class="version-label">{{ t('settings.buildTime') }}</span>
              <span class="version-value">{{ buildTime }}</span>
            </div>
          </div>
        </div>
        </div>
      </div>
      
      <div class="settings-footer">
        <button class="btn" @click="$emit('close')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" @click="save">{{ t('common.save') }}</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { availableLocales } from '../i18n'
import { GetCurrentAPIConfigID, SwitchAPIConfig, TestAPIConfig, SetLang } from '../../wailsjs/go/main/App'
import ConfigSettings from './ConfigSettings.vue'

const { t, locale } = useI18n()

const currentLocale = ref(locale.value)

function changeLocale() {
  locale.value = currentLocale.value
  localStorage.setItem('argus-locale', currentLocale.value)
  // 同步到后端
  SetLang(currentLocale.value).catch((e: Error) => {
    console.error('后端语言设置失败:', e)
  })
}

const version = '1.0.21'
const buildTime = __BUILD_TIME__
const currentTab = ref('api')

interface ApiConfig {
  id: string
  name: string
  provider: string
  baseUrl: string
  apiKey: string
  modelName: string
  isDefault: boolean
  supportsMultimodal: boolean
  testPassed: boolean
}

const props = defineProps<{
  config: {
    apiConfigs?: ApiConfig[]
    imConfigs?: ImConfig[]
    showCodeBlocks?: boolean
    showThinking?: boolean
    pmDecisionAlert?: boolean
    http?: {
      enabled: boolean
      port: number
      apiToken: string
      allowRemote: boolean
    }
    apEnabled?: boolean
    apConfig?: ApiConfig | null
  }
}>()

const emit = defineEmits(['close', 'save', 'api-config-changed'])

const localConfig = ref({
  showCodeBlocks: props.config.showCodeBlocks ?? true,
  showThinking: props.config.showThinking ?? true,
  pmDecisionAlert: props.config.pmDecisionAlert ?? false
})

const localHttpConfig = ref({
  enabled: props.config.http?.enabled ?? false,
  port: props.config.http?.port ?? 8080,
  apiToken: props.config.http?.apiToken ?? '',
  allowRemote: props.config.http?.allowRemote ?? false
})

// AP 配置（使用已配置好的 API）
const apConfigId = ref<string>('') // 选中的API配置ID
const useIndependentModel = ref<boolean>(false) // 是否使用独立模型
const apConfig = ref<ApiConfig | null>(null) // 选中的独立API配置
const apTestStatus = ref<{type: string, message: string} | null>(null) // AP测试状态

function onUseIndependentModelChange() {
  if (!useIndependentModel.value) {
    apConfigId.value = ''
    apConfig.value = null
    apTestStatus.value = null
  } else {
    // 启用独立模型时，默认选中第一个API配置
    if (apiConfigs.value.length > 0) {
      apConfigId.value = apiConfigs.value[0].id
      apConfig.value = { ...apiConfigs.value[0] }
    }
  }
}

function onAPConfigChange() {
  if (apConfigId.value) {
    const selected = apiConfigs.value.find(c => c.id === apConfigId.value)
    if (selected) {
      apConfig.value = { ...selected }
    }
  } else {
    apConfig.value = null
  }
  apTestStatus.value = null
}

async function apTestConfig() {
  if (!apConfig.value) return
  apTestStatus.value = { type: 'testing', message: '测试中...' }
  try {
    const result = await TestAPIConfig(
      apConfig.value.provider,
      apConfig.value.baseUrl,
      apConfig.value.apiKey,
      apConfig.value.modelName
    )
    if (result.success) {
      apTestStatus.value = { type: 'success', message: result.message }
      apConfig.value.testPassed = true
    } else {
      apTestStatus.value = { type: 'error', message: result.message }
      apConfig.value.testPassed = false
    }
  } catch (e) {
    apTestStatus.value = { type: 'error', message: (e as Error).message }
    apConfig.value.testPassed = false
  }
}

const selectedConfigIndex = ref(0)
const testStatus = ref<{type: string, message: string} | null>(null)
const currentApiConfigId = ref<string>('')

// 获取当前正在使用的API配置ID
async function loadCurrentApiConfigId() {
  try {
    const id = await GetCurrentAPIConfigID()
    currentApiConfigId.value = id || ''
  } catch (e) {
    console.error('获取当前API配置失败:', e)
  }
}

// 切换到指定配置（立即生效，不重启）
async function switchToConfig(configId: string) {
  try {
    await SwitchAPIConfig(configId)
    currentApiConfigId.value = configId
    // 更新本地 isDefault 状态
    apiConfigs.value.forEach(config => {
      config.isDefault = (config.id === configId)
    })
    const newDefault = apiConfigs.value.find(c => c.id === configId)
    emit('api-config-changed', newDefault)
  } catch (e) {
    console.error('切换API配置失败:', e)
    alert(t('settings.switchFailed') + ': ' + (e as Error).message)
  }
}

// 组件挂载时加载当前配置
loadCurrentApiConfigId()

// API 配置列表 - 从 props 加载
const apiConfigs = ref<ApiConfig[]>(props.config.apiConfigs?.length > 0 
  ? props.config.apiConfigs 
  : [
      {
        id: '1',
        name: '阿里通义千问',
        provider: 'qwen',
        baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
        apiKey: '',
        modelName: 'qwen-turbo',
        isDefault: true,
        supportsMultimodal: false,
        testPassed: false
      }
    ])

const selectedConfig = ref<ApiConfig | null>(apiConfigs.value[0] || null)

// 提供商默认配置
const providerDefaults: Record<string, { baseUrl: string, modelName: string }> = {
  qwen: { baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', modelName: 'qwen-turbo' },
  zhipu: { baseUrl: 'https://open.bigmodel.cn/api/paas/v4', modelName: 'glm-4' },
  openai: { baseUrl: 'https://api.openai.com/v1', modelName: 'gpt-3.5-turbo' },
  claude: { baseUrl: 'https://api.anthropic.com/v1', modelName: 'claude-3-haiku-20240307' },
  deepseek: { baseUrl: 'https://api.deepseek.com/v1', modelName: 'deepseek-chat' },
  nvidia: { baseUrl: 'https://integrate.api.nvidia.com/v1', modelName: 'qwen/qwen3.5-122b-a10b' },
  custom: { baseUrl: '', modelName: '' }
}

function getProviderName(provider: string): string {
  const keyMap: Record<string, string> = {
    qwen: 'settings.providerQwen',
    zhipu: 'settings.providerZhipu',
    openai: 'settings.providerOpenAI',
    claude: 'settings.providerClaude',
    deepseek: 'settings.providerDeepSeek',
    nvidia: 'settings.providerNvidia',
    custom: 'settings.providerCustom'
  }
  return keyMap[provider] ? t(keyMap[provider]) : provider
}

function selectConfig(index: number) {
  selectedConfigIndex.value = index
  selectedConfig.value = apiConfigs.value[index]
  testStatus.value = null
}

function addApiConfig() {
  const newConfig: ApiConfig = {
    id: Date.now().toString(),
    name: t('settings.unnamedConfig'),
    provider: 'qwen',
    baseUrl: providerDefaults.qwen.baseUrl,
    apiKey: '',
    modelName: providerDefaults.qwen.modelName,
    isDefault: false,
    supportsMultimodal: false,
    testPassed: false
  }
  apiConfigs.value.push(newConfig)
  selectConfig(apiConfigs.value.length - 1)
}

function deleteConfig(index: number) {
  if (apiConfigs.value.length <= 1) {
    alert(t('settings.atLeastOneConfig'))
    return
  }
  apiConfigs.value.splice(index, 1)
  if (selectedConfigIndex.value >= apiConfigs.value.length) {
    selectConfig(apiConfigs.value.length - 1)
  } else {
    selectConfig(selectedConfigIndex.value)
  }
}

function onProviderChange() {
  if (selectedConfig.value) {
    const defaults = providerDefaults[selectedConfig.value.provider]
    if (defaults) {
      selectedConfig.value.baseUrl = defaults.baseUrl
      selectedConfig.value.modelName = defaults.modelName
    }
  }
}

function onDefaultChange() {
  if (selectedConfig.value?.isDefault) {
    // 取消其他配置的默认状态
    apiConfigs.value.forEach((config, index) => {
      if (index !== selectedConfigIndex.value) {
        config.isDefault = false
      }
    })
  }
}

async function testConfig() {
  if (!selectedConfig.value) return
  testStatus.value = { type: 'testing', message: t('settings.testing') }
  try {
    const result = await TestAPIConfig(
      selectedConfig.value.provider,
      selectedConfig.value.baseUrl,
      selectedConfig.value.apiKey,
      selectedConfig.value.modelName
    )
    if (result.success) {
      testStatus.value = { type: 'success', message: result.message }
      selectedConfig.value.testPassed = true
    } else {
      testStatus.value = { type: 'error', message: result.message }
      selectedConfig.value.testPassed = false
    }
  } catch (e) {
    testStatus.value = { type: 'error', message: (e as Error).message }
    selectedConfig.value.testPassed = false
  }
}

// ========== IM 集成配置 ==========
interface ImConfig {
  id: string
  name: string
  provider: 'dingtalk' | 'wecom' | 'feishu'
  enabled: boolean
  clientId?: string
  clientSecret?: string
  robotCode?: string
  mode?: 'stream' | 'webhook'
  apiUrl?: string
  corpId?: string
  corpSecret?: string
  agentId?: string
  appId?: string
  appSecret?: string
}

const selectedImConfigIndex = ref(0)
const imTestStatus = ref<{type: string, message: string} | null>(null)

// IM 配置列表 - 从 props 加载
const imConfigs = ref<ImConfig[]>(props.config.imConfigs?.length > 0
  ? props.config.imConfigs
  : [
      {
        id: '1',
        name: '钉钉机器人',
        provider: 'dingtalk',
        enabled: false,
        clientId: '',
        clientSecret: '',
        robotCode: '',
        mode: 'stream'
      }
    ])

const selectedImConfig = ref<ImConfig | null>(imConfigs.value[0] || null)

function getImProviderName(provider: string): string {
  const keyMap: Record<string, string> = {
    dingtalk: 'settings.providerDingTalk',
    wecom: 'settings.providerWeCom',
    feishu: 'settings.providerFeishu'
  }
  return keyMap[provider] ? t(keyMap[provider]) : provider
}

function selectImConfig(index: number) {
  selectedImConfigIndex.value = index
  selectedImConfig.value = imConfigs.value[index]
  imTestStatus.value = null
}

function addImConfig() {
  const newConfig: ImConfig = {
    id: Date.now().toString(),
    name: t('settings.unnamedConfig'),
    provider: 'dingtalk',
    clientId: '',
    clientSecret: '',
    robotCode: '',
    mode: 'stream',
    enabled: false
  }
  imConfigs.value.push(newConfig)
  selectImConfig(imConfigs.value.length - 1)
}

function deleteImConfig(index: number) {
  if (imConfigs.value.length <= 1) {
    alert(t('settings.atLeastOneConfig'))
    return
  }
  imConfigs.value.splice(index, 1)
  if (selectedImConfigIndex.value >= imConfigs.value.length) {
    selectImConfig(imConfigs.value.length - 1)
  } else {
    selectImConfig(selectedImConfigIndex.value)
  }
}

function testImConnection() {
  imTestStatus.value = { type: 'testing', message: t('settings.testing') }
  setTimeout(() => {
    if (selectedImConfig.value?.clientId && selectedImConfig.value?.clientSecret) {
      imTestStatus.value = { type: 'success', message: t('settings.testSuccess') + '!' }
    } else {
      imTestStatus.value = { type: 'error', message: t('settings.fillCompleteInfo') }
    }
  }, 1500)
}

function save() {
  emit('save', {
    showCodeBlocks: localConfig.value.showCodeBlocks,
    showThinking: localConfig.value.showThinking,
    pmDecisionAlert: localConfig.value.pmDecisionAlert,
    apiConfigs: apiConfigs.value,
    imConfigs: imConfigs.value,
    http: localHttpConfig.value,
    apEnabled: true,
    apConfig: useIndependentModel.value ? apConfig.value : null
  })
}

watch(() => props.config, (newVal) => {
  localConfig.value = {
    showCodeBlocks: newVal.showCodeBlocks ?? true,
    showThinking: newVal.showThinking ?? true,
    pmDecisionAlert: newVal.pmDecisionAlert ?? false
  }
  if (newVal.http) {
    localHttpConfig.value = { ...newVal.http }
  }
  if (newVal.apiConfigs?.length > 0) {
    apiConfigs.value = newVal.apiConfigs
    if (!selectedConfig.value && apiConfigs.value.length > 0) {
      selectedConfig.value = apiConfigs.value[0]
    }
  }
  if (newVal.imConfigs?.length > 0) {
    imConfigs.value = newVal.imConfigs
    if (!selectedImConfig.value && imConfigs.value.length > 0) {
      selectedImConfig.value = imConfigs.value[0]
    }
  }
  apConfig.value = newVal.apConfig ?? null
  if (apConfig.value) {
    apConfigId.value = apConfig.value.id
    useIndependentModel.value = true
  } else {
    apConfigId.value = ''
    useIndependentModel.value = false
  }
}, { deep: true, immediate: true })
</script>

<style scoped>
.settings-overlay {
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

.settings-panel {
  width: 600px;
  max-height: 80vh;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
}

.settings-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-color);
}

.settings-header h3 {
  font-size: 16px;
  font-weight: 600;
}

.close-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 20px;
}

.close-btn:hover {
  color: var(--text-primary);
}

.settings-layout {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}

/* AutoCAD 风格顶部 Tab 栏 */
.settings-tabs {
  display: flex;
  gap: 0;
  background: #f0f0f0;
  padding: 4px 8px 0 8px;
  border-bottom: 1px solid #c0c0c0;
}

.tab-item {
  padding: 6px 16px;
  border: 1px solid transparent;
  border-bottom: none;
  border-radius: 3px 3px 0 0;
  background: transparent;
  color: #555;
  font-size: 13px;
  cursor: pointer;
  position: relative;
  margin-bottom: -1px;
  transition: all 0.15s;
}

.tab-item:hover {
  background: #e0e0e0;
  color: #333;
}

.tab-item.active {
  background: var(--bg-primary, #fff);
  color: #333;
  font-weight: 500;
  border-color: #c0c0c0;
  z-index: 1;
}

.settings-content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.tab-panel {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 20px;
  animation: fadeIn 0.2s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateX(10px); }
  to { opacity: 1; transform: translateX(0); }
}

.settings-section {
  margin-bottom: 24px;
}

.version-section {
  border-top: 1px solid var(--border-color);
  padding-top: 20px;
  margin-top: 20px;
}

.version-info {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 12px;
  background: var(--bg-tertiary);
  border-radius: 6px;
}

.version-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.version-label {
  font-size: 13px;
  color: var(--text-secondary);
}

.version-value {
  font-size: 13px;
  color: var(--text-primary);
  font-family: monospace;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.settings-section h4 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
}

.btn-small {
  padding: 4px 12px;
  background: var(--accent);
  border: none;
  color: #fff;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
}

.btn-small:hover {
  opacity: 0.9;
}

.api-config-list {
  border: 1px solid var(--border-color);
  border-radius: 6px;
  margin-bottom: 16px;
  max-height: 150px;
  overflow-y: auto;
}

.api-config-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  cursor: pointer;
  border-bottom: 1px solid var(--border-color);
}

.api-config-item:last-child {
  border-bottom: none;
}

.api-config-item:hover {
  background: var(--bg-tertiary);
}

.api-config-item.active {
  background: var(--accent);
  color: #fff;
}

.api-config-item.active .config-provider {
  color: rgba(255, 255, 255, 0.7);
}

.api-config-item.enabled {
  border-left: 3px solid var(--success);
}

/* 当前正在使用的配置 */
.api-config-item.current-use {
  border-left: 3px solid #22c55e;
  background: rgba(34, 197, 94, 0.08);
}

.api-config-item.current-use .config-name {
  color: #16a34a;
  font-weight: 600;
}

.config-info {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.config-name {
  font-weight: 500;
}

.config-provider {
  font-size: 12px;
  color: var(--text-secondary);
}

.default-badge {
  font-size: 10px;
  padding: 2px 6px;
  background: var(--success);
  color: #fff;
  border-radius: 10px;
}

.current-badge {
  font-size: 11px;
  padding: 2px 8px;
  background: linear-gradient(135deg, #22c55e, #16a34a);
  color: #fff;
  border-radius: 10px;
  font-weight: 500;
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}

.config-item-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.switch-btn {
  padding: 4px 10px;
  font-size: 11px;
  border: 1px solid var(--accent-color);
  background: transparent;
  color: var(--accent-color);
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s;
}

.switch-btn:hover {
  background: var(--accent-color);
  color: #fff;
}

.delete-btn {
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 16px;
}

.delete-btn:hover {
  color: var(--error);
}

.config-detail {
  padding: 16px;
  background: var(--bg-tertiary);
  border-radius: 6px;
}

.form-group {
  margin-bottom: 12px;
}

.form-group label {
  display: block;
  font-size: 12px;
  margin-bottom: 4px;
  color: var(--text-secondary);
}

.form-group.checkbox label {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  color: var(--text-primary);
}

.form-group.checkbox input {
  width: auto;
}

.input {
  width: 100%;
  padding: 8px 12px;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 13px;
}

.input:focus {
  outline: none;
  border-color: var(--accent);
}

.config-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
}

.test-status {
  font-size: 12px;
}

.test-status.testing {
  color: var(--warning);
}

.test-status.success {
  color: var(--success);
}

.test-status.error {
  color: var(--error);
}

/* IM 配置占位提示 */
.placeholder-msg {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 20px;
  color: var(--text-secondary);
  font-size: 14px;
}

.placeholder-msg .msg-icon {
  font-size: 20px;
}

.settings-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 20px;
  border-top: 1px solid var(--border-color);
}

.btn {
  padding: 8px 16px;
  border: 1px solid var(--border-color);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}

.btn:hover {
  background: var(--border-color);
}

.btn-primary {
  background: var(--accent);
  border-color: var(--accent);
}

.btn-primary:hover {
  opacity: 0.9;
}
</style>
