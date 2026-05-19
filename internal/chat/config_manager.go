package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"argus/internal/types"
)

// ConfigManager 配置管理器（统一管理决策配置和权限配置）
type ConfigManager struct {
	mu               sync.RWMutex
	workDir          string
	configDir        string
	decisionConfig   *types.DecisionConfig
	permissionConfig *types.PermissionConfig
	commandPolicy    *types.CommandPolicy
}

// NewConfigManager 创建配置管理器
func NewConfigManager(workDir string) (*ConfigManager, error) {
	cm := &ConfigManager{
		workDir:   workDir,
		configDir: filepath.Join(workDir, ".argus"),
	}

	if err := os.MkdirAll(cm.configDir, 0755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %v", err)
	}

	if err := cm.loadDecisionConfig(); err != nil {
		fmt.Printf("[ConfigManager] ⚠️ 加载决策配置失败，使用默认值: %v\n", err)
	}

	if err := cm.loadPermissionConfig(); err != nil {
		fmt.Printf("[ConfigManager] ⚠️ 加载权限配置失败，使用默认值: %v\n", err)
	}

	if err := cm.loadCommandPolicy(); err != nil {
		fmt.Printf("[ConfigManager] ⚠️ 加载命令策略失败，使用默认值: %v\n", err)
	}

	return cm, nil
}

// loadDecisionConfig 加载决策配置
func (cm *ConfigManager) loadDecisionConfig() error {
	path := filepath.Join(cm.configDir, "decision_config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaultConfig := types.GetDefaultDecisionConfig()
			cm.decisionConfig = &defaultConfig
			return cm.SaveDecisionConfig()
		}
		return err
	}

	var config types.DecisionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	cm.decisionConfig = &config
	fmt.Printf("[ConfigManager] ✅ 决策配置已加载 (%d 条规则)\n", len(config.Rules))
	return nil
}

// loadPermissionConfig 加载权限配置
func (cm *ConfigManager) loadPermissionConfig() error {
	path := filepath.Join(cm.configDir, "permission_config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaultConfig := types.GetDefaultPermissionConfig(cm.workDir)
			cm.permissionConfig = &defaultConfig
			return cm.SavePermissionConfig()
		}
		return err
	}

	var config types.PermissionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	cm.permissionConfig = &config

	// 迁移：补齐新版内置规则（如 C:/Windows/**）
	builtinRules := types.GetDefaultPermissionConfig(cm.workDir).Rules
	hasWindowsRule := false
	for _, r := range cm.permissionConfig.Rules {
		if r.PathPattern == "C:/Windows/**" || r.PathPattern == "C:\\Windows\\**" {
			hasWindowsRule = true
			break
		}
	}
	if !hasWindowsRule {
		for _, br := range builtinRules {
			if br.PathPattern == "C:/Windows/**" || br.PathPattern == "C:\\Windows\\**" {
				cm.permissionConfig.Rules = append(cm.permissionConfig.Rules, br)
			}
		}
		cm.permissionConfig.UpdatedAt = time.Now()
		fmt.Printf("[ConfigManager] 🔄 迁移: 添加 Windows 系统目录保护规则\n")
		if err := cm.SavePermissionConfig(); err != nil {
			fmt.Printf("[ConfigManager] ⚠️ 迁移保存失败: %v\n", err)
		}
	}

	fmt.Printf("[ConfigManager] ✅ 权限配置已加载 (%d 条规则)\n", len(cm.permissionConfig.Rules))
	return nil
}

func (cm *ConfigManager) loadCommandPolicy() error {
	path := filepath.Join(cm.configDir, "command_policy.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaultPolicy := types.GetDefaultCommandPolicy()
			cm.commandPolicy = &defaultPolicy
			return cm.SaveCommandPolicy()
		}
		return err
	}

	var policy types.CommandPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return err
	}

	cm.commandPolicy = &policy
	fmt.Printf("[ConfigManager] ✅ 命令策略已加载 (%d 条规则)\n", len(policy.Rules))
	return nil
}

func (cm *ConfigManager) SaveCommandPolicy() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.commandPolicy == nil {
		return fmt.Errorf("命令策略未初始化")
	}

	data, err := json.MarshalIndent(cm.commandPolicy, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(cm.configDir, "command_policy.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	fmt.Printf("[ConfigManager] 💾 命令策略已保存\n")
	return nil
}

func (cm *ConfigManager) CheckCommand(command string) (types.CommandBlockLevel, string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.commandPolicy == nil {
		return types.CmdBlockAllow, ""
	}

	level, desc := cm.commandPolicy.CheckCommand(command)

	switch level {
	case types.CmdBlockDeny:
		fmt.Printf("[ConfigManager] 🚫 命令被拒绝: %s (%s)\n", command, desc)
	case types.CmdBlockAsk:
		fmt.Printf("[ConfigManager] ⚠️ 命令需确认: %s (%s)\n", command, desc)
	default:
		fmt.Printf("[ConfigManager] ✅ 命令允许: %s\n", command)
	}

	return level, desc
}

func (cm *ConfigManager) GetCommandPolicy() *types.CommandPolicy {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.commandPolicy
}

func (cm *ConfigManager) AddCommandRule(rule types.CommandRule) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.commandPolicy == nil {
		return fmt.Errorf("命令策略未初始化")
	}

	cm.commandPolicy.Rules = append(cm.commandPolicy.Rules, rule)
	cm.commandPolicy.UpdatedAt = time.Now()

	fmt.Printf("[ConfigManager] ➕ 已添加命令规则: %s → %s\n", rule.Pattern, rule.Level)
	return nil
}

