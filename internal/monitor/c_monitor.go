package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"argus/internal/types"
)

// CMonitor C监控模块
type CMonitor struct {
	stateFile          string
	workDir            string
	checkInterval      time.Duration
	gitTimeout         time.Duration
	commitInterval     time.Duration // 自动commit间隔
	lastGitChange      int64
	lastAutoCommit     int64 // 上次自动commit时间
	noResponseCount    int
	pmRestarter        func() error // PM重启函数
	messageSender      func(string) // 消息发送函数（发给PM）
	alertFunc          func(string) // 弹框提醒函数
	onStateChange      func(int)    // 状态变化回调
	pmPingChan         chan bool    // PM响应通道
	mu                 sync.RWMutex
	running            bool
	stopChan           chan struct{}
	pmBusySince        int64
	seBusySince        int64
	lastSeChunkTime    int64         // SE最后一次收到流式chunk的时间
	noProgressTimeout  time.Duration // 无进展超时（默认2分钟）
	escalationTimeout  time.Duration // 升级超时（默认5分钟）
	pmBusyNotified2min bool          // PM busy 2分钟是否已通知（防止重复）
	pmEscalationCount  int           // PM busy 升级弹框次数（渐进式催促）
	alertedDone        bool
	alertedError       bool
	pmWaitingChecker   func() (bool, int64, int) // 检查PM是否等待USR决策
	pmWaitingNudger    func() int                // 催促USR(返回当前催促次数)
	notifyPM           func(string)              // 通知PM（C监控发现问题→告诉PM决策）

	// 状态查询依赖（用于 GetSystemStatus）
	chatManagerStatus   func() map[string]interface{} // ChatManager 状态查询
	memoryStatus        func() map[string]interface{} // 记忆系统状态查询
	idleAlertedTime     int64                         // 上次空闲提醒时间（去重）
	doubleIdleStartTime int64                         // 双空闲开始时间（PM和SE都idle时记录）
	// 时间感知 + 社交调度
	socialScheduler *SocialScheduler      // 社交调度器
	timeChecker     func() (int64, error) // 获取最后交互时间

	// 自动重试
	retryCallback func() error // 重置+重试回调
	retriedOnce   bool         // 本轮是否已重试过
	resetCount    int          // 本轮自动复位次数
	maxResetCount int          // 最大自动复位次数（默认3次）

	// [FIX-20260528-D] SE完成状态检测
	seCompletedChecker func() bool // 检查SE是否已报告完成任务
	workDirChecker     func() string // 获取工作目录（用于文件检测）

	// 交接安全网
	handoverStateGetter func() interface{}                   // 获取交接状态
	handoverClearer     func()                               // 清空交接状态
	handoverNudger      func() int                           // 增加催促次数
	handoverForcer      func()                               // 标记强制执行
	handoverForced      bool                                 // 是否已强制执行
	lastRoleMsgGetter   func(role string) string             // 获取角色最后消息
	handoverForceAction func(step string, forced bool) error // 强制执行交接回调
}

// NewCMonitor 创建C监控
func NewCMonitor(workDir string, pmRestarter func() error, messageSender func(string), alertFunc func(string)) *CMonitor {
	return &CMonitor{
		stateFile:         ".argus/state.json",
		workDir:           workDir,
		checkInterval:     30 * time.Second,
		gitTimeout:        300 * time.Second,
		noProgressTimeout: 360 * time.Second, // 6分钟无进展触发提醒
		escalationTimeout: 300 * time.Second, // 5分钟升级弹框
		commitInterval:    5 * time.Minute,   // 每5分钟自动commit
		lastGitChange:     time.Now().Unix(),
		lastAutoCommit:    time.Now().Unix(),
		noResponseCount:   0,
		pmRestarter:       pmRestarter,
		messageSender:     messageSender,
		alertFunc:         alertFunc,
		pmPingChan:        make(chan bool, 1),
		running:           false,
		stopChan:          make(chan struct{}),
		socialScheduler:   NewSocialScheduler(),
		maxResetCount:     3,
	}
}

// Start 启动监控
func (c *CMonitor) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	fmt.Println("[C] 监控启动")

	// 启动监控循环
	go c.monitorLoop()
}

// Stop 停止监控
func (c *CMonitor) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopChan)
	c.stopChan = make(chan struct{})
	fmt.Println("[C] 监控停止")
}

// monitorLoop 监控主循环
func (c *CMonitor) monitorLoop() {
	fmt.Println("[C] monitorLoop 启动")

	// 🚀 启动时立即执行第一次检查（不等30秒）
	fmt.Println("[C] ⚡ 立即执行首次检查")
	c.check()

	// 然后开始定期检查
	ticker := time.NewTicker(c.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Println("[C] ticker 触发")
			c.check()
		case <-c.stopChan:
			fmt.Println("[C] monitorLoop 停止")
			return
		}
	}
}

