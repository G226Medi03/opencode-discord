@echo off
:: 1. Get commit message
set /p msg="Commit message: "

echo [1/3] Staging...
git add "*.go" go.mod go.sum .gitignore .env.example bot-config.json channel-policy.json
git add autogit.bat

echo [2/3] Committing and pushing...
git commit -m "%msg%"
git push origin main
echo.

echo [3/3] Upload finished.
pause
