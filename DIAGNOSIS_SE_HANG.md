# SE任务执行卡住问题 - 诊断文档

**日期**: 2026-05-30  
**版本**: bb2b51d (detached HEAD, 基于 main)  
**现象**: PM分配任务给SE后，前端SE状态闪一下就熄灭，SE不再响应

---

## 1. 故障现象

1. 用户发送任务（如"写一个hello.go打印Hello World然后运行验证"）
2. PM正常响应，流式输出任务分配 → ✅
3. 前端SE状态短暂显示busy → 立即变为idle（"闪一下就没了"）
4. SE不执行任何文件操作
5. 手动@SE可以正常工作（间隔较长时）

## 2. 探针追踪结果

在关键调用链路中插入了文件探针（写入 `%TEMP%\argus_*_probe.log`）：

| 探针位置 | 文件 | 状态 |
|----------|------|------|
| PM chatStreamOnce入口 | client.go | ✅ 触发 |
| SE BEFORE-API | manager.go (startSETask) | ✅ 触发 |
| SE PROBE-CALL (fmt.Printf) | manager.go (startSETask) | ❓ 未知 |
| **SE ProcessTaskStream入口** | **se_prompt.go** | **❌ 未触发** |
| SE ChatStream入口 | client.go chatStreamOnce | ❌ 未触发 |

### route.log 记录
```
[SE-TASK] from=pm task='...'     ← PM分配任务成功
[SE-TASK] ✅ Turn passed, executing...
[SE-API-START] from=pm attempt=0/2 ctx_err=<nil> time=13:33:09
                                   ← 之后无任何后续日志
```

### argus_se_state_probe.log
```
[BEFORE-API] SE="busy" PM="idle" currentRole="se" isProcessing=false  ← 状态正确
（无AFTER-API记录）
```

## 3. 根因分析

**核心矛盾**: manager.go 中 `BEFORE-API` 探针成功写入文件，紧跟着的 `m.seProcessor.ProcessTaskStream(...)` 调用发起后，ProcessTaskStream 函数内部的入口探针**完全没有触发**。

**最可能原因**:
- `m.seProcessor.ProcessTaskStream(...)` 调用触发了 **nil指针panic**
- `startSETask` 函数顶部的 `recover()` 静默捕获了panic
- recover后立即 `m.cMonitor.UpdateSeStatus(RoleStatusIdle)` → 前端SE状态闪退
- 由于是静默恢复，没有错误日志输出

**可疑代码路径** (manager.go L2365):
```go
defer func() {
    if r := recover(); r != nil {
        fmt.Printf("[startSETask] 💥 panic recovered: %v\n", r)
        m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)  // ← SE状态被重置
    }
    ...
}()
```

## 4. 之前已排除的问题

- ❌ HTTP连接池复用死连接 → 已修复（CloseIdleConnections），但非根因
- ❌ API重试条件不全 → 已扩展重试条件，但API调用根本没到达
- ❌ C监控器过早触发 → C监控器6分钟超时，不会在几秒内触发
- ❌ FALLBACK-FIX死循环 → 已修复
- ❌ PM重复分配任务 → 已修复

## 5. 下一步排查方向

1. **在 ProcessTaskStream 调用前用 `os.Stderr` 直接写入**（绕过文件IO可能的延迟）
2. **检查 `m.seProcessor` 是否为nil或内部字段异常**
3. **对比手动@SE触发路径**：
   - 自动路由: `handleToPM → startSETask → ProcessTaskStream` (失败)
   - 手动@SE:  `handleToSE → startSETask → ProcessTaskStream` (成功)
   - 检查handleToPM和handleToSE在调用startSETask前的状态差异
4. **在recover中添加详细panic堆栈输出** (`debug.Stack()`)

## 6. 关键文件

- `internal/chat/manager.go` - 主流程控制、SE启动(startSETask)、continueSETask
- `internal/ai/se_prompt.go` - SE处理器、ProcessTaskStream
- `internal/ai/client.go` - AI API调用、ChatStream、chatStreamOnce
- `internal/monitor/c_monitor.go` - C监控器、自动复位逻辑

## 7. 当前commit

```
bb2b51d 诊断状态：追踪SE调用链探针 - 发现根因线索
```

包含探针代码、重试扩展、CloseIdleConnections等诊断修改。
