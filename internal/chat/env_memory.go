package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"argus/internal/types"
)

var knownTools = map[string]bool{
	"node": true, "npm": true, "npx": true, "pnpm": true, "yarn": true,
	"git": true, "python": true, "pip": true, "pip3": true, "poetry": true,
	"go": true, "cargo": true, "rustc": true, "rustup": true,
	"docker": true, "docker-compose": true, "java": true, "javac": true,
	"mvn": true, "gradle": true, "gcc": true, "g++": true, "make": true,
	"cmake": true, "ruby": true, "gem": true, "php": true, "composer": true,
	"perl": true, "swift": true, "bun": true, "deno": true,
	"winget": true, "scoop": true, "choco": true,
	"code": true, "notepad++": true, "vim": true, "nano": true,
	"7z": true, "tar": true, "curl": true, "wget": true, "ssh": true,
	"scp": true, "rsync": true, "openssl": true,
}

var systemExcludes = map[string]bool{
	"conhost": true, "ctfmon": true, "dllhost": true, "svchost": true,
	"RuntimeBroker": true, "SearchHost": true, "Taskhostw": true,
	"explorer": true, "SystemSettings": true, "sihost": true,
	"fontdrvhost": true, "smss": true, "csrss": true, "wininit": true,
	"winlogon": true, "services": true, "lsass": true, "lsm": true,
	"svchos": true, "taskhostw": true, "UserManager": true,
	"dwm": true, "audiodg": true, "wmpnetwk": true,
}

type EnvMemory struct {
	mu       sync.RWMutex
	data     *types.EnvMemory
	filePath string
	workDir  string
}

func NewEnvMemory(workDir string) (*EnvMemory, error) {
	configDir := filepath.Join(workDir, ".argus")
	em := &EnvMemory{
		data:     types.GetDefaultEnvMemory(),
		filePath: filepath.Join(configDir, "env_memory.json"),
		workDir:  workDir,
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("创建环境记忆目录失败: %v", err)
	}

	if err := em.load(); err != nil {
		fmt.Printf("[EnvMemory] ⚠️ 加载环境记忆失败，使用空记忆: %v\n", err)
	}

	go em.scanBasicSystemInfo()

	return em, nil
}

func (em *EnvMemory) load() error {
	em.mu.Lock()
	defer em.mu.Unlock()

	data, err := os.ReadFile(em.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var mem types.EnvMemory
	if err := json.Unmarshal(data, &mem); err != nil {
		return err
	}

	if mem.Tools == nil {
		mem.Tools = make(map[string]types.ToolInfo)
	}
	if mem.Configs == nil {
		mem.Configs = make(map[string]types.EnvConfig)
	}
	em.data = &mem

	fmt.Printf("[EnvMemory] ✅ 环境记忆已加载 (%d 个工具, %d 个配置)\n", len(mem.Tools), len(mem.Configs))
	return nil
}

func (em *EnvMemory) Save() error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	em.data.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(em.data, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(em.filePath, data, 0644); err != nil {
		return err
	}

	fmt.Printf("[EnvMemory] 💾 环境记忆已保存 (%d 个工具)\n", len(em.data.Tools))
	return nil
}

func (em *EnvMemory) LearnTool(name, path string, source string) bool {
	em.mu.Lock()
	defer em.mu.Unlock()

	name = strings.ToLower(strings.TrimSpace(name))
	path = strings.TrimSpace(path)

	if name == "" || path == "" {
		return false
	}

	if !em.isToolSoftware(name) {
		return false
	}

	existing, exists := em.data.Tools[name]
	now := time.Now()

	if exists && existing.Path == path {
		em.data.Tools[name] = types.ToolInfo{
			Path:      path,
			FirstSeen: existing.FirstSeen,
			LastUsed:  now,
			UseCount:  existing.UseCount + 1,
			Source:    existing.Source,
		}
		fmt.Printf("[EnvMemory] 🔄 工具已更新: %s (使用%d次)\n", name, existing.UseCount+1)
	} else {
		firstSeen := now
		if exists {
			firstSeen = existing.FirstSeen
		}
		em.data.Tools[name] = types.ToolInfo{
			Path:      path,
			FirstSeen: firstSeen,
			LastUsed:  now,
			UseCount:  1,
			Source:    source,
		}
		fmt.Printf("[EnvMemory] 🆕 新工具记录: %s → %s (来源:%s)\n", name, path, source)
	}

	go em.Save()
	return true
}

func (em *EnvMemory) LearnConfig(key, value, source string) {
	em.mu.Lock()
	defer em.mu.Unlock()

	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)

	if key == "" || value == "" {
		return
	}

	em.data.Configs[key] = types.EnvConfig{
		Value:     value,
		UpdatedAt: time.Now(),
		Source:    source,
	}

	fmt.Printf("[EnvMemory] 📝 配置记录: %s = %s\n", key, value)
	go em.Save()
}

