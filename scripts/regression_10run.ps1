# Argus 10-Round Hello World Regression Test
# Sends via GUI (--send), validates via conversation.log
param()

$ErrorActionPreference = "Continue"
$ArgusExe = "F:\ArgusTek\Argus\build\bin\argus-desktop.exe"
$WorkDir = "F:\GithubArgus"
$LogFile = "F:\ArgusTek\Argus\logs\conversation.log"

# 10 test cases: (filename, message)
$tests = @(
    @{file="hw_alpha.go";  msg="Hello Alpha"},
    @{file="hw_beta.go";   msg="Hello Beta"},
    @{file="hw_gamma.go";  msg="Hello Gamma"},
    @{file="hw_delta.go";  msg="Hello Delta"},
    @{file="hw_epsilon.go";msg="Hello Epsilon"},
    @{file="hw_zeta.go";   msg="Hello Zeta"},
    @{file="hw_eta.go";    msg="Hello Eta"},
    @{file="hw_theta.go";  msg="Hello Theta"},
    @{file="hw_iota.go";   msg="Hello Iota"},
    @{file="hw_kappa.go";  msg="Hello Kappa Done"}
)

$passCount = 0
$failCount = 0
$allResults = New-Object System.Collections.ArrayList

Write-Host "========================================"
Write-Host "  Argus 10-Round Hello World Regression"
Write-Host "========================================"
Write-Host ""

foreach ($test in $tests) {
    $i = $tests.IndexOf($test) + 1
    $file = $test.file
    $msg = $test.msg

    Write-Host ("[{0:D2}/10] {1} -> '{2}' " -f $i, $file, $msg) -NoNewline

    # 1. Kill existing process
    taskkill /f /im argus-desktop.exe 2>$null | Out-Null
    Start-Sleep 2

    # 2. Verify process is dead
    $proc = Get-Process -Name "argus-desktop" -ErrorAction SilentlyContinue
    if ($proc) {
        Write-Host "[SKIP] process still alive" -ForegroundColor Yellow
        $failCount++
        [void]$allResults.Add(@{num=$i; file=$file; status="SKIP"; reason="process alive"})
        continue
    }

    # 3. Record log line count before test
    $beforeLines = 0
    if (Test-Path $LogFile) {
        $beforeLines = (Get-Content $LogFile).Count
    }

    # 4. Launch + send task via GUI path
    $task = "Write a Go program $file that prints '$msg' to console and run it"
    Start-Process -FilePath $ArgusExe -ArgumentList "--send", $task -WorkingDirectory "F:\ArgusTek\Argus"
    Write-Host "[RUN]" -NoNewline

    # 5. Wait for completion (max 75s)
    $maxWait = 75
    $waited = 0
    $finalStatus = "UNKNOWN"

    while ($waited -lt $maxWait) {
        Start-Sleep 5
        $waited += 5

        if (Test-Path $LogFile) {
            $recentLog = Get-Content $LogFile -Tail 10 -ErrorAction SilentlyContinue
            if ($recentLog -match "V2-Done success=true") {
                $finalStatus = "PASS"
                break
            }
            if ($recentLog -match "V2-Error" -and $recentLog -notmatch "circuit breaker") {
                $finalStatus = "FAIL"
                break
            }
        }
        Write-Host "." -NoNewline
    }

    # 6. Final check
    if ($finalStatus -eq "UNKNOWN" -and (Test-Path $LogFile)) {
        $finalCheck = Get-Content $LogFile -Tail 10 -ErrorAction SilentlyContinue
        if ($finalCheck -match "V2-Done success=true") {
            $finalStatus = "PASS"
        } elseif ($finalCheck -match "V2-Error") {
            $finalStatus = "FAIL"
        } else {
            $finalStatus = "TIMEOUT"
        }
    }

    # 7. Report result
    if ($finalStatus -eq "PASS") {
        Write-Host " PASS" -ForegroundColor Green
        $passCount++
        [void]$allResults.Add(@{num=$i; file=$file; status="PASS"})
    } else {
        Write-Host " $finalStatus" -ForegroundColor Red
        $failCount++
        $reason = "N/A"
        if (Test-Path $LogFile) {
            $tail = Get-Content $LogFile -Tail 5
            $reason = ($tail | Select-String "error|failed|ERROR" | Select-Object -Last 1) -replace ".*DEBUG: ", ""
            if (-not $reason) { $reason = ($tail | Select-Object -Last 2) -join " | " }
        }
        [void]$allResults.Add(@{num=$i; file=$file; status=$finalStatus; reason=$reason})
    }

    Start-Sleep 3
}

# Summary
Write-Host ""
Write-Host "========================================"
Write-Host "  RESULTS: $passCount PASS / $failCount FAIL (out of 10)"
Write-Host "========================================"

foreach ($r in $allResults) {
    $color = if ($r.status -eq "PASS") { "Green" } else { "Red" }
    Write-Host ("  [{0:D2}] {1} | {2}" -f $r.num, $r.file, $r.status) -ForegroundColor $color
    if ($r.status -ne "PASS") {
        Write-Host ("        reason: {0}" -f $r.reason) -ForegroundColor DarkYellow
    }
}
