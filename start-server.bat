@echo off
rem Interactive launcher for a self-hosted Wormhole server (Windows).
rem Creates .env on first run, installs Go if missing, builds and starts the server.
setlocal
cd /d "%~dp0"

set "ENV_FILE=.env"
set "BIN=bin\wormhole-server.exe"

if not exist "%ENV_FILE%" goto configure
set "REUSE=Y"
set /p "REUSE=Found existing %ENV_FILE%. Use it? [Y/n]: "
if /i "%REUSE%"=="n" goto configure
goto ensure_go

:configure
echo.
echo Wormhole needs a wildcard TLS certificate, issued through the Cloudflare DNS-01 challenge.
echo Create a Cloudflare API token with "Zone - DNS - Edit" permission for your domain.
echo.

:ask_token
set "CF_TOKEN="
set /p "CF_TOKEN=CLOUDFLARE_API_TOKEN: "
if "%CF_TOKEN%"=="" echo   This field is required.& goto ask_token

:ask_email
set "EMAIL="
set /p "EMAIL=EMAIL_ID (Let's Encrypt expiry notices): "
if "%EMAIL%"=="" echo   This field is required.& goto ask_email

:ask_domains
set "DOMAINS="
set /p "DOMAINS=DOMAINS, comma separated (e.g. example.com,*.example.com): "
if "%DOMAINS%"=="" echo   This field is required.& goto ask_domains
set "DOMAINS=%DOMAINS: =%"

for /f "tokens=1 delims=," %%a in ("%DOMAINS%") do set "FIRST=%%a"
if "%FIRST:~0,2%"=="*." set "FIRST=%FIRST:~2%"
set "HOST=%FIRST%"
set /p "HOST=SERVER_HOST [%FIRST%]: "

echo.
echo Staging certificates are NOT trusted by browsers. Use them while testing to avoid Let's Encrypt rate limits.
:ask_prod
set "PROD_ANSWER=N"
set /p "PROD_ANSWER=Use Let's Encrypt production certificates? [y/N]: "
if /i "%PROD_ANSWER:~0,1%"=="y" set "PRODUCTION=true"& goto write_env
if /i "%PROD_ANSWER:~0,1%"=="n" set "PRODUCTION=false"& goto write_env
goto ask_prod

:write_env
>  "%ENV_FILE%" echo CLOUDFLARE_API_TOKEN=%CF_TOKEN%
>> "%ENV_FILE%" echo EMAIL_ID=%EMAIL%
>> "%ENV_FILE%" echo DOMAINS=%DOMAINS%
>> "%ENV_FILE%" echo SERVER_HOST=%HOST%
>> "%ENV_FILE%" echo PRODUCTION=%PRODUCTION%
echo Wrote %ENV_FILE%

:ensure_go
call :check_go && goto build
if exist "%ProgramFiles%\Go\bin\go.exe" set "PATH=%ProgramFiles%\Go\bin;%PATH%"
call :check_go && goto build

where winget >nul 2>&1
if errorlevel 1 (
  echo Go was not found and winget is unavailable. Install Go from https://go.dev/dl/ and re-run.
  exit /b 1
)
echo Installing Go with winget ...
winget install -e --id GoLang.Go --accept-source-agreements --accept-package-agreements
if exist "%ProgramFiles%\Go\bin\go.exe" set "PATH=%ProgramFiles%\Go\bin;%PATH%"
call :check_go && goto build
echo Go was installed but is not on PATH yet. Open a new terminal and re-run this script.
exit /b 1

:build
echo Building ...
go build -ldflags="-s -w" -o "%BIN%" .\cmd\server
if errorlevel 1 exit /b 1

echo.
echo Make sure TCP ports 443 (web visitors) and 4443 (tunnel clients) are allowed through Windows Firewall.
echo.
"%BIN%"
exit /b %errorlevel%

rem Go >= 1.21 is enough: it fetches the exact toolchain named in go.mod on its own.
:check_go
where go >nul 2>&1 || exit /b 1
for /f "tokens=3" %%v in ('go version') do set "GOVER=%%v"
set "GOVER=%GOVER:go1.=%"
for /f "delims=." %%m in ("%GOVER%") do set "GOMINOR=%%m"
if %GOMINOR% GEQ 21 (exit /b 0) else (exit /b 1)