func (em *EnvMemory) GetTool(name string) (string, bool) {
	em.mu.RLock()
	defer em.mu.RUnlock()

	name = strings.ToLower(strings.TrimSpace(name))
	info, ok := em.data.Tools[name]
	if !ok {
		return "", false
	}
	return info.Path, true
}

func (em *EnvMemory) GetAllTools() map[string]types.ToolInfo {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make(map[string]types.ToolInfo, len(em.data.Tools))
	for k, v := range em.data.Tools {
		result[k] = v
	}
	return result
}

func (em *EnvMemory) GetAllConfigs() map[string]types.EnvConfig {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make(map[string]types.EnvConfig, len(em.data.Configs))
	for k, v := range em.data.Configs {
		result[k] = v
	}
	return result
}

func (em *EnvMemory) LearnFromCommand(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	toolName := strings.ToLower(filepath.Base(parts[0]))
	toolName = strings.TrimSuffix(toolName, ".exe")
	toolName = strings.TrimSuffix(toolName, ".cmd")
	toolName = strings.TrimSuffix(toolName, ".bat")

	if !em.isToolSoftware(toolName) {
		return
	}

	rawPath := parts[0]
	if !filepath.IsAbs(rawPath) {
		return
	}

	if _, statErr := os.Stat(rawPath); statErr != nil {
		return
	}

	em.LearnTool(toolName, rawPath, "learned")
}

func (em *EnvMemory) isToolSoftware(name string) bool {
	if knownTools[name] {
		return true
	}

	if systemExcludes[name] {
		return false
	}

	if len(name) <= 2 {
		return false
	}

	dotExt := filepath.Ext(name)
	if dotExt != "" && dotExt != ".exe" && dotExt != ".cmd" && dotExt != ".bat" && dotExt != ".ps1" {
		return false
	}

	return true
}

func (em *EnvMemory) Summary() string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("## 已知工具路径\n")

	if len(em.data.Tools) == 0 {
		sb.WriteString("（暂无记录，使用过程中自动积累）\n")
	} else {
		for name, info := range em.data.Tools {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s` (使用%d次)\n", name, info.Path, info.UseCount))
		}
	}

	if len(em.data.Configs) > 0 {
		sb.WriteString("\n## 已知配置\n")
		for key, cfg := range em.data.Configs {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", key, cfg.Value))
		}
	}

	sys := em.data.System
	if sys.OS != "" {
		sb.WriteString(fmt.Sprintf("\n## 系统\n- OS: %s\n", sys.OS))
		if sys.CPU != "" {
			sb.WriteString(fmt.Sprintf("- CPU: %s\n", sys.CPU))
		}
		if sys.RAM != "" {
			sb.WriteString(fmt.Sprintf("- RAM: %s\n", sys.RAM))
		}
	}

	return sb.String()
}

func (em *EnvMemory) ToolCount() int {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return len(em.data.Tools)
}

func (em *EnvMemory) scanBasicSystemInfo() {
	time.Sleep(2 * time.Second)

	osName := os.Getenv("OS")
	if osName == "" {
		osName = "Windows"
	}

	em.mu.Lock()
	em.data.System.OS = osName
	em.data.System.GoVer = detectGoVersion()
	em.data.System.NodeVer = detectNodeVersion()
	em.mu.Unlock()

	go em.Save()
	fmt.Printf("[EnvMemory] 🖥️ 系统信息已扫描: OS=%s Go=%s Node=%s\n",
		osName, em.data.System.GoVer, em.data.System.NodeVer)
}

func detectGoVersion() string {
	out, err := runQuickCommand("go version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func detectNodeVersion() string {
	out, err := runQuickCommand("node --version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func runQuickCommand(cmd string) (string, error) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("空命令")
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	return string(out), err
}
