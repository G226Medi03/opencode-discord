@echo off
setlocal
cd /d "%~dp0"

echo [1/3] Stopping oc-discord...
taskkill /IM oc-discord.exe /T /F >nul 2>&1

echo [2/3] Waiting for the process to exit...
ping -n 3 127.0.0.1 >nul

echo [3/3] Starting oc-discord...
start "oc-discord" "%~dp0run.bat"

echo Restart requested.
exit /b 0