func (cm *ConfigManager) RemoveCommandRule(pattern string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.commandPolicy == nil {
		return fmt.Errorf("命令策略未初始化")
	}

	newRules := make([]types.CommandRule, 0)
	for _, rule := range cm.commandPolicy.Rules {
		if rule.Pattern != pattern {
			newRules = append(newRules, rule)
		}
	}

	if len(newRules) == len(cm.commandPolicy.Rules) {
		return fmt.Errorf("未找到规则: %s", pattern)
	}

	cm.commandPolicy.Rules = newRules
	cm.commandPolicy.UpdatedAt = time.Now()

	fmt.Printf("[ConfigManager] ➖ 已删除命令规则: %s\n", pattern)
	return nil
}

func (cm *ConfigManager) ResetCommandPolicyToDefault() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	defaultPolicy := types.GetDefaultCommandPolicy()
	cm.commandPolicy = &defaultPolicy

	fmt.Printf("[ConfigManager] 🔄 已重置命令策略为缺省值\n")
	return nil
}

// SaveDecisionConfig 保存决策配置
func (cm *ConfigManager) SaveDecisionConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.decisionConfig == nil {
		return fmt.Errorf("决策配置未初始化")
	}

	data, err := json.MarshalIndent(cm.decisionConfig, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(cm.configDir, "decision_config.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	fmt.Printf("[ConfigManager] 💾 决策配置已保存\n")
	return nil
}

// SavePermissionConfig 保存权限配置
func (cm *ConfigManager) SavePermissionConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.permissionConfig == nil {
		return fmt.Errorf("权限配置未初始化")
	}

	data, err := json.MarshalIndent(cm.permissionConfig, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(cm.configDir, "permission_config.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	fmt.Printf("[ConfigManager] 💾 权限配置已保存\n")
	return nil
}

// ========== 决策检查接口 ==========

// CheckDecision 检查操作是否需要人工确认
// 返回: (是否自动执行, 操作描述, 错误)
func (cm *ConfigManager) CheckDecision(decisionType types.DecisionType) (bool, string, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.decisionConfig == nil {
		return false, "", fmt.Errorf("决策配置未加载")
	}

	mode := cm.decisionConfig.GetDecisionMode(decisionType)

	var desc string
	for _, rule := range cm.decisionConfig.Rules {
		if rule.Type == decisionType {
			desc = rule.Description
			break
		}
	}

	isAuto := mode == types.DecisionAuto

	if isAuto {
		fmt.Printf("[ConfigManager] ✅ 自动执行: %s (%s)\n", decisionType, desc)
	} else {
		fmt.Printf("[ConfigManager] 🛑 需要人工确认: %s (%s)\n", decisionType, desc)
	}

	return isAuto, desc, nil
}

// GetDecisionConfig 获取完整决策配置（供前端显示）
func (cm *ConfigManager) GetDecisionConfig() *types.DecisionConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.decisionConfig
}

// UpdateDecisionRule 更新单个决策规则
func (cm *ConfigManager) UpdateDecisionRule(decisionType types.DecisionType, mode types.DecisionMode) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.decisionConfig == nil {
		return fmt.Errorf("决策配置未初始化")
	}

	found := false
	for i := range cm.decisionConfig.Rules {
		if cm.decisionConfig.Rules[i].Type == decisionType {
			cm.decisionConfig.Rules[i].Mode = mode
			cm.decisionConfig.UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("未找到决策类型: %s", decisionType)
	}

	fmt.Printf("[ConfigManager] ✏️ 已更新决策规则: %s → %s\n", decisionType, mode)
	return nil
}

// ResetDecisionToDefault 重置所有决策为缺省值
func (cm *ConfigManager) ResetDecisionToDefault() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	defaultConfig := types.GetDefaultDecisionConfig()
	cm.decisionConfig = &defaultConfig

	fmt.Printf("[ConfigManager] 🔄 已重置决策配置为缺省值\n")
	return nil
}

// ========== 权限检查接口 ==========

