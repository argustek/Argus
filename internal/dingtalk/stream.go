package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"argus/internal/i18n"
)

// StreamConfig Stream模式配置
type StreamConfig struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

var logDir = ".argus"

func SetLogDir(dir string) {
	logDir = dir
}

var (
	streamClient       *client.StreamClient
	streamCancel       context.CancelFunc
	messageHandler     func(content string, sender string)
	lastSenderID       string // 记录最后发送消息的钉钉用户ID
	lastConversationID string // 记录最后发送消息的会话ID
)

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// InitStream 初始化Stream模式
func InitStream(cfg StreamConfig, handler func(content string, sender string)) {
	if !cfg.Enabled {
		fmt.Println("钉钉 Stream 未启用")
		return
	}

	fmt.Printf("钉钉 Stream 配置: ClientID=%s, Secret长度=%d\n", cfg.ClientID, len(cfg.ClientSecret))
	logToFile(fmt.Sprintf("钉钉 Stream 配置: ClientID=%s, Secret长度=%d", cfg.ClientID, len(cfg.ClientSecret)))

	// 保存配置供发送消息使用
	SetStreamConfig(&cfg)

	messageHandler = handler
	ctx, cancel := context.WithCancel(context.Background())
	streamCancel = cancel

	// 创建 Stream 客户端
	streamClient = client.NewStreamClient(
		client.WithAppCredential(client.NewAppCredentialConfig(cfg.ClientID, cfg.ClientSecret)),
	)

	// 注册机器人消息回调
	streamClient.RegisterChatBotCallbackRouter(onChatBotMessage)

	// 启动客户端（带自动重连和 panic 恢复）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logToFile(fmt.Sprintf("【Stream】捕获到 panic: %v", r))
				fmt.Printf("钉钉 Stream 捕获到 panic: %v\n", r)
			}
		}()
		
		retryCount := 0
		maxRetries := 100 // 最大重试次数
		
		for retryCount < maxRetries {
			fmt.Printf("钉钉 Stream 客户端启动... (尝试 %d/%d)\n", retryCount+1, maxRetries)
			logToFile(fmt.Sprintf("钉钉 Stream 客户端启动... (尝试 %d/%d)", retryCount+1, maxRetries))
			logToFile(fmt.Sprintf("ClientID: %s", cfg.ClientID))
			
			// 添加启动前的日志
			logToFile("准备调用 streamClient.Start...")
			
			// 在单独的 goroutine 中启动客户端，并捕获 panic
			startErr := make(chan error, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logToFile(fmt.Sprintf("【Stream】Start 方法 panic: %v", r))
						startErr <- fmt.Errorf("panic: %v", r)
					}
				}()
				startErr <- streamClient.Start(ctx)
			}()
			
			var err error
			select {
			case err = <-startErr:
				// 正常返回或 panic
			case <-ctx.Done():
				logToFile(fmt.Sprintf("Stream 上下文被取消，停止重连: %v", ctx.Err()))
				return
			}
			
			if err != nil {
				fmt.Printf("钉钉 Stream 客户端错误: %v\n", err)
				logToFile(fmt.Sprintf("钉钉 Stream 客户端错误: %v", err))
				
				// 检查上下文是否被取消
				select {
				case <-ctx.Done():
					logToFile(fmt.Sprintf("Stream 上下文被取消，停止重连: %v", ctx.Err()))
					return
				default:
					// 上下文未取消，等待后重试
					retryCount++
					waitTime := time.Duration(min(retryCount*5, 60)) * time.Second
					logToFile(fmt.Sprintf("等待 %v 后重连...", waitTime))
					time.Sleep(waitTime)
					
					// 重新创建客户端
					streamClient = client.NewStreamClient(
						client.WithAppCredential(client.NewAppCredentialConfig(cfg.ClientID, cfg.ClientSecret)),
					)
					streamClient.RegisterChatBotCallbackRouter(onChatBotMessage)
					continue
				}
			}
			
			logToFile("钉钉 Stream 客户端已启动，等待上下文取消...")
			
			// 阻塞等待上下文取消
			<-ctx.Done()
			logToFile(fmt.Sprintf("Stream 上下文被取消: %v", ctx.Err()))
			return
		}
		
		logToFile("达到最大重试次数，停止重连")
	}()
}

// onChatBotMessage 处理机器人消息
func onChatBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	fmt.Printf("【Stream】收到钉钉消息: %s from %s\n", data.Text.Content, data.SenderStaffId)
	logToFile(fmt.Sprintf("【Stream】收到钉钉消息: %s from %s", data.Text.Content, data.SenderStaffId))

	// 记录发送者ID和会话ID，用于回复
	lastSenderID = data.SenderStaffId
	lastConversationID = data.ConversationId
	logToFile(fmt.Sprintf("【Stream】记录发送者ID: %s, 会话ID: %s", lastSenderID, lastConversationID))

	// 调用消息处理器
	if messageHandler != nil {
		fmt.Println("【Stream】调用messageHandler")
		logToFile("【Stream】调用messageHandler")
		messageHandler(data.Text.Content, data.SenderStaffId)
		fmt.Println("【Stream】messageHandler调用完成")
		logToFile("【Stream】messageHandler调用完成")
	} else {
		fmt.Println("【Stream】messageHandler为nil")
		logToFile("【Stream】messageHandler为nil")
	}

	return []byte(""), nil
}