// check 执行一次检查
func (c *CMonitor) check() {
	fmt.Println("[C] 开始检查...")
	state, err := c.readState()
	if err != nil {
		fmt.Printf("[C] 读取状态失败: %v\n", err)
		return
	}
	fmt.Printf("[C] 状态: project_state=%d, pm=%s, se=%s\n", state.ProjectState, state.PmStatus, state.SeStatus)

	now := time.Now().Unix()

	c.autoCommit(now)

	switch state.ProjectState {
	case types.ProjectStateDone: // 2 = 项目完成
		c.handleProjectDone()

	case types.ProjectStateError: // 4 = 出错
		c.handleProjectError()

	case types.ProjectStateRunning: // 1 = 进行中
		c.handleProjectRunning(state, now)

	case types.ProjectStateIdle: // 0 = 无项目
	}

	// 🎭 社交互动检查（时间感知+智能寒暄）
	c.checkSocialInteraction(state, now)

	// 🔄 交接安全网检查
	c.checkHandoverTimeout(now)
}

// autoCommit 自动提交Git改动（每5分钟，有改动才提交）
func (c *CMonitor) autoCommit(now int64) {
	if now-c.lastAutoCommit < int64(c.commitInterval.Seconds()) {
		return
	}

	changedFiles := c.gitChangedFiles()
	if changedFiles == 0 {
		return
	}

	fmt.Printf("[C] 自动commit: 检测到 %d 个文件改动\n", changedFiles)

	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = c.workDir
	addCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := addCmd.CombinedOutput(); err != nil {
		fmt.Printf("[C] git add 失败: %v, output: %s\n", err, string(out))
		return
	}

	commitMsg := fmt.Sprintf("auto save by Argus-C (%s)", time.Now().Format("2006-01-02 15:04:05"))
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = c.workDir
	commitCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := commitCmd.CombinedOutput(); err != nil {
		output := string(out)
		if !strings.Contains(output, "nothing to commit") {
			fmt.Printf("[C] git commit 失败: %v, output: %s\n", err, output)
		}
		return
	}

	c.lastAutoCommit = now
	fmt.Println("[C] 自动commit成功:", commitMsg)
}

// handleProjectDone 项目完成处理
// 当 state=done(2) 时，硬编码强制等待AP审批，超时后C直接干预
func (c *CMonitor) handleProjectDone() {
	now := time.Now().Unix()

	// 第一次进入 done 状态：记录时间 + 硬编码触发AP审批
	if !c.alertedDone {
		c.alertedDone = true
		c.alertedError = false
		fmt.Println("[C] 项目已完成 (state=done)，硬编码触发AP审批...")
		c.alertFunc("项目已完成，强制启动AP审批流程")

		// 🔴 第一层硬编码保护：立即强制触发AP审批
		// C监控检测到done状态，直接调用notifyPM触发AP审批
		if c.notifyPM != nil {
			fmt.Println("[C] 🔴 硬编码：强制触发AP审批")
			c.notifyPM("[System] C监控检测到项目完成，强制触发AP审批流程")
		}
		return
	}

	// 🔴 第二层硬编码保护：done状态超过30秒，C直接强制完成（防卡死）
	state, err := c.readState()
	if err != nil {
		fmt.Printf("[C] 读取状态失败: %v\n", err)
		return
	}

	doneDuration := now - state.LastChange
	fmt.Printf("[C] done状态持续: %d秒\n", doneDuration)

	if doneDuration > 60 && !c.handoverForced {
		// 🔴 [FIX-20260528] 智能超时处理：根据交接状态决定推进策略
		var currentStep string = "unknown"

		if c.handoverStateGetter != nil {
			stateRaw := c.handoverStateGetter()
			if stateMap, ok := stateRaw.(map[string]interface{}); ok {
				if pending, ok := stateMap["pending"].(bool); ok && pending {
					if step, ok := stateMap["step"].(string); ok {
						currentStep = step
					}
				}
			}
		}

		fmt.Printf("[C] 🔴 done状态超过60秒，当前交接步骤: %s\n", currentStep)

		switch currentStep {
		case "se_to_pm":
			// SE已完成但PM未接手 → 强制触发PM审核
			fmt.Printf("[C] → 强制触发PM审核（SE→PM卡住）\n")
			c.handoverForced = true
			c.alertFunc("⚠️ SE完成但PM未响应，C强制触发PM审核")

			if c.handoverForceAction != nil {
				if err := c.handoverForceAction("se_to_pm", true); err != nil {
					fmt.Printf("[C] ❌ 强制触发PM审核失败: %v\n", err)
				}
			}

		case "pm_to_ap":
			// PM已approve但未移交AP → 强制交给AP审批
			fmt.Printf("[C] → 强制转交AP审批（PM→AP卡住）\n")
			c.handoverForced = true
			c.alertFunc("⚠️ PM已完成但未移交AP，C强制转交AP审批")

			if c.handoverForceAction != nil {
				if err := c.handoverForceAction("pm_to_ap", true); err != nil {
					fmt.Printf("[C] ❌ 强制转交AP失败: %v\n", err)
				}
			}

		case "ap_to_done", "unknown":
			// AP审批超时或无交接状态 → 强制通过（最后手段）
			fmt.Printf("[C] → AP审批超时或状态异常，强制通过（最后手段）\n")
			c.handoverForced = true
			c.alertFunc("🔴⚠️ AP审批超时，C强制通过（非正常流程！）")

			if c.handoverForceAction != nil {
				if err := c.handoverForceAction("ap_to_done", true); err != nil {
					fmt.Printf("[C] ❌ 强制推进到approved失败: %v\n", err)
				}
			}

		default:
			// 其他未知状态 → 安全起见，强制通过
			fmt.Printf("[C] → 未知交接状态(%s)，强制通过\n", currentStep)
			c.handoverForced = true
			c.alertFunc(fmt.Sprintf("🔴⚠️ 项目完成但状态异常(%s)，C强制通过", currentStep))

			if c.handoverForceAction != nil {
				if err := c.handoverForceAction("ap_to_done", true); err != nil {
					fmt.Printf("[C] ❌ 强制推进失败: %v\n", err)
				}
			}
		}
	}
}

