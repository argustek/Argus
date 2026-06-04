# P0 Full Regression Suite - One Click Run
# Usage: powershell -ExecutionPolicy Bypass -File regress_p0_all.ps1
$ErrorActionPreference = "Continue"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$logFile = "F:\ArgusTek\Argus\logs\p0_regress_all.log"
$exe = "F:\ArgusTek\Argus\build\bin\argus-desktop.exe"

function Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    $line = "[$ts] $msg"
    Write-Host $line
    Add-Content -Path $logFile -Value $line -EA SilentlyContinue
}

if (Test-Path $logFile) { Remove-Item $logFile }

Log "============================================"
Log "  P0 FULL REGRESSION TEST SUITE"
Log "  P0-1: LSP Code Integration"
Log "  P0-2: Undo/Rollback"
Log "  P0-3: Multimodal Input (vision)"
Log "============================================"

$results = @()

# Pre-check: verify exe exists (already built with wails build)
if (-not (Test-Path $exe)) {
    Log "FATAL: exe not found at $exe"
    exit 1
}
Log "[PRE-CHECK] Exe OK: $(Get-Item $exe).Length bytes"

# Unit tests
Log "`n[UNIT TESTS] Running Go unit tests..."
Push-Location F:\ArgusTek\Argus
$unitResult = go test ./internal/ai/ -count=1 -json 2>&1
$unitPass = ($unitResult | Select-String '"Action":"pass"' | Measure-Object).Count
$unitFail = ($unitResult | Select-String '"Action":"fail"' | Measure-Object).Count
Pop-Location
Log "  Unit Tests: $unitPass passed, $unitFail failed"
$results += @{ name="Unit Tests"; passed=$unitPass; failed=$unitFail }

# Run each regression test
$tests = @(
    @{ name="P0-2 Undo"; script="regress_p02_undo.ps1" },
    @{ name="P0-1 LSP"; script="regress_p01_lsp.ps1" },
    @{ name="P0-3 Vision"; script="regress_p03_vision.ps1" }
)

foreach ($t in $tests) {
    Log "`n[REGRESS] Running $($t.name)..."
    $output = & "$scriptDir\$($t.script)" 2>&1
    # Find last non-empty result line
    $lastLine = ""
    if ($output -and $output.Count -gt 0) {
        $candidate = $output[-1]
        if ($candidate -is [string]) { $lastLine = $candidate.Trim() }
    }
    Log "  $($t.name): $lastLine"
    $resultStr = if ($lastLine) { $lastLine } else { "NO OUTPUT" }
    $results += @{ name=$t.name; result=$resultStr }
}

# Final report
Log "`n============================================"
Log "  FINAL REPORT"
Log "============================================"
foreach ($r in $results) {
    if ($r.ContainsKey("passed")) {
        Log "  [$($r.name)] $($r.passed) pass / $($r.fail) fail"
    } else {
        Log "  [$($r.name)] $($r.result)"
    }
}
Log "============================================"
