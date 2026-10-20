@echo off
rem China Finance RSS Bridge - Stop Script

set PORT=8053

rem Find and kill process listening on port 8053
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":%PORT% LISTENING"') do (
    taskkill /F /PID %%a >nul 2>&1
    echo Stopped process PID %%a
)
echo China Finance RSS Bridge stopped.