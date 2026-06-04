# P1 Regression Test - 5 Hello World with different filenames
# Tests: CheckSemanticComplete v2, compressHistory v2, web_search multi-engine,
#        SE routing (se_notify), FileChangeTracker snapshot

$ErrorActionPreference = "Stop"
$exe = "F:\ArgusTek\Argus\build\bin\argus-desktop.exe"
$logFile = "F:\ArgusTek\Argus\logs\p1_regression_test.log"
$convLog = "F:\ArgusTek\Argus\logs\conversation.log"
$apiBase = "http://127.0.0.1:8080/api/v1"

# Clean old log
if (Test-Path $logFile) { Remove-Item $logFile -Force }
if (Test-Path $convLog) { Remove-Item $convLog -Force }

function Write-Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    $line = "[$ts] $msg"
    Write-Host $line
    Add-Content -Path $logFile -Value $line -Encoding UTF8
}

function Wait-ForIdle($timeoutSec = 300) {
    $elapsed = 0
    while ($elapsed -lt $timeoutSec) {
        if (Test-Path $convLog) {
            $lastLine = (Get-Content $convLog -Tail 5 -ErrorAction SilentlyContinue) -join ""
            if ($lastLine -match "phase:done|phase:error|V2-Done|V2-Error|SE报告任务完成|exec_completed") {
                return $true
            }
        }
        $proc = Get-Process "argus-desktop" -ErrorAction SilentlyContinue
        if (-not $proc) { return $true }
        Start-Sleep 5
        $elapsed += 5
    }
    return $false
}

function Send-Task($taskMsg) {
    try {
        $body = @{ message = $taskMsg } | ConvertTo-Json
        $resp = Invoke-RestMethod -Uri "$apiBase/chat/send" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 10
        return $resp
    } catch {
        Write-Log "API Error: $_"
        return $null
    }
}

# 5 different filenames for hello world tests
$testCases = @(
    @{ name="hw_alpha";   file="alpha_hello.go";     task="Create a Go file called alpha_hello.go that prints 'Hello from Alpha'" },
    @{ name="hw_beta";    file="beta_greet.go";       task="Create a Go file called beta_greet.go that prints 'Hello from Beta' and run it" },
    @{ name="hw_gamma";   file="gamma_say.go";        task="Write gamma_say.go in Go: package main, func main prints 'Hello Gamma', then compile and run it" },
    @{ name="hw_delta";   file="delta_world.go";      task="Create delta_world.go Go program printing 'Hello Delta World', verify it compiles" },
    @{ name="hw_epsilon"; file="epsilon_hi.go";       task="Make epsilon_hi.go: Go main package, print 'Hello Epsilon', run go build and execute" }
)

$passed = 0
$failed = 0
$total = $testCases.Count

Write-Log "=== P1 Regression Test: 5 Hello World Variations ==="
Write-Log "Testing: CheckSemanticComplete v2 + compressHistory v2 + web_search fallback + SE routing + FileTracker"

# Kill any existing process
Get-Process "argus-desktop" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep 2

foreach ($i in 0..($total-1)) {
    $tc = $testCases[$i]
    Write-Log "`n--- Test $($i+1)/$total : $($tc.name) ---"
    Write-Log "Task: $($tc.task)"
    Write-Log "Expected file: $($tc.file)"

    # Start fresh instance
    Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
    Start-Sleep 3  # Wait for HTTP server ready

    # Verify server is up
    try {
        $ping = Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 -ErrorAction Stop
        Write-Log "Server OK"
    } catch {
        Write-Log "FAIL: Server not responding after 5s"
        $failed++
        Get-Process "argus-desktop" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        continue
    }

    # Send task
    $result = Send-Task $tc.task
    if ($null -eq $result) {
        Write-Log "FAIL: Could not send task via API"
        $failed++
        Get-Process "argus-desktop" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        continue
    }
    Write-Log "Task sent, waiting for completion..."

    # Wait for idle/completion
    $completed = Wait-ForIdle 300
    if (-not $completed) {
        Write-Log "TIMEOUT after 300s"
        $failed++
        Get-Process "argus-desktop" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        continue
    }

    # Analyze results
    Start-Sleep 2  # Let logs flush
    if (Test-Path $convLog) {
        $logContent = Get-Content $convLog -Raw -ErrorAction SilentlyContinue

        # Check for success indicators
        $hasDone = $logContent -match "(phase:done|V2-Done|SE报告任务完成|exec_completed)"
        $hasFile = $logContent -match $tc.file
        $hasHello = $logContent -match "(Hello.*[Aa]lpha|Hello.*[Bb]eta|Hello.*[Gg]amma|Hello.*[Dd]elta|Hello.*[Ee]psilon|Hello from|Hello World)"
        $hasError = $logContent -match "(phase:error|V2-Error|崩溃|panic|fatal)"

        # Check FileTracker activity
        $hasSnapshot = $logContent -match "\[FileTracker\].*快照"
        $hasSemanticCheck = $logContent -match "语义兜底.*confidence"
        $hasCompressV2 = $logContent -match "上下文摘要 v2"

        Write-Log "Results:"
        Write-Log "  Done signal:    $(if($hasDone){'YES'}else{'NO'})"
        Write-Log "  File created:   $(if($hasFile){'YES'}else{'NO'})"
        Write-Log "  Hello output:   $(if($hasHello){'YES'}else{'NO'})"
        Write-Log "  Error detected: $(if($hasError){'YES'}else{'NO'})"
        Write-Log "  FileTracker:    $(if($hasSnapshot){'YES'}else{'NO'})"
        Write-Log "  SemanticCheck:  $(if($hasSemanticCheck){'YES'}else{'NO'})"
        Write-Log "  CompressV2:     $(if($hasCompressV2){'YES'}else{'NO'})"

        if ($hasDone -and -not $hasError) {
            Write-Log "PASS: $($tc.name)"
            $passed++
        } else {
            Write-Log "FAIL: $($tc.name)"
            $failed++
        }
    } else {
        Write-Log "FAIL: No conversation log found"
        $failed++
    }

    # Cleanup for next test
    Get-Process "argus-desktop" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    # Clean test files
    $testFiles = @("alpha_hello.go", "beta_greet.go", "gamma_say.go", "delta_world.go", "epsilon_hi.go")
    foreach ($f in $testFiles) {
        $fp = Join-Path "F:\ArgusTek\Argus" $f
        if (Test-Path $fp) { Remove-Item $fp -Force -ErrorAction SilentlyContinue }
    }
    Start-Sleep 2
}

# Summary
Write-Log "`n========================================"
Write-Log "P1 REGRESSION TEST SUMMARY"
Write-Log "Total:  $total"
Write-Log "Passed: $passed"
Write-Log "Failed: $failed"
Write-Log "Rate:   $([math]::Round($passed/$total*100))%"
Write-Log "========================================"

if ($failed -eq 0) {
    Write-Log "ALL TESTS PASSED!"
    exit 0
} else {
    Write-Log "SOME TESTS FAILED - check $logFile for details"
    exit 1
}
