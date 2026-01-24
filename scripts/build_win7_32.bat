@echo off
setlocal
cd /d "%~dp0.."

set GO20=go1.20.14

for /f "usebackq delims=" %%i in (`go env GOBIN`) do set "GOBIN=%%i"
if "%GOBIN%"=="" for /f "usebackq delims=;" %%i in (`go env GOPATH`) do set "GOPATH=%%i"
if "%GOBIN%"=="" set "GOBIN=%GOPATH%\\bin"
set "GO20_EXE=%GOBIN%\\%GO20%.exe"

if not exist "%GO20_EXE%" (
	echo Installing %GO20% toolchain...
	go install golang.org/dl/%GO20%@latest
	if errorlevel 1 goto :err
)

if not exist "%GO20_EXE%" (
	echo ERROR: %GO20_EXE% not found after install.
	exit /b 1
)

"%GO20_EXE%" download
if errorlevel 1 goto :err

echo Using %GO20%:
"%GO20_EXE%" version

REM Build for Win7 32-bit (Go 1.20 is the last version supporting Win7).
set GOOS=windows
set CGO_ENABLED=0
REM Use vendored deps (offline / reproducible builds)
set GOFLAGS=-mod=vendor

echo Building Release (GUI only, no console)...
"%GO20_EXE%" build -trimpath -ldflags "-H=windowsgui -w -s" -o ofind_win7_32_release.exe ./cmd/ofind
if errorlevel 1 goto :err

echo Building Debug (Console enabled for output observation, includes extension support)...
REM Debug exe name contains "debug": app enables debug logging/metrics.
"%GO20_EXE%" build -trimpath -o ofind_win7_32_debug.exe ./cmd/ofind
if errorlevel 1 goto :err

echo Build Complete.
if /i "%~1"=="--nopause" goto :eof
pause
goto :eof

:err
echo Build failed with errorlevel %errorlevel%.
exit /b %errorlevel%