// CheckPermission 检查路径的权限级别
// 返回: (权限级别, 匹配到的规则说明, 是否允许操作)
func (cm *ConfigManager) CheckPermission(operation string, filePath string) (types.PermissionLevel, string, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.permissionConfig == nil {
		return types.PermNoAccess, "权限配置未加载", false
	}

	relPath, _ := filepath.Rel(cm.workDir, filePath)

	matchedRule := ""
	permission := cm.permissionConfig.DefaultPermission

	// 路径匹配顺序：relPath > fileName > 原始绝对路径
	// 支持用 C:/Windows/** 这种绝对路径模式
	for _, rule := range cm.permissionConfig.Rules {
		matched := cm.matchPathPattern(rule.PathPattern, relPath) ||
			cm.matchPathPattern(rule.PathPattern, filepath.Base(filePath)) ||
			cm.matchPathPattern(rule.PathPattern, filePath) // 绝对路径匹配
		if matched {
			if matchedRule == "" || rule.Priority < cm.getPriority(matchedRule) {
				permission = rule.Permission
				matchedRule = rule.PathPattern
			}
		}
	}

	isAllowed := permission != types.PermNoAccess && permission != types.PermProtected

	switch operation {
	case "read":
		isAllowed = permission == types.PermFullAccess ||
			permission == types.PermReadWrite ||
			permission == types.PermReadOnly ||
			permission == types.PermProtected
	case "write", "modify":
		isAllowed = permission == types.PermFullAccess ||
			permission == types.PermReadWrite
	case "delete":
		isAllowed = permission == types.PermFullAccess
	case "execute":
		isAllowed = permission == types.PermFullAccess
	}

	fmt.Printf("[ConfigManager] 🔍 权限检查: %s → %s (操作:%s, 允许:%v)\n",
		filePath, permission, operation, isAllowed)

	return permission, matchedRule, isAllowed
}

// matchPathPattern 简单的通配符匹配（支持 *.go, .git/** 等）
func (cm *ConfigManager) matchPathPattern(pattern, path string) bool {
	pattern = strings.ToLower(pattern)
	path = strings.ToLower(path)

	if strings.Contains(pattern, "**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		prefix = strings.TrimSuffix(prefix, "\\**")
		return strings.HasPrefix(path, prefix)
	}

	if strings.HasPrefix(pattern, "*.") {
		ext := pattern[1:]
		return strings.HasSuffix(path, ext)
	}

	if strings.Contains(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix)
	}

	return pattern == path
}

// getPriority 获取已匹配规则的优先级
func (cm *ConfigManager) getPriority(pattern string) int {
	for _, rule := range cm.permissionConfig.Rules {
		if rule.PathPattern == pattern {
			return rule.Priority
		}
	}
	return 999
}

// GetPermissionConfig 获取完整权限配置（供前端显示）
func (cm *ConfigManager) GetPermissionConfig() *types.PermissionConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.permissionConfig
}

// AddPathRule 添加新的路径权限规则
func (cm *ConfigManager) AddPathRule(rule types.PathRule) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.permissionConfig == nil {
		return fmt.Errorf("权限配置未初始化")
	}

	cm.permissionConfig.Rules = append(cm.permissionConfig.Rules, rule)
	cm.permissionConfig.UpdatedAt = time.Now()

	fmt.Printf("[ConfigManager] ➕ 已添加权限规则: %s → %s\n", rule.PathPattern, rule.Permission)
	return nil
}

// RemovePathRule 删除路径权限规则
func (cm *ConfigManager) RemovePathRule(pattern string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.permissionConfig == nil {
		return fmt.Errorf("权限配置未初始化")
	}

	newRules := make([]types.PathRule, 0)
	for _, rule := range cm.permissionConfig.Rules {
		if rule.PathPattern != pattern {
			newRules = append(newRules, rule)
		}
	}

	if len(newRules) == len(cm.permissionConfig.Rules) {
		return fmt.Errorf("未找到规则: %s", pattern)
	}

	cm.permissionConfig.Rules = newRules
	cm.permissionConfig.UpdatedAt = time.Now()

	fmt.Printf("[ConfigManager] ➖ 已删除权限规则: %s\n", pattern)
	return nil
}

// ResetPermissionToDefault 重置权限为缺省值
func (cm *ConfigManager) ResetPermissionToDefault() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	defaultConfig := types.GetDefaultPermissionConfig(cm.workDir)
	cm.permissionConfig = &defaultConfig

	fmt.Printf("[ConfigManager] 🔄 已重置权限配置为缺省值\n")
	return nil
}

// GetConfigStatus 获取配置系统状态（供 C 监控调用）
func (cm *ConfigManager) GetConfigStatus() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	status := map[string]interface{}{
		"initialized": true,
		"workDir":     cm.workDir,
		"configDir":   cm.configDir,
	}

	if cm.decisionConfig != nil {
		status["decision"] = map[string]interface{}{
			"version":   cm.decisionConfig.Version,
			"ruleCount": len(cm.decisionConfig.Rules),
			"updatedAt": cm.decisionConfig.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	if cm.permissionConfig != nil {
		status["permission"] = map[string]interface{}{
			"version":           cm.permissionConfig.Version,
			"ruleCount":         len(cm.permissionConfig.Rules),
			"defaultPermission": cm.permissionConfig.DefaultPermission,
			"updatedAt":         cm.permissionConfig.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	if cm.commandPolicy != nil {
		status["commandPolicy"] = map[string]interface{}{
			"version":   cm.commandPolicy.Version,
			"ruleCount": len(cm.commandPolicy.Rules),
			"updatedAt": cm.commandPolicy.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	return status
}
