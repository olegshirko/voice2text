@echo off
setlocal
chcp 65001 >nul
rem voice2text - offline speech to text from video or audio.
rem
rem   transcribe.bat <file> [lang]      or drag a file onto this .bat
rem
rem lang: ru (default), en, de, ... or auto. Writes <name>.txt and <name>.srt
rem next to the input file.

set "HERE=%~dp0"

if "%~1"=="--vcredist-only" ( call :vcredist & exit /b 0 )
if "%~1"=="" (
  echo usage: transcribe.bat ^<video-or-audio-file^> [lang]
  echo        or drag a file onto transcribe.bat
  pause
  exit /b 1
)
set "IN=%~1"
set "OUTDIR=%~dp1"
set "OUTNAME=%~n1"
set "LANGC=%~2"
if "%LANGC%"=="" set "LANGC=ru"

set "MODEL="
for %%f in ("%HERE%models\ggml-*.bin") do if not defined MODEL set "MODEL=%%~ff"
if not defined MODEL (
  echo no model found in %HERE%models
  pause
  exit /b 1
)

rem whisper-cli is built with MSVC and needs the VC++ runtime. Install it
rem from Microsoft if it is missing (asks for admin rights once).
if not exist "%SystemRoot%\System32\vcruntime140_1.dll" call :vcredist
if not exist "%SystemRoot%\System32\msvcp140.dll" call :vcredist

rem Paths with non-ASCII characters do not survive the trip into whisper-cli,
rem so it works on an ASCII temp file and the result is moved afterwards.
set "TMPBASE=%TEMP%\voice2text-%RANDOM%%RANDOM%"

echo [1/2] extracting audio...
"%HERE%bin\ffmpeg.exe" -nostdin -v error -y -i "%IN%" -vn -ac 1 -ar 16000 -c:a pcm_s16le "%TMPBASE%.wav"
if errorlevel 1 goto :err

echo [2/2] transcribing, lang=%LANGC%, threads=%NUMBER_OF_PROCESSORS%...
"%HERE%bin\whisper-cli.exe" -m "%MODEL%" -l %LANGC% -t %NUMBER_OF_PROCESSORS% -f "%TMPBASE%.wav" -otxt -osrt -of "%TMPBASE%" -np -pp
if errorlevel 1 goto :err

move /y "%TMPBASE%.txt" "%OUTDIR%%OUTNAME%.txt" >nul
move /y "%TMPBASE%.srt" "%OUTDIR%%OUTNAME%.srt" >nul
del "%TMPBASE%.wav" 2>nul

echo.
echo done: %OUTDIR%%OUTNAME%.txt
pause
exit /b 0

:err
del "%TMPBASE%.wav" 2>nul
echo.
echo failed.
pause
exit /b 1

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
