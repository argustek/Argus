package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/dist
var assets embed.FS

var heartbeatStop = make(chan struct{})

func main() {
	fmt.Fprintln(os.Stderr, "=== 程序启动 ===")
	os.Stdout.Sync()

	defer func() {
		if r := recover(); r != nil {
			timestamp := time.Now().Format("2006-01-02 15:04:05")
			msg := fmt.Sprintf("[%s] [PANIC] %v\n%s", timestamp, r, string(debugStack()))
			fmt.Print(msg)
			writeExitLog(msg)
			os.Exit(1)
		}
	}()

	if len(os.Args) > 1 {
		fmt.Fprintf(os.Stderr, "[main] 检测到参数: %s\n", os.Args[1])
		app := NewApp()

		switch os.Args[1] {
		case "--status":
			handleCLICommand(app, "status")
			return
		case "--memory":
			handleCLICommand(app, "memory")
			return
		case "--monitor":
			handleCLICommand(app, "monitor")
			return
		case "--recover":
			handleCLICommand(app, "recover")
			return
		case "--dump-tasks":
			// fall through to wails.Run → SingleInstanceLock → 查询运行中实例
			break
		case "--send":
			break
		default:
			fmt.Printf("❌ 未知命令: %s\n", os.Args[1])
			fmt.Println("可用命令:")
			fmt.Println("  --send <消息>    发送消息到聊天窗口")
			fmt.Println("  --status         查看系统完整状态")
			fmt.Println("  --memory         查看记忆系统状态")
			fmt.Println("  --monitor        查看 C 监控状态")
			fmt.Println("  --recover        强制恢复未完成任务")
			fmt.Println("  --dump-tasks     查看全局任务列表")
			return
		}
	}

	writeExitLog(fmt.Sprintf("[%s] [START] Argus 启动 PID=%d\n", time.Now().Format("2006-01-02 15:04:05"), os.Getpid()))

	go startHeartbeat()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		msg := fmt.Sprintf("[%s] [SIGNAL] 收到信号: %v\n", time.Now().Format("2006-01-02 15:04:05"), sig)
		fmt.Print(msg)
		writeExitLog(msg)
		os.Exit(1)
	}()

	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Argus",
		Width:  1280,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 24, G: 24, B: 24, A: 255},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:   false,
			WebviewUserDataPath:   "",
		},
		OnStartup: func(ctx context.Context) {
			fmt.Println("🚀 [0] OnStartup CALLBACK")
			app.Startup(ctx)
		},
		OnDomReady: func(ctx context.Context) {
			fmt.Println("🌐 [1] OnDomReady CALLBACK - WebView2 已就绪")
			writeExitLog(fmt.Sprintf("[%s] [WEBVIEW] WebView2 加载完成\n", time.Now().Format("2006-01-02 15:04:05")))

			go func() {
				time.Sleep(1500 * time.Millisecond)
				app.Ready()
			}()

			if len(os.Args) > 1 && os.Args[1] == "--send" && len(os.Args) > 2 {
				msg := strings.Join(os.Args[2:], " ")
				fmt.Printf("[FirstInstance] OnDomReady 发送消息: %s\n", msg)
				go func() {
					time.Sleep(500 * time.Millisecond)
					app.SendMessage(msg)
				}()
			}
		},
		OnShutdown: func(ctx context.Context) {
			close(heartbeatStop)
			app.Shutdown()
			timestamp := time.Now().Format("2006-01-02 15:04:05")
			msg := fmt.Sprintf("[%s] [SHUTDOWN] Argus 正常关闭\n", timestamp)
			fmt.Print(msg)
			writeExitLog(msg)
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "argus-desktop-vibecoding",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				secondArgs := secondInstanceData.Args
				for i, arg := range secondArgs {
					if arg == "--send" && i+1 < len(secondArgs) {
						msg := strings.Join(secondArgs[i+1:], " ")
						fmt.Printf("[SecondInstance] 收到消息: %s\n", msg)
						app.SendMessage(msg)
						return
					}
					if arg == "--dump-tasks" {
						tasksJSON := app.GetGlobalTasks()
						app.chatManager.WriteDebugLog("[DUMP-TASKS] " + tasksJSON)
						fmt.Println("[SecondInstance] [DUMP-TASKS] " + tasksJSON)
						return
					}
				}
			},
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		msg := fmt.Sprintf("[%s] [ERROR] Wails退出: %v\n", timestamp, err)
		fmt.Print(msg)
		writeExitLog(msg)
		log.Fatal(err)
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("[%s] [EXIT] Wails.Run 返回（正常退出）\n", timestamp)
	fmt.Print(msg)
	writeExitLog(msg)
}

