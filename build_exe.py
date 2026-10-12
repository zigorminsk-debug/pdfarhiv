#!/usr/bin/env python3
"""Скрипт сборки PDF Compressor в .exe для Windows"""
import subprocess
import sys
import os
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

def main():
    script = Path(__file__).parent / "pdf_compressor.py"
    if not script.exists():
        print("❌ pdf_compressor.py не найден")
        return 1

    # Проверяем pyinstaller
    try:
        import PyInstaller
    except ImportError:
        print("📦 Устанавливаю PyInstaller...")
        subprocess.run([sys.executable, "-m", "pip", "install", "pyinstaller"], check=True)

    # Проверяем tkinterdnd2
    try:
        import tkinterdnd2
    except ImportError:
        print("📦 Устанавливаю tkinterdnd2 (для drag&drop)...")
        subprocess.run([sys.executable, "-m", "pip", "install", "tkinterdnd2"], check=True)

    # Создаём простую иконку если нет
    icon_path = Path(__file__).parent / "icon.ico"
    if not icon_path.exists():
        print("🎨 Создаю простую иконку...")
        create_simple_icon(icon_path)

    # Команда PyInstaller
    cmd = [
        sys.executable, "-m", "PyInstaller",
        "--onefile",                    # Один файл
        "--windowed",                   # Без консоли (GUI)
        "--name", "PDF_Compressor",     # Имя выходного файла
        "--clean",                      # Очистить кэш
        "--noconfirm",                  # Не спрашивать подтверждение
    ]

    # Иконка
    if icon_path.exists():
        cmd.extend(["--icon", str(icon_path)])

    # Добавляем README
    readme = Path(__file__).parent / "README.md"
    if readme.exists():
        cmd.extend(["--add-data", f"{readme}{os.pathsep}."])

    # Скрытые импорты
    cmd.extend([
        "--hidden-import", "tkinterdnd2",
        "--hidden-import", "tkinter",
        "--hidden-import", "queue",
        "--hidden-import", "pathlib",
    ])

    # Главный скрипт
    cmd.append(str(script))

    print("🔨 Запускаю PyInstaller...")
    print(f"   Команда: {' '.join(cmd)}")
    result = subprocess.run(cmd)

    if result.returncode == 0:
        exe_path = Path(__file__).parent / "dist" / "PDF_Compressor.exe"
        if exe_path.exists():
            size_mb = exe_path.stat().st_size / (1024*1024)
            print(f"\n✅ Готово! EXE: {exe_path}")
            print(f"   Размер: {size_mb:.1f} MB")
            print(f"\n📋 Для распространения нужен только этот файл + Ghostscript на целевой машине")
        else:
            print("⚠ Сборка прошла, но EXE не найден в dist/")
    else:
        print("❌ Ошибка сборки")
        return 1

    return 0


def create_simple_icon(path: Path):
    """Создаёт простую иконку 256x256 через Pillow"""
    try:
        from PIL import Image, ImageDraw, ImageFont
    except ImportError:
        subprocess.run([sys.executable, "-m", "pip", "install", "pillow"], check=True)
        from PIL import Image, ImageDraw, ImageFont

    img = Image.new("RGBA", (256, 256), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    # Фон - скруглённый квадрат
    draw.rounded_rectangle([16, 16, 240, 240], radius=32, fill=(30, 100, 200, 255))

    # Текст "PDF"
    try:
        font = ImageFont.truetype("arial.ttf", 90)
    except:
        font = ImageFont.load_default()

    text = "PDF"
    bbox = draw.textbbox((0, 0), text, font=font)
    w, h = bbox[2] - bbox[0], bbox[3] - bbox[1]
    draw.text(((256-w)//2, (256-h)//2 - 10), text, fill="white", font=font)

    # Стрелка вниз (сжатие)
    draw.polygon([
        (128, 170), (100, 140), (110, 140), (110, 120),
        (146, 120), (146, 140), (156, 140)
    ], fill="white")

    # Сохраняем как ICO с несколькими размерами
    img.save(path, format="ICO", sizes=[(16,16), (32,32), (48,48), (64,64), (128,128), (256,256)])
    print(f"   Иконка сохранена: {path}")


if __name__ == "__main__":
    sys.exit(main())