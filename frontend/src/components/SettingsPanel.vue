<template>
  <div class="settings-overlay" @click.self="$emit('close')">
    <div class="settings-panel" :style="{ left: panelPos.x + 'px', top: panelPos.y + 'px', width: panelWidth + 'px' }">
      <div class="settings-header" @mousedown.prevent="startDrag($event)" title="按住拖动窗口">
        <div class="header-left">
          <span class="drag-hint">⠿</span>
          <h3>{{ t('settings.title') }}</h3>
        </div>
        <button class="close-btn" @click="$emit('close')">×</button>
      </div>
      <div class="panel-resize-handle" @mousedown.prevent="startPanelResize($event)" title="拖拽调整宽度"></div>
      
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
          <!-- 表1：模型配置 -->
          <div class="settings-section">
            <div class="section-header">
              <h4>{{ t('settings.modelConfigs') }}</h4>
              <button class="btn-small" @click="addApiConfig">+ {{ t('settings.addModel') }}</button>
            </div>

            <div class="model-table-wrap">
              <table class="model-table">
                <thead>
                  <tr>
                    <th class="col-provider" data-col="provider">
                      {{ t('settings.provider') }}<span class="col-resize-handle" @mousedown.prevent="startResize($event, 'provider')"></span>
                    </th>
                    <th class="col-url" data-col="url">
                      URL<span class="col-resize-handle" @mousedown.prevent="startResize($event, 'url')"></span>
                    </th>
                    <th class="col-model" data-col="model">
                      {{ t('settings.modelName') }}<span class="col-resize-handle" @mousedown.prevent="startResize($event, 'model')"></span>
                    </th>
                    <th class="col-key" data-col="key">
                      {{ t('settings.apiKey') }}<span class="col-resize-handle" @mousedown.prevent="startResize($event, 'key')"></span>
                    </th>
                    <th class="col-multimodal">{{ t('settings.multimodal') }}</th>
                    <th class="col-default">{{ t('common.default') }}</th>
                    <th class="col-test">{{ t('settings.testConnection') }}</th>
                    <th class="col-del"></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(cfg, idx) in apiConfigs" :key="cfg.id">
                    <td>
                      <select class="table-select" v-model="cfg.provider" @change="onProviderChangeFor(cfg)">
                        <option value="qwen">{{ t('settings.providerQwen') }}</option>
                        <option value="zhipu">{{ t('settings.providerZhipu') }}</option>
                        <option value="openai">{{ t('settings.providerOpenAI') }}</option>
                        <option value="claude">{{ t('settings.providerClaude') }}</option>
                        <option value="deepseek">{{ t('settings.providerDeepSeek') }}</option>
                        <option value="nvidia">{{ t('settings.providerNvidia') }}</option>
                        <option value="custom">{{ t('settings.providerCustom') }}</option>
                      </select>
                    </td>
                    <td><input class="table-input" v-model="cfg.baseUrl" placeholder="https://api.example.com/v1" /></td>
                    <td><input class="table-input" v-model="cfg.modelName" placeholder="model-name" /></td>
                    <td>
                      <div class="key-cell">
                        <input :type="showKeys[idx] ? 'text' : 'password'" class="table-input" v-model="cfg.apiKey" placeholder="sk-..." />
                        <button class="key-toggle" @click="showKeys[idx] = !showKeys[idx]" :title="showKeys[idx] ? '隐藏' : '显示'">👁</button>
                      </div>
                    </td>
                    <td class="td-center"><input type="checkbox" v-model="cfg.supportsMultimodal" /></td>
                    <td class="td-center"><input type="radio" name="defaultModel" :value="cfg.id" v-model="defaultModelId" /></td>
                    <td class="td-center">
                      <button class="btn-test" @click="testTableConfig(idx)" :disabled="testLoading[idx]">
                        <span v-if="testLoading[idx]">⏳</span>
                        <span v-else-if="testResults[idx] === true">✅</span>
                        <span v-else-if="testResults[idx] === false">❌</span>
                        <span v-else>🧪</span>
                      </button>
                    </td>
                    <td class="td-center">
                      <button class="delete-btn" @click="deleteConfig(idx)" :disabled="apiConfigs.length <= 1">🗑</button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- 表2：角色模型绑定 -->
          <div class="settings-section">
            <div class="section-header">
              <h4>{{ t('settings.roleModelBinding') }}</h4>
            </div>

            <div class="form-group checkbox" style="margin-bottom: 12px;">
              <label>
                <input type="checkbox" v-model="useSeparateModels" />
                {{ t('settings.useSeparateModels') }}
              </label>
            </div>

            <!-- 不勾选：统一模型 -->
            <div v-if="!useSeparateModels" class="role-single-row">
              <span class="role-label">{{ t('settings.allRoles') }}</span>
              <select class="input role-select" v-model="sharedModelId">
                <option v-for="cfg in apiConfigs" :key="cfg.id" :value="cfg.id">
                  {{ cfg.modelName || cfg.baseUrl }} ({{ truncateUrl(cfg.baseUrl) }})
                </option>
              </select>
            </div>

            <!-- 勾选：三行独立 -->
            <table v-else class="model-table role-table">
              <thead>
                <tr>
                  <th class="col-role">{{ t('settings.role') }}</th>
                  <th class="col-model-select">{{ t('settings.model') }}</th>
                  <th class="col-note">{{ t('settings.note') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>🧠 PM</td>
                  <td>
                    <select class="input role-select" v-model="pmConfigId">
                      <option v-for="cfg in apiConfigs" :key="cfg.id" :value="cfg.id">
                        {{ cfg.modelName || cfg.baseUrl }} ({{ truncateUrl(cfg.baseUrl) }})
                      </option>
                    </select>
                  </td>
                  <td class="role-note">{{ t('settings.pmNote') }}</td>
                </tr>
                <tr>
                  <td>🔧 SE</td>
                  <td>
                    <select class="input role-select" v-model="seConfigId">
                      <option v-for="cfg in apiConfigs" :key="cfg.id" :value="cfg.id">
                        {{ cfg.modelName || cfg.baseUrl }} ({{ truncateUrl(cfg.baseUrl) }})
                      </option>
                    </select>
                  </td>
                  <td class="role-note">{{ t('settings.seNote') }}</td>
                </tr>
                <tr>
                  <td>🛡️ AP</td>
                  <td>
                    <select class="input role-select" v-model="apConfigId">
                      <option v-for="cfg in apiConfigs" :key="cfg.id" :value="cfg.id">
                        {{ cfg.modelName || cfg.baseUrl }} ({{ truncateUrl(cfg.baseUrl) }})
                      </option>
                    </select>
                  </td>
                  <td class="role-note">{{ t('settings.apNote') }}</td>
                </tr>
              </tbody>
            </table>
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
import { ref, watch, onMounted, nextTick } from 'vue'
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
const buildTime: string = (typeof __BUILD_TIME__ !== 'undefined') ? __BUILD_TIME__ : new Date().toLocaleString('zh-CN')
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

// ========== 表1：模型配置 ==========
const showKeys = ref<Record<number, boolean>>({})
const testLoading = ref<Record<number, boolean>>({})
const testResults = ref<Record<number, boolean | null>>({})
const defaultModelId = ref<string>('')

// ========== 表2：角色模型绑定 ==========
const useSeparateModels = ref(false)
const sharedModelId = ref<string>('')
const pmConfigId = ref<string>('')
const seConfigId = ref<string>('')
const apConfigId = ref<string>('')

// ========== 列宽拖拽 ==========
const COL_WIDTHS_KEY = 'argus-col-widths'
const defaultColWidths: Record<string, string> = {
  provider: '12%', url: '22%', model: '15%', key: '18%',
}
const colWidths = ref<Record<string, string>>(
  JSON.parse(localStorage.getItem(COL_WIDTHS_KEY) || '{}') || { ...defaultColWidths }
)
let resizingCol: string | null = null
let resizeStartX = 0
let resizeStartWidth = 0

function startResize(e: MouseEvent, col: string) {
  resizingCol = col
  resizeStartX = e.clientX
  const th = (e.target as HTMLElement).closest('th') as HTMLElement
  resizeStartWidth = th.offsetWidth
  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

function onResizeMove(e: MouseEvent) {
  if (!resizingCol) return
  const diff = e.clientX - resizeStartX
  const tableWrap = document.querySelector('.model-table-wrap') as HTMLElement
  if (!tableWrap) return
  const tableWidth = tableWrap.querySelector('.model-table')!.clientWidth
  const newPx = Math.max(60, resizeStartWidth + diff)
  const newPct = ((newPx / tableWidth) * 100).toFixed(1) + '%'
  colWidths.value[resizingCol] = newPct
  // 实时应用
  applyColWidths()
}

function onResizeEnd() {
  if (resizingCol) {
    localStorage.setItem(COL_WIDTHS_KEY, JSON.stringify(colWidths.value))
  }
  resizingCol = null
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

function applyColWidths() {
  for (const [col, w] of Object.entries(colWidths.value)) {
    const th = document.querySelector(`.model-table th[data-col="${col}"]`) as HTMLElement
    if (th) th.style.width = w
  }
}

// 组件挂载时恢复列宽和面板位置
onMounted(() => {
  nextTick(() => applyColWidths())
  // 首次打开时居中
  if (panelPos.value.x === 0 && panelPos.value.y === 0) {
    nextTick(() => {
      const panel = document.querySelector('.settings-panel') as HTMLElement
      if (panel) {
        const x = Math.round((window.innerWidth - panel.offsetWidth) / 2)
        const y = Math.round((window.innerHeight - panel.offsetHeight) / 2)
        panelPos.value = { x: Math.max(0, x), y: Math.max(0, y) }
      }
    })
  }
})

// ========== 面板拖拽 ==========
const DRAG_POS_KEY = 'argus-settings-pos'
const PANEL_WIDTH_KEY = 'argus-settings-width'
const panelPos = ref<{ x: number; y: number }>(
  JSON.parse(localStorage.getItem(DRAG_POS_KEY) || 'null') || { x: 0, y: 0 }
)
const panelWidth = ref<number>(
  parseInt(localStorage.getItem(PANEL_WIDTH_KEY) || '920', 10)
)
let dragging = false
let dragOffsetX = 0
let dragOffsetY = 0

// ========== 面板宽度拖拽 ==========
let panelResizing = false
let panelResizeStartX = 0
let panelResizeStartWidth = 0

function startPanelResize(e: MouseEvent) {
  panelResizing = true
  panelResizeStartX = e.clientX
  panelResizeStartWidth = panelWidth.value
  document.addEventListener('mousemove', onPanelResizeMove)
  document.addEventListener('mouseup', onPanelResizeEnd)
  document.body.style.cursor = 'ew-resize'
  document.body.style.userSelect = 'none'
  e.preventDefault()
}

function onPanelResizeMove(e: MouseEvent) {
  if (!panelResizing) return
  const diff = e.clientX - panelResizeStartX
  const newWidth = Math.max(600, Math.min(window.innerWidth - 40, panelResizeStartWidth + diff))
  panelWidth.value = newWidth
}

function onPanelResizeEnd() {
  if (panelResizing) {
    localStorage.setItem(PANEL_WIDTH_KEY, String(panelWidth.value))
  }
  panelResizing = false
  document.removeEventListener('mousemove', onPanelResizeMove)
  document.removeEventListener('mouseup', onPanelResizeEnd)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

function startDrag(e: MouseEvent) {
  const panel = (e.currentTarget as HTMLElement).closest('.settings-panel') as HTMLElement
  if (!panel) return
  dragging = true
  dragOffsetX = e.clientX - panel.offsetLeft
  dragOffsetY = e.clientY - panel.offsetTop
  document.addEventListener('mousemove', onDragMove)
  document.addEventListener('mouseup', onDragEnd)
}

function onDragMove(e: MouseEvent) {
  if (!dragging) return
  const panel = document.querySelector('.settings-panel') as HTMLElement
  if (!panel) return
  let nx = e.clientX - dragOffsetX
  let ny = e.clientY - dragOffsetY
  // 边界限制：不超出窗口
  const maxW = window.innerWidth - panel.offsetWidth
  const maxH = window.innerHeight - panel.offsetHeight
  nx = Math.max(0, Math.min(nx, maxW))
  ny = Math.max(0, Math.min(ny, maxH))
  panelPos.value = { x: nx, y: ny }
  panel.style.left = nx + 'px'
  panel.style.top = ny + 'px'
}

function onDragEnd() {
  if (dragging) {
    localStorage.setItem(DRAG_POS_KEY, JSON.stringify(panelPos.value))
  }
  dragging = false
  document.removeEventListener('mousemove', onDragMove)
  document.removeEventListener('mouseup', onDragEnd)
}

function truncateUrl(url: string): string {
  if (!url) return ''
  const u = url.replace(/^https?:\/\//, '')
  return u.length > 25 ? u.substring(0, 25) + '...' : u
}

// 提供商默认 URL 和模型名
const providerDefaults: Record<string, { baseUrl: string; modelName: string }> = {
  qwen: { baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', modelName: 'qwen-turbo' },
  zhipu: { baseUrl: 'https://open.bigmodel.cn/api/paas/v4', modelName: 'glm-4' },
  openai: { baseUrl: 'https://api.openai.com/v1', modelName: 'gpt-3.5-turbo' },
  claude: { baseUrl: 'https://api.anthropic.com/v1', modelName: 'claude-3-haiku-20240307' },
  deepseek: { baseUrl: 'https://api.deepseek.com/v1', modelName: 'deepseek-chat' },
  nvidia: { baseUrl: 'https://integrate.api.nvidia.com/v1', modelName: 'qwen/qwen3.5-122b-a10b' },
  custom: { baseUrl: '', modelName: '' }
}

function onProviderChangeFor(cfg: ApiConfig) {
  const defaults = providerDefaults[cfg.provider || 'custom']
  if (defaults) {
    cfg.baseUrl = defaults.baseUrl
    cfg.modelName = defaults.modelName
  }
}

// API 配置列表 - 从 props 加载
const apiConfigs = ref<ApiConfig[]>(props.config.apiConfigs?.length > 0 
  ? props.config.apiConfigs 
  : [
      {
        id: '1',
        name: '默认配置',
        provider: 'custom',
        baseUrl: 'https://api.openai.com/v1',
        apiKey: '',
        modelName: 'gpt-4o',
        isDefault: true,
        supportsMultimodal: false,
        testPassed: false
      }
    ])

function initFromConfig() {
  // 初始化默认模型ID
  const defCfg = apiConfigs.value.find(c => c.isDefault)
  if (defCfg) {
    defaultModelId.value = defCfg.id
  } else if (apiConfigs.value.length > 0) {
    defaultModelId.value = apiConfigs.value[0].id
  }

  // 初始化角色绑定
  useSeparateModels.value = props.config.useSeparateModels ?? false
  const defId = defaultModelId.value

  if (useSeparateModels.value) {
    pmConfigId.value = props.config.pmConfigId || defId
    seConfigId.value = props.config.seConfigId || defId
    apConfigId.value = props.config.apConfigId || defId
  } else {
    sharedModelId.value = props.config.pmConfigId || defId
    pmConfigId.value = sharedModelId.value
    seConfigId.value = sharedModelId.value
    apConfigId.value = sharedModelId.value
  }
}

// 调用初始化
initFromConfig()

function addApiConfig() {
  const newConfig: ApiConfig = {
    id: Date.now().toString(),
    name: '新模型',
    provider: 'custom',
    baseUrl: '',
    apiKey: '',
    modelName: '',
    isDefault: false,
    supportsMultimodal: false,
    testPassed: false
  }
  apiConfigs.value.push(newConfig)
}

function deleteConfig(index: number) {
  if (apiConfigs.value.length <= 1) return
  const removedId = apiConfigs.value[index].id
  apiConfigs.value.splice(index, 1)
  // 如果删除的是默认模型，选第一个为默认
  if (defaultModelId.value === removedId) {
    defaultModelId.value = apiConfigs.value[0]?.id || ''
  }
}

async function testTableConfig(index: number) {
  const cfg = apiConfigs.value[index]
  if (!cfg) return
  testLoading.value[index] = true
  testResults.value[index] = null
  try {
    const result = await TestAPIConfig(cfg.provider || 'custom', cfg.baseUrl, cfg.apiKey, cfg.modelName)
    if (result.success) {
      testResults.value[index] = true
      cfg.testPassed = true
    } else {
      testResults.value[index] = false
      cfg.testPassed = false
    }
  } catch (e) {
    testResults.value[index] = false
    cfg.testPassed = false
  } finally {
    testLoading.value[index] = false
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
  // [FIX] 共享模式下，sharedModelId 同步到 defaultModelId（否则 isDefault 不更新，重启后用错模型）
  if (!useSeparateModels.value && sharedModelId.value) {
    defaultModelId.value = sharedModelId.value
  }

  // 同步默认模型到 apiConfigs
  apiConfigs.value.forEach(cfg => {
    cfg.isDefault = (cfg.id === defaultModelId.value)
  })

  // 角色模型ID：根据 useSeparateModels 决定
  let pmId: string, seId: string, apId: string
  if (useSeparateModels.value) {
    pmId = pmConfigId.value
    seId = seConfigId.value
    apId = apConfigId.value
  } else {
    pmId = sharedModelId.value
    seId = sharedModelId.value
    apId = sharedModelId.value
  }

  emit('save', {
    showCodeBlocks: localConfig.value.showCodeBlocks,
    showThinking: localConfig.value.showThinking,
    pmDecisionAlert: localConfig.value.pmDecisionAlert,
    apiConfigs: apiConfigs.value,
    imConfigs: imConfigs.value,
    http: localHttpConfig.value,
    apEnabled: true,
    useSeparateModels: useSeparateModels.value,
    pmConfigId: pmId,
    seConfigId: seId,
    apConfigId: apId
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
    initFromConfig()
  }
  if (newVal.imConfigs?.length > 0) {
    imConfigs.value = newVal.imConfigs
    if (!selectedImConfig.value && imConfigs.value.length > 0) {
      selectedImConfig.value = imConfigs.value[0]
    }
  }
  // 角色模型绑定
  useSeparateModels.value = newVal.useSeparateModels ?? false
  if (newVal.pmConfigId) pmConfigId.value = newVal.pmConfigId
  if (newVal.seConfigId) seConfigId.value = newVal.seConfigId
  if (newVal.apConfigId) apConfigId.value = newVal.apConfigId
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
  z-index: 1000;
}

.settings-panel {
  width: 920px;
  max-height: 80vh;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  position: absolute;
}

/* 面板右侧宽度拖拽手柄 */
.panel-resize-handle {
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  width: 8px;
  cursor: ew-resize;
  background: transparent;
  z-index: 10;
}

.panel-resize-handle::before {
  content: '';
  position: absolute;
  left: 3px;
  top: 20px;
  bottom: 20px;
  width: 2px;
  background: rgba(255, 255, 255, 0.25);
  border-radius: 1px;
  transition: background 0.2s;
}

.panel-resize-handle:hover::before {
  background: rgba(100, 180, 255, 0.6);
}

.settings-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-color);
  cursor: grab;
  user-select: none;
}

.settings-header h3 {
  font-size: 16px;
  font-weight: 600;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.drag-hint {
  font-size: 14px;
  color: var(--text-tertiary);
  opacity: 0.6;
  letter-spacing: -1px;
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

/* ========== 模型配置表 ========== */
.model-table-wrap {
  overflow-x: auto;
  margin-bottom: 8px;
}

.model-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  table-layout: fixed;
}

.model-table th {
  padding: 8px 6px;
  text-align: left;
  font-weight: 600;
  color: var(--text-secondary);
  border-bottom: 2px solid var(--border-color);
  white-space: nowrap;
  background: var(--bg-tertiary);
}

.model-table td {
  padding: 4px 6px;
  border-bottom: 1px solid var(--border-color);
  vertical-align: middle;
}

.model-table .td-center {
  text-align: center;
}

.col-provider { width: 12%; position: relative; }
.col-url { width: 22%; position: relative; }
.col-model { width: 15%; position: relative; }
.col-key { width: 18%; position: relative; }

/* 列宽拖拽手柄 — 明显可见 */
.col-resize-handle {
  position: absolute;
  right: -3px;
  top: 0;
  bottom: 0;
  width: 8px;
  cursor: col-resize;
  background: transparent;
  z-index: 2;
}
.col-resize-handle::before {
  content: '';
  position: absolute;
  right: 3px;
  top: 6px;
  bottom: 6px;
  width: 2px;
  background: rgba(255,255,255,0.25);
  border-radius: 1px;
  transition: all 0.15s;
}
.col-resize-handle:hover::before {
  background: var(--accent);
  width: 3px;
  box-shadow: 0 0 6px rgba(64,158,255,0.5);
}
.col-multimodal { width: 6%; }
.col-default { width: 6%; }
.col-test { width: 7%; }
.col-del { width: 4%; }

.table-select {
  width: 100%;
  padding: 4px 4px;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 3px;
  color: var(--text-primary);
  font-size: 11px;
  box-sizing: border-box;
}

.col-role { width: 15%; }
.col-model-select { width: 55%; }
.col-note { width: 30%; }

.table-input {
  width: 100%;
  padding: 5px 6px;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 3px;
  color: var(--text-primary);
  font-size: 12px;
  box-sizing: border-box;
}

.table-input:focus {
  outline: none;
  border-color: var(--accent);
}

.key-cell {
  display: flex;
  align-items: center;
  gap: 2px;
}

.key-cell .table-input {
  flex: 1;
}

.key-toggle {
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  cursor: pointer;
  font-size: 14px;
  padding: 0;
  flex-shrink: 0;
}

.btn-test {
  padding: 2px 8px;
  border: 1px solid var(--border-color);
  background: var(--bg-primary);
  border-radius: 3px;
  cursor: pointer;
  font-size: 14px;
}

.btn-test:hover {
  background: var(--border-color);
}

.delete-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

/* ========== 角色模型绑定 ========== */
.role-single-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0;
}

.role-label {
  font-weight: 500;
  font-size: 13px;
  white-space: nowrap;
  min-width: 80px;
}

.role-select {
  flex: 1;
}

.role-note {
  font-size: 11px;
  color: var(--text-secondary);
  font-style: italic;
}

.role-table th {
  text-align: left;
}
</style>
