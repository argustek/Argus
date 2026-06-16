# Featherweight 直执 + MessageBus 全面审计清理

**日期**: 2026-06-15  
**涉及版本**: v0.9.2（合并后）

---

## 1. Featherweight PM 直执（Condition C）

### 背景

简单任务（hello world、fibonacci、单文件创建）走完 PM→SE→AP 全链路耗时过长。新增条件 C 检测，命中后由 PM 直接调用 LLM → write_file → exec，绕过 PM/SE/AP 管道。

### 改动

| 文件 | 改动 |
|------|------|
| `internal/core/argus.go` | `pmDirectExecute` 方法 — 1 次 LLM 调用 + 直接执行 write_file/exec |
| `internal/core/argus.go` | 条件 C 启发式检测（`isTinyTask` + `isRunTask`） |
| `internal/core/argus.go` | Step 4 emit: `c.emit("pm_to_user")` + `c.memory.Add(RolePM)` |
| `internal/chat/manager.go` | `bridgeBusyFunc` 字段 + `SetBridgeBusyChecker()` + `messageSender` 守卫 |
| `app.go` | `SetBridgeBusyChecker(a.bridge.IsProcessing)` 注入 |

### 关键设计

- `bridgeBusyFunc` 回调方式（非直接 Manager-Bridge 耦合），C Monitor 在 `handleToPM` 前检查
- short 任务完成后状态设为 `approved`（而非 `done`），跳过 C Monitor AP 审批触发

---

## 2. conversation.log 重构：统一走 Ack

### 原则

> conversation.log 只由 MessageBus.Ack 写入，保证「前端看到什么，日志记什么」。

### 问题

v0.9.2 合并后，`onCoreMessage` 对 `pm_to_user` 走了 `msgBus.Send`（依赖 Ack 写 log），但对非 pm_to_user 消息（se_to_user 等）仍走旧 `b.onMessage` 回调，且 `b.onMessage` 内使用 `PathCoreOutput`（`shouldTrack=false`），导致 SE/AP 消息既不追踪也不落日志。

### 改动

| # | 文件 | 问题 | 修复 |
|---|------|------|------|
| 1 | `bridge.go:onCoreMessage` | 为 pm_to_user 直接 `writeDebugLog("PM:…")`，与 Ack 重复 | 删除 |
| 2 | `bridge.go:handleToPM` | 直接 `writeDebugLog("USER:…")`，与 `PathUserInput` Ack 重复 | 删除 |
| 3 | `bridge.go` | `sendToMsgBus()` 定义但无人调用 | 删除死代码 |
| 4 | `app.go:b.onMessage` | 全部用 `PathCoreOutput` → 无追踪无日志 | 拆为 `PathSEToUser` / `PathAPToUser` / `PathSystem` |
| 5 | `message_bus.go:Ack` | PM case 被误改为 no-op | 恢复 `writeDebugLog` |

### 清理后的日志写入路径

```
emit → msgBus.Send(path=PathXxxToUser) → emitToFrontend → 前端
                                              ↓ Ack
                                       conversation.log
                                              ↑
                                    只在此处写 PM/SE/AP/USER
```

系统调试日志（`[Bridge-CTX]`、`[G-DEBUG]`、`SYS_C:` 等）仍由 `bridge.go` 直接 `writeDebugLog` 写入，不经过 Ack。

---

## 3. MessageBus 双推修复

### 问题

`manager.go:msgBusSend()` 同时调用 `msgBus.Send()`（内部已 `runtime.EventsEmit`）和直接 `runtime.EventsEmit()`，导致前端收到双倍事件。

### 改动

| 文件 | 问题 | 修复 |
|------|------|------|
| `internal/chat/manager.go:3489` | `msgBus.Send` 后重复 `runtime.EventsEmit` | 删除直接 emit，MessageBus 是唯一前向出口 |

---

## 4. 前端 PM 消息流式累加

### 问题

PM 分析输出经 `ProcessStream` 逐 chunk 回调，每个 chunk 独立 `pm_message` 事件 → 前端创建独立气泡，N 个 chunk 产生 N 条消息。

### 改动

| 文件 | 改动 |
|------|------|
| `frontend/src/App.vue` | `pm_message` 处理器从空实现改为累加模式：delta 追加到最后一条 PM 消息，不新建 |

