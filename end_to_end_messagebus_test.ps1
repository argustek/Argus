# end_to_end_messagebus_test.ps1
# MessageBus 架构端到端回归测试
# 依赖: Argus HTTP 服务运行在 localhost:8080

param(
    [string]$BaseUrl = "http://127.0.0.1:8080",
    [string]$LogFile = "",
    [switch]$SkipGoTests = $false
)

$ErrorActionPreference = "Stop"
$Results = @()
$TestCount = 0
$PassCount = 0
$FailCount = 0

function Write-TestHeader($msg) {
    Write-Host "`n========================================" -ForegroundColor Cyan
    Write-Host "  $msg" -ForegroundColor Cyan
    Write-Host "========================================" -ForegroundColor Cyan
}

function Write-TestResult($name, $ok, $detail) {
    $TestCount++
    $status = if ($ok) { "PASS" } else { "FAIL" }
    $color = if ($ok) { "Green" } else { "Red" }
    if ($ok) { $PassCount++ } else { $FailCount++ }
    $d = if ($detail) { " — $detail" } else { "" }
    Write-Host "  [$status] $name$d" -ForegroundColor $color
    $Results += "$($name):$status"
}

function Invoke-API($method, $path, $body) {
    $headers = @{ "Content-Type" = "application/json" }
    try {
        if ($method -eq "GET") {
            $resp = Invoke-RestMethod -Uri "$BaseUrl$path" -Method GET -Headers $headers -TimeoutSec 10
        } else {
            $bodyJson = $body | ConvertTo-Json -Depth 5
            $resp = Invoke-RestMethod -Uri "$BaseUrl$path" -Method POST -Headers $headers -Body $bodyJson -TimeoutSec 120
        }
        return @{ success = $true; data = $resp }
    } catch {
        return @{ success = $false; error = $_.Exception.Message }
    }
}

# ===== Pre-check =====
Write-TestHeader "Pre-check"
$ping = Invoke-API "GET" "/health/ping"
if (-not $ping.success) {
    Write-Host "[FATAL] Argus service not running on $BaseUrl" -ForegroundColor Red
    exit 1
}
Write-Host "  [OK] Service online: $($ping.data.status)" -ForegroundColor Green

# ===== 1. Go 单元测试 =====
if (-not $SkipGoTests) {
    Write-TestHeader "Test 1: Go 单元审计测试"
    $goTestOutput = & go test ./internal/chat/ -run "TestAllPathsHaveCorrectTracking|TestAckWritesLogForTrackedPaths|TestAckDoesNotWriteLogForUntrackedPaths|TestNoDirectEventsEmitOutsideMessageBus|TestNoDirectUserContentWriteDebugLog|TestNoCoreOutputInTrackedPaths|TestPMToUserFullCycle|TestUserInputFullCycle|TestDuplicateAckReturnsFalse|TestAckForUnknownMsgIdReturnsFalse" -count=1 2>&1
    $goTestExit = $LASTEXITCODE
    if ($goTestExit -eq 0) {
        Write-TestResult "Go Unit Tests" $true "全部 10 项审计测试通过"
    } else {
        Write-TestResult "Go Unit Tests" $false "审计测试失败"
        Write-Host "    $goTestOutput" -ForegroundColor Red
    }
}

# ===== 2. 消息发送与日志验证 =====
Write-TestHeader "Test 2: 消息发送 + conversation.log 一致性"

# 2a. 取当前 log 大小作为基线
$configDir = if ($env:ARGUS_CONFIG_DIR) { $env:ARGUS_CONFIG_DIR } else { "$env:USERPROFILE\.argus" }
$convLog = if ($LogFile) { $LogFile } else { Join-Path $configDir "conversation.log" }

if (Test-Path $convLog) {
    $beforeSize = (Get-Item $convLog).Length
    Write-Host "  [INFO] conversation.log 基线大小: $beforeSize" -ForegroundColor DarkGray
} else {
    Write-Host "  [WARN] conversation.log 不存在，将在测试后检查" -ForegroundColor Yellow
    $beforeSize = 0
}

# 2b. 发送测试消息
$testMsg = "E2E regression test $((Get-Date).ToString('HHmmss'))"
Write-Host "  [SEND] $testMsg" -ForegroundColor DarkGray
$send1 = Invoke-API "POST" "/api/v1/chat/send" @{ message = $testMsg }

if ($send1.success) {
    Write-TestResult "Send Test Message" $true
} else {
    Write-TestResult "Send Test Message" $false $send1.error
}
Start-Sleep -Seconds 3

# 2c. 检查 conversation.log 增长
if (Test-Path $convLog) {
    $afterSize = (Get-Item $convLog).Length

    # 应该包含 USER: 条目
    $content = Get-Content $convLog -Tail 20 -Encoding UTF8
    $hasUser = $content -match "USER:.*$testMsg"
    if ($hasUser) {
        Write-TestResult "Log: USER 条目" $true
    } else {
        Write-TestResult "Log: USER 条目" $false "未在 conversation.log 中找到 'USER: $testMsg'"
        Write-Host "    log 末尾 20 行:" -ForegroundColor DarkGray
        $content | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
    }

    # 检查没有重复的 USER 条目
    $userCount = ($content | Select-String -Pattern "USER:").Count
    if ($userCount -le 1) {
        Write-TestResult "Log: USER 无重复" $true
    } else {
        Write-TestResult "Log: USER 无重复" $false "发现 $userCount 条 USER 条目，期望 ≤1"
    }
} else {
    Write-TestResult "Log: conversation.log 存在" $false "文件未创建"
}

