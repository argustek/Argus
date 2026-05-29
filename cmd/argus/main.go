package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"argus/internal/chat"
	"argus/internal/types"
	"gopkg.in/yaml.v3"
)

func main() {
	fmt.Println("[Argus] Starting...")

	// 1. 加载配置
	config, err := loadConfig(".argus/config.yaml")
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
		// C启动失败，继续运行，但功能受限
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
	workDir := "."
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

	// 后台读取输入
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

			// 去掉换行符
			input = input[:len(input)-1]

			// 检查退出
			if input == "quit" || input == "exit" {
				fmt.Println("[Argus] Goodbye!")
				return
			}

			// 处理用户输入
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

// loadConfig 加载配置
func loadConfig(path string) (*types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config types.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// 设置默认值
	if config.CheckInterval == 0 {
		config.CheckInterval = 30
	}
	if config.CommitInterval == 0 {
		config.CommitInterval = 5
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = 300
	}

	return &config, nil
}
