param(
    [string]$Message = "写一个Go程序hello.go，输出Hello World，运行go run hello.go验证",
    [int]$WaitSec = 150
)

$ErrorActionPreference = "Stop"
$appExe = "E:\ArgusTek\Argus\build\bin\argus-desktop.exe"
$workDir = "E:\ArgusTek\Argus"
$outFile = "$env:TEMP\e2e_test_output.txt"

Write-Host "╔════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║       错误闭环端到端自动测试            ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# Step 1: Kill existing GUI
Write-Host "▶ 步骤1: 终止旧进程..." -ForegroundColor Yellow
Get-Process -Name "argus-desktop" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2
if (Get-Process -Name "argus-desktop" -ErrorAction SilentlyContinue) {
    Write-Host "❌ 无法终止旧进程" -ForegroundColor Red
    exit 1
}
Write-Host "  ✅ 旧进程已终止" -ForegroundColor Green

# Step 2: Clean old state
Write-Host "▶ 步骤2: 清理旧状态..." -ForegroundColor Yellow
Remove-Item -Force "$workDir\config\conversation.log" -ErrorAction SilentlyContinue
Remove-Item -Force "$workDir\config\debug_events.log" -ErrorAction SilentlyContinue
Write-Host "  ✅ 状态已清理" -ForegroundColor Green

# Step 3: Start app in background, capture stdout
Write-Host "▶ 步骤3: 启动应用 (带日志捕获)..." -ForegroundColor Yellow
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $appExe
$psi.WorkingDirectory = $workDir
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$p = New-Object System.Diagnostics.Process
$p.StartInfo = $psi
$p.Start() | Out-Null
$appPid = $p.Id
Write-Host "  ✅ 应用已启动 (PID: $appPid)" -ForegroundColor Green

# Wait for app to be ready
Write-Host "  ⏳ 等待应用就绪..." -ForegroundColor Gray
Start-Sleep -Seconds 8

# Check if app is responsive
try {
    $ping = Invoke-RestMethod -Uri "http://localhost:8080/health/ping" -TimeoutSec 5 -ErrorAction Stop
    Write-Host "  ✅ HTTP 服务就绪" -ForegroundColor Green
} catch {
    # /health/ping might not exist, try to send
    Write-Host "  ℹ️ HTTP 健康检查不可用，继续..." -ForegroundColor Gray
}

# Step 4: Send test message via chat API
Write-Host "▶ 步骤4: 发送测试消息..." -ForegroundColor Yellow
Write-Host "  消息: $Message" -ForegroundColor Gray

$body = @{message = $Message} | ConvertTo-Json
try {
    $resp = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/chat/send" -Method Post -Body $body -ContentType "application/json" -TimeoutSec 10
    Write-Host "  ✅ 消息已提交: status=$($resp.status)" -ForegroundColor Green
} catch {
    Write-Host "  ⚠️ 发送失败: $_" -ForegroundColor Yellow
    # Try --send as fallback
    & $appExe --send $Message 2>$null
    Write-Host "  ⚠️ 使用 --send 方式发送" -ForegroundColor Yellow
}

# Step 5: Wait for processing
Write-Host "▶ 步骤5: 等待 AI 处理 (${WaitSec}s)..." -ForegroundColor Yellow
$checkInterval = 10
$elapsed = 0
$done = $false

while ($elapsed -lt $WaitSec -and -not $done) {
    Start-Sleep -Seconds $checkInterval
    $elapsed += $checkInterval
    
    # Check log for completion
    if (Test-Path "$workDir\config\debug_events.log") {
        $logContent = Get-Content "$workDir\config\debug_events.log" -Tail 20 -Encoding UTF8 -ErrorAction SilentlyContinue
        $lastLog = $logContent -join "`n"
        if ($lastLog -match "done|completed|error|顺利完成|完成") {
            Write-Host "  ✅ 检测到完成信号 (${elapsed}s)" -ForegroundColor Green
            $done = $true
        } else {
            Write-Host "  ⏳ 处理中... (${elapsed}s)" -ForegroundColor Gray
        }
    } else {
        Write-Host "  ⏳ 等待日志... (${elapsed}s)" -ForegroundColor Gray
    }
}