func startHeartbeat() {
	dir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logPath := filepath.Join(dir, ".argus", "heartbeat.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			line := fmt.Sprintf("%s\n", time.Now().Format("2006-01-02 15:04:05"))
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				f.WriteString(line)
				f.Close()
			}
		case <-heartbeatStop:
			return
		}
	}
}

func debugStack() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

func handleCLICommand(app *App, command string) {
	fmt.Println("🔍 [CLI] 执行命令:", command)
	app.useCWD = true
	defer fmt.Println("[CLI] 命令执行完成")

	fmt.Println("[CLI] 开始初始化 ChatManager...")
	app.initChatManagerCLI()
	fmt.Println("[CLI] ChatManager 初始化完成")

	switch command {
	case "status":
		printSystemStatus(app)
	case "memory":
		printMemoryStatus(app)
	case "monitor":
		printMonitorStatus(app)
	case "dump-tasks":
		fmt.Println("\n📋 ===== 全局任务列表 =====")
		fmt.Println(app.GetGlobalTasks())
		fmt.Println()
	case "recover":
		forceRecoverTask(app)
	}

	fmt.Println("[CLI] 准备退出...")
	os.Exit(0)
}

func printSystemStatus(app *App) {
	if app.chatManager == nil {
		fmt.Println("❌ ChatManager 未初始化")
		return
	}

	status := app.chatManager.GetCMonitor().GetSystemStatus()

	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println("\n📊 ===== 系统完整状态 =====")
	fmt.Println(string(data))
}

func printMemoryStatus(app *App) {
	if app.chatManager == nil {
		fmt.Println("❌ ChatManager 未初始化")
		return
	}

	memoryStatus := app.chatManager.GetMemoryStatus()

	data, _ := json.MarshalIndent(memoryStatus, "", "  ")
	fmt.Println("\n🧠 ===== 记忆系统状态 =====")
	fmt.Println(string(data))

	if hasUnfinished, ok := memoryStatus["hasUnfinished"].(bool); ok && hasUnfinished {
		if lastTask, ok := memoryStatus["lastTask"].(map[string]interface{}); ok {
			fmt.Println("\n⚠️  发现未完成任务:")
			fmt.Printf("   📝 用户请求: %v\n", lastTask["userRequest"])
			fmt.Printf("   📋 任务描述: %v\n", lastTask["taskDescription"])
			fmt.Printf("   📊 当前状态: %v\n", lastTask["currentState"])
			fmt.Printf("   👤 当前角色: %v\n", lastTask["currentRole"])
			fmt.Printf("   💬 消息数量: %v\n", lastTask["messageCount"])
			fmt.Printf("   ⏰ 最后活跃: %v\n", lastTask["lastActiveTime"])
		}
	}
}

func printMonitorStatus(app *App) {
	if app.chatManager == nil || app.chatManager.GetCMonitor() == nil {
		fmt.Println("❌ C 监控未初始化")
		return
	}

	status := app.chatManager.GetCMonitor().GetSystemStatus()

	if monitorStatus, ok := status["monitor"].(map[string]interface{}); ok {
		data, _ := json.MarshalIndent(monitorStatus, "", "  ")
		fmt.Println("\n🐕 ===== C 监控状态 =====")
		fmt.Println(string(data))

		if running, ok := monitorStatus["running"].(bool); ok && running {
			fmt.Println("✅ C 监控运行中")
		} else {
			fmt.Println("❌ C 监控未运行")
		}
	}
}

func forceRecoverTask(app *App) {
	fmt.Println("\n🔄 [恢复] 开始强制恢复...")

	hasUnfinished, memory, err := app.chatManager.CheckUnfinishedTask()
	if err != nil {
		fmt.Printf("❌ 检查失败: %v\n", err)
		return
	}

	if !hasUnfinished || memory == nil {
		fmt.Println("ℹ️  没有未完成任务")
		return
	}

	fmt.Printf("📝 发现任务: %s\n", memory.TaskDescription)

	if err := app.chatManager.RecoverTask(memory); err != nil {
		fmt.Printf("❌ 恢复失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 恢复成功！恢复了 %d 条消息\n", len(memory.RecentMessages))
}