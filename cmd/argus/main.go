package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"argus/internal/chat"
	"argus/internal/types"
)

func main() {
	fmt.Println("[Argus] Starting...")

	// 1. 加载配置（统一使用 config/config.json）
	config, err := loadConfig("config/config.json")
	if err != nil {
		fmt.Printf("[Argus] Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. 启动C守护进程
	fmt.Println("[Argus] Starting C guardian...")
	cmdC := exec.Command("./argus-c")
	cmdC.Stdout = os.Stdout
	cmdC.Stderr = os.Stderr
	if err := cmdC.Start(); err != nil {
		fmt.Printf("[Argus] Failed to start C: %v\n", err)
	} else {
		fmt.Println("[Argus] C started successfully")
		defer func() {
			cmdC.Process.Signal(syscall.SIGTERM)
			cmdC.Wait()
		}()
	}

	// 3. 等待C启动
	time.Sleep(2 * time.Second)

	// 4. 初始化对话管理器
	workDir := config.WorkDir
	if workDir == "" {
		cwd, _ := os.Getwd()
		workDir = cwd
	}
	workDir, _ = filepath.Abs(workDir)
	chatManager, err := chat.NewManager(*config, workDir, ".")
	if err != nil {
		fmt.Printf("[Argus] Failed to init chat manager: %v\n", err)
		os.Exit(1)
	}

	// 4.5 初始化C监控（CLI模式也需要）
	chatManager.InitCMonitor()
	fmt.Println("[Argus] C monitor initialized")

	// 5. 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("[Argus] Ready!")
	fmt.Printf("[Argus] Work directory: %s\n", workDir)
	fmt.Println("[Argus] Enter your message (or 'quit' to exit):")
	fmt.Println()

	// 6. 主循环 - 读取用户输入
	reader := bufio.NewReader(os.Stdin)
	inputChan := make(chan string)

	go func() {
		for {
			fmt.Print("> ")
			input, err := reader.ReadString('\n')
			if err != nil {
				close(inputChan)
				return
			}
			inputChan <- input
		}
	}()

	for {
		select {
		case input, ok := <-inputChan:
			if !ok {
				fmt.Println("[Argus] Input closed")
				return
			}
			input = input[:len(input)-1]
			if input == "quit" || input == "exit" {
				fmt.Println("[Argus] Goodbye!")
				return
			}
			if err := chatManager.HandleUserInput(input); err != nil {
				fmt.Printf("[Error] %v\n", err)
			}
			fmt.Println()

		case sig := <-sigChan:
			fmt.Printf("\n[Argus] Received signal: %v\n", sig)
			fmt.Println("[Argus] Shutting down...")
			return
		}
	}
}

type guiConfig struct {
	APIConfigs []struct {
		ID        string `json:"id"`
		Provider  string `json:"provider"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
		ModelName string `json:"modelName"`
		IsDefault bool   `json:"isDefault"`
	} `json:"apiConfigs"`
	WorkDir           string `json:"workDir"`
	PmDecisionAlert   bool   `json:"pmDecisionAlert"`
	UseSeparateModels bool   `json:"useSeparateModels"`
	PMConfigID        string `json:"pmConfigId"`
	SEConfigID        string `json:"seConfigId"`
	APConfigID        string `json:"apConfigId"`
	APEnabled         bool   `json:"apEnabled"`
}

func loadConfig(path string) (*types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var gc guiConfig
	if err := json.Unmarshal(data, &gc); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}

	config := &types.Config{
		WorkDir:           gc.WorkDir,
		CheckInterval:     30,
		CommitInterval:    5,
		HeartbeatTimeout:  300,
		PmDecisionAlert:   gc.PmDecisionAlert,
		UseSeparateModels: gc.UseSeparateModels,
	}

	findCfg := func(id string) *struct {
		ID        string `json:"id"`
		Provider  string `json:"provider"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
		ModelName string `json:"modelName"`
		IsDefault bool   `json:"isDefault"`
	} {
		for i := range gc.APIConfigs {
			if gc.APIConfigs[i].ID == id {
				return &gc.APIConfigs[i]
			}
		}
		return nil
	}

	if pm := findCfg(gc.PMConfigID); pm != nil {
		config.APIConfig = types.APIConfig{
			Provider: pm.Provider, BaseURL: pm.BaseURL,
			APIKey: pm.APIKey, Model: pm.ModelName,
		}
	} else {
		for i := range gc.APIConfigs {
			if gc.APIConfigs[i].IsDefault {
				config.APIConfig = types.APIConfig{
					Provider: gc.APIConfigs[i].Provider,
					BaseURL:  gc.APIConfigs[i].BaseURL,
					APIKey:   gc.APIConfigs[i].APIKey,
					Model:    gc.APIConfigs[i].ModelName,
				}
				break
			}
		}
	}
	if config.APIConfig.Provider == "" && len(gc.APIConfigs) > 0 {
		config.APIConfig = types.APIConfig{
			Provider: gc.APIConfigs[0].Provider,
			BaseURL:  gc.APIConfigs[0].BaseURL,
			APIKey:   gc.APIConfigs[0].APIKey,
			Model:    gc.APIConfigs[0].ModelName,
		}
	}

	if gc.UseSeparateModels {
		if pm := findCfg(gc.PMConfigID); pm != nil {
			config.PMConfig = types.APIConfig{
				Provider: pm.Provider, BaseURL: pm.BaseURL,
				APIKey: pm.APIKey, Model: pm.ModelName,
			}
		}
		if se := findCfg(gc.SEConfigID); se != nil {
			config.SEConfig = types.APIConfig{
				Provider: se.Provider, BaseURL: se.BaseURL,
				APIKey: se.APIKey, Model: se.ModelName,
			}
		}
		if gc.APEnabled {
			if ap := findCfg(gc.APConfigID); ap != nil {
				config.APConfig = types.APIConfig{
					Provider: ap.Provider, BaseURL: ap.BaseURL,
					APIKey: ap.APIKey, Model: ap.ModelName,
				}
			}
		}
	}

	return config, nil
}
