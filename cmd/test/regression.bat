@echo off
chcp 65001 >nul 2>&1
set BASE=http://localhost:8080

echo ========================================
echo   Hello World Regression Test x5
echo ========================================

call :run_test 1 hello_test1.go
call :run_test 2 hello_test2.go
call :run_test 3 hello_test3.go
call :run_test 4 hello_test4.go
call :run_test 5 hello_test5.go

echo.
echo ========================================
echo   Done! PASS=%PASS% FAIL=%FAIL%
echo ========================================
goto :eof

:run_test
set ROUND=%1
set FNAME=%2
echo.
echo --- Test #%ROUND%: %FNAME% ---

:: Send message via chat/send
curl.exe -s -X POST %BASE%/api/v1/chat/send -H "Content-Type: application/json" -d "{\"message\":\"Write a %FNAME% that prints Hello World Test %ROUND%\"}"
echo.

:: Wait for PM+SE to finish (poll every 5s, max 90s)
set W=0
:poll
timeout /t 5 /nobreak >nul 2>&1
set /a W+=5
if %W% gtr 90 goto timeout_check
curl.exe -s %BASE%/admin/status > _status.txt 2>nul
findstr /C:"pmBusySince\":0" _status.txt >nul 2>nul
if errorlevel 1 goto poll
findstr /C:"seBusySince\":0" _status.txt >nul 2>nul
if errorlevel 1 goto poll
goto check_file

:timeout_check
echo [WARN] Timeout after %W%s, checking anyway...

:check_file
echo [CHECK] Reading %FNAME%...
curl.exe -s "%BASE%/api/v1/read?path=%FNAME%" > _result.txt 2>nul
findstr /C:"success.*true" _result.txt >nul 2>nul
if errorlevel 1 (
    echo [FAIL] File not found or read error:
    type _result.txt
    set /a FAIL+=1
    goto :end_round
)
findstr /C:"Hello World" _result.txt >nul 2>nul
if errorlevel 1 (
    echo [WARN] File exists but Hello World not in preview
) else (
    echo [PASS] %FNAME% exists with Hello World!
    set /a PASS+=1
)

:end_round
del _status.txt _result.txt 2>nul
goto :eof
