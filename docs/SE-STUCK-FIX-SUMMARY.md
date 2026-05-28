# SE卡住问题修复状态总结
**日期**: 2026-05-28 18:35  
**问题**: SE执行完任务后不路由到PM审核，C监控6分钟后reset破坏流程

---

## 📋 问题现象

### 用户报告的完整流程
```
18:14 USR: write hello world go program and run it
       ↓ PM拆解任务，输出JSON给SE
18:16 SE开始执行（4个操作）
       ✓ 创建 write_file hello.go (成功)
       ✓ 执行 成功
       ✗ check_env 失败
       ○ 创建 hello.go 已创建
       ○ 执行 ▶ 运行 $ go run hello ❌失败
          package hello is not in std (C:\Program Files\Go\src\hello)
18:19 USR: （等待中...）
18:21 UNKNOWN: [C监控] ⚠️ 项目进行中但你和SE都空闲，请检查
```

**关键问题**：
1. SE显示"全部完成"但实际未路由到PM
2. C监控触发的是`doubleIdle`检测（PM+SE都idle），不是正常的`se_to_pm`流程
3. 流程被破坏：应该走 **SE→PM→AP** 但实际走了 **SE→(卡住)→C reset→重新开始**

---

## 🔍 根因分析（4层递进）

### 第1层：表面现象 - C监控误判
**文件**: `internal/monitor/c_monitor.go`  
**位置**: noProgressTimeout分支 (~398行)  
**原逻辑**:
```go
if state.ProjectState == types.ProjectStateDone { // 检查 == 2
    shouldForceRouteToPM = true
}
```

**Bug**:
- `ProjectStateDone = 2` 是**PM审核后**的状态
- SE完成后 `ProjectState = 1` (Running)
- 判断条件**永远不成立** → 走reset分支 ❌

### 第2层：检测机制缺陷 - seReportedComplete未设置
**文件**: `internal/chat/manager.go`  
**现象**: API返回 `seReportedComplete: false`（即使文件已创建）

**原因**: SE走的是`continueSETask()`路径，该函数的4个完成分支全部缺少状态设置

### 第3层：根本原因 - continueSETask() 4个路径缺失状态设置 ⭐⭐⭐
**文件**: `internal/chat/manager.go`  
**函数**: `continueSETask()` (2579行)

| 路径 | TAG | 行号 | seReportedComplete | SetHandoverPending | UpdateProjectState |
|------|-----|------|-------------------|-------------------|-------------------|
| actions执行成功 | TAG-C1 | 2684 | ❌ 缺失 | ❌ 缺失 | ❌ 缺失 |
| resp.Completed != nil | TAG-C2 | 2717 | ❌ 缺失 | ❌ 缺失 | ❌ 缺失 |
| 有内容无actions | TAG-C3 | 2730 | ❌ 缺失 | ❌ 缺失 | ❌ 缺失 |
| 空回复 | TAG-C4 | 2735 | ❌ 缺失 | ❌ 缺失 | ❌ 缺失 |

**对比正常路径** (`startSETaskWithFrom()`):
- TAG-D3/TAG-S3/TAG-E1 都有完整设置 ✅

### 第4层：时间窗口太小
**文件**: `internal/monitor/c_monitor.go`  
**原值**: `< 300秒` (5分钟)  
**实际需要**: SE完成任务到C监控触发可能6-10分钟  
**结果**: 文件已存在但超出时间窗口 → recentFileCount=0 → 检测失败

---

## 🔧 已实施的修复

### Commit 1: `a1c393f` - 基础Bug修复
| Bug | 修复方案 |
|-----|---------|
| SE空actions死循环 | `seEmptyActionCount`计数器，3次强制路由 |
| HTTP配置丢失 | 前端GetConfig()添加http字段读取 |
| AP强制通过 | 智能超时：根据交接状态推进 |

### Commit 2: `173497d` - C监控智能判断v1（❌未解决问题）
- 尝试基于ProjectStateDone判断
- 失败原因：SE完成后PState=1不是2

### Commit 3: `429fd71` - C监控双重检测机制
**新增接口**:
```go
// Manager
func (m *Manager) IsSETaskCompleted() bool           // 暴露seReportedComplete
func (m *Manager) GetChatManagerStatus() map[...]     // 包含seReportedComplete字段

// CMonitor
func (c *CMonitor) SetSECompletedChecker(checker func() bool)
func (c *CMonitor) SetWorkDirChecker(checker func() string)
```

**双重检测逻辑**:
```go
// 方法1: seCompletedChecker (精确)
if c.seCompletedChecker() && c.seCompletedChecker() {
    shouldForceRouteToPM = true  // SE已报告完成
}

// 方法2: workDirChecker (备选/兜底)
else if 工作目录有最近修改的文件(15分钟内) {
    shouldForceRouteToPM = true  // 文件检测
}
```