// handleProjectError 项目出错处理
func (c *CMonitor) handleProjectError() {
	if c.alertedError {
		return
	}
	c.alertedError = true
	c.alertedDone = false
	fmt.Println("[C] 项目出错或需用户介入")
	c.alertFunc("项目出错或需用户介入，请检查")
}

// handleProjectRunning 项目进行中处理
func (c *CMonitor) handleProjectRunning(state types.State, now int64) {
	c.alertedDone = false
	c.alertedError = false

	c.mu.RLock()
	pmBusySince := c.pmBusySince
	seBusySince := c.seBusySince
	c.mu.RUnlock()

	// 条件1: 检查Git改动
	changedFiles := c.gitChangedFiles()
	fmt.Printf("[C] Git改动文件数: %d\n", changedFiles)
	if changedFiles > 0 {
		// 有改动，重置Git计时器
		c.lastGitChange = now
		fmt.Println("[C] 有Git改动，重置Git计时器")
	} else {
		// 无Git改动，检查是否超过2分钟
		if now-c.lastGitChange > int64(c.noProgressTimeout.Seconds()) {
			fmt.Println("[C] 2分钟无Git改动，提醒PM")
			c.messageSender("2分钟无Git改动，请检查任务是否正常推进")
			c.lastGitChange = now // 重置，避免重复提醒
		}
	}

	// 条件2: PM busy 超过2分钟 → silent notifyPM，超过5分钟 → 弹框USR
	if state.PmStatus == types.RoleStatusBusy && pmBusySince > 0 {
		pmBusyDuration := now - pmBusySince
		fmt.Printf("[C] PM busy持续时间: %d秒\n", pmBusyDuration)

		// 2分钟: silent通知PM（只发一次）
		if pmBusyDuration > int64(c.noProgressTimeout.Seconds()) && !c.pmBusyNotified2min {
			fmt.Println("[C] PM busy超过2分钟，silent通知PM")
			if c.notifyPM != nil {
				c.notifyPM("[C监控] 🔔 PM持续处理中超过2分钟，请确认进展正常")
			}
			c.mu.Lock()
			c.pmBusyNotified2min = true
			c.mu.Unlock()
		}

		// 渐进式升级催促（不重置 pmBusySince，用计数器控制）
		escalationInterval := int64(c.escalationTimeout.Seconds()) // 5分钟
		nextEscalation := escalationInterval + (int64(c.pmEscalationCount) * escalationInterval)

		if pmBusyDuration > nextEscalation {
			c.pmEscalationCount++

			switch c.pmEscalationCount {
			case 1:
				fmt.Println("[C] PM busy超过5分钟，第1次弹框提醒USR")
				c.alertFunc("🚨 PM已处理超过5分钟，请检查是否需要中断任务")
			case 2:
				fmt.Println("[C] PM busy超过10分钟，第2次弹框提醒USR")
				c.alertFunc("🔥 PM已处理超过10分钟，可能卡死！建议立即中断任务")
			default:
				minutes := (c.pmEscalationCount * 5)
				fmt.Printf("[C] PM busy超过%d分钟，第%d次弹框提醒USR\n", minutes, c.pmEscalationCount)
				c.alertFunc(fmt.Sprintf("💀 PM已处理超过%d分钟，严重异常！必须立即干预！", minutes))
			}
		}
	}

	// 条件3: SE busy → 只有没在收流式chunk时才检查超时
	if state.SeStatus == types.RoleStatusBusy && seBusySince > 0 {
		receivingChunk := c.lastSeChunkTime > seBusySince && (now-c.lastSeChunkTime) < 60
		if !receivingChunk {
			seBusyDuration := now - seBusySince
			fmt.Printf("[C] SE无chunk持续时间: %d秒\n", seBusyDuration)
			if seBusyDuration > int64(c.noProgressTimeout.Seconds()) {
			if c.CanAutoReset() && c.retryCallback != nil {
				c.resetCount++
				remaining := c.maxResetCount - c.resetCount
				fmt.Printf("[C] SE无进展超过6分钟，延迟10s后自动reset (第%d/%d次)\n", c.resetCount, c.maxResetCount)

			// 🔴 [FIX-20260528-D] 正确判断：通过SE完成状态+文件检测
				shouldForceRouteToPM := false
				forceStepName := ""

				// 方法1: SE完成状态检测器（最准确）
				if c.seCompletedChecker != nil && c.seCompletedChecker() {
					shouldForceRouteToPM = true
					forceStepName = "se_to_pm"
					fmt.Printf("[C] 🛡️ SE已报告完成(seReportedComplete=true)但仍在busy\n")
					} else if c.workDirChecker != nil {
					// 方法2: 文件存在性检测（备选方案）
					workDir := c.workDirChecker()
					if workDir != "" {
						// 检查工作目录是否有最近修改的文件（15分钟内）
						// 注意：SE完成任务到C监控触发可能需要6-10分钟
						files, err := os.ReadDir(workDir)
						if err == nil {
							recentFileCount := 0
							now := time.Now().Unix()
							for _, f := range files {
								if info, err := f.Info(); err == nil {
									modTime := info.ModTime().Unix()
									if now-modTime < 900 && !f.IsDir() { // 15分钟内修改的非目录文件 [FIX-时间窗口]
										recentFileCount++
									}
								}
							}
							if recentFileCount > 0 {
								shouldForceRouteToPM = true
								forceStepName = "se_to_pm"
								fmt.Printf("[C] 🛡️ [文件检测] 工作目录有%d个最近文件(15min窗口)，SE可能已完成\n", recentFileCount)
							}
						}
					}
				}

				if !shouldForceRouteToPM {
					fmt.Printf("[C] 📋 SE未完成或检测器不可用，准备执行正常reset\n")
				}

				time.Sleep(10 * time.Second)

				if shouldForceRouteToPM && c.handoverForceAction != nil {
					// ✅ SE已完成（项目done）→ 强制推进交接流程（不reset！）
					fmt.Printf("[C] 🔧 强制推进交接: %s (基于项目状态判断)\n", forceStepName)
					if err := c.handoverForceAction(forceStepName, true); err != nil {
						fmt.Printf("[C] ❌ 强制推进失败: %v，回退到reset\n", err)
						if err := c.retryCallback(); err != nil {
							fmt.Printf("[C] 自动重试失败: %v\n", err)
						}
					} else {
						fmt.Println("[C] ✅ 已强制推进交接流程（基于项目状态）")
						c.messageSender(fmt.Sprintf("[C自动重试] SE已完成但交接卡住，已强制推进到%s审核", forceStepName))
					}
				} else {
					// 原有逻辑：正常reset（SE未完成的情况）
					if err := c.retryCallback(); err != nil {
						fmt.Printf("[C] 自动重试失败: %v\n", err)
					} else {
						fmt.Println("[C] 自动重试完成")
					}
					c.retriedOnce = true
					c.mu.Lock()
					c.seBusySince = time.Now().Unix()
					c.lastSeChunkTime = 0
					c.mu.Unlock()
					c.messageSender(fmt.Sprintf("[C自动重试] SE卡住已自动reset (剩余%d次)，请PM重新分配任务", remaining))
				}
			} else {
					fmt.Println("[C] SE无进展超过6分钟，已达最大复位次数或无回调，提醒PM")
					c.messageSender("SE无进展超过6分钟，已达最大自动复位次数，请手动处理")
					c.mu.Lock()
					c.seBusySince = now
					c.lastSeChunkTime = 0
					c.mu.Unlock()
				}
			}
		}
	}

	// 条件4: PM和SE都idle但项目进行中（从双空闲开始计时60秒）
	if state.PmStatus == types.RoleStatusIdle && state.SeStatus == types.RoleStatusIdle {
		// 记录双空闲开始时间
		if c.doubleIdleStartTime == 0 {
			c.doubleIdleStartTime = now
			fmt.Printf("[C] PM和SE都变为空闲，开始计时: %d\n", now)
		}
		// 从双空闲开始超过60秒才提醒
		if now-c.doubleIdleStartTime > 60 {
			if now-c.idleAlertedTime > 60 { // 去重：最多每60秒提醒一次
				fmt.Printf("[C] 项目进行中但PM和SE都空闲超过%d秒\n", now-c.doubleIdleStartTime)
				if c.notifyPM != nil {
					c.notifyPM("[C监控] ⚠️ 项目进行中但你和SE都空闲，请检查")
				}
				c.idleAlertedTime = now
			}
		}
	} else {
		// 不是双空闲状态，重置计时
		if c.doubleIdleStartTime != 0 {
			fmt.Printf("[C] PM或SE不再空闲，重置双空闲计时\n")
			c.doubleIdleStartTime = 0
		}
	}

	// 条件5: PM等待USR决策超时催促（三级催促）
	if c.pmWaitingChecker != nil {
		waiting, waitingSince, nudgeCount := c.pmWaitingChecker()
		if waiting && waitingSince > 0 {
			waitDuration := now - waitingSince
			fmt.Printf("[C] PM等待USR决策已: %d秒, 已催促: %d次\n", waitDuration, nudgeCount)

			if waitDuration > 180 && nudgeCount < 3 {
				count := c.pmWaitingNudger()
				if count <= 3 {
					if c.notifyPM != nil {
						c.notifyPM("[C监控] ⚠️ PM等待USR决策已超过3分钟，请决定：继续等待/自动选择默认方案/中止任务")
					}
					c.alertFunc("🚨 PM 等待您决策已超过3分钟，请立即回复！")
				}
			} else if waitDuration > 120 && nudgeCount < 2 {
				count := c.pmWaitingNudger()
				if count <= 2 {
					if c.notifyPM != nil {
						c.notifyPM("[C监控] ⚠️ PM等待USR决策已超过2分钟")
					}
					c.alertFunc("⚠️ PM 仍在等您决策（已2分钟），请尽快回复")
				}
			} else if waitDuration > 60 && nudgeCount < 1 {
				count := c.pmWaitingNudger()
				if count <= 1 {
					if c.notifyPM != nil {
						c.notifyPM("[C监控] 🔔 PM等待USR决策已超过1分钟")
					}
					c.messageSender("@USR 🔔 PM正在等您做决策，请在聊天窗口回复")
				}
			}
		}
	}

	// 健康检查PM
	if !c.pingPM() {
		c.noResponseCount++
		fmt.Printf("[C] PM无响应 (%d/3)\n", c.noResponseCount)
		if c.noResponseCount >= 3 {
			fmt.Println("[C] 重启PM...")
			if err := c.pmRestarter(); err != nil {
				fmt.Printf("[C] 重启PM失败: %v\n", err)
			} else {
				fmt.Println("[C] PM已重启")
				c.noResponseCount = 0
			}
		}
	} else {
		c.noResponseCount = 0
	}
}

