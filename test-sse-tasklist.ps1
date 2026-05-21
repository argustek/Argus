#!/usr/bin/env pwsh
param(
    [string]$BaseUrl = "http://localhost:8080",
    [string]$Token = "test123",
    [string]$TestMessage = "hello"
)

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

Write-Host "=== Argus SSE Quick Test ===" -ForegroundColor Cyan
Write-Host ""

Write-Host "[1] Reset..." -ForegroundColor Yellow
try { Invoke-RestMethod -Uri "$BaseUrl/admin/sse-reset" -Method Post -TimeoutSec 3 | Out-Null; Write-Host "  OK" -ForegroundColor Green }
catch { Write-Host "  SKIP" -ForegroundColor DarkGray }

Write-Host "[2] Send: $TestMessage" -ForegroundColor Yellow
$json = @{message=$TestMessage} | ConvertTo-Json -Compress
$outFile = Join-Path $env:TEMP "sse-$([guid]::NewGuid().ToString('N')).txt"

curl.exe -N -X POST --max-time 90 "$BaseUrl/api/v1/sse/subscribe" -H "Authorization: Bearer $Token" -H "Content-Type: application/json" -d $json -o "$outFile" 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Host "  curl exit: $LASTEXITCODE" -ForegroundColor DarkGray }

Write-Host "[3] Parse..." -ForegroundColor Yellow
$rawBytes = [System.IO.File]::ReadAllBytes($outFile)
$content = [System.Text.Encoding]::UTF8.GetString($rawBytes)
Write-Host "  Size: $($content.Length) chars ($($rawBytes.Length) bytes)" -ForegroundColor Gray

$events=@();$e="";$d=""
foreach($l in ($content -split "`n")) {
    $l=$l.Trim()
    if($l -match "^event:\s*(.+)$"){$e=$Matches[1]}
    elseif($l -match "^data:\s*(.+)$"){
        $d=$Matches[1]
        if($e -and $d){$events+=@{e=$e;d=$d};$e="";$d=""}
    }
}
Remove-Item $outFile -ErrorAction SilentlyContinue
Write-Host "  Events: $($events.Count)" -ForegroundColor Cyan
Write-Host ""

Write-Host "[4] Types:" -ForegroundColor Yellow
$events|ForEach-Object{$_.e}|Group-Object|Sort-Object Count -Descending|ForEach-Object{Write-Host "  $($_.Name) x$($_.Count)"-ForegroundColor White}
Write-Host ""

Write-Host "[5] Checks:" -ForegroundColor Yellow
$p=0;$f=0
function T($n,$x){if($x){Write-Host "  OK $n"-ForegroundColor Green;$script:p++}else{Write-Host "  FAIL $n"-ForegroundColor Red;$script:f++}}
$n=$events|ForEach-Object{$_.e}
T "pm_started" ($n -contains "pm_started")
T "tasklist_start PM" (($events|?{$_.d-match'"roleId".*"pm"'-and $_.e-eq'tasklist_start'}).Count-gt 0)
T "tasklist_update" (($events|?{$_.e-eq'tasklist_update'}).Count-gt 0)
T "detail field" (($events|?{$_.e-eq'tasklist_update'-and $_.d-match'detail'}).Count-gt 0)
T "tasklist_complete" (($events|?{$_.e-eq'tasklist_complete'}).Count-gt 0)
T "done" ($n -contains "done")
Write-Host ""
Write-Host "== $p / $($p+$f) ==" -ForegroundColor $(if($f){'Red'}else{'Green'})
