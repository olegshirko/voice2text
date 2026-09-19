@echo off
rem voice2text - HTTP server (OpenAI-compatible POST /v1/audio/transcriptions).
rem
rem   serve.bat                         listen on 127.0.0.1:8080
rem   serve.bat -addr 0.0.0.0:9000      listen on all interfaces
rem   serve.bat -emulate-429 every:3    every 3rd request gets 429
rem   serve.bat -h                      all flags
chcp 65001 >nul
if not exist "%SystemRoot%\System32\vcruntime140_1.dll" call "%~dp0transcribe.bat" --vcredist-only
"%~dp0voice2text-server.exe" %*
pause
