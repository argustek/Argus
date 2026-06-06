$ErrorActionPreference = "Stop"
$exe = "F:\ArgusTek\Argus\build\bin\argus-desktop.exe"
$logFile = "F:\ArgusTek\Argus\logs\regression_test.log"
$conversationLog = "F:\ArgusTek\Argus\logs\conversation.log"

function Write-Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    $line = "[$ts] $msg"
    Write-Host $line
    Add-Content -Path $logFile -Value $line -Encoding UTF8
}

function Wait-ForIdle($timeoutSec = 300) {
    $elapsed = 0
    while ($elapsed -lt $timeoutSec) {
        $lastLine = (Get-Content $conversationLog -Tail 1) -join ""
        if ($lastLine -match "phase:done|phase:error|V2-Done|V2-Error") { return $true }
        $proc = Get-Process "argus-desktop" -ErrorAction SilentlyContinue
        if (-not $proc) { return $true }
        Start-Sleep 5
        $elapsed += 5
    }
    return $false
}

function Get-Result() {
    $tail = Get-Content $conversationLog -Tail 15
    foreach ($line in $tail) {
        if ($line -match "V2-Done") { return "PASS" }
        if ($line -match "V2-Error") { return "FAIL" }
    }
    return "UNKNOWN"
}

Set-Content -Path $logFile -Value "" -Encoding UTF8

$proc = Get-Process "argus-desktop" -ErrorAction SilentlyContinue
if (-not $proc) {
    Write-Log "Starting argus-desktop..."
    Start-Process $exe
    Start-Sleep 8
} else {
    Write-Log "argus-desktop already running PID=$($proc.Id)"
}

Write-Log "========== Regression Test Start =========="
$pass = 0; $fail = 0

for ($i = 1; $i -le 10; $i++) {
    $taskName = "test_${i}.go"
    $prompt = "Write a Go program that prints Hello World to console. Save as ${taskName} and run it."
    
    $idx = "Test #${i}"
    Write-Log "--- ${idx} ---"
    Write-Log "Send: ${prompt}"

    Start-Process $exe -ArgumentList "--send", $prompt -WindowStyle Hidden
    
    $ok = Wait-ForIdle
    $result = Get-Result
    
    if ($result -eq "PASS") {
        Write-Log "${idx}: PASS OK"
        $pass++
    } elseif ($result -eq "FAIL") {
        Write-Log "${idx}: FAIL"
        $fail++
    } else {
        $p = Get-Process "argus-desktop" -ErrorAction SilentlyContinue
        if (-not $p) {
            Write-Log "${idx}: CRASH process dead"
            $fail++
            Start-Process $exe
            Start-Sleep 8
        } else {
            Write-Log "${idx}: TIMEOUT >5min"
            $fail++
        }
    }

    Start-Sleep 3
}

Write-Log "========== Result: ${pass}/10 PASS, ${fail}/10 FAIL =========="