// ReadState 读取状态文件（公开方法，供外部调用）
func (c *CMonitor) ReadState() (types.State, error) {
	return c.readState()
}

// readState 读取状态文件
func (c *CMonitor) readState() (types.State, error) {
	data, err := os.ReadFile(c.stateFile)
	if err != nil {
		// 文件不存在返回默认状态
		return types.State{
			ProjectState: types.ProjectStateIdle,
			PmStatus:     types.RoleStatusIdle,
			SeStatus:     types.RoleStatusIdle,
			LastChange:   time.Now().Unix(),
		}, nil
	}

	var state types.State
	if err := json.Unmarshal(data, &state); err != nil {
		return types.State{}, err
	}

	return state, nil
}

// writeStateLocked 写入状态文件（内部使用，调用者已持有锁）
func (c *CMonitor) writeStateLocked(state types.State) error {
	os.MkdirAll(".argus", 0755)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.stateFile, data, 0644); err != nil {
		return err
	}
	if c.onStateChange != nil {
		c.onStateChange(state.ProjectState)
	}
	return nil
}

// WriteState 写入状态文件（供外部调用）
func (c *CMonitor) WriteState(state types.State) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeStateLocked(state)
}

// ResetSessionState 重置会话状态（关机/新会话时调用）
func (c *CMonitor) ResetSessionState() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.readState()
	if err != nil {
		return err
	}

	state.ProjectState = types.ProjectStateIdle
	state.PmStatus = types.RoleStatusIdle
	state.SeStatus = types.RoleStatusIdle
	state.LastUserMessage = ""
	state.LastInteractionTime = 0
	return c.writeStateLocked(state)
}

