@echo off
rem voice2text - HTTP server (OpenAI-compatible POST /v1/audio/transcriptions).
rem
rem   serve.bat                         listen on 127.0.0.1:8080
rem   serve.bat -addr 0.0.0.0:9000      listen on all interfaces
rem   serve.bat -emulate-429 every:3    every 3rd request gets 429
rem   serve.bat -h                      all flags
chcp 65001 >nul

rem whisper-cli and voice2text-server are built with MSVC and need the VC++
rem runtime. Install it from Microsoft if it is missing (asks for admin
rem rights once).
if not exist "%SystemRoot%\System32\vcruntime140_1.dll" call :vcredist
if not exist "%SystemRoot%\System32\msvcp140.dll" call :vcredist

"%~dp0voice2text-server.exe" %*
pause
exit /b 0

:vcredist
echo Visual C++ runtime is missing, downloading it from Microsoft...
set "VCR=%TEMP%\vc_redist.%PROCESSOR_ARCHITECTURE%.exe"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" (
  curl -L -o "%VCR%" https://aka.ms/vs/17/release/vc_redist.arm64.exe
) else (
  curl -L -o "%VCR%" https://aka.ms/vs/17/release/vc_redist.x64.exe
)
"%VCR%" /install /passive /norestart
exit /b 0
