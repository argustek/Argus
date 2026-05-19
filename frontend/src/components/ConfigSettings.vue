<template>
  <div class="config-settings">
    <!-- 决策配置 Tab -->
    <div v-if="currentSubTab === 'decision'" class="tab-content">
      <div class="settings-section">
        <div class="section-header">
          <h4>🤖 {{ t('settings.decisionConfigTitle') }}</h4>
          <p class="section-desc">{{ t('settings.decisionConfigDesc') }}</p>
        </div>

        <!-- 决策规则列表 -->
        <div class="rules-list">
          <!-- 文件操作组 -->
          <div class="rule-group">
            <div class="group-header">
              <span class="group-icon">📁</span>
              <span class="group-title">{{ t('settings.fileOperations') }}</span>
            </div>
            
            <div 
              v-for="rule in fileRules" 
              :key="rule.type"
              class="rule-item"
              :class="{ 'is-auto': rule.mode === 'auto' }"
            >
              <div class="rule-info">
                <span class="rule-desc">{{ rule.description }}</span>
                <span v-if="rule.mode !== rule.default_mode" class="changed-badge">{{ t('common.modified') }}</span>
              </div>
              
              <div class="rule-control">
                <label class="switch">
                  <input 
                    type="checkbox" 
                    :checked="rule.mode === 'auto'"
                    @change="toggleDecisionRule(rule.type)"
                  />
                  <span class="slider"></span>
                </label>
                <span class="mode-label">{{ rule.mode === 'auto' ? t('common.auto') : t('common.manual') }}</span>
              </div>
            </div>
          </div>

          <!-- 命令执行组 -->
          <div class="rule-group">
            <div class="group-header">
              <span class="group-icon">⚡</span>
              <span class="group-title">{{ t('settings.commandExec') }}</span>
            </div>
            
            <div 
              v-for="rule in cmdRules" 
              :key="rule.type"
              class="rule-item"
              :class="{ 'is-auto': rule.mode === 'auto' }"
            >
              <div class="rule-info">
                <span class="rule-desc">{{ rule.description }}</span>
                <span v-if="rule.mode !== rule.default_mode" class="changed-badge">{{ t('common.modified') }}</span>
              </div>
              
              <div class="rule-control">
                <label class="switch">
                  <input 
                    type="checkbox" 
                    :checked="rule.mode === 'auto'"
                    @change="toggleDecisionRule(rule.type)"
                  />
                  <span class="slider"></span>
                </label>
                <span class="mode-label">{{ rule.mode === 'auto' ? t('common.auto') : t('common.manual') }}</span>
              </div>
            </div>
          </div>

          <!-- Git 操作组 -->
          <div class="rule-group">
            <div class="group-header">
              <span class="group-icon">📦</span>
              <span class="group-title">Git {{ t('settings.title') }}</span>
            </div>
            
            <div 
              v-for="rule in gitRules" 
              :key="rule.type"
              class="rule-item"
              :class="{ 'is-auto': rule.mode === 'auto' }"
            >
              <div class="rule-info">
                <span class="rule-desc">{{ rule.description }}</span>
                <span v-if="rule.mode !== rule.default_mode" class="changed-badge">{{ t('common.modified') }}</span>
              </div>
              
              <div class="rule-control">
                <label class="switch">
                  <input 
                    type="checkbox" 
                    :checked="rule.mode === 'auto'"
                    @change="toggleDecisionRule(rule.type)"
                  />
                  <span class="slider"></span>
                </label>
                <span class="mode-label">{{ rule.mode === 'auto' ? t('common.auto') : t('common.manual') }}</span>
              </div>
            </div>
          </div>

          <!-- 系统操作组 -->
          <div class="rule-group">
            <div class="group-header">
              <span class="group-icon">⚙️</span>
              <span class="group-title">{{ t('settings.systemOps') }}</span>
            </div>
            
            <div 
              v-for="rule in systemRules" 
              :key="rule.type"
              class="rule-item"
              :class="{ 'is-auto': rule.mode === 'auto' }"
            >
              <div class="rule-info">
                <span class="rule-desc">{{ rule.description }}</span>
                <span v-if="rule.mode !== rule.default_mode" class="changed-badge">{{ t('common.modified') }}</span>
              </div>
              
              <div class="rule-control">
                <label class="switch">
                  <input 
                    type="checkbox" 
                    :checked="rule.mode === 'auto'"
                    @change="toggleDecisionRule(rule.type)"
                  />
                  <span class="slider"></span>
                </label>
                <span class="mode-label">{{ rule.mode === 'auto' ? t('common.auto') : t('common.manual') }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 操作按钮 -->
        <div class="actions">
          <button class="btn btn-secondary" @click="resetToDefault">
            🔄 {{ t('settings.resetDefault') }}
          </button>
        </div>
      </div>
    </div>

    <!-- 权限配置 Tab -->
    <div v-if="currentSubTab === 'permission'" class="tab-content">
      <div class="settings-section">
        <div class="section-header">
          <h4>🔒 {{ t('settings.permissionConfigTitle') }}</h4>
          <p class="section-desc">{{ t('settings.permissionConfigDesc') }}</p>
        </div>

        <!-- 默认权限显示 -->
        <div class="default-perm-info">
          <span class="info-label">{{ t('settings.defaultPermission') }}</span>
          <span :class="['perm-badge', permissionConfig?.default_permission]">
            {{ getPermLabel(permissionConfig?.default_permission) }}
          </span>
          <span class="info-hint">(used for paths not matching any rule)</span>
        </div>

        <!-- 规则列表 -->
        <div class="rules-table-wrapper">
          <table class="rules-table" v-if="permissionConfig && permissionConfig.rules.length > 0">
            <thead>
              <tr>
                <th>{{ t('settings.permPathPattern') }}</th>
                <th>{{ t('settings.permLevel') }}</th>
                <th>{{ t('common.description') }}</th>
                <th>{{ t('settings.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr 
                v-for="(rule, index) in permissionConfig.rules" 
                :key="index"
                :class="{ 'protected': rule.permission === 'protected' }"
              >
                <td><code>{{ rule.path_pattern }}</code></td>
                <td>
      <select
        v-model="rule.permission"
        @change="updatePermissionRule(index, rule.permission)"
        class="perm-select"
      >
        <option value="full">{{ t('settings.permFull') }}</option>
        <option value="readwrite">{{ t('settings.permReadWrite') }}</option>
        <option value="readonly">{{ t('settings.permReadOnly') }}</option>
        <option value="none">{{ t('settings.permDeny') }}</option>
        <option value="protected">{{ t('settings.permProtected') }}</option>
      </select>
    </td>
    <td>{{ rule.description }}</td>
    <td>
      <button 
        class="btn-danger btn-small"
        @click="removePermissionRule(rule.path_pattern)"
        :disabled="rule.path_pattern === '.git/**' || rule.path_pattern === '.argus/**'"
      >
        {{ t('common.delete') }}
      </button>
                </td>
              </tr>
            </tbody>
          </table>
          
          <div v-else class="empty-rules">
            <p>{{ t('settings.noCustomRules') }}</p>
          </div>
        </div>

        <!-- 添加新规则 -->
        <div class="add-rule-form">
          <h5>➕ {{ t('settings.addRule') }}</h5>
          <div class="form-row">
            <input 
              type="text" 
              v-model="newRule.pattern" 
              :placeholder="t('settings.pathPatternPlaceholder')"
              class="input flex-1"
            />
            <select v-model="newRule.permission" class="input">
              <option value="">{{ t('settings.selectPermission') }}</option>
              <option value="full">{{ t('settings.permFull') }}</option>
              <option value="readwrite">{{ t('settings.permReadWrite') }}</option>
              <option value="readonly">{{ t('settings.permReadOnly') }}</option>
              <option value="none">{{ t('settings.permDeny') }}</option>
              <option value="protected">{{ t('settings.permProtected') }}</option>
            </select>
          </div>
          <div class="form-row">
            <input 
              type="text" 
              v-model="newRule.description" 
              :placeholder="t('common.description')"
              class="input flex-1"
            />
            <button 
              class="btn btn-primary btn-small"
              @click="addNewRule"
              :disabled="!newRule.pattern || !newRule.permission"
            >
              {{ t('common.add') }}
            </button>
          </div>
        </div>

        <!-- 操作按钮 -->
        <div class="actions">
          <button class="btn btn-secondary" @click="resetPermissionToDefault">
            🔄 {{ t('settings.resetDefault') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { 
  GetDecisionConfig, 
  UpdateDecisionRule, 
  ResetDecisionToDefault,
  GetPermissionConfig,
  AddPermissionRule,
  RemovePermissionRule,
  ResetPermissionToDefault as ResetPermDefault
} from '../../wailsjs/go/main/App'

interface DecisionRule {
  type: string
  mode: string
  default_mode: string
  description: string
  category: string
}

interface PathRule {
  path_pattern: string
  permission: string
  description: string
  is_directory: boolean
  priority: number
}

interface PermissionConfig {
  version: number
  default_permission: string
  rules: PathRule[]
  updated_at: string
}

const props = defineProps<{
  currentSubTab: string
}>()

const { t } = useI18n()

// 决策配置数据
const decisionConfig = ref<{ rules: DecisionRule[] } | null>(null)
const saveStatus = ref<{type: string, message: string} | null>(null)

// 权限配置数据
const permissionConfig = ref<PermissionConfig | null>(null)
const permSaveStatus = ref<{type: string, message: string} | null>(null)

// 新规则表单
const newRule = ref({
  pattern: '',
  permission: '',
  description: ''
})

// 计算属性：按分类分组
const fileRules = computed(() => 
  decisionConfig.value?.rules.filter(r => r.category === 'file') || []
)

const cmdRules = computed(() => 
  decisionConfig.value?.rules.filter(r => r.category === 'cmd') || []
)

const gitRules = computed(() => 
  decisionConfig.value?.rules.filter(r => r.category === 'git') || []
)

const systemRules = computed(() => 
  decisionConfig.value?.rules.filter(r => r.category === 'system') || []
)

// 加载决策配置
async function loadDecisionConfig() {
  try {
    const config = await GetDecisionConfig()
    if (config) {
      decisionConfig.value = config as any
    }
  } catch (e) {
    console.error('加载决策配置失败:', e)
  }
}

// 加载权限配置
async function loadPermissionConfig() {
  try {
    const config = await GetPermissionConfig()
    if (config) {
      permissionConfig.value = config as any
    }
  } catch (e) {
    console.error('加载权限配置失败:', e)
  }
}

// 切换决策规则
async function toggleDecisionRule(type: string) {
  if (!decisionConfig.value) return
  
  const rule = decisionConfig.value.rules.find(r => r.type === type)
  if (!rule) return
  
  const newMode = rule.mode === 'auto' ? 'manual' : 'auto'
  
  try {
    await UpdateDecisionRule(type, newMode)
    rule.mode = newMode
    
    showSaveStatus('success', t('settings.updateSuccess', { detail: `${rule.description} → ${newMode === 'auto' ? t('common.auto') : t('common.manual')}` }))
    
    setTimeout(() => loadDecisionConfig(), 500)
  } catch (e) {
    showSaveStatus('error', t('settings.updateFailed', { msg: (e as Error).message }))
  }
}

// 重置决策配置为缺省值
async function resetToDefault() {
  if (!confirm(t('settings.confirmResetDecision'))) return
  
  try {
    await ResetDecisionToDefault()
    await loadDecisionConfig()
    showSaveStatus('success', t('settings.resetSuccess'))
  } catch (e) {
    showSaveStatus('error', t('settings.resetFailed', { msg: (e as Error).message }))
  }
}

// 更新权限规则
async function updatePermissionRule(index: number, newPermission: string) {
  if (!permissionConfig.value) return
  
  const rule = permissionConfig.value.rules[index]
  
  try {
    // 先删除旧规则，再添加新规则（因为后端 API 设计如此）
    await RemovePermissionRule(rule.path_pattern)
    await AddPermissionRule(
      rule.path_pattern, 
      newPermission, 
      rule.description, 
      rule.is_directory, 
      rule.priority
    )
    
    showPermSaveStatus('success', t('settings.updateSuccess', { detail: `${rule.path_pattern} → ${getPermLabel(newPermission)}` }))
  } catch (e) {
    showPermSaveStatus('error', t('settings.updateFailed', { msg: (e as Error).message }))
    await loadPermissionConfig()
  }
}

// 添加新权限规则
async function addNewRule() {
  if (!newRule.value.pattern || !newRule.value.permission) {
    alert(t('settings.fillCompleteInfo'))
    return
  }
  
  try {
    await AddPermissionRule(
      newRule.value.pattern,
      newRule.value.permission,
      newRule.value.description || t('settings.userDefined', { desc: newRule.value.pattern }),
      newRule.value.pattern.endsWith('/**') || newRule.value.pattern.endsWith('/'),
      10
    )
    
    newRule.value = { pattern: '', permission: '', description: '' }
    
    showPermSaveStatus('success', t('settings.addSuccess'))
    await loadPermissionConfig()
  } catch (e) {
    showPermSaveStatus('error', t('settings.addFailed', { msg: (e as Error).message }))
  }
}

// 删除权限规则
async function removePermissionRule(pattern: string) {
  if (!confirm(t('settings.confirmDeleteRule', { pattern }))) return
  
  try {
    await RemovePermissionRule(pattern)
    showPermSaveStatus('success', t('settings.deleteSuccess', { detail: pattern }))
    await loadPermissionConfig()
  } catch (e) {
    showPermSaveStatus('error', t('settings.deleteFailed', { msg: (e as Error).message }))
  }
}

// 重置权限配置为缺省值
async function resetPermissionToDefault() {
  if (!confirm(t('settings.confirmResetPermission'))) return
  
  try {
    await ResetPermDefault()
    await loadPermissionConfig()
    showPermSaveStatus('success', t('settings.resetSuccess'))
  } catch (e) {
    showPermSaveStatus('error', t('settings.resetFailed', { msg: (e as Error).message }))
  }
}

// 获取权限级别标签
function getPermLabel(perm: string): string {
  const labels: Record<string, string> = {
    full: t('settings.permFull'),
    readwrite: t('settings.permReadWrite'),
    readonly: t('settings.permReadOnly'),
    none: t('settings.permDeny'),
    protected: t('settings.permProtected')
  }
  return labels[perm || ''] || perm || ''
}


// 显示状态消息
function showSaveStatus(type: string, message: string) {
  saveStatus.value = { type, message }
  setTimeout(() => { saveStatus.value = null }, 3000)
}

function showPermSaveStatus(type: string, message: string) {
  permSaveStatus.value = { type, message }
  setTimeout(() => { permSaveStatus.value = null }, 3000)
}

// 组件挂载时加载数据
onMounted(() => {
  loadDecisionConfig()
  loadPermissionConfig()
})
</script>

<style scoped>
.config-settings {
}

.tab-content {
}

.settings-section {
  margin-bottom: 20px;
}

.section-header {
  margin-bottom: 20px;
}

.section-header h4 {
  font-size: 18px;
  font-weight: 600;
  color: #fff;
  margin-bottom: 5px;
}

.section-desc {
  font-size: 13px;
  color: #888;
}

/* 规则列表 */
.rules-list {
  display: flex;
  flex-direction: column;
  gap: 15px;
}

.rule-group {
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 8px;
  padding: 15px;
}

.group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.group-icon {
  font-size: 16px;
}

.group-title {
  font-weight: 600;
  color: #ccc;
  font-size: 14px;
}

.rule-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  background: rgba(0, 0, 0, 0.2);
  border-radius: 6px;
  margin-bottom: 8px;
  transition: all 0.2s ease;
}

.rule-item:last-child {
  margin-bottom: 0;
}

.rule-item:hover {
  background: rgba(255, 255, 255, 0.05);
}

.rule-item.is-auto {
  border-left: 3px solid #4CAF50;
}

.rule-item:not(.is-auto) {
  border-left: 3px solid #FF9800;
}

.rule-info {
  display: flex;
  align-items: center;
  gap: 10px;
}

.rule-desc {
  color: #ddd;
  font-size: 14px;
}

.changed-badge {
  background: #FF9800;
  color: #000;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}

/* 开关样式 */
.rule-control {
  display: flex;
  align-items: center;
  gap: 8px;
}

.switch {
  position: relative;
  display: inline-block;
  width: 44px;
  height: 24px;
}

.switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.slider {
  position: absolute;
  cursor: pointer;
  top: 0; left: 0; right: 0; bottom: 0;
  background-color: #555;
  transition: .3s;
  border-radius: 24px;
}

.slider:before {
  position: absolute;
  content: "";
  height: 18px;
  width: 18px;
  left: 3px;
  bottom: 3px;
  background-color: white;
  transition: .3s;
  border-radius: 50%;
}

input:checked + .slider {
  background-color: #4CAF50;
}

input:checked + .slider:before {
  transform: translateX(20px);
}

.mode-label {
  font-size: 13px;
  color: #aaa;
  min-width: 35px;
}

/* 默认权限信息 */
.default-perm-info {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 15px;
  background: rgba(76, 175, 80, 0.1);
  border: 1px solid rgba(76, 175, 80, 0.3);
  border-radius: 6px;
  margin-bottom: 20px;
}

.info-label {
  color: #ccc;
  font-size: 14px;
}

.perm-badge {
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 600;
}

.perm-badge.full {
  background: #4CAF50;
  color: #fff;
}

.perm-badge.readwrite {
  background: #2196F3;
  color: #fff;
}

.perm-badge.readonly {
  background: #FF9800;
  color: #000;
}

.perm-badge.none {
  background: #f44336;
  color: #fff;
}

.perm-badge.protected {
  background: #9C27B0;
  color: #fff;
}

.info-hint {
  color: #888;
  font-size: 12px;
}

/* 规则表格 */
.rules-table-wrapper {
  margin-bottom: 20px;
}

.rules-table {
  width: 100%;
  border-collapse: collapse;
  background: rgba(0, 0, 0, 0.2);
  border-radius: 8px;
  overflow: hidden;
}

.rules-table th {
  background: rgba(255, 255, 255, 0.05);
  color: #aaa;
  font-size: 13px;
  font-weight: 600;
  text-align: left;
  padding: 12px 15px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.rules-table td {
  padding: 12px 15px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  color: #ddd;
  font-size: 14px;
}

.rules-table tr.protected {
  background: rgba(156, 39, 176, 0.1);
}

.rules-table code {
  background: rgba(255, 255, 255, 0.1);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Consolas', monospace;
  font-size: 13px;
  color: #4FC3F7;
}

.perm-select {
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ddd;
  padding: 6px 10px;
  border-radius: 4px;
  cursor: pointer;
}

.btn-danger {
  background: #f44336;
  color: white;
  border: none;
  padding: 5px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}

.btn-danger:hover:not(:disabled) {
  background: #d32f2f;
}

.btn-danger:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.empty-rules {
  text-align: center;
  padding: 30px;
  color: #666;
  background: rgba(0, 0, 0, 0.2);
  border-radius: 8px;
}

/* 添加规则表单 */
.add-rule-form {
  background: rgba(255, 255, 255, 0.03);
  border: 1px dashed rgba(255, 255, 255, 0.15);
  border-radius: 8px;
  padding: 15px;
  margin-bottom: 20px;
}

.add-rule-form h5 {
  color: #ccc;
  font-size: 14px;
  margin-bottom: 12px;
}

.form-row {
  display: flex;
  gap: 10px;
  margin-bottom: 10px;
}

.form-row:last-child {
  margin-bottom: 0;
}

.flex-1 {
  flex: 1;
}

.input {
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ddd;
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 14px;
}

.input:focus {
  outline: none;
  border-color: #4FC3F7;
}

/* 操作按钮 */
.actions {
  display: flex;
  align-items: center;
  gap: 15px;
  padding-top: 15px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.btn {
  padding: 8px 16px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.2s ease;
}

.btn-primary {
  background: #4FC3F7;
  color: #000;
}

.btn-primary:hover:not(:disabled) {
  background: #29B6F6;
}

.btn-primary:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.btn-secondary {
  background: transparent;
  color: #aaa;
  border: 1px solid rgba(255, 255, 255, 0.2);
}

.btn-secondary:hover {
  background: rgba(255, 255, 255, 0.05);
  color: #fff;
}

.btn-small {
  padding: 5px 10px;
  font-size: 13px;
}

.status-msg {
  font-size: 13px;
  padding: 5px 10px;
  border-radius: 4px;
}

.status-msg.success {
  background: rgba(76, 175, 80, 0.2);
  color: #4CAF50;
}

.status-msg.error {
  background: rgba(244, 67, 54, 0.2);
  color: #f44336;
}
</style>