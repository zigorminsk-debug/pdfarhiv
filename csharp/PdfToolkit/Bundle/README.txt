Встроенные компоненты PDF Toolkit
=================================

1. gs10051-win64.zip  (встраивается в PdfToolkit.exe)
   Ghostscript 10.05.1 (64-bit, Windows), портативная копия без установщика:
   bin/ (gswin64c.exe, gsdll64.dll), lib/, Resource/, iccprofiles/, COPYING.txt (текст лицензии).
   Источник: https://github.com/ArtifexSoftware/ghostpdl-downloads/releases/tag/gs10051
   Лицензия: GNU AGPL v3 (см. COPYING.txt). Исходный код: https://ghostscript.com/releases/
   Распаковывается при первом запуске в %LOCALAPPDATA%\PdfToolkit\gs-10.05.1.

2. Microsoft Visual C++ Redistributable (x64) — НЕ встроен, скачивается только если нет на ПК:
   https://aka.ms/vs/17/release/vc_redist.x64.exe