// ClearLastUserMessage 清除最后用户消息（新会话开始时调用）
func (c *CMonitor) ClearLastUserMessage() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.readState()
	if err != nil {
		return err
	}

	state.LastUserMessage = ""
	return c.writeStateLocked(state)
}

// SaveLastUserMessage 保存最后一条用户消息（用于智能恢复）
func (c *CMonitor) SaveLastUserMessage(msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.readState()
	if err != nil {
		return err
	}

	state.LastUserMessage = msg
	return c.writeStateLocked(state)
}

// GetLastUserMessage 获取最后一条用户消息
func (c *CMonitor) GetLastUserMessage() (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, err := c.readState()
	if err != nil {
		return "", err
	}

	return state.LastUserMessage, nil
}

// SaveLastInteractionTime 保存最后交互时间
func (c *CMonitor) SaveLastInteractionTime(t int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.readState()
	if err != nil {
		return err
	}

	state.LastInteractionTime = t
	return c.writeStateLocked(state)
}

// GetLastInteractionTime 获取最后交互时间
func (c *CMonitor) GetLastInteractionTime() (int64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, err := c.readState()
	if err != nil {
		return 0, err
	}

	return state.LastInteractionTime, nil
}

// SetTimeChecker 设置时间检查器回调
func (c *CMonitor) SetTimeChecker(checker func() (int64, error)) {
	c.timeChecker = checker
}

