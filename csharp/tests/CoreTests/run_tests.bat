@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion

pushd "%~dp0"

REM Ищем Ghostscript: 1) переменная GS_PATH, 2) стандартная установка, 3) PATH, 4) встроенный Bundle/gs10051-win64.zip
set "GS=%GS_PATH%"
if defined GS if not exist "%GS%" set "GS="

if not defined GS (
    for /d %%d in ("C:\Program Files\gs\gs*") do (
        if exist "%%~fd\bin\gswin64c.exe" set "GS=%%~fd\bin\gswin64c.exe"
    )
)
if not defined GS (
    for /d %%d in ("C:\Program Files (x86)\gs\gs*") do (
        if exist "%%~fd\bin\gswin32c.exe" set "GS=%%~fd\bin\gswin32c.exe"
    )
)
if not defined GS (
    for /f "delims=" %%p in ('where gswin64c.exe 2^>nul') do (
        if not defined GS set "GS=%%p"
    )
)
if not defined GS (
    set "BUNDLE_ZIP=%~dp0..\..\PdfToolkit\Bundle\gs10051-win64.zip"
    set "BUNDLE_DIR=%TEMP%\PdfToolkit_gs_10051"
    if exist "!BUNDLE_ZIP!" (
        if not exist "!BUNDLE_DIR!\bin\gswin64c.exe" (
            echo [ИНФО] Распаковка встроенного Ghostscript из Bundle\gs10051-win64.zip...
            powershell -NoProfile -Command "Expand-Archive -Path '!BUNDLE_ZIP!' -DestinationPath '!BUNDLE_DIR!' -Force"
        )
        if exist "!BUNDLE_DIR!\bin\gswin64c.exe" set "GS=!BUNDLE_DIR!\bin\gswin64c.exe"
    )
)

if not defined GS (
    echo [ОШИБКА] Ghostscript не найден. Установите его или задайте GS_PATH.
    popd
    exit /b 1
)
echo Ghostscript: %GS%
echo.

dotnet run -c Release -- "%GS%"
set RESULT=%ERRORLEVEL%
popd
exit /b %RESULT%