### Commit 4: `7004337` - [关键修复] continueSETask()状态设置 ⭐⭐⭐
**修复内容**: 为所有4个路径补充完整状态设置

**TAG-C1修复 (2688行)**:
```go
m.seReportedComplete = true                    // [新增]
m.currentRole = ""
m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
m.SetHandoverPending(HandoverSEToPM)           // [新增]
m.cMonitor.UpdateProjectState(types.ProjectStateDone)  // [新增]
m.syncBackendStatus("done", "SE任务完成(continue)，路由到PM [TAG-C1]")  // [新增]
```

**TAG-C2/C3/C4修复**: 同样添加以上4项状态设置

**额外修复**: 时间窗口 300秒 → 900秒 (15分钟)

---

## 📊 当前代码状态

### 已修改文件
1. **`internal/chat/manager.go`**
   - 新增 `IsSETaskCompleted()` 方法 (5266行)
   - `GetChatManagerStatus()` 增加 `seReportedComplete` 字段
   - 注册checker到C监控 (752-753行)
   - **TAG-C1/C2/C3/C4 补充状态设置 (+16/-4行)**

2. **`internal/monitor/c_monitor.go`**
   - 新增 `seCompletedChecker` 字段和setter
   - 新增 `workDirChecker` 字段和setter
   - noProgressTimeout分支双重检测逻辑 (+45/-8行)
   - 时间窗口调整: 300→900秒 (+2/-2行)

### 编译状态
✅ 最新编译成功 (Commit: `7004337`)  
✅ 二进制文件: `build/bin/argus-desktop.exe`  
⏳ 运行中但**尚未验证修复效果**

---

## 🧪 测试验证情况

### 测试1: 18:14发送任务 → 18:21出现doubleIdle
**结果**: ❌ 修复前行为（旧二进制）

**时间线**:
```
18:14:48 任务发送
18:16:28 SE执行中, File=Y, SEComp=False ← 文件已创建！
18:18:08 SE仍busy
18:19:48 SE仍busy
18:20+   C监控应触发noProgressTimeout
18:21    实际触发: doubleIdle (说明已被reset过)
```

**分析**: 
- SE在~18:16完成任务（文件存在）
- 但seReportedComplete=false + handover未设置
- C监控6分钟后触发，双重检测失败（时间窗口5分钟<6.7分钟）
- 执行retryCallback() reset
- Reset后PM+SE都idle → 触发doubleIdle警告

### 测试2: 18:30发送任务 → 进行中
**状态**: ⏳ 使用新二进制(7004337)，等待结果

---

## 🎯 下次测试要点

### 预期正确行为
```
时间线:
00s  USR发送任务
05s  PM拆解任务，输出JSON给SE
10s  SE开始执行
30s  SE执行操作（创建hello.go等）
40s  SE完成（走TAG-C1/C2/C3/C4之一）
     ↓ 关键检查点
41s  seReportedComplete = true ✅
42s  SetHandoverPending(se_to_pm) ✅
43s  UpdateProjectState(done) ✅
44s  syncBackendStatus("done") ✅
45s  ProcessMessageFrom("se", ...) 路由到PM ✅
46s  Stage变为 pm_processing ✅
60s  PM收到消息，开始审核
```

### 需要观察的关键指标
1. **API查询**:
   ```powershell
   # 每30秒检查一次
   Invoke-RestMethod http://localhost:8080/admin/status | Select-Object -ExpandProperty chatManager
   ```
   
   **预期变化**:
   - SE执行时: `seReportedComplete=False`, `currentRole="se"`
   - SE完成后: `seReportedComplete=True`, `currentRole=""` ← **必须看到这个！**

2. **Backend Status**:
   ```powershell
   Invoke-RestMethod http://localhost:8080/admin/backend-status
   ```
   
   **预期变化**:
   - SE执行时: `stage=se_processing`, `se_status=busy`
   - 完成后: `stage=pm_processing`, `se_status=idle`, `pm_status=busy`

3. **日志关键词搜索**:
   ```powershell
   # 应该看到的TAG标记
   TAG-C1 或 TAG-C2 或 TAG-C3 或 TAG-C4  ← continueSETask完成路径
   "SE任务完成(continue)"                  ← syncBackendStatus消息
   
   # 不应该看到的
   "[C自动重试] SE卡住已自动reset"         ← 说明修复失败
   "[C监控] 项目进行中但PM和SE都空闲"      ← 说明被reset了
   ```

### 如果仍然失败的排查方向
1. **确认走的哪个路径**:
   - 搜索日志中的 `[System]` 和 `[TRACE-SE]` 标记
   - 确认是否进入continueSETask()
   - 确认走的是TAG-C1/C2/C3/C4哪个分支

2. **检查错误处理分支**:
   - 如果`go run hello`命令失败，会走错误恢复逻辑
   - 错误恢复后会调用`continueSETask()`
   - 需要确认continueSETask()的返回值是否正确

