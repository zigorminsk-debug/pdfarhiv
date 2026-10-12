@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion
pushd "%~dp0"
title Сборка PDF Toolkit (Go)

echo ============================================
echo   Сборка PDF Toolkit 2.0 (Go / Win32 GUI)
echo ============================================
echo.

REM --- 1. Проверяем Go ---
where go >nul 2>nul
if errorlevel 1 (
    echo [ОШИБКА] Компилятор Go не найден в PATH.
    echo Скачайте Go: https://go.dev/dl/
    if not defined CI pause
    popd
    exit /b 1
)
for /f "delims=" %%v in ('go version') do set GO_VER=%%v
echo [OK] %GO_VER%
echo.

REM --- 2. Подготовка встроенного Ghostscript ---
set "BUNDLE_SRC=..\csharp\PdfToolkit\Bundle\gs10051-win64.zip"
set "BUNDLE_DST=internal\core\bundle\gs.zip"
if exist "%BUNDLE_SRC%" (
    echo [1/4] Копирование встроенного Ghostscript в bundle\gs.zip...
    copy /y "%BUNDLE_SRC%" "%BUNDLE_DST%" >nul
)

REM --- 3. Запуск тестов ядра ---
echo [2/4] Запуск тестов ядра (go test)...
if not "%SKIP_TESTS%"=="1" (
    go test -v ./internal/core/...
    if errorlevel 1 (
        echo [ОШИБКА] Тесты не пройдены.
        if not defined CI pause
        popd
        exit /b 1
    )
)
echo.

REM --- 4. Компиляция манифеста Windows (если есть windres) ---
echo [3/4] Подготовка ресурсов Win32...
where windres >nul 2>nul
if not errorlevel 1 (
    pushd cmd\pdftoolkit
    windres -i app.rc -o rsrc_windows_amd64.syso 2>nul
    popd
)

REM --- 5. Сборка однофайлового EXE без консольного окна ---
echo [4/4] Сборка dist\PdfToolkit_Go.exe...
if not exist "dist" mkdir "dist"
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

go build -trimpath -ldflags="-s -w -H=windowsgui" -o "dist\PdfToolkit_Go.exe" ./cmd/pdftoolkit
set BUILD_ERR=%ERRORLEVEL%

if exist "%BUNDLE_DST%" del /q "%BUNDLE_DST%"

if not "%BUILD_ERR%"=="0" (
    echo [ОШИБКА] Сборка не удалась.
    if not defined CI pause
    popd
    exit /b 1
)

echo.
echo ============================================
echo   ГОТОВО: %CD%\dist\PdfToolkit_Go.exe
echo   Нативный Win32 GUI со встроенным Ghostscript 10.05.1
echo ============================================
if not defined CI pause
popd
endlocal
