@echo off
REM ============================================================
REM  NOC Sentinel — AI NOC berbasis Go + WhatsApp Gateway
REM  Satu perintah: build bila perlu, jalankan, buka dashboard.
REM ============================================================
setlocal
cd /d "%~dp0"

set BIN=bin\ai-noc-go.exe
set PORT=8090

echo.
echo  ================================================
echo   NOC Sentinel - AI NOC + WhatsApp Gateway
echo  ================================================
echo.

where go >nul 2>nul
if errorlevel 1 (
  if not exist "%BIN%" (
    echo [X] Go tidak ditemukan di PATH dan %BIN% belum ada.
    echo     Install Go dari https://go.dev/dl/ lalu jalankan ulang.
    pause & exit /b 1
  )
)

REM Node.js diperlukan untuk WhatsApp Gateway (Go yang menyalakannya).
where node >nul 2>nul
if errorlevel 1 (
  echo [!] Node.js tidak ditemukan. WhatsApp Gateway tidak akan jalan.
  echo     Install dari https://nodejs.org/ ^(LTS^) untuk integrasi WA.
  echo.
)

REM Build ulang bila sumber lebih baru dari binary, atau binary belum ada.
if not exist "%BIN%" goto :build
for /f %%i in ('powershell -NoProfile -Command "(Get-ChildItem -Recurse -Include *.go,*.html | Sort-Object LastWriteTime -Descending | Select-Object -First 1).LastWriteTime.Ticks"') do set NEWEST=%%i
for /f %%i in ('powershell -NoProfile -Command "(Get-Item '%BIN%').LastWriteTime.Ticks"') do set BINTIME=%%i
if "%NEWEST%" GTR "%BINTIME%" goto :build
goto :run

:build
echo [*] Membangun ai-noc-go...
if not exist bin mkdir bin
go build -o "%BIN%" . || (echo [X] Build gagal. & pause & exit /b 1)
echo [+] Build selesai.

:run
echo.
echo  Dashboard  : http://127.0.0.1:%PORT%
echo  WA Gateway : http://127.0.0.1:%PORT%/whatsapp  ^(scan QR dari panel dashboard^)
echo  Hentikan   : tekan Ctrl+C di jendela ini
echo.
start "" "http://127.0.0.1:%PORT%"
"%BIN%" -addr :%PORT%
