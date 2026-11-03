@echo off
REM Checks the code builds: web pages, go vet, tests, program (to a temp file).
REM Results go to scripts\check-build.log. Does not touch apex-billing.exe.
setlocal
cd /d "%~dp0.."
set LOG=%~dp0check-build.log
echo === %date% %time% > "%LOG%"
echo Checking... (results in scripts\check-build.log)
cd frontend
call npm run build >> "%LOG%" 2>&1
echo NPM_EXIT=%errorlevel% >> "%LOG%"
cd ..
go vet ./... >> "%LOG%" 2>&1
echo VET_EXIT=%errorlevel% >> "%LOG%"
go test ./internal/... >> "%LOG%" 2>&1
echo TEST_EXIT=%errorlevel% >> "%LOG%"
go build -o "%TEMP%\apex-billing-check.exe" . >> "%LOG%" 2>&1
echo BUILD_EXIT=%errorlevel% >> "%LOG%"
echo DONE >> "%LOG%"
echo Done.
