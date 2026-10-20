@echo off
rem News RSS Bridge - Stop Script

set PORT=1200

rem Find and kill process listening on port 1200
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":%PORT% LISTENING"') do (
    taskkill /F /PID %%a >nul 2>&1
    echo Stopped process PID %%a
)
echo News RSS Bridge stopped.