实现逻辑：
1. 收到 `pm_message` → 查找 messages 中最后一条 `role === 'pm'` 的消息
2. 存在则追加 `content += data.delta`
3. 不存在则 push 新消息

---

## 5. 审计结果总览

### 最终架构

```
前端 ←── runtime.EventsEmit ── message_bus.go:211 (唯一出口)
                                      ↑
                               msgBus.Send() (唯一入口)
                               /      |       \
                     bridge.go  app.go  manager.go
                         
conversation.log ←── Ack ── message_bus.go:342-351 (唯一入口)
```

### 清理统计

| 类别 | 数量 |
|------|------|
| 直接 `writeDebugLog` 删除（用户内容） | 2 处 |
| 直接 `runtime.EventsEmit` 删除 | 1 处 |
| 不正确 MessagePath 修正 | 1 处（3 个角色分支） |
| 死代码删除 | 1 处 |
| Ack 路径恢复 | 1 处 |
| 前端推送路径统一 | 全部收敛到 `msgBus.Send` |

### 验证

- `wails build -skipbindings -f` 编译通过，`build/bin/argus-desktop.exe` 生成成功
- `go test ./...` 全项目 11 个包全部通过
- 3 轮 HTTP API hello world 回归测试：文件全部创建成功，conversation.log 有 USER 条目
- `--send` 模式因无桌面环境不可用，但通过 Go 单元测试覆盖了 auto-ACK 逻辑（见第 7 节）

---

## 6. 回归测试套件

### 新增文件

| 文件 | 类型 | 用途 |
|------|------|------|
| `internal/chat/messagebus_audit_test.go` | Go 单元测试 | MessageBus 架构合规性审计（12 项） |
| `end_to_end_messagebus_test.ps1` | PowerShell 端到端 | HTTP API 层回归 + conversation.log 一致性校验 |

### 审计测试清单（Go）

| 测试名称 | 验证内容 |
|----------|----------|
| `TestAllPathsHaveCorrectTracking` | 10 个 MessagePath 的 `shouldTrack` 配置正确 |
| `TestAckWritesLogForTrackedPaths` | PM/SE/AP/USER 可追踪路径 Send→Ack 后 log 正确写入 |
| `TestAckDoesNotWriteLogForUntrackedPaths` | PathCoreOutput/PathStatus 不追踪、不写 log |
| `TestNoDirectEventsEmitOutsideMessageBus` | 静态扫描：`runtime.EventsEmit` 只出现在 message_bus.go（白名单例外） |
| `TestNoDirectUserContentWriteDebugLog` | 静态扫描：`writeDebugLog("PM:/SE:/AP:/USER:")` 只出现在 Ack |
| `TestNoCoreOutputInTrackedPaths` | 静态扫描：`PathCoreOutput` 不出现在 emitToFrontend 调用中 |
| `TestPMToUserFullCycle` | PM 流式 delta 完整 Send→Ack→log 周期 |
| `TestUserInputFullCycle` | 用户输入 Send→Ack→log 周期 |
| `TestDuplicateAckReturnsFalse` | 重复 Ack 安全处理 |
| `TestAckForUnknownMsgIdReturnsFalse` | 未知 msgId 的 Ack 安全处理 |
| `TestSendRecordsTimestamp` | Send 记录正确时间戳 |
| `TestAllPathsReportedInStats` | GetStats 报告所有路径统计 |

### 运行方式

```powershell
# Go 单元审计（无需外部依赖）
cd E:\ArgusTek\Argus
go test ./internal/chat/ -run "TestAllPaths|TestAck|TestNoDirect|TestPMToUser|TestUserInput|TestDuplicate|TestAckForUnknown|TestSend|TestAllPathsReported" -v

# 全量测试
go test ./...

# 端到端测试（需要 HTTP 服务运行在 localhost:8080）
./end_to_end_messagebus_test.ps1

# Wails 编译验证
wails build -skipbindings -f
```

### 静态扫描白名单

`runtime.EventsEmit` 允许的 3 处（均在 `message_bus.go` + 1 处 fallback）：
1. `message_bus.go:211` — `emitToFrontend` 单一前向出口
2. `message_bus.go:524` — `backgroundChecker` 消息丢失告警
3. `app.go:974` — `emitToFrontend` fallback（msgBus=nil 时降级）
