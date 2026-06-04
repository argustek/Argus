# Hello World 回归测试脚本
# 通过 HTTP API 发送消息，SSE 接收结果

$base = "http://localhost:8080"
$results = @()

for ($i = 1; $i -le 5; $i++) {
    Write-Host ""
    Write-Host "========== 测试 #$i/5 ==========" -ForegroundColor Cyan
    
    $filename = "hello_test$($i).go"
    $msg = "帮我写一个$($filename)，输出Hello World Test $($i)"
    
    # 准备请求体
    $body = @{ message = $msg } | ConvertTo-Json
    
    Write-Host "[发送] $msg" -ForegroundColor Yellow
    
    # 1. 发送消息
    try {
        $sendResp = Invoke-RestMethod -Uri "$base/api/v1/chat/send" -Method POST `
            -ContentType "application/json" -Body $body -TimeoutSec 10
        Write-Host "[Send] $($sendResp.status)" -ForegroundColor Green
    } catch {
        Write-Host "[Send] FAIL: $_" -ForegroundColor Red
        $results += @{ round=$i; status="FAIL"; error="send failed"; file=$filename }
        continue
    }
    
    # 2. 等待处理完成（轮询状态，最多等120秒）
    $maxWait = 120
    $waited = 0
    $done = $false
    
    while ($waited -lt $maxWait) {
        Start-Sleep -Seconds 3
        $waited += 3
        
        try {
            $status = Invoke-RestMethod -Uri "$base/admin/status" -TimeoutSec 5
            $pm = $status.monitor.pmBusySince
            $se = $status.monitor.seBusySince
            
            if ($pm -eq 0 -and $se -eq 0 -and $waited -gt 5) {
                # PM和SE都空闲了，说明处理完了
                $done = $true
                break
            }
            
            if ($waited % 15 -eq 0) {
                Write-Host "[等待] ${waited}s... pm_busy=$pm se_busy=$se" -ForegroundColor Gray
            }
        } catch {
            Write-Host "[轮询] 状态查询失败: $_" -ForegroundColor DarkGray
        }
    }
    
    # 3. 检查结果
    Write-Host "[检查] 文件是否存在: $filename" -ForegroundColor Yellow
    
    try {
        $readResp = Invoke-RestMethod -Uri "$base/api/v1/read?path=$filename" -TimeoutSec 5
        if ($readResp.success) {
            $hasHello = $readResp.content -match "Hello World"
            Write-Host "[结果] ✅ 文件存在! 包含Hello World: $hasHello" -ForegroundColor Green
            Write-Host "[预览] $($readResp.content.Substring(0, [Math]::Min(200, $readResp.content.Length)))" -ForegroundColor Gray
            
            $results += @{ round=$i; status="PASS"; file=$filename; hasHello=$hasHello }
        } else {
            Write-Host "[结果] ❌ 文件不存在或读取失败: $($readResp.error)" -ForegroundColor Red
            $results += @{ round=$i; status="FAIL"; error="file not found"; file=$filename }
        }
    } catch {
        Write-Host "[结果] ❌ 读取API异常: $_" -ForegroundColor Red
        $results += @{ round=$i; status="FAIL"; error="read exception"; file=$filename }
    }
    
    # 4. 查history确认
    try {
        $hist = Invoke-RestMethod -Uri "$base/api/v1/chat/history" -TimeoutSec 5
        Write-Host "[History] 共 $($hist.count) 条消息" -ForegroundColor DarkGray
    } catch {
        Write-Host "[History] 查询失败" -ForegroundColor DarkGray
    }
}

# 汇总
Write-Host ""
Write-Host "========================================" -ForegroundColor White
Write-Host "       回归测试汇总报告" -ForegroundColor White
Write-Host "========================================" -ForegroundColor White

$pass = ($results | Where-Object { $_.status -eq "PASS" }).Count
$fail = ($results | Where-Object { $_.status -eq "FAIL" }).Count

foreach ($r in $results) {
    if ($r.status -eq "PASS") {
        Write-Host "  #$($r.round): ✅ PASS - $($r.file)" -ForegroundColor Green
    } else {
        Write-Host "  #$($r.round): ❌ FAIL - $($r.file) - $($r.error)" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "总计: $pass PASS / $fail FAIL / $($results.Count) total" -ForegroundColor $(if($fail -eq 0){"Green"}else{"Red"})
if ($fail -eq 0) {
    Write-Host "🎉 全部通过!" -ForegroundColor Green
}