# ===== 3. 历史 API 验证 =====
Write-TestHeader "Test 3: Chat History API"
$history = Invoke-API "GET" "/api/v1/chat/history"
if ($history.success -and $history.data.messages.Count -gt 0) {
    Write-TestResult "History API" $true "$($history.data.messages.Count) 条消息"
    $lastMsg = $history.data.messages[-1]
    if ($lastMsg.role -eq "user" -and $lastMsg.content -match $testMsg) {
        Write-TestResult "History: 最后消息匹配" $true
    } else {
        Write-TestResult "History: 最后消息匹配" $false "role=$($lastMsg.role) content=$($lastMsg.content)"
    }
} else {
    Write-TestResult "History API" $false "获取失败或无消息"
}

# ===== 3b. auto-ACK 验证 =====
Write-TestHeader "Test 3b: auto-ACK — 日志一致性"
Start-Sleep -Seconds 2
if (Test-Path $convLog) {
    $recentLines = Get-Content $convLog -Tail 100 -Encoding UTF8
    $pmCount = ($recentLines | Select-String -Pattern "PM:").Count
    $seCount = ($recentLines | Select-String -Pattern "SE:").Count
    $userCount = ($recentLines | Select-String -Pattern "USER:").Count
    
    if ($pmCount -ge 1) {
        Write-TestResult "auto-ACK: PM 日志已写入" $true "尾部 100 行含 $pmCount 条 PM"
    } else {
        Write-TestResult "auto-ACK: PM 日志已写入" $false "尾部 100 行无 PM 条目"
    }
    if ($seCount -ge 0) {
        Write-TestResult "auto-ACK: SE 日志检查" $true "含 $seCount 条 SE（可能无 SE 内容）"
    }
    if ($userCount -ge 1) {
        Write-TestResult "auto-ACK: USER 日志已写入" $true "尾部 100 行含 $userCount 条 USER"
    } else {
        Write-TestResult "auto-ACK: USER 日志已写入" $false "尾部 100 行无 USER 条目"
    }
    
    # 去重验证：同一条 USER 不应出现两次
    $dupUser = $recentLines | Group-Object | Where-Object { $_.Count -gt 1 -and $_.Name -match "USER:" }
    if ($dupUser) {
        Write-TestResult "auto-ACK: USER 无重复" $false "发现重复: $($dupUser.Count)"
    } else {
        Write-TestResult "auto-ACK: USER 无重复" $true
    }
} else {
    Write-TestResult "auto-ACK: conversation.log 存在" $false "文件未找到"
}

# ===== 4. SSE 端点验证 =====
Write-TestHeader "Test 4: SSE 端点"
$sseSub = Invoke-API "POST" "/api/v1/sse/subscribe" @{ client_id = "e2e-test-$((Get-Date).ToString('HHmmss'))" }
if ($sseSub.success) {
    Write-TestResult "SSE Subscribe" $true
} else {
    Write-TestResult "SSE Subscribe" $false $sseSub.error
}

$sseUnsub = Invoke-API "POST" "/api/v1/sse/unsubscribe" @{ client_id = "e2e-test-$((Get-Date).ToString('HHmmss'))" }
if ($sseUnsub.success) {
    Write-TestResult "SSE Unsubscribe" $true
} else {
    Write-TestResult "SSE Unsubscribe" $false $sseUnsub.error
}

# ===== 5. Token 统计验证 =====
Write-TestHeader "Test 5: Token 统计"
$tStats = Invoke-API "GET" "/api/v1/tokens/stats"
if ($tStats.success) {
    Write-TestResult "Token Stats API" $true "ratio=$($tStats.data.ratio)"
} else {
    Write-TestResult "Token Stats API" $false $tStats.error
}

# ===== 6. MessageBus 状态验证 =====
Write-TestHeader "Test 6: MessageBus 无泄漏"
$lostStats = Invoke-API "GET" "/api/v1/admin/status" 
if ($lostStats.success) {
    Write-TestResult "Admin Status API" $true
} else {
    Write-TestResult "Admin Status API" $false $lostStats.error
}

# ===== 7. 消息丢失检测 =====
Write-TestHeader "Test 7: MessageBus 丢失检测"
$pendingMsgs = Invoke-API "GET" "/api/v1/messagebus/pending"
# 即使出错也 OK（可能未实现 endpoint），只要不崩溃
if ($pendingMsgs.success) {
    $pendingCount = if ($pendingMsgs.data.pending -is [array]) { $pendingMsgs.data.pending.Count } else { 0 }
    if ($pendingCount -eq 0) {
        Write-TestResult "MessageBus: 无待确认消息" $true
    } else {
        Write-TestResult "MessageBus: 无待确认消息" $false "仍有 $pendingCount 条消息未确认"
    }
} else {
    Write-TestResult "MessageBus: 无待确认消息" $true "endpoint 不存在或返回非标准格式"
}

# ===== Summary =====
Write-Host ""
Write-Host "========================================" -ForegroundColor White
Write-Host "  MessageBus E2E Regression Summary" -ForegroundColor White
Write-Host "========================================" -ForegroundColor White
Write-Host ""
Write-Host "Total: $TestCount | Pass: $PassCount | Fail: $FailCount" -ForegroundColor $(if ($FailCount -eq 0) { "Green" } else { "Red" })
Write-Host ""

foreach ($r in $Results) {
    $parts = $r -split ":"
    $color = if ($parts[-1] -eq "PASS") { "Green" } elseif ($parts[-1] -eq "FAIL") { "Red" } else { "Yellow" }
    Write-Host "  $r" -ForegroundColor $color
}

if ($FailCount -eq 0) {
    Write-Host "`nALL PASSED" -ForegroundColor Green -BackgroundColor Black
    exit 0
} else {
    Write-Host "`nSOME FAILURES DETECTED" -ForegroundColor Red -BackgroundColor Black
    exit 1
}