// checkSocialInteraction 检查是否需要进行社交互动
func (c *CMonitor) checkSocialInteraction(state types.State, now int64) {
	if c.socialScheduler == nil || c.timeChecker == nil {
		return
	}

	// 如果任务进行中，不干扰工作
	if state.ProjectState == types.ProjectStateRunning &&
		(state.PmStatus == "busy" || state.SeStatus == "busy") {
		return
	}

	// 获取最后交互时间
	lastTime, err := c.timeChecker()
	if err != nil || lastTime == 0 {
		return
	}

	// 生成时间上下文
	ctx := GenerateTimeContext(lastTime)

	// 重置每日问候状态（新的一天）
	today := time.Unix(now, 0).Format("2006-01-02")
	c.socialScheduler.ResetDailyGreeting(today)

	// 判断是否该社交了
	shouldSocialize, trigger := c.socialScheduler.ShouldSocialize(ctx, now)
	if !shouldSocialize {
		return
	}

	// 生成社交消息
	message := PickSocialMessage(trigger, ctx)

	fmt.Printf("[C-Social] 🎭 触发社交: %s | 消息: %s\n", trigger, message)

	// 发送社交消息（标记为系统社交）
	c.messageSender(fmt.Sprintf("@USR %s", message))

	// 更新最后社交时间
	c.socialScheduler.UpdateLastSocial(trigger, now)
}

// gitChangedFiles 获取Git改动文件数
func (c *CMonitor) gitChangedFiles() int {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = c.workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	// 计算换行符数量
	if len(out) == 0 {
		return 0
	}
	return bytes.Count(out, []byte("\n"))
}

const (
	handoverNudge1Interval = 15  // 第1次催促: 15秒
	handoverNudge2Interval = 30  // 第2次催促: 30秒
	handoverNudge3Interval = 60  // 第3次催促: 1分钟
	handoverForceInterval  = 90  // 强制执行: 1.5分钟(3次催促后)
	maxHandoverNudges      = 3   // 最大催促次数
)

// checkHandoverTimeout 检查交接超时（三层安全网）
func (c *CMonitor) checkHandoverTimeout(now int64) {
	if c.handoverStateGetter == nil {
		return
	}

	stateRaw := c.handoverStateGetter()
	stateMap, ok := stateRaw.(map[string]interface{})
	if !ok {
		return
	}

	pending := false
	if v, ok := stateMap["pending"].(bool); ok {
		pending = v
	}
	if !pending {
		return
	}

	var step string
	if v, ok := stateMap["step"].(string); ok {
		step = v
	}

	var since int64
	if v, ok := stateMap["since"].(float64); ok {
		since = int64(v)
	}

	var nudgeCount int
	if v, ok := stateMap["nudge_count"].(float64); ok {
		nudgeCount = int(v)
	}

	var forced bool
	if v, ok := stateMap["forced"].(bool); ok {
		forced = v
	}

	if forced {
		return
	}

	waitDuration := now - since

	switch {
	case waitDuration > handoverForceInterval:
		c.forceHandover(step)

	case waitDuration > handoverNudge3Interval && nudgeCount < maxHandoverNudges:
		c.nudgeHandover(step, 3)

	case waitDuration > handoverNudge2Interval && nudgeCount < 2:
		c.nudgeHandover(step, 2)

	case waitDuration > handoverNudge1Interval && nudgeCount < 1:
		c.nudgeHandover(step, 1)
	}
}

