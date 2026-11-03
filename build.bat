@echo off
REM Builds APEX Billing into one program: apex-billing.exe
REM (the web pages are built first and placed inside it).
setlocal
cd /d "%~dp0"

echo [1/2] Building the web pages...
cd frontend
if not exist node_modules (
  call npm install || goto :fail
)
call npm run build || goto :fail
cd ..

echo [2/2] Building the program...
go build -ldflags "-s -w" -o apex-billing.exe . || goto :fail

echo.
echo Done: apex-billing.exe
echo Copy .env.example to .env, fill it in, then run apex-billing.exe
exit /b 0

:fail
echo Build failed.
exit /b 1
