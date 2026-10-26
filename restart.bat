@echo off
setlocal
cd /d "%~dp0"

echo Restarting oc-viewer (oc-discord is not touched)...
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0restart-viewer.ps1"
echo Viewer restart requested.
exit /b 0
