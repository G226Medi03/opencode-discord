@echo off
:: 1. Get commit message
set /p msg="Commit message: "

echo [1/3] Staging...
:: Quoted pathspecs are expanded by git, so subfolders are included (cmd's *.go only matches the current folder)
git add "*.go"
git add "*.py"
:: Defensive: unstage auto-generated ent files if they exist (ignored when missing)
git rm --cached -r --ignore-unmatch -q ent 2>nul
:: Include this script itself
git add autogit.bat

echo [2/3] Committing and pushing...
git commit -m "%msg%"
git push origin main
echo.

echo [3/3] Go/Python upload finished.
pause
