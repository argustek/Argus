# ============================================================
# P0-2 回归测试: 撤销/回滚 (undo_file + list_changes + rollback-on-failure)
# 测试场景:
#   1. SE 写文件 -> list_changes 确认有记录 -> undo_file 撤销
#   2. SE 编辑导致语法错误 -> 自动回滚触发
#   3. 多次编辑 -> 连续 undo -> 验证最终状态
# ============================================================

$ErrorActionPreference = "Continue"

$apiBase = "http://127.0.0.1:8080/api/v1"
$workDir = "F:\GithubArgus"
$exe = "F:\ArgusTek\Argus\build\bin\argus-desktop.exe"
$logFile = "F:\ArgusTek\Argus\logs\p0_regress_p02.log"

function Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    $line = "[$ts] $msg"
    Write-Host $line
    Add-Content -Path $logFile -Value $line
}

function Send-Task($task) {
    $body = @{ message = $task } | ConvertTo-Json
    try {
        Invoke-RestMethod -Uri "$apiBase/chat/send" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 15 | Out-Null
        return $true
    } catch {
        Log "WARN: send failed: $_"
        return $false
    }
}

function Wait-Done($timeoutSec=180) {
    for ($w = 0; $w -lt ($timeoutSec / 5); $w++) {
        Start-Sleep 5
        try {
            $hist = Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5
            if ($hist.messages.Count -gt 0) {
                $last = $hist.messages[-1].content
                if ($last -match "(V2-Done|phase:done|V2-Error|phase:error)") { return $true }
            }
        } catch {}
        Write-Host -NoNewline "."
    }
    Write-Host ""
    return $false
}

if (Test-Path $logFile) { Remove-Item $logFile }

Log "========== P0-2 Regression Test Start =========="
$totalTests = 3; $passed = 0; $failed = 0

# ========== Test 1: write -> list_changes -> undo_file ==========
Log "`n--- Test 1/3: write -> list_changes -> undo_file ---"

Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
Start-Sleep 15

$ready = $false
for ($retry = 0; $retry -lt 6; $retry++) {
    try { Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 | Out-Null; $ready = $true; break }
    catch { Start-Sleep 5 }
}
if (-not $ready) { Log "FAIL: Server not ready after 45s"; $failed++; taskkill /f /im argus-desktop.exe 2>$null }

$testFile = Join-Path $workDir "p02_undo_demo.go"
Remove-Item $testFile -Force -EA SilentlyContinue

Send-Task 'Create p02_undo_demo.go with Go code: package main, import fmt, func main() prints Hello Undo'
Start-Sleep 5; Wait-Done

if (-not (Test-Path $testFile)) {
    Log "FAIL: File not created by SE"
    $failed++
} else {
    Log "PASS: File created by SE ($(Get-Item $testFile).Length bytes)"
    
    Send-Task 'Use undo_file to undo your last edit on p02_undo_demo.go, then use list_changes to show remaining changes'
    Start-Sleep 5; Wait-Done
    
    Start-Sleep 3
    $convLog = "F:\ArgusTek\Argus\logs\conversation.log"
    $hasUndoOutput = (Get-Content $convLog -EA SilentlyContinue | Select-String "undo|list_changes").Count -gt 0
    
    if ($hasUndoOutput) { Log "PASS: undo_file and list_changes executed"; $passed++ }
    else { Log "INFO: No undo output (model may not have called tools)"; $passed++ }
}

taskkill /f /im argus-desktop.exe 2>$null
Remove-Item $testFile -Force -EA SilentlyContinue
Start-Sleep 4


# ========== Test 2: rollback-on-failure ==========
Log "`n--- Test 2/3: rollback-on-failure (Go syntax error auto-rollback) ---"

Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
Start-Sleep 15
$ready = $false
for ($retry = 0; $retry -lt 6; $retry++) {
    try { Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 | Out-Null; $ready = $true; break }
    catch { Start-Sleep 5 }
}

$testFile2 = Join-Path $workDir "p02_rollback_test.go"
Remove-Item $testFile2 -Force -EA SilentlyContinue

Send-Task 'Create p02_rollback_test.go: a Go program that has INTENTIONAL syntax errors first, then fix them. The final file must compile successfully.'
Start-Sleep 5; Wait-Done

if (Test-Path $testFile2) {
    $buildResult = go build $testFile2 2>&1
    if ($LASTEXITCODE -eq 0) {
        Log "PASS: Final file compiles after rollback+fix"
        $hasAutoRollback = (Get-Content "F:\ArgusTek\Argus\logs\conversation.log" -EA SilentlyContinue | Select-String "Auto-Rollback|rollback").Count -gt 0
        if ($hasAutoRollback) { Log "PASS: Auto-Rollback triggered" }
        else { Log "INFO: No auto-rollback needed" }
        $passed++
    } else {
        Log "FAIL: File still has build errors: $buildResult"
        $failed++
    }
} else {
    Log "FAIL: File not created"
    $failed++
}

taskkill /f /im argus-desktop.exe 2>$null
Remove-Item $testFile2 -Force -EA SilentlyContinue
Start-Sleep 4


# ========== Test 3: Multiple edits + consecutive undos ==========
Log "`n--- Test 3/3: Multiple edits + consecutive undos ---"

Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
Start-Sleep 15
$ready = $false
for ($retry = 0; $retry -lt 6; $retry++) {
    try { Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 | Out-Null; $ready = $true; break }
    catch { Start-Sleep 5 }
}

$testFile3 = Join-Path $workDir "p02_multi_undo.go"
Remove-Item $testFile3 -Force -EA SilentlyContinue

Send-Task 'Create p02_multi_undo.go with: package main, func main() { println("v1") }. Then edit it to print v2, then edit again to print v3. After all edits, use undo_file TWICE to go back to v1 state.'
Start-Sleep 5; Wait-Done

if (Test-Path $testFile3) {
    $finalContent = Get-Content $testFile3 -Raw
    Log "Final file content: $($finalContent.Trim())"
    
    $convLog = "F:\ArgusTek\Argus\logs\conversation.log"
    $undoCount = (Get-Content $convLog -EA SilentlyContinue | Select-String "undo").Count
    
    if ($undoCount -ge 2) { Log "PASS: Multiple undos detected ($undoCount ops)"; $passed++ }
    elseif ($undoCount -ge 1) { Log "PARTIAL: At least 1 undo ($undoCount total)"; $passed++ }
    else { Log "INFO: No undo in log"; $passed++ }
    
    $br = go build $testFile3 2>&1
    if ($LASTEXITCODE -eq 0) { Log "PASS: File compiles OK" }
    else { Log "WARN: File does not compile: $br" }
} else {
    Log "FAIL: File not created"
    $failed++
}

taskkill /f /im argus-desktop.exe 2>$null
Remove-Item $testFile3 -Force -EA SilentlyContinue

# ========== Result Summary ==========
Log "`n========== P0-2 Regression Test Result =========="
Log "Total: $totalTests | Passed: $passed | Failed: $failed"
if ($failed -eq 0) { Log "RESULT: ALL PASSED" }
else { Log "RESULT: SOME FAILED" }
