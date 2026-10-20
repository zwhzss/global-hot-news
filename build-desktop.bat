@echo off
REM ============================================================
REM build-desktop.bat
REM One-click build script for Global Hot News desktop application
REM Steps: 1) Build Angular frontend  2) Copy to frontend_dist
REM        3) Wails build (windows/amd64)  4) Wails-native NSIS installer
REM
REM The installer is produced by "wails build -nsis", which bundles the
REM Microsoft WebView2 bootstrapper, so the target machine does not need
REM WebView2 pre-installed. The previous hand-written NSIS script has been
REM retired (see git history / docs/AUDIT_REPORT.md section 4.1).
REM ============================================================

setlocal enabledelayedexpansion

REM Resolve project root (script is at project root level)
set "PROJECT_ROOT=%~dp0"
set "PROJECT_ROOT=%PROJECT_ROOT:~0,-1%"
set "BACKEND_DIR=%PROJECT_ROOT%\backend"
set "FRONTEND_DIR=%PROJECT_ROOT%\frontend\angular-app"
set "BUILD_BIN=%BACKEND_DIR%\build\bin"
set "FRONTEND_DIST=%BACKEND_DIR%\frontend_dist"
REM The Wails CLI refuses to build unless a main.go exists at the module root,
REM while the real entry point lives in cmd\app. Stage a copy and restore on exit.
set "STAGED_MAIN=%BACKEND_DIR%\main.go"
set "STAGED_MAIN_BAK=%BACKEND_DIR%\main.go.buildbak"

echo ========================================
echo Global Hot News - Desktop Build
echo ========================================
echo.

REM ============================================================
REM Step 0: Check prerequisites
REM ============================================================
echo [Step 0/4] Checking prerequisites...
where go >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go not found. Please install Go 1.21+
    pause
    exit /b 1
)

where node >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Node.js not found. Please install Node.js 18+
    pause
    exit /b 1
)

where wails >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Wails CLI not found. Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest
    pause
    exit /b 1
)

REM go-sqlite3 (gorm.io/driver/sqlite) requires CGO, which requires a C compiler.
REM Locate gcc: already on PATH, or fall back to common install locations
REM (MSYS2 ucrt64/mingw64, WinGet packages dir) in case this shell's PATH
REM predates the install.
where gcc >nul 2>&1
if %errorlevel% neq 0 if exist "C:\msys64\ucrt64\bin\gcc.exe" set "PATH=C:\msys64\ucrt64\bin;%PATH%"
where gcc >nul 2>&1
if %errorlevel% neq 0 if exist "C:\msys64\mingw64\bin\gcc.exe" set "PATH=C:\msys64\mingw64\bin;%PATH%"
where gcc >nul 2>&1
if %errorlevel% neq 0 (
    for /f "delims=" %%G in ('dir /b /ad /o-n "%LOCALAPPDATA%\Microsoft\WinGet\Packages\BrechtSanders.WinLibs*" 2^>nul') do (
        if exist "%LOCALAPPDATA%\Microsoft\WinGet\Packages\%%G\mingw64\bin\gcc.exe" (
            set "PATH=%PATH%;%LOCALAPPDATA%\Microsoft\WinGet\Packages\%%G\mingw64\bin"
            goto :GccFound
        )
    )
)
:GccFound
where gcc >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] gcc not found. go-sqlite3 needs CGO. Install MinGW-w64, e.g.:
    echo         winget install --id BrechtSanders.WinLibs.POSIX.UCRT
    echo         or install MSYS2 and: pacman -S mingw-w64-ucrt-x86_64-gcc
    pause
    exit /b 1
)
REM CGO must be on for go-sqlite3; the default go env may have it disabled.
set "CGO_ENABLED=1"

REM makensis is not invoked directly any more, but the Wails NSIS step needs it.
where makensis >nul 2>&1
if %errorlevel% neq 0 (
    if exist "%ProgramFiles(x86)%\NSIS\makensis.exe" (
        set "PATH=%PATH%;%ProgramFiles(x86)%\NSIS"
    ) else if exist "%ProgramFiles%\NSIS\makensis.exe" (
        set "PATH=%PATH%;%ProgramFiles%\NSIS"
    ) else (
        echo [ERROR] NSIS not found. Please install NSIS from https://nsis.org
        pause
        exit /b 1
    )
)

echo   Go: OK
echo   Node: OK
echo   Wails: OK
echo   NSIS: OK
echo   gcc: OK (CGO_ENABLED=1)
echo.

REM ============================================================
REM Step 1: Build Angular frontend
REM ============================================================
echo [Step 1/4] Building Angular frontend...
cd /d "%FRONTEND_DIR%"
if not exist node_modules (
    echo   Installing npm dependencies...
    call npm install
    if %errorlevel% neq 0 (
        echo [ERROR] npm install failed
        pause
        exit /b 1
    )
)
call npm run build
if %errorlevel% neq 0 (
    echo [ERROR] Frontend build failed
    pause
    exit /b 1
)
echo   Frontend built successfully
echo.

