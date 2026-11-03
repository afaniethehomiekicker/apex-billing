@echo off
REM Builds APEX Billing for a Linux server (64-bit Intel/AMD): apex-billing
REM (no .exe). Run on this Windows PC, then copy the file to the server.
REM For an ARM server (uname -m says aarch64) run:  build-linux.bat arm64
setlocal
cd /d "%~dp0.."
set "ARCH=%~1"
if "%ARCH%"=="" set "ARCH=amd64"

echo [1/2] Building the web pages...
cd frontend
if not exist node_modules (
  call npm install || goto :fail
)
call npm run build || goto :fail
cd ..

echo [2/2] Building the program for Linux (%ARCH%)...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=%ARCH%
go build -ldflags "-s -w" -o apex-billing . || goto :fail

echo.
echo Done: apex-billing  (the Linux program). See DEPLOY-LINUX.md for the next steps.
exit /b 0

:fail
echo Build failed.
exit /b 1
