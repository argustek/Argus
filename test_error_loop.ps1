# 错误闭环测试 — SSE 方式验证 PM 错误处理
param(
    [string]$Message = "写一个Go程序hello.go，输出Hello World。故意在代码中写一个语法错误(比如把fmt.Println写成fmt.Priln)，然后编译运行，观察错误处理流程",
    [int]$TimeoutSec = 180
)

$outFile = "$env:TEMP\sse_test_output.txt"
$summaryFile = "$env:TEMP\sse_test_summary.txt"

Write-Host "=== 错误闭环 SSE 测试 ===" -ForegroundColor Cyan
Write-Host "消息: $Message"
Write-Host "超时: ${TimeoutSec}s"
Write-Host ""

# 清理旧输出
Remove-Item -Force $outFile -ErrorAction SilentlyContinue
Remove-Item -Force $summaryFile -ErrorAction SilentlyContinue

# 启动后台 curl SSE 连接
$bodyJson = "{`"message`": `"$Message`"}"
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = "curl.exe"
$psi.Arguments = "-N --max-time $TimeoutSec -X POST http://localhost:8080/api/v1/sse/subscribe -H `"Content-Type: application/json`" -d `"$bodyJson`""
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$p = New-Object System.Diagnostics.Process
$p.StartInfo = $psi
$p.Start() | Out-Null

# 读取流（异步）
$outputBuilder = New-Object System.Text.StringBuilder
$reader = $p.StandardOutput
$timeout = [DateTime]::Now.AddSeconds($TimeoutSec)

Write-Host "⏳ 等待 SSE 事件流..." -ForegroundColor Yellow

$eventCount = @{}
$errorFound = $false
$pmAfterError = $false
$autoFixCount = 0
$doneFound = $false

while (-not $reader.EndOfStream -and [DateTime]::Now -lt $timeout) {
    $line = $reader.ReadLine()
    if ($line -eq $null) { break }
    $outputBuilder.AppendLine($line) | Out-Null
    
    # 实时显示事件
    if ($line -match '^event: (.+)') {
        $evt = $matches[1]
        if (-not $eventCount.ContainsKey($evt)) { $eventCount[$evt] = 0 }
        $eventCount[$evt]++
    } elseif ($line -match '^data: (.+)') {
        $data = $matches[1]
        if ($data -match '"error"') { $errorFound = $true }
        if ($data -match '"status"\s*:\s*"(completed|cancelled)"') { $doneFound = $true }
        if ($data -match '自动修复|auto.fix|AUTO.FIX') { $autoFixCount++ }
        # 检查PM是否在SE错误后介入
        if ($data -match 'pm_message|PM.*分析|PM.*处理|PM.*错误') { $pmAfterError = $true }
    }
    
    # 显示进度
    if ($line -match 'event: (se_action|se_output|pm_message|pm_started|se_completed|done|error|ide_message)') {
        $evt = $matches[1]
        $dataLine = $reader.ReadLine()
        Write-Host "  [$evt] $dataLine" -ForegroundColor Green
    }
}

$p.WaitForExit(5000) | Out-Null
$allOutput = $outputBuilder.ToString()
Set-Content -Path $outFile -Value $allOutput -Encoding UTF8

Write-Host ""
Write-Host "=== 测试结果 ===" -ForegroundColor Cyan

# 分析事件序列
Write-Host "事件统计:" -ForegroundColor Yellow
$eventCount.Keys | Sort-Object | ForEach-Object { Write-Host "  $_ : $($eventCount[$_])" }

Write-Host ""
if ($doneFound) {
    Write-Host "✅ 任务正常完成 (done 事件收到)" -ForegroundColor Green
} else {
    Write-Host "❌ 任务未完成 (无 done 事件)" -ForegroundColor Red
}

# 检查错误闭环
if ($errorFound) {
    Write-Host "⚠️ 检测到 error 事件" -ForegroundColor Yellow
    if ($pmAfterError) {
        Write-Host "✅ PM 在错误后介入处理 (闭环正常)" -ForegroundColor Green
    } else {
        Write-Host "❌ PM 未在错误后介入 (闭环断裂)" -ForegroundColor Red
    }
} else {
    if ($autoFixCount -gt 0) {
        Write-Host "✅ auto-fix 机制触发 ($autoFixCount 次)" -ForegroundColor Green
        Write-Host "✅ 错误被 auto-fix 自动修复 (无需PM介入)" -ForegroundColor Green
    } else {
        Write-Host "ℹ️ 未检测到错误 (任务可能直接成功)" -ForegroundColor Gray
    }
}

# 关键事件序列
Write-Host ""
Write-Host "事件序列:" -ForegroundColor Yellow
$lines = $allOutput -split "`n"
$events = @()
for ($i = 0; $i -lt $lines.Length; $i++) {
    if ($lines[$i] -match '^event: (.+)') {
        $evt = $matches[1]
        if ($i + 1 -lt $lines.Length -and $lines[$i+1] -match '^data: (.+)') {
            $data = $lines[$i+1]
            $events += @{Event=$evt; Data=$data}
            Write-Host "  $evt → $(if ($data.Length -gt 80) { $data.Substring(0,80) + '...' } else { $data })"
        }
    }
}

# 判断测试结论
Write-Host ""
if ($errorFound -and $pmAfterError) {
    Write-Host "✅ 测试结论: 错误闭环正常工作" -ForegroundColor Green
    Write-Host "   错误出现 → PM 介入分析处理" -ForegroundColor Green
} elseif ($errorFound -and -not $pmAfterError) {
    Write-Host "❌ 测试结论: 错误闭环断裂" -ForegroundColor Red
    Write-Host "   错误出现但 PM 未介入" -ForegroundColor Red
} elseif ($autoFixCount -gt 0) {
    Write-Host "✅ 测试结论: auto-fix 成功修复错误 (无需PM)" -ForegroundColor Green
} else {
    Write-Host "ℹ️ 测试结论: 未触发错误场景" -ForegroundColor Gray
    Write-Host "   完整输出已保存到: $outFile" -ForegroundColor Gray
}

Write-Host ""
Write-Host "详细输出文件: $outFile" -ForegroundColor Gray
