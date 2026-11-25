@echo off
call "%~dp0scripts\Build-Windows.cmd"
if errorlevel 1 (echo Build failed. & pause & exit /b 1)
pause
