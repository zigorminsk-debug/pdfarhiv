@echo off
chcp 65001 >nul
setlocal
pushd "%~dp0"
title Сборка PDF Toolkit (C#)

echo ============================================
echo   Сборка PDF Toolkit 2.0 (C# / .NET 8)
echo ============================================
echo.

REM --- 1. Проверяем .NET SDK ---
where dotnet >nul 2>nul
if errorlevel 1 (
    echo [ОШИБКА] Не найден .NET SDK 8.
    echo Скачайте: https://dotnet.microsoft.com/download/dotnet/8.0
    echo Нужен пакет ".NET SDK", не только Runtime.
    if not defined CI pause
    popd
    exit /b 1
)
for /f "delims=" %%v in ('dotnet --version') do set DOTNET_VER=%%v
echo [OK] .NET SDK: %DOTNET_VER%
echo.

REM --- 2. Тесты ядра (нужен Ghostscript) ---
echo [1/2] Запуск тестов ядра...
echo       (пропустить: установите переменную SKIP_TESTS=1)
if not "%SKIP_TESTS%"=="1" (
    if exist "tests\CoreTests\run_tests.bat" (
        call "tests\CoreTests\run_tests.bat"
        if errorlevel 1 (
            echo [ОШИБКА] Тесты не пройдены. Сборка остановлена.
            if not defined CI pause
            popd
            exit /b 1
        )
    ) else (
        echo [ПРОПУЩЕНО] tests\CoreTests\run_tests.bat не найден
    )
)
echo.

REM --- 3. Публикация однофайлового EXE ---
echo [2/2] Сборка PdfToolkit.exe (self-contained, один файл)...
set OUT=dist
if exist "%OUT%" rmdir /s /q "%OUT%"

dotnet publish "PdfToolkit\PdfToolkit.csproj" -c Release -r win-x64 ^
    --self-contained true ^
    -p:PublishSingleFile=true ^
    -p:IncludeNativeLibrariesForSelfExtract=true ^
    -p:EnableCompressionInSingleFile=true ^
    -p:DebugType=none ^
    -o "%OUT%"

if errorlevel 1 (
    echo.
    echo [ОШИБКА] Сборка не удалась. См. сообщения выше.
    if not defined CI pause
    popd
    exit /b 1
)

echo.
echo ============================================
echo   ГОТОВО: %CD%\%OUT%\PdfToolkit.exe
echo   Встроенный Ghostscript 10.05.1 включён внутрь EXE.
echo ============================================
if not defined CI pause
popd
endlocal
