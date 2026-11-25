@echo off
setlocal
cd /d "%~dp0\.."
where go >nul 2>&1 || (echo Go 1.23 or newer is required: https://go.dev/dl/ & exit /b 1)
if not exist "vendor\tor\windows\tor\tor.exe" (echo Tor bundle is missing. & exit /b 1)
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
if not exist dist mkdir dist
go build -trimpath -ldflags="-s -w -H windowsgui" -o dist\DarkusSurf.exe .
if errorlevel 1 exit /b 1
echo Built dist\DarkusSurf.exe