// logToFile 记录日志到文件
func logToFile(message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logLine := fmt.Sprintf("[钉钉日志] %s: %s\n", timestamp, message)
	fmt.Print(logLine)
	
	// 写入日志文件
	logFile := logDir + "/dingtalk.log"
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(logLine)
}

// StopStream 停止Stream连接
func StopStream() {
	if streamCancel != nil {
		streamCancel()
	}
	if streamClient != nil {
		streamClient.Close()
	}
}

// SendMessage 发送消息到钉钉私聊
func SendMessage(userID string, content string) error {
	// 使用 OpenAPI 发送消息
	// 需要 AccessToken，先获取
	return sendMessageViaOpenAPI(userID, content)
}

// AccessToken 缓存
type accessTokenCache struct {
	token     string
	expiresAt time.Time
}

var tokenCache *accessTokenCache

// getAccessToken 获取钉钉 AccessToken
func getAccessToken(clientID, clientSecret string) (string, error) {
	// 检查缓存
	if tokenCache != nil && time.Now().Before(tokenCache.expiresAt) {
		return tokenCache.token, nil
	}

	// 调用钉钉接口获取 AccessToken
	url := fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s", clientID, clientSecret)
	
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf(i18n.T("err.dingtalk_token"), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf(i18n.T("err.dingtalk_read_resp"), err)
	}

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf(i18n.T("err.dingtalk_parse_resp"), err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf(i18n.T("err.dingtalk_token_err"), result.ErrMsg)
	}

	// 缓存 Token，提前5分钟过期
	tokenCache = &accessTokenCache{
		token:     result.AccessToken,
		expiresAt: time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second),
	}

	logToFile(fmt.Sprintf("获取 AccessToken 成功，有效期 %d 秒", result.ExpiresIn))
	return result.AccessToken, nil
}

// sendMessageViaOpenAPI 通过 OpenAPI 发送消息（使用批量发送API）
func sendMessageViaOpenAPI(userID string, content string) error {
	// 获取配置
	cfg := getStreamConfig()
	if cfg == nil {
		return fmt.Errorf(i18n.T("err.dingtalk_config"))
	}

	// 获取 AccessToken
	accessToken, err := getAccessToken(cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return fmt.Errorf(i18n.T("err.dingtalk_token"), err)
	}

	// 使用批量发送 API（测试成功的API）
	// https://open.dingtalk.com/document/development/chatbots-send-one-on-one-chat-messages-in-batches
	url := "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"

	// msgParam 必须是 JSONString 格式
	msgParamJSON, _ := json.Marshal(map[string]string{
		"content": content,
	})

	requestBody := map[string]interface{}{
		"robotCode": cfg.ClientID,
		"userIds":   []string{userID},
		"msgKey":    "sampleText",
		"msgParam":  string(msgParamJSON),
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf(i18n.T("err.dingtalk_build_req"), err)
	}

	logToFile(fmt.Sprintf("发送消息请求: URL=%s, Body=%s", url, string(jsonBody)))

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf(i18n.T("err.dingtalk_create_req"), err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", accessToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf(i18n.T("err.dingtalk_send_req"), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf(i18n.T("err.dingtalk_read_resp"), err)
	}

	logToFile(fmt.Sprintf("发送消息响应: %s", string(body)))

	// 解析钉钉错误码
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	json.Unmarshal(body, &result)

	if result.ErrCode != 0 {
		return fmt.Errorf(i18n.T("err.dingtalk_api_error"), result.ErrCode, result.ErrMsg)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(i18n.T("err.dingtalk_http_error"), resp.StatusCode, string(body))
	}

	logToFile(fmt.Sprintf("发送消息成功，用户 %s", userID))
	return nil
}

// StreamConfig 指针，用于获取配置
var streamConfig *StreamConfig

// SetStreamConfig 设置 Stream 配置
func SetStreamConfig(cfg *StreamConfig) {
	streamConfig = cfg
}

// getStreamConfig 获取 Stream 配置
func getStreamConfig() *StreamConfig {
	return streamConfig
}

// GetLastSenderID 获取最后发送消息的钉钉用户ID
func GetLastSenderID() string {
	return lastSenderID
}

// SendMessageToLastSender 发送消息给最后发送者
func SendMessageToLastSender(content string) error {
	senderID := lastSenderID
	if senderID == "" {
		// 如果没有记录发送者，使用默认用户（师兄的钉钉ID）
		senderID = "6650602729107294"
		logToFile(fmt.Sprintf("⚠️ 没有记录发送者，使用默认用户: %s", senderID))
	} else {
		logToFile(fmt.Sprintf("✓ 使用记录的发送者: %s", senderID))
	}
	
	// 截断过长消息用于日志
	contentPreview := content
	if len(content) > 100 {
		contentPreview = content[:100] + "..."
	}
	logToFile(fmt.Sprintf("发送消息内容预览: %s", contentPreview))
	
	return SendMessage(senderID, content)
}
