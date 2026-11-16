@echo off
powershell.exe -NoProfile -File "%~dp0start-windows.ps1" %*
if errorlevel 1 pause
