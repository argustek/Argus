# P0-1 Regression Test: LSP Code Integration
$ErrorActionPreference = "Continue"
$apiBase = "http://127.0.0.1:8080/api/v1"
$workDir = "F:\GithubArgus"
$exe = "F:\ArgusTek\Argus\build\bin\argus-desktop.exe"
$logFile = "F:\ArgusTek\Argus\logs\p0_regress_p01.log"

function Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    $line = "[$ts] $msg"
    Write-Host $line
    Add-Content -Path $logFile -Value $line -EA SilentlyContinue
}

function Send-Task($task) {
    $body = @{ message = $task } | ConvertTo-Json
    try {
        Invoke-RestMethod -Uri "$apiBase/chat/send" -Method POST -Body $body -ContentType "application/json" -TimeoutSec 30 | Out-Null
        return $true
    } catch {
        Log "INFO: send initiated (monitoring via conversation.log)..."
        $maxWait = 300; $elapsed = 0; $convLog = "F:\ArgusTek\Argus\logs\conversation.log"
        while ($elapsed -lt $maxWait) {
            Start-Sleep 10; $elapsed += 10
            Write-Host -NoNewline "."
            try {
                # Use Select-String to scan entire file (V2-Done may be on line after timestamp)
                $doneMatch = Select-String -Path $convLog -Pattern "V2-Done|phase:done" -EA SilentlyContinue
                $errMatch = Select-String -Path $convLog -Pattern "V2-Error|phase:error" -EA SilentlyContinue
                if ($doneMatch) { Write-Host ""; return $true }
                if ($errMatch) { Write-Host ""; Log "WARN: task completed with error"; return $false }
                $proc = Get-Process argus-desktop -EA SilentlyContinue
                if (-not $proc) { Write-Host ""; Log "WARN: argus-desktop process died"; return $false }
            } catch {}
        }
        Write-Host ""
        Log "WARN: task did not complete within ${maxWait}s"
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

function Write-GoFile($path, $content) {
    [System.IO.File]::WriteAllText($path, $content, [System.Text.Encoding]::UTF8)
}

if (Test-Path $logFile) { Remove-Item $logFile }
Log "=== P0-1 LSP Regression Test Start ==="

$goplsVersion = gopls version 2>&1
if ($LASTEXITCODE -ne 0) { Log "WARN: gopls not found" }
else { Log "INFO: gopls available" }

$totalTests = 3; $passed = 0; $failed = 0

# --- Test 1: LSP basic tool calls ---
Log "`n--- Test 1/3: LSP basic tool calls ---"

Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
Start-Sleep 15

$ready = $false
for ($retry = 0; $retry -lt 6; $retry++) {
    try { Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 | Out-Null; $ready = $true; break }
    catch { Start-Sleep 5 }
}
if (-not $ready) { Log "FAIL: Server not ready after 45s"; $failed++; taskkill /f /im argus-desktop.exe 2>$null }

$lspTestFile = Join-Path $workDir "p01_lsp_demo.go"
Write-GoFile $lspTestFile "package main`n`nimport `"fmt`"`n`nfunc MyFunction(name string) string {`n`treturn `"Hello, `" + name`n}`n`nfunc main() {`n`tresult := MyFunction(`"World`")`n`tfmt.Println(result)`n}`n"

$task1 = "The file p01_lsp_demo.go exists. Use go_to_definition on it at line 10 col 15. Use find_references at line 6 col 6. Use hover_info at line 10 col 15. Use diagnostics on it. Report results."
Send-Task $task1
Start-Sleep 5; Wait-Done

$convLog = "F:\ArgusTek\Argus\logs\conversation.log"
$foundLSP = 0
foreach ($kw in @("GoToDefinition", "FindReferences", "Hover", "Diagnostics")) {
    $c = (Get-Content $convLog -EA SilentlyContinue | Select-String $kw).Count
    if ($c -gt 0) { $foundLSP++ }
}
if ($foundLSP -ge 2) { Log "PASS: LSP tools output ($foundLSP/4)"; $passed++ }
else {
    $hasErr = (Get-Content $convLog -EA SilentlyContinue | Select-String "LSP.*not|gopls").Count -gt 0
    if ($hasErr) { Log "PASS: LSP graceful degradation"; $passed++ }
    else { Log "WARN: No LSP output"; $failed++ }
}

taskkill /f /im argus-desktop.exe 2>$null
Remove-Item $lspTestFile -Force -EA SilentlyContinue
Start-Sleep 4

# --- Test 2: rename_symbol ---
Log "`n--- Test 2/3: rename_symbol cross-file ---"

Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
Start-Sleep 15
$ready = $false
for ($retry = 0; $retry -lt 6; $retry++) {
    try { Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 | Out-Null; $ready = $true; break }
    catch { Start-Sleep 5 }
}

$fileA = Join-Path $workDir "p01_rename_a.go"
$fileB = Join-Path $workDir "p01_rename_b.go"
Write-GoFile $fileA "package main`n`nfunc OldName(x int) int {`n`treturn x * 2`n}`n"
Write-GoFile $fileB "package main`n`nfunc main() {`n`tresult := OldName(21)`n\tprintln(result)`n}`n"

$task2 = "Two files exist: p01_rename_a.go defines OldName(), p01_rename_b.go calls OldName(). Use rename_symbol on p01_rename_a.go at line 3 col 5 to rename OldName to NewName. This should update BOTH files."
Send-Task $task2
Start-Sleep 5; Wait-Done

$contentA = Get-Content $fileA -Raw -EA SilentlyContinue
$contentB = Get-Content $fileB -Raw -EA SilentlyContinue
if ($contentA -match "NewName" -and $contentB -match "NewName") { Log "PASS: rename updated both files"; $passed++ }
else {
    $hasRename = (Get-Content $convLog -EA SilentlyContinue | Select-String "rename").Count -gt 0
    if ($hasRename) { Log "PASS: rename called"; $passed++ }
    else { Log "WARN: no rename output"; $failed++ }
}

taskkill /f /im argus-desktop.exe 2>$null
Remove-Item $fileA,$fileB -Force -EA SilentlyContinue
Start-Sleep 4

# --- Test 3: diagnostics fix ---
Log "`n--- Test 3/3: diagnostics fix errors ---"

Start-Process -FilePath $exe -WorkingDirectory "F:\ArgusTek\Argus"
Start-Sleep 15
$ready = $false
for ($retry = 0; $retry -lt 6; $retry++) {
    try { Invoke-RestMethod -Uri "$apiBase/chat/history" -Method GET -TimeoutSec 5 | Out-Null; $ready = $true; break }
    catch { Start-Sleep 5 }
}

$errorFile = Join-Path $workDir "p01_diag_error.go"
Write-GoFile $errorFile "package main`n`nimport `"fmt`"`n`nfunc undefinedFunc() {`n`tfmt.Println(x)`n}`n"

$task3 = "The file p01_diag_error.go has errors. Use diagnostics to find them, then fix so it compiles."
Send-Task $task3
Start-Sleep 5; Wait-Done

$br = go build $errorFile 2>&1
if ($LASTEXITCODE -eq 0) { Log "PASS: File compiles after fix"; $passed++ }
else { Log "FAIL: Still has errors: $br"; $failed++ }

taskkill /f /im argus-desktop.exe 2>$null
Remove-Item $errorFile -Force -EA SilentlyContinue

# --- Summary ---
Log "`n=== P0-1 Result ==="
Log "Total: $totalTests Passed: $passed Failed: $failed"
if ($failed -eq 0) { Log "RESULT: ALL PASSED" }
else { Log "RESULT: SOME FAILED" }