func (c *CMonitor) nudgeHandover(step string, level int) {
	if c.handoverNudger == nil || c.notifyPM == nil {
		return
	}

	count := c.handoverNudger()

	var msg string
	var stepDesc string

	switch step {
	case "se_to_pm":
		stepDesc = "SE→PM交接"
	case "pm_to_ap":
		stepDesc = "PM→AP交接"
	case "ap_to_done":
		stepDesc = "AP→完成交接"
	default:
		stepDesc = step
	}

	switch level {
	case 1:
		msg = fmt.Sprintf("⚠️ [C监控] 待办提醒: %s 已等待，请及时处理", stepDesc)
	case 2:
		msg = fmt.Sprintf("⚠️⚠️ [C监控] %s 已等待较久，请尽快处理！", stepDesc)
	case 3:
		msg = fmt.Sprintf("🔴 [C监控] 最后警告！%s 超时，即将强制执行", stepDesc)
	}

	fmt.Printf("[C-Handover] 催促第%d次: %s\n", count+1, msg)
	c.notifyPM(msg)
}

func (c *CMonitor) forceHandover(step string) {
	if c.handoverForcer == nil || c.handoverForceAction == nil {
		return
	}

	c.handoverForced = true

	var actionMsg string
	var stepDesc string

	switch step {
	case "se_to_pm":
		stepDesc = "SE→PM"
		actionMsg = "[C强制] SE已完成但PM未接手，C强制触发PM审核"
	case "pm_to_ap":
		stepDesc = "PM→AP"
		actionMsg = "[C强制] PM已approve但未移交AP，C强制触发AP审批"
	case "ap_to_done":
		stepDesc = "AP→完成"
		actionMsg = "[C强制⚠️] AP审批超时，C强制通过（非正常流程！）"
	default:
		stepDesc = step
		actionMsg = fmt.Sprintf("[C强制] %s 超时，C强制执行", step)
	}

	fmt.Printf("[C-Handover] 🔴 强制执行: %s - %s\n", stepDesc, actionMsg)

	if err := c.handoverForceAction(step, true); err != nil {
		fmt.Printf("[C-Handover] ❌ 强制执行失败: %v\n", err)
		c.notifyPM(fmt.Sprintf("❌ [C监控] %s 强制执行失败: %v", stepDesc, err))
		return
	}

	if step == "ap_to_done" {
		c.alertFunc(fmt.Sprintf("🔴🔴🔴 C强制通过警告！\n\n%s\n\n这不是正常AP审批结果！", actionMsg))
	} else {
		c.notifyPM(actionMsg)
	}
}

// pingPM 健康检查PM（发送ping消息，等待PM回复）
func (c *CMonitor) pingPM() bool {
	// 发送ping消息给PM（只发钉钉，不走聊天流程）
	fmt.Println("[C] 发送健康检查ping给PM")

	// 等待PM回复（5秒超时）
	select {
	case <-c.pmPingChan:
		// 收到PM回复
		return true
	case <-time.After(5 * time.Second):
		// 超时未回复
		return false
	}
}

// PongPM PM回复健康检查（供PM调用）
func (c *CMonitor) PongPM() {
	select {
	case c.pmPingChan <- true:
		// 发送成功
	default:
		// 通道已满，丢弃
	}
}

// UpdatePmStatus 更新PM状态（供PM调用）
func (c *CMonitor) UpdatePmStatus(status string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.readState()
	if err != nil {
		return err
	}

	// 记录进入busy状态的时间
	if status == types.RoleStatusBusy && state.PmStatus != types.RoleStatusBusy {
		c.pmBusySince = time.Now().Unix()
		c.pmBusyNotified2min = false
		c.pmEscalationCount = 0
		fmt.Printf("[C] PM进入busy状态，时间: %d\n", c.pmBusySince)
	}

	// PM变为idle时重置催促状态
	if status == types.RoleStatusIdle && state.PmStatus != types.RoleStatusIdle {
		c.pmBusySince = 0
		c.pmBusyNotified2min = false
		c.pmEscalationCount = 0
	}

	state.PmStatus = status
	state.LastChange = time.Now().Unix()

	return c.writeStateLocked(state)
}

// UpdateSeStatus 更新SE状态（供SE调用）
func (c *CMonitor) UpdateSeStatus(status string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.readState()
	if err != nil {
		return err
	}

	// 记录进入busy状态的时间
	if status == types.RoleStatusBusy && state.SeStatus != types.RoleStatusBusy {
		c.seBusySince = time.Now().Unix()
		fmt.Printf("[C] SE进入busy状态，时间: %d\n", c.seBusySince)
	}

	state.SeStatus = status
	state.LastChange = time.Now().Unix()

	return c.writeStateLocked(state)
}

// GetSeStatus 获取SE当前状态
func (c *CMonitor) GetSeStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.readState()
	if err != nil {
		return types.RoleStatusIdle
	}
	return state.SeStatus
}

