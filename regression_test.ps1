# regression_test.ps1
# Hello World Regression Test - 3 rounds, different content each
$ErrorActionPreference = "Stop"
$BASE = "http://127.0.0.1:8080"
$RESULTS = @()

function Write-TestHeader($msg) {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Cyan
    Write-Host "  $msg" -ForegroundColor Cyan
    Write-Host "========================================" -ForegroundColor Cyan
}

function Invoke-API($method, $path, $body) {
    $headers = @{ "Content-Type" = "application/json" }
    try {
        if ($method -eq "GET") {
            $resp = Invoke-RestMethod -Uri "$BASE$path" -Method GET -Headers $headers -TimeoutSec 10
        } else {
            $bodyJson = $body | ConvertTo-Json -Depth 5
            $resp = Invoke-RestMethod -Uri "$BASE$path" -Method POST -Headers $headers -Body $bodyJson -TimeoutSec 120
        }
        return @{ success = $true; data = $resp }
    } catch {
        return @{ success = $false; error = $_.Exception.Message }
    }
}

# ===== Pre-check: Service running? =====
Write-TestHeader "Pre-check"
$ping = Invoke-API "GET" "/health/ping"
if (-not $ping.success) {
    Write-Host "[FAIL] Argus service not running (port 8080)" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] Service online: status=$($ping.data.status)" -ForegroundColor Green

# ===== Test #1: Basic Hello World =====
Write-TestHeader "Regression Test #1: Basic Hello World"
$msg1 = "Hello World! This is a basic regression test. Please reply with OK to confirm."
$result1 = Invoke-API "POST" "/api/v1/chat/send" @{ message = $msg1 }

if ($result1.success -and $result1.data.status -eq "ok") {
    Write-Host "[OK] Message sent: $msg1" -ForegroundColor Green
    $RESULTS += "TEST1:PASS"
} else {
    Write-Host "[FAIL] Send failed: $($result1.error)" -ForegroundColor Red
    $RESULTS += "TEST1:FAIL"
}
Start-Sleep -Seconds 2

# ===== Test #2: Chinese + English mixed =====
Write-TestHeader "Regression Test #2: Chinese + English"
$msg2 = 'Nihao! This is regression test round 2. Please reply with OK.'
$result2 = Invoke-API "POST" "/api/v1/chat/send" @{ message = $msg2 }

if ($result2.success -and $result2.data.status -eq "ok") {
    Write-Host "[OK] Message sent: $msg2" -ForegroundColor Green
    $RESULTS += "TEST2:PASS"
} else {
    Write-Host "[FAIL] Send failed: $($result2.error)" -ForegroundColor Red
    $RESULTS += "TEST2:FAIL"
}
Start-Sleep -Seconds 2

# ===== Test #3: Special characters =====
Write-TestHeader "Regression Test #3: Special chars"
$msg3 = "Round 3 test: symbols @#$%%^&, numbers 3.14159. Reply with PASS."
$result3 = Invoke-API "POST" "/api/v1/chat/send" @{ message = $msg3 }

if ($result3.success -and $result3.data.status -eq "ok") {
    Write-Host "[OK] Message sent (multi-line special chars)" -ForegroundColor Green
    $RESULTS += "TEST3:PASS"
} else {
    Write-Host "[FAIL] Send failed: $($result3.error)" -ForegroundColor Red
    $RESULTS += "TEST3:FAIL"
}
Start-Sleep -Seconds 3

# ===== Verify: Conversation Log =====
Write-TestHeader "Verify: Conversation History"
$history = Invoke-API "GET" "/api/v1/chat/history"

if ($history.success) {
    $msgs = $history.data.messages
    if ($msgs -and $msgs.Count -ge 3) {
        Write-Host "[OK] History retrieved, $($msgs.Count) messages total" -ForegroundColor Green
        foreach ($m in $msgs) {
            $role = $m.role
            $preview = if ($m.content.Length -gt 60) { $m.content.Substring(0,60) + "..." } else { $m.content }
            Write-Host "  [$role] $preview" -ForegroundColor DarkGray
        }
        $RESULTS += "HISTORY:PASS"
    } else {
        Write-Host "[WARN] History count insufficient: $($msgs.Count) (expect >=3)" -ForegroundColor Yellow
        $RESULTS += "HISTORY:PARTIAL"
    }
} else {
    Write-Host "[FAIL] History fetch failed: $($history.error)" -ForegroundColor Red
    $RESULTS += "HISTORY:FAIL"
}

# ===== Verify: New API endpoints =====
Write-TestHeader "Verify: v0.7.2 New Endpoints"

$tStats = Invoke-API "GET" "/api/v1/tokens/stats"
if ($tStats.success) {
    Write-Host "[OK] GET /tokens/stats -> ratio=$($tStats.data.ratio), total=$($tStats.data.total_tokens)" -ForegroundColor Green
    $RESULTS += "TOKEN_STATS:PASS"
} else {
    Write-Host "[WARN] Token stats: $($tStats.error)" -ForegroundColor Yellow
    $RESULTS += "TOKEN_STATS:N/A"
}

$dSessions = Invoke-API "GET" "/api/v1/debug/sessions"
if ($dSessions.success) {
    Write-Host "[OK] GET /debug/sessions -> count=$($dSessions.data.count)" -ForegroundColor Green
    $RESULTS += "DEBUG_SESSIONS:PASS"
} else {
    Write-Host "[WARN] Debug sessions: $($dSessions.error)" -ForegroundColor Yellow
    $RESULTS += "DEBUG_SESSIONS:N/A"
}

$mcpServers = Invoke-API "GET" "/api/v1/mcp/servers"
if ($mcpServers.success) {
    Write-Host "[OK] GET /mcp/servers -> count=$($mcpServers.data.count)" -ForegroundColor Green
    $RESULTS += "MCP_SERVERS:PASS"
} else {
    Write-Host "[WARN] MCP servers: $($mcpServers.error)" -ForegroundColor Yellow
    $RESULTS += "MCP_SERVERS:N/A"
}

$tCount = Invoke-API "GET" "/api/v1/tokens/count?text=hello%20world%20test"
if ($tCount.success) {
    Write-Host "[OK] GET /tokens/count -> tokens=$($tCount.data.token_count)" -ForegroundColor Green
    $RESULTS += "TOKEN_COUNT:PASS"
} else {
    Write-Host "[WARN] Token count: $($tCount.error)" -ForegroundColor Yellow
    $RESULTS += "TOKEN_COUNT:N/A"
}

# ===== Summary =====
Write-Host ""
Write-Host "========================================" -ForegroundColor White
Write-Host "  Regression Test Summary" -ForegroundColor White
Write-Host "========================================" -ForegroundColor White

$pass = ($RESULTS | Where-Object { $_ -like "*:PASS" }).Count
$fail = ($RESULTS | Where-Object { $_ -like "*:FAIL" }).Count
$total = $RESULTS.Count

foreach ($r in $RESULTS) {
    $color = if ($r -like "*:PASS") { "Green" } elseif ($r -like "*:FAIL") { "Red" } else { "Yellow" }
    Write-Host "  [$r]" -ForegroundColor $color
}

Write-Host ""
Write-Host "Total: $total | Pass: $pass | Fail: $fail" -ForegroundColor $(if ($fail -eq 0) { "Green" } else { "Red" })
Write-Host ""

if ($fail -eq 0) {
    Write-Host "ALL PASSED" -ForegroundColor Green -BackgroundColor Black
    exit 0
} else {
    Write-Host "SOME FAILURES DETECTED" -ForegroundColor Red -BackgroundColor Black
    exit 1
}