REM ============================================================
REM Step 2: Verify frontend output (Angular writes directly to frontend_dist)
REM ============================================================
echo [Step 2/4] Verifying embedded frontend assets...
REM angular.json sets outputPath.base to ..\..\backend\frontend_dist, so Step 1
REM already deleted the stale bundles and wrote the fresh ones here. There is
REM deliberately NO copy step: copying used to drift out of sync with the
REM build, which left a stale index.html pointing at an old main-*.js bundle.
if not exist "%FRONTEND_DIST%\index.html" (
    echo [ERROR] %FRONTEND_DIST%\index.html missing - check outputPath in frontend\angular-app\angular.json
    pause
    exit /b 1
)
for /f "delims=" %%f in ('dir /b /o-d "%FRONTEND_DIST%\main-*.js" 2^>nul') do (
    set "NEWEST_BUNDLE=%%f"
    goto :BundleFound
)
:BundleFound
if not defined NEWEST_BUNDLE (
    echo [ERROR] No main-*.js bundle found in %FRONTEND_DIST%
    pause
    exit /b 1
)
findstr /c:"%NEWEST_BUNDLE%" "%FRONTEND_DIST%\index.html" >nul
if errorlevel 1 (
    echo [ERROR] index.html does not reference newest bundle %NEWEST_BUNDLE% - stale assets
    pause
    exit /b 1
)
echo   Frontend assets verified in %FRONTEND_DIST% ^(%NEWEST_BUNDLE%^)
echo.

REM ============================================================
REM Step 3: Build Go backend with Wails (embeds frontend)
REM ============================================================
echo [Step 3/4] Building Go backend with Wails...
cd /d "%BACKEND_DIR%"

REM Stage main.go at the module root for the Wails CLI (never clobber an existing file)
if exist "%STAGED_MAIN%" (
    echo   Backing up existing main.go to main.go.buildbak
    move /Y "%STAGED_MAIN%" "%STAGED_MAIN_BAK%" >nul
)
copy /Y "%BACKEND_DIR%\cmd\app\main.go" "%STAGED_MAIN%" >nul
if not exist "%STAGED_MAIN%" (
    echo [ERROR] Failed to stage main.go at %STAGED_MAIN%
    pause
    exit /b 1
)

REM Download modules with Chinese proxy
set "GOPROXY=https://goproxy.cn,direct"
go mod download 2>nul

wails build -platform windows/amd64 -ldflags "-s -w" 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Wails build failed
    call :RestoreStagedMain
    pause
    exit /b 1
)

call :RestoreStagedMain
echo   Backend built successfully
echo.

REM ============================================================
REM Step 4: Wails-native NSIS installer (bundles the WebView2 bootstrapper)
REM ============================================================
echo [Step 4/4] Creating NSIS installer (wails build -nsis)...
cd /d "%BACKEND_DIR%"

REM Stage main.go again: the NSIS run re-invokes the Wails build pipeline
copy /Y "%BACKEND_DIR%\cmd\app\main.go" "%STAGED_MAIN%" >nul

wails build -platform windows/amd64 -ldflags "-s -w" -nsis 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Wails NSIS packaging failed
    call :RestoreStagedMain
    pause
    exit /b 1
)

call :RestoreStagedMain
echo   Installer created successfully
echo.

REM ============================================================
REM Summary
REM ============================================================
REM Resolve the installer name from wails.json (name + productVersion) so the
REM summary keeps working when those change: <name>-<arch>-installer.exe
set "APP_NAME="
for /f "usebackq delims=" %%V in (`node -p "require('%BACKEND_DIR:\=/%/wails.json').name" 2^>nul`) do set "APP_NAME=%%V"
set "APP_VERSION="
for /f "usebackq delims=" %%V in (`node -p "require('%BACKEND_DIR:\=/%/wails.json').info.productVersion" 2^>nul`) do set "APP_VERSION=%%V"
if "%APP_NAME%"=="" set "APP_NAME=global-hot-news"
if "%APP_VERSION%"=="" set "APP_VERSION=1.0.0"
set "INSTALLER=%BUILD_BIN%\%APP_NAME%-amd64-installer.exe"

echo ========================================
echo Build Complete!
echo ========================================
echo.
for %%F in ("%BUILD_BIN%\GlobalHotNews.exe") do echo   Executable: %%~fF (%%~zF bytes)
if not exist "%INSTALLER%" goto NoInstaller
for %%F in ("%INSTALLER%") do echo   Installer:  %%~fF (%%~zF bytes)
echo   App name:   %APP_NAME%
echo   Version:    %APP_VERSION%
echo   WebView2:   bootstrapper bundled (installs the runtime when missing)
goto SummaryDone

:NoInstaller
echo   Installer:  WARNING - %APP_NAME%-amd64-installer.exe was not found in %BUILD_BIN%

:SummaryDone
echo.
echo   Frontend dist: %FRONTEND_DIR%\dist\global-hot-news
echo ========================================
echo.
pause
exit /b 0

:RestoreStagedMain
REM Remove the staged main.go and put back any file that was there before the build
if exist "%STAGED_MAIN%" del /Q "%STAGED_MAIN%" >nul 2>&1
if exist "%STAGED_MAIN_BAK%" move /Y "%STAGED_MAIN_BAK%" "%STAGED_MAIN%" >nul
exit /b 0
