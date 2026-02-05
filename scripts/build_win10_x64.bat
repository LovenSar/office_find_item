@echo off
setlocal
cd /d "%~dp0.."

REM Build for Win10 64-bit
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
REM Use vendored deps (offline / reproducible builds)
set GOFLAGS=-mod=vendor

echo Building Release (GUI only, no console)...
go build -trimpath -ldflags "-H=windowsgui -w -s" -o ofind_win10_x64_release.exe ./cmd/ofind
if errorlevel 1 goto :err

echo Building Debug (Console enabled for output observation)...
go build -trimpath -o ofind_win10_x64_debug.exe ./cmd/ofind
if errorlevel 1 goto :err

echo Build Complete.
if /i "%~1"=="--nopause" goto :eof
pause
goto :eof

:err
echo Build failed with errorlevel %errorlevel%.
exit /b %errorlevel%
