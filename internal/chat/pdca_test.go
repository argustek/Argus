package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"argus/internal/types"

	"github.com/stretchr/testify/require"
)

// 测试配置
type pdcaConfig struct {
	apiProvider string
	apiBaseURL  string
	apiKey      string
	apiModel    string
	maxIter     int // 反复运行次数
	timeoutSec  int // 单次超时秒数
}

// 测试结果
type pdcaResult struct {
	iter      int
	duration  time.Duration
	completed bool
	finalState int
	error     string
}

// loadTestConfig 从环境变量或配置文件加载测试用API配置
func loadTestConfig(t *testing.T) pdcaConfig {
	cfg := pdcaConfig{
		maxIter:    5, // 默认跑5次
		timeoutSec: 60, // 超时60秒
	}

	// 优先从环境变量读取
	if key := os.Getenv("TEST_API_KEY"); key != "" {
		cfg.apiKey = key
		cfg.apiBaseURL = os.Getenv("TEST_API_BASE_URL")
		if cfg.apiBaseURL == "" {
			cfg.apiBaseURL = "https://integrate.api.nvidia.com/v1"
		}
		cfg.apiModel = os.Getenv("TEST_API_MODEL")
		if cfg.apiModel == "" {
			cfg.apiModel = "qwen/qwen3.5-122b-a10b"
		}
		cfg.apiProvider = os.Getenv("TEST_API_PROVIDER")
		if cfg.apiProvider == "" {
			cfg.apiProvider = "nvidia"
		}
		return cfg
	}

	// 从项目 config 目录加载
	tryPaths := []string{
		filepath.Join("..", "..", "config", "config.json"),
		filepath.Join("E:", "Argus", "argus-desktop", "config", "config.json"),
	}

	for _, path := range tryPaths {
		absPath, _ := filepath.Abs(path)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		var raw struct {
			APIConfigs []struct {
				Provider  string `json:"provider"`
				BaseURL   string `json:"baseUrl"`
				APIKey    string `json:"apiKey"`
				ModelName string `json:"modelName"`
				IsDefault bool   `json:"isDefault"`
			} `json:"apiConfigs"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		for _, ac := range raw.APIConfigs {
			key := ac.APIKey
			if strings.HasPrefix(key, "enc:") {
				fmt.Printf("[PDCA] ⚠️ API Key 已加密，请通过 TEST_API_KEY 环境变量提供明文 key\n")
				continue
			}
			if key != "" {
				cfg.apiKey = key
				cfg.apiBaseURL = ac.BaseURL
				cfg.apiModel = ac.ModelName
				cfg.apiProvider = ac.Provider
				fmt.Printf("[PDCA] ✅ 从配置文件加载API配置: %s / %s\n", ac.Provider, ac.ModelName)
				return cfg
			}
		}
	}

	t.Skip("[PDCA] ❌ 未找到有效API配置，跳过测试\n" +
		"请设置环境变量:\n" +
		"  $env:TEST_API_KEY=\"your-api-key\"\n" +
		"  $env:TEST_API_BASE_URL=\"https://...\"\n" +
		"  $env:TEST_API_MODEL=\"model-name\"")
	return cfg
}

// waitForCompletion 等待任务完成，通过 state.json + conversation.log 双重检测
func waitForCompletion(m *Manager, workDir string, timeout time.Duration) (bool, int, string) {
	statePath := filepath.Join(workDir, ".argus", "state.json")
	logPath := filepath.Join(workDir, ".argus", "conversation.log")

	// 预期输出文件列表（SE 通常创建其中之一）
	completionFiles := []string{"hello.go", "main.go"}

	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	lastLogSize := int64(0)
	var lastActivity time.Time
	activityTimeout := 20 * time.Second

	for time.Now().Before(deadline) {
		<-tick.C

		// 1. 检测输出文件是否存在（hello.go / main.go）
		for _, f := range completionFiles {
			fpath := filepath.Join(workDir, f)
			if _, err := os.Stat(fpath); err == nil {
				fmt.Printf("[PDCA] ✅ 检测到输出文件: %s\n", fpath)
				state, _ := readStateFile(statePath)
				return true, state.ProjectState, fmt.Sprintf("output_file=%s, state=%d", f, state.ProjectState)
			}
		}

		// 2. 检测 state.json
		state, err := readStateFile(statePath)
		if err == nil {
			if state.ProjectState == types.ProjectStateDone ||
				state.ProjectState == types.ProjectStateApproved {
				return true, state.ProjectState, fmt.Sprintf("state=%d", state.ProjectState)
			}
			if state.ProjectState == types.ProjectStateError {
				return false, state.ProjectState, "project_error"
			}
		}

		// 2. 检测 conversation.log 是否有新内容（判断任务是否卡死）
		logData, err := os.ReadFile(logPath)
		if err == nil {
			size := int64(len(logData))
			if size > lastLogSize {
				lastLogSize = size
				lastActivity = time.Now()
			}
		}

		if !lastActivity.IsZero() && time.Since(lastActivity) > activityTimeout {
			// 超时无新日志，但尚未到 deadline
			fmt.Printf("[PDCA] ⚠️ 任务日志停止更新超过 %v，可能卡死\n", activityTimeout)
		}
	}

	// 超时 - 最后一次检查
	state, _ := readStateFile(statePath)
	return false, state.ProjectState, "timeout"
}

// readStateFile 读取state.json
func readStateFile(path string) (types.State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return types.State{}, err
	}
	var state types.State
	if err := json.Unmarshal(data, &state); err != nil {
		return types.State{}, err
	}
	return state, nil
}

// printPDCAReport 打印PDCA测试报告
func printPDCAReport(results []pdcaResult) {
	var passed, failed int
	var totalTime time.Duration

	for _, r := range results {
		if r.completed {
			passed++
		} else {
			failed++
		}
		totalTime += r.duration
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("📊 PDCA 自动化测试报告\n")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("总运行次数: %d\n", len(results))
	fmt.Printf("✅ 成功: %d\n", passed)
	fmt.Printf("❌ 失败: %d\n", failed)
	if len(results) > 0 {
		fmt.Printf("平均耗时: %v\n", totalTime/time.Duration(len(results)))
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("各次详情:")
	for _, r := range results {
		status := "✅"
		if !r.completed {
			status = "❌"
		}
		fmt.Printf("  #%d %s 耗时=%v state=%d err=%s\n",
			r.iter, status, r.duration.Round(time.Millisecond), r.finalState, r.error)
	}
	fmt.Println(strings.Repeat("=", 60))
}

// TestPDCA_HelloWorld PDCA模式：反复运行"hello world"任务
func TestPDCA_HelloWorld(t *testing.T) {
	cfg := loadTestConfig(t)

	// P(Plan): 规划测试目标
	helloMsg := "写一个 hello world 的 go 程序"
	results := make([]pdcaResult, 0, cfg.maxIter)
	resultsMu := sync.Mutex{}

	fmt.Printf("\n🔵 [PDCA Plan] 计划运行 %d 次 hello world 任务\n", cfg.maxIter)
	fmt.Printf("   超时: %ds/次, API: %s / %s\n", cfg.timeoutSec, cfg.apiProvider, cfg.apiModel)

	for i := 0; i < cfg.maxIter; i++ {
		iter := i + 1
		fmt.Printf("\n%s\n", strings.Repeat("─", 50))
		fmt.Printf("🔄 [PDCA Do] 第 %d/%d 次运行\n", iter, cfg.maxIter)

		// D(Do): 执行测试
		start := time.Now()
		completed := false
		finalState := 0
		errMsg := ""

		func() {
			// 每个迭代使用独立的工作目录
			tmpDir := t.TempDir()

			apiCfg := types.APIConfig{
				Provider: cfg.apiProvider,
				BaseURL:  cfg.apiBaseURL,
				APIKey:   cfg.apiKey,
				Model:    cfg.apiModel,
			}
			mgrCfg := types.Config{
				APIConfig: apiCfg,
				WorkDir:   tmpDir,
			}

			manager, err := NewManager(mgrCfg, tmpDir)
			require.NoError(t, err, "NewManager failed")
			manager.InitCMonitor() // 初始化C监控（必须调用，否则cMonitor为nil）
			defer manager.StopGoroutines()
			defer func() {
				manager.StopCurrentTask()
				if manager.cMonitor != nil {
					manager.cMonitor.Stop()
				}
			}()

			// 注册 SSE 事件监听
			sseBridge := manager.GetSSEBridge()
			if sseBridge != nil {
				sseCh, _ := sseBridge.Subscribe("pdca_test")
				defer sseBridge.Unsubscribe("pdca_test")
				go func() {
					for evt := range sseCh {
						fmt.Printf("[PDCA SSE] 事件: type=%s data=%v\n", evt.Type, evt.Data)
						if evt.Type == "done" || evt.Type == "project_state_changed" {
							fmt.Printf("[PDCA SSE] ✅ 检测到完成事件\n")
						}
					}
				}()
			}

			// 发送 hello world 消息
			fmt.Printf("[PDCA] 发送消息: %s\n", helloMsg)
			response, err := manager.ProcessMessage(helloMsg)
			if err != nil {
				errMsg = fmt.Sprintf("ProcessMessage error: %v", err)
				fmt.Printf("[PDCA] ❌ %s\n", errMsg)
				return
			}
			fmt.Printf("[PDCA] 初始回复: %s\n", truncateStr(response, 100))

			// 等待完成
			fmt.Printf("[PDCA] ⏳ 等待任务完成 (超时 %ds)...\n", cfg.timeoutSec)
			completed, finalState, errMsg = waitForCompletion(manager, tmpDir, time.Duration(cfg.timeoutSec)*time.Second)
		}()

		elapsed := time.Since(start)
		fmt.Printf("[PDCA] 第 %d 次耗时: %v\n", iter, elapsed.Round(time.Millisecond))

		// C(Check): 检查结果
		result := pdcaResult{
			iter:       iter,
			duration:   elapsed,
			completed:  completed,
			finalState: finalState,
			error:      errMsg,
		}
		resultsMu.Lock()
		results = append(results, result)
		resultsMu.Unlock()

		if !completed {
			fmt.Printf("[PDCA Check] ❌ 第 %d 次失败: %s\n", iter, errMsg)
		} else {
			fmt.Printf("[PDCA Check] ✅ 第 %d 次成功, state=%d\n", iter, finalState)
		}

		// 每次运行间隔5秒，给系统喘息时间
		if i < cfg.maxIter-1 {
			fmt.Printf("[PDCA] 💤 等待5秒后继续下一轮...\n")
			time.Sleep(5 * time.Second)
		}
	}

	// A(Act): 输出报告
	printPDCAReport(results)

	// 如果失败率 > 30%，标记测试失败
	var failedCount int
	for _, r := range results {
		if !r.completed {
			failedCount++
		}
	}
	failRate := float64(failedCount) / float64(len(results)) * 100
	fmt.Printf("\n[PDCA Act] 失败率: %.1f%%\n", failRate)

	if failRate > 30 {
		t.Errorf("[PDCA] ❌ 失败率 %.1f%% 超过阈值 30%%，需要人工排查", failRate)
	} else if failedCount > 0 {
		t.Logf("[PDCA] ⚠️ 有 %d/%d 次失败 (%.1f%%)，可接受范围内", failedCount, len(results), failRate)
	} else {
		fmt.Printf("[PDCA Act] ✅ 全部通过！\n")
	}
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}