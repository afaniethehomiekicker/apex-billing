@echo off
REM APEX Billing: put your changes live on the server.
REM
REM   deploy "what you changed"   save (commit) all changes, upload, deploy
REM   deploy                      upload and deploy what is already committed
REM
REM On the server it tests, builds, backs up, installs and checks the app;
REM if the new version doesn't start, the previous one is put back.
setlocal enabledelayedexpansion
cd /d "%~dp0"
set "SERVER=root@103.172.26.29"
set "MSG=%~1"

where git >nul 2>&1 || (echo Git is not installed. & exit /b 1)

REM ---- 1. Save the changes (commit) -----------------------------------------
set "DIRTY="
for /f "delims=" %%i in ('git status --porcelain') do set "DIRTY=1"
if defined DIRTY (
  if "!MSG!"=="" (
    echo You have changes that are not saved in Git yet:
    git status --short
    echo.
    echo Run again with a short description, for example:
    echo    deploy "fixed invoice total"
    exit /b 1
  )
  git add -A || goto :fail
  REM Safety net: never upload settings, keys or client files.
  set "BAD="
  for /f "delims=" %%f in ('git diff --cached --name-only') do (
    echo %%f| findstr /r /i /c:"\.env$" /c:"\.env\." /c:"\.pem$" /c:"\.key$" /c:"\.p12$" /c:"\.pfx$" /c:"^uploads/" /c:"^tls/" /c:"\.dump$" /c:"rootCA" >nul && (
      echo   NOT ALLOWED: %%f
      set "BAD=1"
    )
  )
  if defined BAD (
    git reset -q
    echo.
    echo Stopped: the files above must never go to GitHub. Nothing was uploaded.
    exit /b 1
  )
  echo Saving these changes:
  git diff --cached --stat
  git commit -q -m "!MSG!" || goto :fail
)

REM ---- 2. Upload to GitHub ----------------------------------------------------
echo.
echo ==^> Uploading to GitHub...
git push || goto :fail

REM ---- 3. Update the server -----------------------------------------------------
echo.
echo ==^> Updating the server (%SERVER%)...
ssh -o BatchMode=yes %SERVER% "cd /root/apex-billing && git pull --ff-only && bash scripts/update-server.sh"
if errorlevel 1 goto :fail

echo.
echo Done. Open https://103.172.26.29/ and check your change.
exit /b 0

:fail
echo.
echo DEPLOY STOPPED - read the messages above.
exit /b 1