# Step 6: Collect output
Write-Host "▶ 步骤6: 收集输出..." -ForegroundColor Yellow
Start-Sleep -Seconds 3

# Capture stdout
$stdout = $p.StandardOutput.ReadToEnd()
$stderr = $p.StandardError.ReadToEnd()

# Stop the app
Write-Host "  ⏳ 停止应用..." -ForegroundColor Gray
$p.Kill()
$p.WaitForExit(5000)

# Save output
$output = @"
=== STDOUT ===
$stdout

=== STDERR ===
$stderr
"@
Set-Content -Path $outFile -Value $output -Encoding UTF8

# Step 7: Analyze
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "         测试结果分析" -ForegroundColor Cyan
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan

# Debug log analysis
$hasPM = $false
$hasSE = $false
$hasAutoFix = $false
$hasPMAfterError = $false
$hasError = $false
$hasDone = $false

if (Test-Path "$workDir\config\debug_events.log") {
    $logContent = Get-Content "$workDir\config\debug_events.log" -Encoding UTF8 -ErrorAction SilentlyContinue
    $logText = $logContent -join "`n"
    
    $hasPM = $logText -match "pm_started|PM|pm_message|ProcessReview|handleToPM"
    $hasSE = $logText -match "se_action|se_output|se_completed|startSETask"
    $hasAutoFix = $logText -match "auto.fix|AUTO.FIX|自动修复"
    $hasPMAfterError = $logText -match "error.*pm|PM.*error|handleSEAskPM|SE.*帮助"
    $hasError = $logText -match "error|失败|failed"
    $hasDone = $logText -match "done|completed|顺利完成"
}

Write-Host ""
Write-Host "关键指标:" -ForegroundColor Yellow
Write-Host "  PM 参与: $(if($hasPM){'✅'}else{'❌'})" 
Write-Host "  SE 执行: $(if($hasSE){'✅'}else{'❌'})"
Write-Host "  Auto-fix: $(if($hasAutoFix){'✅'}else{'ℹ️'}) (可能不需要修复)"
Write-Host "  错误检测: $(if($hasError){'⚠️'}else{'ℹ️'})"
Write-Host "  PM 错误后介入: $(if($hasPMAfterError){'✅'}else{'❌'})"
Write-Host "  任务完成: $(if($hasDone){'✅'}else{'❌'})"

# Output analysis
$outputText = $stdout + "`n" + $stderr
if ($outputText -match "handleSEAskPM|ProcessMessageFrom.*@PM|PM.*分析") {
    Write-Host ""
    Write-Host "✅ 错误闭环验证: PASS" -ForegroundColor Green
    Write-Host "  SE 错误后 PM 成功介入分析" -ForegroundColor Green
} elseif ($outputText -match "AUTO.FIX.*成功|自动修复成功") {
    Write-Host ""
    Write-Host "✅ 错误闭环验证: PASS (auto-fix 修复)" -ForegroundColor Green
    Write-Host "  auto-fix 成功修复了错误，无需 PM 介入" -ForegroundColor Green
} elseif ($hasPMAfterError) {
    Write-Host ""
    Write-Host "✅ 错误闭环验证: PASS" -ForegroundColor Green
    Write-Host "  debug_events.log 显示 PM 在错误后介入" -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "ℹ️ 错误闭环验证: 未触发错误场景" -ForegroundColor Gray
    Write-Host "  任务可能直接成功，未触发错误处理路径" -ForegroundColor Gray
}

if ($hasDone) {
    Write-Host ""
    Write-Host "最终结果: ✅ 任务完成" -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "最终结果: ⚠️ 未检测到完成信号" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "完整输出: $outFile" -ForegroundColor Gray
