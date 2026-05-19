package main

import (
	"context"
	"fmt"
	"os"

	"argus/internal/ai"
	"argus/internal/types"
	"gopkg.in/yaml.v3"
)

func testAI() {
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

	client := ai.NewClient(config.APIConfig)

	fmt.Println("测试 AI 对话...")
	response, err := client.Chat(context.Background(), "你是一个 helpful assistant", "你好，请简单介绍一下自己", "中文")
	if err != nil {
		fmt.Printf("AI 调用失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("AI 回复:\n%s\n", response)
}
