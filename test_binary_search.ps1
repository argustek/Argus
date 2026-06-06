$ErrorActionPreference = "Continue"

$logFile = "F:\ArgusTek\Argus\binary_search_results.log"
"" | Out-File -FilePath $logFile

$commitsToTest = @(
    @{hash="05c6c47"; desc="(baseline)"},
    @{hash="8a32e4a"; desc="SE retry loop"},
    @{hash="82807be"; desc="PM prompt"},
    @{hash="a061d64"; desc="Todo List feat"},
    @{hash="6158581"; desc="message tracing"}
)

function Write-Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    "$ts | $msg" | Out-File -Append -FilePath $logFile
    Write-Host "$ts | $msg"
}

function Test-GUICommit($commit) {
    Write-Log "=========================================="
    Write-Log "TESTING: $($commit.hash) - $($commit.desc)"
    Write-Log "=========================================="

    git checkout $commit.hash --force 2>&1 | Out-Null
    Start-Sleep 1

    Write-Log "Building..."
    $buildResult = wails build 2>&1 | Out-String
    if ($buildResult -notmatch "Built 'F:") {
        Write-Log "BUILD FAILED"
        return "BUILD-FAIL"
    }
    Write-Log "Build OK"

    Stop-Process -Name "argus-desktop" -Force -ErrorAction SilentlyContinue
    Start-Sleep 2

    Remove-Item "F:\ArgusTek\Argus\argus.log" -Force -ErrorAction SilentlyContinue

    Write-Log "Launching with --send (GUI SendMessage path)..."
    $proc = Start-Process -FilePath "F:\ArgusTek\Argus\build\bin\argus-desktop.exe" `
        -ArgumentList "--send", "Write a Go program that prints Hello World to console." `
        -PassThru

    Write-Log "Waiting 45s for processing..."
    Start-Sleep 45

    $logContent = ""
    if (Test-Path "F:\ArgusTek\Argus\argus.log") {
        $logContent = Get-Content "F:\ArgusTek\Argus\argus.log" -Raw
    }

    Write-Log "--- argus.log check ---"
    $hasSend = $logContent -match "SendMessage|准备处理消息"
    $hasPM = $logContent -match "pm_started|Phase.*PM|handleToPM|V2 Bridge.Process"
    $hasSE = $logContent -match "se_action|executeActions|Phase.*SE|SE execution"
    $hasDone = $logContent -match "phase:done|V2-Done|completed|success=true"
    $hasError = $logContent -match "error|Error|failed|FAILED"

    Write-Log "  SendMessage called: $hasSend"
    Write-Log "  PM started: $hasPM"
    Write-Log "  SE actions: $hasSE"
    Write-Log "  Done/Success: $hasDone"
    Write-Log "  Error: $hasError"

    Stop-Process -Name "argus-desktop" -Force -ErrorAction SilentlyContinue
    Start-Sleep 2

    if ($hasDone) {
        Write-Log "RESULT: PASS-FULL - Pipeline completed!"
        return "PASS-FULL"
    } elseif ($hasSE) {
        Write-Log "RESULT: PASS - PM+SE ran"
        return "PASS-SE"
    } elseif ($hasPM) {
        Write-Log "RESULT: PARTIAL - PM started only"
        return "PARTIAL-PM"
    } elseif ($hasSend) {
        Write-Log "RESULT: PARTIAL - SendMessage called but no PM"
        return "PARTIAL-NO-PM"
    } else {
        Write-Log "RESULT: FAIL - No activity at all"
        return "FAIL"
    }
}

Write-Log "=============================================="
Write-Log "GUI FRONTEND TEST (--send path via OnDomReady)"
Write-Log "Started: $(Get-Date)"
Write-Log "=============================================="
Write-Log ""

$results = @()

foreach ($commit in $commitsToTest) {
    $result = Test-GUICommit $commit
    $results += [PSCustomObject]@{Hash=$commit.hash; Desc=$commit.desc; Result=$result}
    Write-Log ">>> $result"
    Start-Sleep 3
}

git checkout main --force 2>&1 | Out-Null

Write-Log ""
Write-Log "=============================================="
Write-Log "FINAL RESULTS"
Write-Log "=============================================="
$results | Format-Table -AutoSize -Wrap | Out-String | Write-Log

$firstBreak = $results | Where-Object { $_.Result -notlike "PASS*" } | Select-Object -First 1
if ($firstBreak) {
    Write-Log ""
    Write-Log "FIRST BROKEN: $($firstBreak.Hash) - $($firstBreak.Desc)"
} else {
    Write-Log ""
    Write-Log "ALL PASS!"
}

Write-Log "Completed: $(Get-Date)"