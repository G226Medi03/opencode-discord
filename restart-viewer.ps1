$ErrorActionPreference = 'SilentlyContinue'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

# Stop the old viewer (the bot is not touched).
Get-CimInstance Win32_Process -Filter "Name='oc-viewer.exe'" | ForEach-Object {
    & taskkill /PID $_.ProcessId /T /F 2>$null | Out-Null
}

Start-Sleep -Seconds 2

# Launch detached through WMI so the viewer is not part of the bot's process tree.
$cmd = 'cmd.exe /c cd /d "' + $root + '" && go run ./cmd/oc-viewer'
Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $cmd } | Out-Null
