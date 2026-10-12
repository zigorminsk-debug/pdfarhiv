@echo off
chcp 65001 >nul
REM ==========================================
REM Сборка PDF Compressor в EXE для Windows
REM Запустите этот файл на Windows с установленным Python
REM ==========================================

echo 🔨 PDF Compressor — Сборка EXE
echo.

REM Проверяем Python
python --version 2>nul
if errorlevel 1 (
    echo ❌ Python не найден в PATH
    echo Установите Python с https://python.org (галочка "Add to PATH")
    if not defined CI pause
    exit /b 1
)

echo ✅ Python найден
echo.

REM Устанавливаем зависимости
echo 📦 Установка зависимостей...
pip install --quiet pyinstaller tkinterdnd2 pillow pypdf 2>nul
if errorlevel 1 (
    echo ⚠ Не удалось установить зависимости, пробую продолжить...
)

REM Собираем
echo 🔨 Запуск PyInstaller...
python build_exe.py

if errorlevel 1 (
    echo.
    echo ❌ Сборка не удалась
    if not defined CI pause
    exit /b 1
)

echo.
echo ==========================================
echo ✅ ГОТОВО!
echo.
echo 📁 EXE находится в: dist\PDF_Compressor.exe
echo.
echo 📋 Для работы на другом компьютере нужно:
echo    1. Скопировать PDF_Compressor.exe
echo    2. Установить Ghostscript (https://ghostscript.com/releases/gsdnld.html)
echo.
if not defined CI pause
