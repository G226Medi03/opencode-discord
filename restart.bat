@echo off
setlocal
cd /d "%~dp0"

echo [1/3] Stopping oc-viewer...
taskkill /IM oc-viewer.exe /T /F >nul 2>&1

echo [2/3] Waiting for the process to exit...
ping -n 3 127.0.0.1 >nul

echo [3/3] Starting oc-viewer...
start "oc-viewer" /D "%~dp0" cmd /c "go run ./cmd/oc-viewer"

echo Viewer restart requested. (oc-discord is not touched.)
exit /b 0
