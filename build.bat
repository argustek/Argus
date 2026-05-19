@echo off
chcp 65001 >nul
echo ========================================
echo   Argus Full Build Script
echo ========================================
echo.

echo [1/5] Stopping running processes...
taskkill /f /im argus-desktop.exe 2>nul
echo.

echo [2/5] Removing old dist...
if exist frontend\dist (
    rmdir /s /q frontend\dist
    echo Removed frontend\dist
) else (
    echo frontend\dist not found, skipping
)
echo.

echo [3/5] Building frontend...
cd frontend
call npm run build
if errorlevel 1 (
    echo Frontend build failed!
    cd ..
    pause
    exit /b 1
)
cd ..
echo.

echo [4/5] Building Go application...
wails build
if errorlevel 1 (
    echo Go build failed!
    pause
    exit /b 1
)
echo.

echo [5/5] Starting application...
echo.
echo ========================================
echo   Build complete, launching Argus...
echo ========================================
echo.

start /b build\bin\argus-desktop.exe
echo Application started in background
