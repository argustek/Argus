$LOG = "E:\ArgusTek\Argus\logs\conversation.log"
$STATUS = "http://localhost:8080/admin/status"
$SEND = "http://localhost:8080/api/v1/chat/send"

# Discover workdir from running instance
try {
    $status = Invoke-RestMethod -Uri $STATUS -Method Get -TimeoutSec 5
    $workDir = $status.chatManager.workDir
    Write-Host "WorkDir: $workDir" -ForegroundColor Cyan
} catch { Write-Host "FAIL: cannot reach Argus HTTP" -ForegroundColor Red; exit 1 }

Write-Host "=== Argus Regression Test ===" -ForegroundColor Cyan

$tests = @(
    @{ file="hello_reg_a.go"; msg="create hello_reg_a.go that prints 'Hello Regression A'" },
    @{ file="hello_reg_b.go"; msg="create hello_reg_b.go that prints 'Hello Regression B'" },
    @{ file="hello_reg_g.go"; msg="create hello_reg_g.go that prints 'Hello Regression G'" }
)

$passed = 0; $failed = 0
$total = $tests.Count
$start = Get-Date

foreach ($t in $tests) {
    Write-Host "`n--- [$($t.file)] ---" -ForegroundColor Yellow
    $logBefore = if (Test-Path $LOG) { (Get-Item $LOG).Length } else { 0 }
    
    $body = @{message = $t.msg } | ConvertTo-Json -Compress
    try { $null = Invoke-RestMethod -Uri $SEND -Method Post -Body $body -ContentType "application/json" -TimeoutSec 15 }
    catch { Write-Host "  SEND FAIL: $_" -ForegroundColor Red; $failed++; continue }
    Write-Host "  Sent: $($t.msg)" -ForegroundColor DarkGray
    
    # Wait and check conversation.log for V2-Done + file name
    $done = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Seconds 1.5
        $logAfter = if (Test-Path $LOG) { (Get-Item $LOG).Length } else { 0 }
        if ($logAfter -eq $logBefore) { continue }
        $logBefore = $logAfter
        $tail = Get-Content -LiteralPath $LOG -Tail 10 -Encoding UTF8 -ErrorAction SilentlyContinue
        foreach ($line in $tail) {
            if ($line -match "V2-Done success=true" -and $line -notmatch "actions=0") { $done = $true; break }
        }
        # Also check if file appears on disk
        $f = Get-ChildItem -LiteralPath $workDir -Filter $t.file -ErrorAction SilentlyContinue
        if ($f -and $done) { break }
    }
    
    if ($done -and (Test-Path (Join-Path $workDir $t.file))) {
        $passed++; Write-Host "  PASS ($workDir\$($t.file))" -ForegroundColor Green
    } else {
        $reason = if (-not $done) { "no V2-Done in conversation.log" } else { "file not in workdir" }
        $failed++; Write-Host "  FAIL: $reason" -ForegroundColor Red
    }
}

$elapsed = [math]::Round(((Get-Date) - $start).TotalSeconds)
Write-Host "`n=== $passed/$total passed, $failed failed (${elapsed}s) ===" -ForegroundColor $(if ($failed -eq 0) { "Green" } else { "Red" })
# Cleanup test files
Get-ChildItem -LiteralPath $workDir -Filter "hello_reg_*.go" -ErrorAction SilentlyContinue | Remove-Item -Force
if ($failed -gt 0) { exit 1 }
