package main

import (
	"fmt"
	"os"

	"argus/internal/ai"
	"argus/internal/board"
	"argus/internal/chat"
	"argus/internal/types"
	"gopkg.in/yaml.v3"
)

func main() {
	// 加载配置
	data, err := os.ReadFile("../../.argus/config.yaml")
	if err != nil {
		fmt.Printf("读取配置失败: %v\n", err)
		os.Exit(1)
	}

	var config types.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		fmt.Printf("解析配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化组件
	aiClient := ai.NewClient(config.APIConfig)
	boardManager := board.NewManager("../../.argus/board.json")
	boardManager.Load()

	router := chat.NewRouter()
	pmProcessor := ai.NewPMProcessor(aiClient, ".", nil)
	seProcessor := ai.NewSEProcessor(aiClient, ".")

	// 测试1: 用户普通对话
	fmt.Println("=== 测试1: 用户普通对话 ===")
	userInput := "你好"
	msg := router.Parse("user", userInput)
	fmt.Printf("[用户] %s -> 发给: %s\n", msg.Content, msg.To)

	if msg.To == "pm" {
		resp, err := pmProcessor.Process(msg.Content, nil)
		if err != nil {
			fmt.Printf("PM处理失败: %v\n", err)
		} else {
			fmt.Printf("[PM] %s\n", resp.Content)
		}
	}

	fmt.Println()

	// 测试2: 用户编程需求
	fmt.Println("=== 测试2: 用户编程需求 ===")
	userInput = "帮我写一个hello.go，输出Hello World"
	msg = router.Parse("user", userInput)
	fmt.Printf("[用户] %s -> 发给: %s\n", msg.Content, msg.To)

	if msg.To == "pm" {
		resp, err := pmProcessor.Process(msg.Content, nil)
		if err != nil {
			fmt.Printf("PM处理失败: %v\n", err)
		} else {
			fmt.Printf("[PM] %s\n", resp.Content)
			if resp.HasTasks {
				fmt.Printf("[系统] PM创建了任务: %s\n", resp.Tasks.CurrentTask)

				// 启动SE
				fmt.Println("[系统] 启动SE执行任务...")
				boardManager.UpdateTask(resp.Tasks.CurrentTask, 1)

				seResp, err := seProcessor.ProcessTaskWithTools(resp.Tasks.CurrentTask, nil)
				if err != nil {
					fmt.Printf("SE处理失败: %v\n", err)
				} else {
					fmt.Printf("[SE] %s\n", seResp.Content)
					if len(seResp.Actions) > 0 {
						fmt.Printf("[系统] SE需要执行 %d 个操作\n", len(seResp.Actions))
						for i, action := range seResp.Actions {
							fmt.Printf("  [%d] %s: %s\n", i+1, action.Type, action.Path)
						}
					}
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("=== 流程测试完成 ===")
}
