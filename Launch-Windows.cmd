@echo off
cd /d "%~dp0"
if not exist "dist\DarkusSurf.exe" call scripts\Build-Windows.cmd
if errorlevel 1 (pause & exit /b 1)
start "" "%~dp0dist\DarkusSurf.exe"
