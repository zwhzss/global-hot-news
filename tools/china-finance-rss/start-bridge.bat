@echo off
set "BRIDGE_DIR=%~dp0"
set "PYTHON=C:\Python314\python.exe"
if not exist "%PYTHON%" set "PYTHON=python"
set PORT=8053
netstat -ano | findstr /C:":%PORT%" | findstr "LISTENING" >nul 2>&1
if %errorlevel%==0 (
    echo Already running on port %PORT%
    exit /b 0
)
cd /d "%BRIDGE_DIR%"
rem Empty title after /MIN is required: without it cmd treats the quoted
rem python path as the window title and tries to run "server.py" directly
rem (fails: .PY is not in PATHEXT). Output goes to server.log for diagnosis.
start /B /MIN "" "%PYTHON%" -u server.py >> "%BRIDGE_DIR%\server.log" 2>&1
echo Started China Finance RSS Bridge on http://localhost:%PORT%/
echo Feeds: cls/telegraph, eastmoney/kuaixun, ths/kuaixun, jin10/flash, wallstreetcn/live
echo Health: http://localhost:%PORT%/healthz?check=1