$ProgressPreference = "SilentlyContinue"
$BASE = "http://localhost:8080"

function Send-Chat-Retry($msg, $maxRetry = 10) {
    for ($i = 0; $i -lt $maxRetry; $i++) {
        if ($i -gt 0) {
            Write-Host "  Retry in 10s..." 
            Start-Sleep 10
        }
        $body = @{ message = $msg } | ConvertTo-Json -Compress
        try {
            $r = Invoke-RestMethod -Uri "$BASE/api/v1/chat/send" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 120
            if ($r.status -eq "ok") { return $r }
            Write-Host "  API: $($r.error)"
        } catch {
            Write-Host "  API ERROR: $_"
        }
    }
    return $null
}

function Wait-Idle($maxSec = 180) {
    for ($w = 5; $w -le $maxSec; $w += 5) {
        Start-Sleep 5
        $s = Invoke-RestMethod -Uri "$BASE/admin/status" -TimeoutSec 5 -ErrorAction SilentlyContinue
        if ($s -and $s.monitor.pmBusySince -eq 0 -and $s.monitor.seBusySince -eq 0 -and $s.chatManager.messageCount -ge 0) {
            Start-Sleep 3
            return $true
        }
    }
    return $false
}

Write-Host ("=" * 40)
Write-Host "Hello World Regression x5"
Write-Host ("=" * 40)

# Pre-check: is argus-desktop running?
try {
    $check = Invoke-RestMethod -Uri "$BASE/admin/status" -TimeoutSec 5 -ErrorAction Stop
    Write-Host "  Connected: $($check.version)"
} catch {
    Write-Host "  FATAL: argus-desktop not reachable at $BASE"
    Write-Host "  Start it first: .\build\bin\argus-desktop.exe"
    exit 1
}

$PASS = 0; $FAIL = 0

for ($n = 1; $n -le 5; $n++) {
    $fname = "hello_r$n.go"
    $msg = "Write a Go file named $fname. Print 'Hello World $n' and exit cleanly."

    Write-Host ""
    Write-Host ("--- Test #$n : $fname ---")

    $api = Send-Chat-Retry $msg
    if (-not $api) {
        Write-Host "  [FAIL] API"
        $FAIL++
        continue
    }

    if (Wait-Idle) {
        Write-Host "  IDLE"
        if (Test-Path "E:\TempArgusTest\$fname") {
            Write-Host "  [PASS]"
            $PASS++
        } else {
            Write-Host "  [FAIL] file not found"
            $FAIL++
        }
    } else {
        Write-Host "  [FAIL] timeout"
        $FAIL++
    }
    Start-Sleep 5
}

Write-Host ""
Write-Host ("=== DONE: PASS=$PASS FAIL=$FAIL ===")