3. **检查ProcessMessageFrom**:
   - continueSETask最后调用`ProcessMessageFrom("se", ...)` 
   - 这个函数内部是否会再次启动SE？（可能导致循环）

---

## 📝 待解决问题清单

### 高优先级
- [ ] **验证修复效果**: 发送Hello World任务，观察是否立即路由到PM
- [ ] **确认TAG路径**: 日志中出现TAG-C1/C2/C3/C4中的哪一个
- [ ] **检查seReportedComplete**: API返回从false变为true的时间点

### 中优先级
- [ ] **SE命令错误修复**: `go run hello` 应该是 `go run hello.go`（AI生成错误）
- [ ] **回归测试**: 确保正常流程（startSETaskWithFrom）未被破坏
- [ ] **边界条件**: continueSETask超过20轮的处理（2592行限制）

### 低优先级
- [ ] **性能优化**: C监控文件扫描可以缓存结果
- [ ] **日志增强**: 在关键状态设置点添加更多debug日志
- [ ] **单元测试**: 为continueSETask的4个路径编写测试用例

---

## 🔗 相关文件索引

| 文件 | 关键行号 | 功能说明 |
|------|---------|---------|
| `internal/chat/manager.go` | 2340-2419 | startSETaskWithFrom() 正常完成路径 |
| `internal/chat/manager.go` | 2579-2744 | **continueSETask() 修复重点** |
| `internal/chat/manager.go` | 2684-2697 | TAG-C1: actions执行成功 |
| `internal/chat/manager.go` | 2711-2725 | TAG-C2: resp.Completed != nil |
| `internal/chat/manager.go` | 2730-2737 | TAG-C3: 有内容无actions |
| `internal/chat/manager.go` | 2738-2744 | TAG-C4: 空回复 |
| `internal/monitor/c_monitor.go` | 398-473 | noProgressTimeout 双重检测 |
| `internal/monitor/c_monitor.go` | 411-455 | seCompletedChecker + workDirChecker |
| `internal/monitor/c_monitor.go` | 503-505 | doubleIdle 检测 |
| `internal/types/types.go` | 45-46 | ProjectState定义 |

---

## 💡 经验教训

### 1. 状态机完整性至关重要
**教训**: 任何完成路径都必须设置完整的状态标志  
**规则**: 
- `seReportedComplete = true` - 标记SE已完成
- `SetHandoverPending(step)` - 设置交接待处理
- `UpdateProjectState(state)` - 更新项目状态
- `syncBackendStatus(msg)` - 同步到前端/API

### 2. 时间窗口要留足余量
**教训**: 5分钟窗口在实际场景中不够用  
**建议**: 至少15分钟，或使用相对时间（如SE busy持续时间的2倍）

### 3. 多路径代码必须对称
**教训**: `startSETaskWithFrom()`有完整状态设置，但`continueSETask()`遗漏  
**建议**: 提取公共函数`markSECompleted()`统一处理

### 4. 监控是最后的防线，不是主要机制
**教训**: 不应该依赖C监控来推进正常流程  
**原则**: SE完成后应立即路由到PM，C监控只用于异常恢复

---

## 📞 下一步行动

### 回家后测试步骤
1. 启动程序: `./build/bin/argus-desktop.exe`
2. 发送任务: 
   ```powershell
   $body = '{"message": "write hello world go program and run it"}'
   Invoke-RestMethod -Uri "http://localhost:8080/api/v1/chat/send" -Method POST `
     -Body $body -ContentType "application/json" `
     -Headers @{"Authorization"="Bearer test-token"}
   ```
3. 监控状态（每30秒）:
   ```powershell
   while($true) {
     Start-Sleep -Seconds 30
     try {
       $s = Invoke-RestMethod http://localhost:8080/admin/status
       Write-Host "$(Get-Date -Format 'HH:mm:ss') SEComp=$($s.chatManager.seReportedComplete) Role=$($s.chatManager.currentRole)"
     } catch {
       Write-Host "$(Get-Date -Format 'HH:mm:ss') Error"
     }
   }
   ```

### 预期结果
- **30-60秒内**: `SEComp=True`, `Role=""` (空字符串表示无当前角色)
- **Stage变化**: `se_processing` → `pm_processing`
- **不应出现**: `[C自动重试]` 或 `[C监控] doubleIdle`

### 如果成功
- ✅ 修复验证通过
- 可以继续测试更复杂的任务
- 考虑合并到main分支

### 如果仍然失败
- 收集完整日志（argus.log）
- 搜索关键词: `TAG-C`, `continueSETask`, `seReportedComplete`
- 检查是否有其他未覆盖的代码路径
- 可能需要进一步调试continueSETask的调用链

---

**文档版本**: v1.0  
**最后更新**: 2026-05-28 18:35  
**作者**: AI Assistant  
**状态**: ⏳ 等待验证
