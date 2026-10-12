#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Автотесты PDF Toolkit: гоняют реальные операции через Ghostscript.
Запуск:  python tests/selftest.py
Требуется Ghostscript в системе. Создаёт временные PDF автоматически."""

import sys
import os
import tempfile
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from pdf_compressor import (  # noqa: E402
    find_ghostscript, gs_merge, gs_extract_pages, gs_page_count,
    parse_ranges, split_into_chunks, unique_path, format_size, PROFILES,
)
from pdf_compressor import PDFToolkitApp  # noqa: E402

PASSED = []
FAILED = []


def check(name: str, condition: bool, detail: str = "") -> None:
    (PASSED if condition else FAILED).append(name)
    mark = "✅" if condition else "❌"
    print(f"  {mark} {name}" + (f" — {detail}" if detail else ""))


def make_test_pdf(path: Path, pages: int = 5, noisy: bool = True, seed: int = 7) -> None:
    """Генерирует многостраничный PDF (шум + текст) через Pillow.
    seed разный для разных файлов, иначе Ghostscript схлопывает одинаковые страницы
    (что, кстати, доказывает работу DetectDuplicateImages)."""
    from PIL import Image, ImageDraw
    import random

    random.seed(seed)
    imgs = []
    for p in range(pages):
        img = Image.new("RGB", (1200, 1700), "white")
        d = ImageDraw.Draw(img)
        if noisy:
            for _ in range(120):
                x, y = random.randint(0, 1100), random.randint(0, 1600)
                w, h = random.randint(40, 200), random.randint(40, 200)
                d.rectangle([x, y, x + w, y + h],
                            fill=(random.randint(0, 255), random.randint(0, 255), random.randint(0, 255)))
        for i in range(25):
            d.text((60, 60 + i * 60), f"Page {p + 1}, line {i} - test PDF document", fill="black")
        imgs.append(img)
    imgs[0].save(path, save_all=True, append_images=imgs[1:], resolution=200.0)


def count_pages(path: Path, gs: str) -> int:
    return gs_page_count(gs, path) or 0


def main() -> int:
    gs = find_ghostscript()
    print(f"Ghostscript: {gs}\n")
    if not gs:
        print("❌ Ghostscript не найден — установите его и повторите")
        return 1

    tmp = Path(tempfile.mkdtemp(prefix="pdftk_test_"))
    print(f"Временная папка: {tmp}\n")

    print("[1] Генерация тестовых PDF...")
    pdf_a = tmp / "doc_a.pdf"
    pdf_b = tmp / "doc_b.pdf"
    make_test_pdf(pdf_a, pages=5, seed=7)
    make_test_pdf(pdf_b, pages=3, seed=99)  # другой seed — файлы не должны схлопываться
    print(f"  ✅ doc_a.pdf ({format_size(pdf_a.stat().st_size)}, 5 стр.)")
    print(f"  ✅ doc_b.pdf ({format_size(pdf_b.stat().st_size)}, 3 стр.)\n")

    print("[2] Подсчёт страниц...")
    check("page count doc_a = 5", count_pages(pdf_a, gs) == 5, f"получено {count_pages(pdf_a, gs)}")
    check("page count doc_b = 3", count_pages(pdf_b, gs) == 3, f"получено {count_pages(pdf_b, gs)}")
    print()

    print("[3] Объединение...")
    merged = tmp / "merged.pdf"
    gs_merge(gs, [pdf_a, pdf_b], merged)
    check("merged.pdf создан", merged.exists())
    check("страниц 5+3=8", count_pages(merged, gs) == 8, f"получено {count_pages(merged, gs)}")
    src_total = pdf_a.stat().st_size + pdf_b.stat().st_size
    check("размер ≈ сумма исходных (без перекодирования)",
          merged.stat().st_size >= src_total * 0.9,
          f"{format_size(merged.stat().st_size)} vs {format_size(src_total)}")
    print()

    print("[4] Извлечение диапазонов...")
    ext = tmp / "pages_2_4.pdf"
    gs_extract_pages(gs, pdf_a, 2, 4, ext)
    check("извлечено 3 страницы (2-4)", count_pages(ext, gs) == 3, f"получено {count_pages(ext, gs)}")

    ext_single = tmp / "page_5.pdf"
    gs_extract_pages(gs, pdf_a, 5, 5, ext_single)
    check("извлечена 1 страница (5)", count_pages(ext_single, gs) == 1)
    print()

    print("[5] Разбор диапазонов '1-3,5,7-'...")
    r = parse_ranges("1-3,5,7-", 10)
    check("'1-3,5,7-' -> [(1,3),(5,5),(7,10)]", r == [(1, 3), (5, 5), (7, 10)], f"{r}")
    r = parse_ranges("2", 5)
    check("'2' -> [(2,2)]", r == [(2, 2)], f"{r}")
    try:
        parse_ranges("0-2", 10)
        check("ошибка на '0-2'", False, "исключение не поднято")
    except ValueError:
        check("ошибка на '0-2'", True)
    try:
        parse_ranges("9-12", 5)
        check("ошибка на выход за пределы", False)
    except ValueError:
        check("ошибка на выход за пределы (9-12 при 5 стр.)", True)
    check("split_into_chunks(7,3) = [(1,3),(4,6),(7,7)]",
          split_into_chunks(7, 3) == [(1, 3), (4, 6), (7, 7)])

    print("\n[6] unique_path не перезатирает файлы...")
    p1 = unique_path(tmp / "merged.pdf")
    check("существующий merged.pdf -> merged (1).pdf", p1.name == "merged (1).pdf", p1.name)
    check("новый путь не занят", not (tmp / "fresh.pdf").exists() and unique_path(tmp / "fresh.pdf").name == "fresh.pdf")
    print()

    print("[7] Сжатие: АВТО-режим, цель 2x...")
    app = object.__new__(PDFToolkitApp)
    app.gs_path = gs
    app.stop_flag = False
    app._log = lambda m: None
    app.target_ratio = type("V", (), {"get": lambda s: 2.0})()

    src = tmp / "doc_a.pdf"
    orig = src.stat().st_size
    out = tmp / "compressed.pdf"
    size = app._auto_compress(src, out, orig)
    check("цель 2x достигнута", size > 0 and size <= orig / 2.0,
          f"{format_size(orig)} → {format_size(size)} (x{orig / size:.1f})" if size else "не удалось")
    check("страницы на месте", count_pages(out, gs) == 5, f"{count_pages(out, gs)} стр.")
    check("временных файлов не осталось", not list(tmp.glob("*.autotmp*")))

    print("\n[8] Все профили по отдельности...")
    for i, p in enumerate(PROFILES[1:], start=1):
        dst = tmp / f"profile_{i}.pdf"
        sz = app._compress_single(src, dst, p)
        pages_ok = count_pages(dst, gs) == 5
        check(f"«{p.name}» → {format_size(sz)}, стр. ок", sz > 0 and pages_ok)
    print()

    print("[9] Разделение на каждую страницу (как делает вкладка)...")
    outdir = tmp / "doc_a_pages"
    outdir.mkdir(exist_ok=True)
    pages = count_pages(src, gs)
    for n in range(1, pages + 1):
        f = outdir / f"doc_a_p{n:03d}.pdf"
        gs_extract_pages(gs, src, n, n, f)
    created = sorted(outdir.glob("*.pdf"))
    check(f"создано {pages} файлов по 1 стр.", len(created) == pages)
    check("каждый файл 1-страничный", all(count_pages(f, gs) == 1 for f in created))

    print("\n[10] Обработка повреждённого файла...")
    bad = tmp / "bad.pdf"
    bad.write_bytes(b"%PDF-1.4 broken garbage")
    try:
        size = app._auto_compress(bad, tmp / "bad_out.pdf", 1000)
        check("повреждённый PDF не роняет программу", size == 0, f"вернул {size}")
    except Exception as e:
        check("повреждённый PDF не роняет программу", False, f"{type(e).__name__}: {e}")

    print("\n" + "=" * 60)
    print(f"ИТОГО: {len(PASSED)} пройдено, {len(FAILED)} провалено")
    if FAILED:
        print("Провалены:")
        for name in FAILED:
            print(f"  ❌ {name}")
    import shutil as _sh
    _sh.rmtree(tmp, ignore_errors=True)
    return 1 if FAILED else 0


if __name__ == "__main__":
    sys.exit(main())
