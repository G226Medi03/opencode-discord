@echo off
:: Move to the project folder (absolute path)
cd /d "D:\Coding\opencode_discord"

echo [1/1] Running: go run .
echo.
go run .

echo.
echo Exited with code %errorlevel%.
pause
