@echo off
REM APEX Billing backup: the database and the payment slips, in a dated
REM folder. Keeps the newest 30. Run it daily (Task Scheduler, see
REM SECURITY.md) and copy the backup folder somewhere else too (another
REM drive or an encrypted USB disk kept off-site).
REM
REM The database password comes from your PostgreSQL password file
REM (%APPDATA%\postgresql\pgpass.conf), not from this script. See SECURITY.md.
setlocal enableextensions
cd /d "%~dp0\.."

if "%BACKUP_DIR%"=="" set "BACKUP_DIR=D:\apex-billing-backups"
if "%DB_USER%"=="" set "DB_USER=apex_billing_app"
if "%DB_NAME%"=="" set "DB_NAME=apex_billing"
if "%DB_HOST%"=="" set "DB_HOST=localhost"

REM Find pg_dump: on the PATH, or the newest PostgreSQL in Program Files.
set "PGDUMP="
for %%x in (pg_dump.exe) do set "PGDUMP=%%~$PATH:x"
if not defined PGDUMP for /d %%d in ("C:\Program Files\PostgreSQL\*") do if exist "%%d\bin\pg_dump.exe" set "PGDUMP=%%d\bin\pg_dump.exe"
if not defined PGDUMP (
  echo pg_dump.exe not found. Install PostgreSQL's command line tools or add its bin folder to PATH.
  exit /b 1
)

for /f %%i in ('powershell -NoProfile -Command "Get-Date -Format yyyy-MM-dd_HHmm"') do set "STAMP=%%i"
set "OUT=%BACKUP_DIR%\%STAMP%"
mkdir "%OUT%" || goto :fail

echo Backing up the database to %OUT% ...
"%PGDUMP%" -h %DB_HOST% -U %DB_USER% -d %DB_NAME% -Fc -w -f "%OUT%\apex_billing.dump" || goto :fail

echo Copying payment slips ...
if exist uploads (
  robocopy uploads "%OUT%\uploads" /E /NFL /NDL /NJH /NJS /NP >nul
  if errorlevel 8 goto :fail
)

REM Keep the newest 30 backups.
powershell -NoProfile -Command "Get-ChildItem -LiteralPath '%BACKUP_DIR%' -Directory | Sort-Object Name -Descending | Select-Object -Skip 30 | Remove-Item -Recurse -Force"

echo Backup done: %OUT%
exit /b 0

:fail
echo BACKUP FAILED. Check the messages above.
exit /b 1