// UpdateSeChunkTime 更新SE最后一次流式chunk时间（由Manager在流式回调中调用）
func (c *CMonitor) UpdateSeChunkTime() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeChunkTime = time.Now().Unix()
}

// UpdateProjectState 更新项目状态（供PM调用）
func (c *CMonitor) UpdateProjectState(projectState int) error {
	state, err := c.readState()
	if err != nil {
		return err
	}

	state.ProjectState = projectState
	state.LastChange = time.Now().Unix()

	err = c.WriteState(state)
	if err != nil {
		return err
	}

	if c.onStateChange != nil {
		c.onStateChange(projectState)
	}

	return nil
}

// SetWorkDir 更新工作目录
func (c *CMonitor) SetWorkDir(workDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workDir = workDir
}

// IsRunning 检查监控是否正在运行
func (c *CMonitor) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// SetOnStateChange 设置状态变化回调
func (c *CMonitor) SetOnStateChange(callback func(int)) {
	c.onStateChange = callback
}

func (c *CMonitor) SetPMWaitingCallbacks(checker func() (bool, int64, int), nudger func() int) {
	c.pmWaitingChecker = checker
	c.pmWaitingNudger = nudger
}

// SetNotifyPM 设置通知PM回调（C监控发现问题→告诉PM决策）
func (c *CMonitor) SetNotifyPM(callback func(string)) {
	c.notifyPM = callback
}

// SetRetryCallback 设置自动重试回调（C检测到卡死→reset+重试）
func (c *CMonitor) SetRetryCallback(cb func() error) {
	c.retryCallback = cb
}

// SetHandoverCallbacks 设置交接安全网回调
func (c *CMonitor) SetHandoverCallbacks(
	stateGetter func() interface{},
	clearer func(),
	nudger func() int,
	forcer func(),
	lastRoleMsgGetter func(role string) string,
	forceAction func(step string, forced bool) error,
) {
	c.handoverStateGetter = stateGetter
	c.handoverClearer = clearer
	c.handoverNudger = nudger
	c.handoverForcer = forcer
	c.lastRoleMsgGetter = lastRoleMsgGetter
	c.handoverForceAction = forceAction
}

// ResetRetryFlag 重置重试标记（新任务开始时调用）
func (c *CMonitor) ResetRetryFlag() {
	c.retriedOnce = false
	c.resetCount = 0
}

func (c *CMonitor) CanAutoReset() bool {
	return c.resetCount < c.maxResetCount
}

func (c *CMonitor) GetProjectState() int {
	state, err := c.readState()
	if err != nil {
		return types.ProjectStateIdle
	}
	return state.ProjectState
}

// SetStatusProviders 设置状态查询依赖（用于 GetSystemStatus）
func (c *CMonitor) SetStatusProviders(chatManagerStatus, memoryStatus func() map[string]interface{}) {
	c.chatManagerStatus = chatManagerStatus
	c.memoryStatus = memoryStatus
	fmt.Println("[C] ✅ 状态查询依赖已设置")
}

// SetSECompletedChecker [FIX-20260528-D] 设置SE完成状态检查器
func (c *CMonitor) SetSECompletedChecker(checker func() bool) {
	c.seCompletedChecker = checker
	fmt.Println("[C] ✅ SE完成状态检测器已设置")
}

// SetWorkDirChecker [FIX-20260528-D] 设置工作目录检查器
func (c *CMonitor) SetWorkDirChecker(checker func() string) {
	c.workDirChecker = checker
	fmt.Println("[C] ✅ 工作目录检测器已设置")
}

// GetSystemStatus 获取系统完整状态（C的核心能力：实时监控所有模块）
func (c *CMonitor) GetSystemStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := make(map[string]interface{})

	// 1. C 自身状态
	status["monitor"] = map[string]interface{}{
		"running":         c.running,
		"checkInterval":   c.checkInterval.String(),
		"lastGitChange":   c.lastGitChange,
		"lastAutoCommit":  c.lastAutoCommit,
		"noResponseCount": c.noResponseCount,
		"pmBusySince":     c.pmBusySince,
		"seBusySince":     c.seBusySince,
		"alertedDone":     c.alertedDone,
		"alertedError":    c.alertedError,
		"timestamp":       time.Now().Unix(),
	}

	// 2. ChatManager 状态（通过回调获取）
	if c.chatManagerStatus != nil {
		status["chatManager"] = c.chatManagerStatus()
	} else {
		status["chatManager"] = map[string]interface{}{
			"error": "状态查询依赖未设置",
		}
	}

	// 3. 记忆系统状态（通过回调获取）
	if c.memoryStatus != nil {
		status["memory"] = c.memoryStatus()
	} else {
		status["memory"] = map[string]interface{}{
			"error": "状态查询依赖未设置",
		}
	}

	return status
}
