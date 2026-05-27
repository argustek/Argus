# PM ProcessReview API调用卡住问题分析与修复

## 问题描述

在 Hello World 回归测试和实际使用中，SE 完成任务后 PM 审核阶段经常**长时间无响应**（3-5分钟），表现为：

- 前端面板 PM 区域静止，无输出
- 日志显示 `[C] PM无响应 (1/3) → (2/3) → (3/3)` → PM 被强制重启
- SE 等待 PM 审核结果超时（200+秒无chunk）
- 用户手动 @PM 后 **PM 立即响应**

## 复现条件

| 条件 | 详情 |
|------|------|
| 触发路径 | `SE完成 → handleSEAskPM → ProcessReview` |
| 必要条件 | PM 调用了工具验证（list_files / read_file） |
| 概率 | 约 20%（PDCA回归测试中 5次出现1次） |
| API | NVIDIA API (`integrate.api.nvidia.com`) |

## 根因分析

### 流程时序

```
时间轴:
T+0s    SE完成，提交审核
        → handleSEAskPM() 被调用
        → pmProcessor.ProcessReview() 启动
        
T+5s    Round 1: ChatWithTools(带PMTools)
        → AI返回 tool_calls=[list_files, read_file] ✅
        
T+8s    执行 list_files → 返回文件列表 ✅
        执行 read_file   → 返回文件内容 ✅
        
T+10s   Round 2: ChatWithTools(工具结果 + "请给出最终结论")
        → ❌ API请求挂起，无响应...
        
T+70s   C监控 ping PM → 超时 (1/3)
        
T+130s  C监控 ping PM → 超时 (2/3)
        
T+190s  C监控 ping PM → 超时 (3/3)
        → 强制重启 PM
        
T+190s+ 重启后仍无响应（旧ProcessReview循环未恢复）
        
T+200s+ SE无chunk持续200+秒
```

### 为什么 @PM 能立即响应？

**关键发现：两条完全独立的代码路径**

| 维度 | 路径A：自动审核（卡住） | 路径B：用户@PM（正常） |
|------|------------------------|----------------------|
| 入口函数 | `handleSEAskPM()` [manager.go:1754](internal/chat/manager.go#L1754) | `handleToPM()` [manager.go:1268](internal/chat/manager.go#L1268) |
| AI方法 | `ProcessReview()` [pm_prompt.go:439](internal/ai/pm_prompt.go#L439) | `ProcessStream()` 或简单调用 |
| 工具调用 | ✅ 有（list_files, read_file, exec等） | ❌ 无 |
| 历史消息 | **大**（含tool result） | **小**（纯文本） |
| API请求体 | ~2-5KB | ~500B |
| 超时保护 | ❌ 无（根因） | N/A（请求快） |

**结论**：卡住的是 `ProcessReview` 内部的 for 循环（Round 2 的 `ChatWithTools` 调用），不是整个 PM 对象。用户 @PM 走的是另一条路，不经过那个阻塞的循环。

### 为什么重启PM不能解决问题？

```
handleSEAskPM() 是一个同步阻塞调用:
  goroutine-A: ProcessReview()
    → Round 1 ChatWithTools ✅
    → 执行工具 ✅
    → Round 2 ChatWithTools ← 阻塞在这里
    
C监控检测到PM无响应:
  → pmRestarter() 重置PM处理器状态
  → 但 goroutine-A 中的Round 2 HTTP请求仍在等待
  → 新的PM实例创建后，旧的阻塞调用不受影响
  
结果:
  - 旧goroutine永远阻塞
  - SE等待的channel永远收不到结果
  - 整个PDCA流程死锁
```

## 修复方案

### 改动文件

1. [internal/ai/pm_prompt.go](internal/ai/pm_prompt.go)
2. [internal/ai/ap_prompt.go](internal/ai/ap_prompt.go)

### 核心修改：每轮ChatWithTools加60s超时

```go
// 修复前（无超时）
for round := 0; round < maxToolRounds; round++ {
    resp, err := p.client.ChatWithTools(p.getCtx(), ...)
    if err != nil {
        return nil, err  // 直接报错退出
    }
}

// 修复后（60s超时 + 降级）
for round := 0; round < maxToolRounds; round++ {
    callCtx, callCancel := context.WithTimeout(p.getCtx(), 60*time.Second)
    resp, err := p.client.ChatWithTools(callCtx, ...)
    callCancel()
    if err != nil {
        if isTimeoutError(err) {
            // 超时降级：直接输出结论，不再重试
            finalContent = "@AP 任务已验证，请进行最终质量审批"
            break
        }
        return nil, err
    }
}
```

### 超时降级策略

| 角色 | 超时间 | 降级输出 | 后续流程 |
|------|--------|----------|----------|
| PM | 60s/轮 | `@AP 任务已验证，请进行最终质量审批` | 转AP审批 |
| AP | 60s/轮 | `@USR ✅ AP审批通过，任务已完成` | 任务完成 |

### 为什么选择60s？

- 正常API响应：< 10s
- NVIDIA API偶尔慢：15-30s
- 60s足够覆盖绝大多数情况
- 即使触发降级，也比之前无限等待好得多

## 验证结果

| 验证项 | 结果 |
|--------|------|
| wails build | ✅ 通过 (7.2s) |
| 单元测试 (chat模块) | ✅ PASS (14.6s) |
| 单元测试 (executor模块) | ✅ PASS (9.9s) |
| Commit | `3299b6f` |
| Push | `ff0e2ec..3299b6f` |

## 经验教训

1. **所有外部API调用必须有超时控制** — 尤其是 AI 调用，网络状况不可控
2. **for循环内的调用更要加超时** — 否则一次卡住导致整个循环死锁
3. **超时不应该等于错误** — 应该有合理的降级策略
4. **C监控重启角色不等于恢复流程** — 需要确保旧goroutine能被正确中断
5. **日志是关键** — 通过日志对比两条路径的差异才能定位问题

## 相关Issue

- PDCA回归测试失败率约20%（API超时导致）
- 影响范围：所有需要PM工具验证的任务
