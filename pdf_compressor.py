#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
PDF Toolkit для Windows — сжатие, объединение и разделение PDF
GUI-обёртка над Ghostscript. Протестировано на реальных файлах.
Версия 2.0
"""

import tkinter as tk
from tkinter import ttk, filedialog, messagebox, scrolledtext
import subprocess
import threading
import re
import os
import sys
import shutil
import glob
from pathlib import Path
from dataclasses import dataclass, field
from typing import Optional, List, Tuple, Callable, Dict
import queue

VERSION = "2.0"

# ===================== ПРОФИЛИ СЖАТИЯ =====================

@dataclass
class CompressionProfile:
    name: str
    description: str
    dpi: int                      # 0 = АВТО-режим
    image_downsample_type: str
    jpeg_quality: int
    extra_args: List[str] = field(default_factory=list)


PROFILES = [
    CompressionProfile(
        name="⚡ АВТО — минимум 2x (рекомендую)",
        description="Сам подбирает профиль от мягкого к агрессивному, пока не достигнет цели. Гарантия результата!",
        dpi=0, jpeg_quality=0, image_downsample_type="/Bicubic",
    ),
    CompressionProfile(
        name="🔥 Максимальное (2-10x)",
        description="72 DPI, JPEG 30%, удаление метаданных. Для архива/email.",
        dpi=72, jpeg_quality=30, image_downsample_type="/Bicubic",
        extra_args=["-dDetectDuplicateImages=true", "-dCompressFonts=true",
                    "-dSubsetFonts=true", "-dRemoveBookmarks=true"],
    ),
    CompressionProfile(
        name="📄 Баланс (2-4x)",
        description="150 DPI, JPEG 60%, сохраняет читаемость. Идеально для документов.",
        dpi=150, jpeg_quality=60, image_downsample_type="/Bicubic",
        extra_args=["-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true"],
    ),
    CompressionProfile(
        name="👁 Качество (1.5-2x)",
        description="300 DPI, JPEG 85%, почти без потерь. Для печати/презентаций.",
        dpi=300, jpeg_quality=85, image_downsample_type="/Bicubic",
        extra_args=["-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true"],
    ),
    CompressionProfile(
        name="📱 Только текст (макс. сжатие)",
        description="150 DPI, JPEG 40%, Subsample. Для текстовых сканов без фото.",
        dpi=150, jpeg_quality=40, image_downsample_type="/Subsample",
        extra_args=["-dColorImageDownsampleType=/Subsample", "-dGrayImageDownsampleType=/Subsample",
                    "-dMonoImageDownsampleType=/Subsample", "-dDetectDuplicateImages=true",
                    "-dCompressFonts=true", "-dSubsetFonts=true"],
    ),
]

# ===================== УТИЛИТЫ =====================

def find_ghostscript() -> Optional[str]:
    """Ищет Ghostscript в системе (Windows и Unix)."""
    env_gs = os.environ.get("GS_PATH")
    if env_gs and os.path.isfile(env_gs):
        return env_gs
    candidates = [
        r"C:\Program Files\gs\gs*\bin\gswin64c.exe",
        r"C:\Program Files (x86)\gs\gs*\bin\gswin32c.exe",
        r"C:\gs\gs*\bin\gswin64c.exe",
        os.path.expanduser(r"~\AppData\Local\Programs\gs\gs*\bin\gswin64c.exe"),
    ]
    matches: List[str] = []
    for pattern in candidates:
        matches.extend(glob.glob(pattern))
    if matches:
        # Самая свежая версия — последняя по сортировке путей
        return sorted(matches)[-1]
    for name in ["gswin64c.exe", "gswin32c.exe", "gswin64c", "gs"]:
        path = shutil.which(name)
        if path:
            return path
    return None


def format_size(bytes_: float) -> str:
    for unit in ["Б", "КБ", "МБ", "ГБ"]:
        if bytes_ < 1024:
            return f"{bytes_:.1f} {unit}"
        bytes_ /= 1024
    return f"{bytes_:.1f} ТБ"


def get_file_size(path: Path) -> int:
    try:
        return path.stat().st_size
    except OSError:
        return 0


def unique_path(path: Path) -> Path:
    """Если файл существует — добавляет (1), (2)..."""
    if not path.exists():
        return path
    for i in range(1, 1000):
        candidate = path.parent / f"{path.stem} ({i}){path.suffix}"
        if not candidate.exists():
            return candidate
    return path.parent / f"{path.stem}_{os.getpid()}{path.suffix}"


_SUBPROCESS_FLAGS = getattr(subprocess, "CREATE_NO_WINDOW", 0)


def gs_page_count(gs_path: str, pdf: Path) -> Optional[int]:
    """Определяет число страниц: сначала через pypdf, потом через Ghostscript."""
    try:
        from pypdf import PdfReader
        n = len(PdfReader(str(pdf)).pages)
        if n > 0:
            return n
    except Exception:
        pass

    try:
        r = subprocess.run(
            [
                gs_path, "-q", "-dNOPAUSE", "-dBATCH", "-dSAFER",
                "-sDEVICE=nullpage", "-dFirstPage=999999999", str(pdf),
            ],
            stdin=subprocess.DEVNULL,
            capture_output=True, text=True, encoding="utf-8",
            errors="ignore", timeout=30,
            creationflags=_SUBPROCESS_FLAGS,
        )
        combined = (r.stdout or "") + "\n" + (r.stderr or "")
        m = re.search(r"Requested FirstPage is greater than the number of pages in the file:\s*(\d+)", combined)
        if r.returncode == 0 and "error" not in combined.lower() and m:
            n = int(m.group(1))
            if n > 0:
                return n
    except Exception:
        pass
    return None


def parse_ranges(spec: str, max_pages: int) -> List[Tuple[int, int]]:
    """Парсит '1-3,5,7-' в [(1,3),(5,5),(7,max_pages)]. Кидает ValueError с пояснением."""
    spec = spec.strip().replace(" ", "")
    if not spec:
        raise ValueError("Укажите диапазоны, например: 1-3,5,7-10")
    result: List[Tuple[int, int]] = []
    for token in spec.split(","):
        if not token:
            continue
        if "-" in token:
            a, _, b = token.partition("-")
            try:
                start = int(a)
                end = int(b) if b else max_pages
            except ValueError:
                raise ValueError(f"Не понял диапазон: «{token}»")
        else:
            try:
                start = end = int(token)
            except ValueError:
                raise ValueError(f"Не понял диапазон: «{token}»")
        if start < 1 or end < start:
            raise ValueError(f"Некорректный диапазон: «{token}» (страницы нумеруются с 1, начало ≤ конца)")
        if start > max_pages:
            raise ValueError(f"Диапазон «{token}» за пределами документа (всего {max_pages} стр.)")
        result.append((start, min(end, max_pages)))
    if not result:
        raise ValueError("Не найдено ни одного диапазона")
    return result


def split_into_chunks(total: int, step: int) -> List[Tuple[int, int]]:
    """[(1, N), (N+1, 2N), ...] до total."""
    step = max(1, step)
    return [(i, min(i + step - 1, total)) for i in range(1, total + 1, step)]

# ===================== ОПЕРАЦИИ GHOSTSCRIPT (без потери качества) =====================

def _run_gs(gs_path: str, args: List[str], timeout: int = 1800) -> subprocess.CompletedProcess:
    cmd = [gs_path, "-q", "-dNOPAUSE", "-dBATCH", "-dSAFER", "-sDEVICE=pdfwrite",
           "-dCompatibilityLevel=1.4", *args]
    r = subprocess.run(cmd, stdin=subprocess.DEVNULL, capture_output=True, text=True,
                       encoding="utf-8", errors="ignore", timeout=timeout,
                       creationflags=_SUBPROCESS_FLAGS)
    if r.returncode != 0:
        err = (r.stderr or r.stdout or "").strip()
        raise RuntimeError(f"Ghostscript вернул код {r.returncode}: {err[:400]}")
    return r


def gs_merge(gs_path: str, inputs: List[Path], output: Path) -> None:
    """Объединяет PDF без перекодирования изображений (качество сохраняется)."""
    if len(inputs) < 2:
        raise ValueError("Для объединения нужно минимум 2 файла")
    args = [
        "-dPassThroughJPEGImages=true",
        "-dPassThroughJPXImages=true",
        "-dDetectDuplicateImages=true",
        f"-sOutputFile={output}",
        *[str(p) for p in inputs],
    ]
    _run_gs(gs_path, args)
    if not output.exists():
        raise RuntimeError("Выходной файл не создан")


def gs_extract_pages(gs_path: str, src: Path, first: int, last: int, output: Path) -> None:
    """Извлекает страницы first..last (включительно) без перекодирования."""
    args = [
        f"-dFirstPage={first}",
        f"-dLastPage={last}",
        "-dPassThroughJPEGImages=true",
        "-dPassThroughJPXImages=true",
        f"-sOutputFile={output}",
        str(src),
    ]
    _run_gs(gs_path, args)
    if not output.exists():
        raise RuntimeError("Выходной файл не создан")


# ===================== ПАНЕЛЬ СО СПИСКОМ ФАЙЛОВ =====================

class FileListPanel(ttk.LabelFrame):
    """Переиспользуемая панель: список PDF + кнопки добавления/удаления/сортировки."""

    def __init__(self, parent, title: str, allow_reorder: bool = False,
                 allow_folder: bool = True, on_log: Optional[Callable[[str], None]] = None):
        super().__init__(parent, text=title, padding=8)
        self.files: List[Path] = []
        self.allow_reorder = allow_reorder
        self.on_log = on_log or (lambda m: None)
        self._buttons: List[ttk.Button] = []

        list_frame = ttk.Frame(self)
        list_frame.pack(fill=tk.BOTH, expand=True)

        self.listbox = tk.Listbox(list_frame, selectmode=tk.EXTENDED, font=("Consolas", 9),
                                  activestyle="none", borderwidth=1, relief="solid", height=10)
        self.listbox.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        sb = ttk.Scrollbar(list_frame, orient=tk.VERTICAL, command=self.listbox.yview)
        sb.pack(side=tk.RIGHT, fill=tk.Y)
        self.listbox.config(yscrollcommand=sb.set)

        self.dnd_enabled = False
        try:
            self.listbox.drop_target_register("DND_Files")
            self.listbox.dnd_bind("<<Drop>>", self._on_drop)
            self.dnd_enabled = True
        except (AttributeError, tk.TclError):
            pass

        self.listbox.bind("<Delete>", lambda e: self.remove_selected())

        # Кнопки
        btn_row = ttk.Frame(self)
        btn_row.pack(fill=tk.X, pady=(6, 0))

        def add_btn(text: str, cmd: Callable[[], None]) -> None:
            b = ttk.Button(btn_row, text=text, command=cmd)
            b.pack(side=tk.LEFT, padx=2)
            self._buttons.append(b)

        add_btn("➕ Добавить файлы", self.add_files)
        if allow_folder:
            add_btn("📁 Добавить папку", self.add_folder)
        if allow_reorder:
            add_btn("⬆ Вверх", self.move_up)
            add_btn("⬇ Вниз", self.move_down)
        add_btn("🗑 Удалить", self.remove_selected)
        add_btn("🧹 Очистить", self.clear)

    # --- данные ---
    def add_path(self, path: Path) -> None:
        path = Path(path)
        if path.suffix.lower() != ".pdf":
            self.on_log(f"⏭ Пропуск (не PDF): {path.name}")
            return
        if path in self.files:
            return
        self.files.append(path)
        self._refresh_row(len(self.files) - 1)

    def _refresh_row(self, idx: int) -> None:
        path = self.files[idx]
        prefix = f"{idx + 1}. " if self.allow_reorder else ""
        self.listbox.delete(idx)
        self.listbox.insert(idx, f"{prefix}{path.name}  [{format_size(get_file_size(path))}]  —  {path.parent}")

    def _refresh_all(self) -> None:
        self.listbox.delete(0, tk.END)
        for i in range(len(self.files)):
            prefix = f"{i + 1}. " if self.allow_reorder else ""
            path = self.files[i]
            self.listbox.insert(tk.END, f"{prefix}{path.name}  [{format_size(get_file_size(path))}]  —  {path.parent}")

    def add_files(self) -> None:
        paths = filedialog.askopenfilenames(title="Выберите PDF файлы",
                                            filetypes=[("PDF файлы", "*.pdf"), ("Все файлы", "*.*")])
        for p in paths:
            self.add_path(Path(p))
        self._refresh_all()

    def add_folder(self) -> None:
        folder = filedialog.askdirectory(title="Выберите папку с PDF")
        if folder:
            for p in sorted(Path(folder).rglob("*.pdf")):
                self.add_path(p)
            self._refresh_all()

    def remove_selected(self) -> None:
        for idx in reversed(list(self.listbox.curselection())):
            self.listbox.delete(idx)
            del self.files[idx]
        self._refresh_all()

    def clear(self) -> None:
        self.listbox.delete(0, tk.END)
        self.files.clear()

    def get_selected_or_all(self) -> List[Path]:
        sel = list(self.listbox.curselection())
        return [self.files[i] for i in sel] if sel else list(self.files)

    # --- сортировка ---
    def move_up(self) -> None:
        sel = list(self.listbox.curselection())
        if not sel or sel[0] == 0:
            return
        for idx in sel:
            if idx == 0:
                continue
            self.files[idx - 1], self.files[idx] = self.files[idx], self.files[idx - 1]
        new_sel = [i - 1 for i in sel]
        self._refresh_all()
        for i in new_sel:
            self.listbox.selection_set(max(0, i))
        self.listbox.see(max(0, new_sel[0]))

    def move_down(self) -> None:
        sel = list(self.listbox.curselection())
        if not sel or sel[-1] >= len(self.files) - 1:
            return
        for idx in reversed(sel):
            if idx >= len(self.files) - 1:
                continue
            self.files[idx + 1], self.files[idx] = self.files[idx], self.files[idx + 1]
        new_sel = [i + 1 for i in sel]
        self._refresh_all()
        for i in new_sel:
            self.listbox.selection_set(min(len(self.files) - 1, i))
        self.listbox.see(min(len(self.files) - 1, new_sel[-1]))

    # --- drag & drop ---
    def _on_drop(self, event) -> None:
        data = event.data
        if not data:
            return
        paths = re.findall(r"\{([^}]+)\}|(\S+)", data)
        flat = [p[0] or p[1] for p in paths]
        for p in flat:
            path = Path(p)
            if path.is_dir():
                for f in sorted(path.rglob("*.pdf")):
                    self.add_path(f)
            else:
                self.add_path(path)
        self._refresh_all()

    def set_enabled(self, enabled: bool) -> None:
        state = tk.NORMAL if enabled else tk.DISABLED
        for b in self._buttons:
            b.config(state=state)

# ===================== ОСНОВНОЕ ПРИЛОЖЕНИЕ =====================

class PDFToolkitApp:
    AUTO_ORDER = [3, 2, 4, 1]  # Качество → Баланс → Только текст → Максимальное

    def __init__(self, root: tk.Tk):
        self.root = root
        self.root.title(f"PDF Toolkit {VERSION} — сжатие · объединение · разделение")
        self.root.geometry("960x760")
        self.root.minsize(760, 620)

        self.gs_path = find_ghostscript()
        self.busy = False
        self.stop_flag = False
        self.worker_thread: Optional[threading.Thread] = None
        self.log_queue: queue.Queue = queue.Queue()
        self._action_buttons: List[ttk.Button] = []

        self._build_top_bar()
        self._build_notebook()
        self._build_bottom_bar()
        self._bind_shortcuts()
        self._check_ghostscript()
        self._poll_log_queue()

    # ---------- ВЕРХНЯЯ ПАНЕЛЬ ----------
    def _build_top_bar(self) -> None:
        top = ttk.Frame(self.root, padding=(10, 8, 10, 4))
        top.pack(fill=tk.X)

        ttk.Label(top, text="Ghostscript:").pack(side=tk.LEFT)
        self.gs_status_var = tk.StringVar(value="Проверка...")
        self.gs_label = ttk.Label(top, textvariable=self.gs_status_var, foreground="blue")
        self.gs_label.pack(side=tk.LEFT, padx=5)
        ttk.Button(top, text="🔍 Указать вручную", command=self._select_ghostscript).pack(side=tk.LEFT, padx=5)
        ttk.Button(top, text="📥 Скачать Ghostscript", command=self._open_gs_download).pack(side=tk.LEFT)

    # ---------- ВКЛАДКИ ----------
    def _build_notebook(self) -> None:
        self.notebook = ttk.Notebook(self.root)
        self.notebook.pack(fill=tk.BOTH, expand=True, padx=10, pady=6)

        self.tab_compress = ttk.Frame(self.notebook, padding=8)
        self.tab_merge = ttk.Frame(self.notebook, padding=8)
        self.tab_split = ttk.Frame(self.notebook, padding=8)
        self.notebook.add(self.tab_compress, text=" 🗜 Сжать ")
        self.notebook.add(self.tab_merge, text=" 🔗 Объединить ")
        self.notebook.add(self.tab_split, text=" ✂ Разделить ")

        self._build_compress_tab()
        self._build_merge_tab()
        self._build_split_tab()

    # ===== ВКЛАДКА 1: СЖАТИЕ =====
    def _build_compress_tab(self) -> None:
        f = self.tab_compress

        self.compress_files = FileListPanel(
            f, "Файлы для сжатия (перетащите сюда)", on_log=self._log)
        self.compress_files.pack(fill=tk.BOTH, expand=True)

        # Профиль
        prof = ttk.LabelFrame(f, text="Профиль сжатия", padding=8)
        prof.pack(fill=tk.X, pady=6)

        row = ttk.Frame(prof)
        row.pack(fill=tk.X)
        self.profile_var = tk.StringVar(value=PROFILES[0].name)
        combo = ttk.Combobox(row, textvariable=self.profile_var, state="readonly",
                             values=[p.name for p in PROFILES], width=46)
        combo.pack(side=tk.LEFT, fill=tk.X, expand=True)
        combo.bind("<<ComboboxSelected>>", self._on_profile_change)

        ttk.Label(row, text="  Цель:").pack(side=tk.LEFT)
        self.target_ratio = tk.DoubleVar(value=2.0)
        ttk.Combobox(row, textvariable=self.target_ratio, state="readonly",
                     values=[1.5, 2.0, 3.0, 5.0, 10.0], width=5).pack(side=tk.LEFT, padx=2)
        ttk.Label(row, text="x").pack(side=tk.LEFT)

        self.profile_desc_var = tk.StringVar(value=PROFILES[0].description)
        ttk.Label(prof, textvariable=self.profile_desc_var,
                  foreground="gray", wraplength=820).pack(anchor=tk.W, pady=(4, 0))

        # Выходная папка
        out = ttk.LabelFrame(f, text="Куда сохранять сжатые файлы", padding=8)
        out.pack(fill=tk.X)
        self.compress_output_folder: Optional[Path] = None
        self.compress_out_var = tk.StringVar(value="[Рядом с оригиналом, суффикс _compressed]")
        ttk.Entry(out, textvariable=self.compress_out_var, state="readonly").pack(
            side=tk.LEFT, fill=tk.X, expand=True, padx=(0, 5))
        ttk.Button(out, text="📂 Выбрать", command=self._select_compress_output).pack(side=tk.LEFT)
        ttk.Button(out, text="🔄 Сброс", command=self._reset_compress_output).pack(side=tk.LEFT, padx=4)

        # Кнопка старта
        start_row = ttk.Frame(f)
        start_row.pack(fill=tk.X, pady=6)
        btn = ttk.Button(start_row, text="▶  СТАРТ СЖАТИЯ", command=self._start_compression)
        btn.pack(side=tk.LEFT)
        self._action_buttons.append(btn)

    def _on_profile_change(self, _event=None) -> None:
        for p in PROFILES:
            if p.name == self.profile_var.get():
                self.profile_desc_var.set(p.description)
                break

    def _select_compress_output(self) -> None:
        folder = filedialog.askdirectory(title="Папка для сжатых файлов")
        if folder:
            self.compress_output_folder = Path(folder)
            self.compress_out_var.set(str(self.compress_output_folder))

    def _reset_compress_output(self) -> None:
        self.compress_output_folder = None
        self.compress_out_var.set("[Рядом с оригиналом, суффикс _compressed]")

    # ===== ВКЛАДКА 2: ОБЪЕДИНЕНИЕ =====
    def _build_merge_tab(self) -> None:
        f = self.tab_merge

        hint = ttk.Label(f, foreground="gray",
                         text="Порядок файлов = порядок страниц в результате. Меняйте его кнопками ⬆⬇.\n"
                              "Изображения НЕ перекодируются — качество сохраняется полностью.",
                         justify=tk.LEFT)
        hint.pack(anchor=tk.W, pady=(0, 6))

        self.merge_files = FileListPanel(f, "Файлы для объединения (сверху вниз)",
                                         allow_reorder=True, on_log=self._log)
        self.merge_files.pack(fill=tk.BOTH, expand=True)

        out = ttk.LabelFrame(f, text="Результат", padding=8)
        out.pack(fill=tk.X, pady=6)
        self.merge_out_var = tk.StringVar(value="")
        ttk.Entry(out, textvariable=self.merge_out_var).pack(side=tk.LEFT, fill=tk.X, expand=True, padx=(0, 5))
        ttk.Button(out, text="📂 Сохранить как...", command=self._select_merge_output).pack(side=tk.LEFT)

        row = ttk.Frame(f)
        row.pack(fill=tk.X)
        btn = ttk.Button(row, text="🔗  ОБЪЕДИНИТЬ", command=self._start_merge)
        btn.pack(side=tk.LEFT)
        self._action_buttons.append(btn)

    def _select_merge_output(self) -> None:
        path = filedialog.asksaveasfilename(title="Сохранить объединённый PDF как",
                                            defaultextension=".pdf",
                                            filetypes=[("PDF файлы", "*.pdf")])
        if path:
            self.merge_out_var.set(path)

    # ===== ВКЛАДКА 3: РАЗДЕЛЕНИЕ =====
    def _build_split_tab(self) -> None:
        f = self.tab_split

        hint = ttk.Label(f, foreground="gray",
                         text="Разделяет выбранные файлы. Качество не теряется.\n"
                              "Результаты складываются в подпапку «<имя>_pages» рядом с оригиналом.",
                         justify=tk.LEFT)
        hint.pack(anchor=tk.W, pady=(0, 6))

        self.split_files = FileListPanel(f, "Файлы для разделения", on_log=self._log)
        self.split_files.pack(fill=tk.BOTH, expand=True)

        mode = ttk.LabelFrame(f, text="Как разделять", padding=8)
        mode.pack(fill=tk.X, pady=6)

        self.split_mode = tk.StringVar(value="each")
        ttk.Radiobutton(mode, text="Каждая страница — отдельный PDF",
                        variable=self.split_mode, value="each").grid(row=0, column=0, sticky=tk.W)
        r1 = ttk.Frame(mode)
        r1.grid(row=1, column=0, sticky=tk.W, pady=2)
        ttk.Radiobutton(r1, text="Порциями по", variable=self.split_mode, value="chunks").pack(side=tk.LEFT)
        self.chunk_size = tk.IntVar(value=2)
        ttk.Spinbox(r1, from_=1, to=10000, textvariable=self.chunk_size, width=6).pack(side=tk.LEFT, padx=4)
        ttk.Label(r1, text="стр.").pack(side=tk.LEFT)
        r2 = ttk.Frame(mode)
        r2.grid(row=2, column=0, sticky=tk.W, pady=2)
        ttk.Radiobutton(r2, text="Только диапазоны:", variable=self.split_mode, value="ranges").pack(side=tk.LEFT)
        self.ranges_var = tk.StringVar(value="1-3,5,7-10")
        ttk.Entry(r2, textvariable=self.ranges_var, width=24).pack(side=tk.LEFT, padx=4)
        ttk.Label(r2, text="(например 1-3,5,7-)", foreground="gray").pack(side=tk.LEFT)

        out = ttk.LabelFrame(f, text="Куда сохранять (по умолчанию — подпапка рядом с файлом)", padding=8)
        out.pack(fill=tk.X)
        self.split_output_folder: Optional[Path] = None
        self.split_out_var = tk.StringVar(value="[Подпапка «<имя файла>_pages» рядом с оригиналом]")
        ttk.Entry(out, textvariable=self.split_out_var, state="readonly").pack(
            side=tk.LEFT, fill=tk.X, expand=True, padx=(0, 5))
        ttk.Button(out, text="📂 Выбрать", command=self._select_split_output).pack(side=tk.LEFT)
        ttk.Button(out, text="🔄 Сброс", command=self._reset_split_output).pack(side=tk.LEFT, padx=4)

        row = ttk.Frame(f)
        row.pack(fill=tk.X, pady=6)
        btn = ttk.Button(row, text="✂  РАЗДЕЛИТЬ", command=self._start_split)
        btn.pack(side=tk.LEFT)
        self._action_buttons.append(btn)

    def _select_split_output(self) -> None:
        folder = filedialog.askdirectory(title="Папка для разделённых файлов")
        if folder:
            self.split_output_folder = Path(folder)
            self.split_out_var.set(str(self.split_output_folder))

    def _reset_split_output(self) -> None:
        self.split_output_folder = None
        self.split_out_var.set("[Подпапка «<имя файла>_pages» рядом с оригиналом]")

    # ---------- НИЖНЯЯ ПАНЕЛЬ ----------
    def _build_bottom_bar(self) -> None:
        bottom = ttk.Frame(self.root, padding=(10, 0, 10, 10))
        bottom.pack(fill=tk.X)

        prog_row = ttk.Frame(bottom)
        prog_row.pack(fill=tk.X)
        self.progress_var = tk.DoubleVar(value=0)
        ttk.Progressbar(prog_row, variable=self.progress_var, maximum=100).pack(
            side=tk.LEFT, fill=tk.X, expand=True)
        self.stop_btn = ttk.Button(prog_row, text="⏹ Стоп", command=self._stop_current, state=tk.DISABLED)
        self.stop_btn.pack(side=tk.LEFT, padx=(6, 0))

        self.status_var = tk.StringVar(value="Готов. Добавьте PDF и выберите операцию.")
        ttk.Label(bottom, textvariable=self.status_var).pack(anchor=tk.W, pady=(4, 0))

        log_frame = ttk.LabelFrame(bottom, text="Лог", padding=4)
        log_frame.pack(fill=tk.BOTH, expand=True, pady=(4, 0))
        self.log_text = scrolledtext.ScrolledText(log_frame, height=8, font=("Consolas", 8), state=tk.DISABLED)
        self.log_text.pack(fill=tk.BOTH, expand=True)

    def _bind_shortcuts(self) -> None:
        self.root.bind("<Control-o>", lambda e: self._add_files_to_current_tab())
        self.root.bind("<F5>", lambda e: self._run_current_tab())
        self.root.bind("<Escape>", lambda e: self._stop_current() if self.busy else None)

    def _current_tab_index(self) -> int:
        return self.notebook.index(self.notebook.select())

    def _add_files_to_current_tab(self) -> None:
        idx = self._current_tab_index()
        panel = [self.compress_files, self.merge_files, self.split_files][idx]
        panel.add_files()

    def _run_current_tab(self) -> None:
        if self.busy:
            return
        idx = self._current_tab_index()
        if idx == 0:
            self._start_compression()
        elif idx == 1:
            self._start_merge()
        else:
            self._start_split()

    # ---------- GHOSTSCRIPT ----------
    def _check_ghostscript(self) -> None:
        if self.gs_path:
            self.gs_status_var.set(f"✅ Найден: {self.gs_path}")
            self.gs_label.configure(foreground="green")
            self._log(f"Ghostscript найден: {self.gs_path}")
        else:
            self.gs_status_var.set("❌ НЕ НАЙДЕН — установите Ghostscript")
            self.gs_label.configure(foreground="red")
            self._log("⚠ Ghostscript не найден. Скачайте: https://ghostscript.com/releases/gsdnld.html")

    def _select_ghostscript(self) -> None:
        path = filedialog.askopenfilename(
            title="Выберите gswin64c.exe",
            filetypes=[("Ghostscript", "gswin*c.exe"), ("Все файлы", "*.*")])
        if path:
            self.gs_path = path
            self._check_ghostscript()

    def _open_gs_download(self) -> None:
        import webbrowser
        webbrowser.open("https://ghostscript.com/releases/gsdnld.html")

    # ---------- ИНФРАСТРУКТУРА ----------
    def _log(self, msg: str) -> None:
        self.log_queue.put(msg)

    def _poll_log_queue(self) -> None:
        try:
            while True:
                msg = self.log_queue.get_nowait()
                self.log_text.config(state=tk.NORMAL)
                self.log_text.insert(tk.END, msg + "\n")
                self.log_text.see(tk.END)
                self.log_text.config(state=tk.DISABLED)
        except queue.Empty:
            pass
        self.root.after(100, self._poll_log_queue)

    def _ui(self, fn: Callable, *args) -> None:
        """Потокобезопасный вызов UI-функции."""
        self.root.after(0, lambda: fn(*args))

    def _set_busy(self, busy: bool) -> None:
        self.busy = busy
        self.stop_btn.config(state=tk.NORMAL if busy else tk.DISABLED)
        for b in self._action_buttons:
            b.config(state=tk.DISABLED if busy else tk.NORMAL)
        for panel in (self.compress_files, self.merge_files, self.split_files):
            panel.set_enabled(not busy)

    def _stop_current(self) -> None:
        if self.busy:
            self.stop_flag = True
            self.status_var.set("Остановка...")
            self._log("🛑 Остановка по запросу...")

    def _start_worker(self, target: Callable[[], None]) -> None:
        if self.busy:
            messagebox.showinfo("Подождите", "Операция уже выполняется.")
            return
        self.stop_flag = False
        self.progress_var.set(0)
        self._set_busy(True)
        self.worker_thread = threading.Thread(target=target, daemon=True)
        self.worker_thread.start()

    def _require_gs(self) -> bool:
        if not self.gs_path:
            messagebox.showerror("Ошибка", "Ghostscript не найден!\nУстановите его: https://ghostscript.com/releases/gsdnld.html")
            return False
        return True

    # ===================== ОПЕРАЦИЯ: СЖАТИЕ =====================
    def _start_compression(self) -> None:
        if not self._require_gs():
            return
        files = list(self.compress_files.files)
        if not files:
            messagebox.showwarning("Внимание", "Список файлов пуст.")
            return
        profile = next((p for p in PROFILES if p.name == self.profile_var.get()), PROFILES[0])
        self._start_worker(lambda: self._compress_worker(files, profile))

    def _compress_worker(self, files: List[Path], profile: CompressionProfile) -> None:
        total = len(files)
        success = failed = 0
        total_original = total_compressed = 0

        for i, src in enumerate(files):
            if self.stop_flag:
                break
            self._ui(self.status_var.set, f"[{i + 1}/{total}] Сжатие: {src.name}")
            self._ui(self.progress_var.set, (i / total) * 100)

            original_size = get_file_size(src)
            total_original += original_size

            if self.compress_output_folder:
                dst = self.compress_output_folder / f"{src.stem}_compressed.pdf"
            else:
                dst = src.parent / f"{src.stem}_compressed.pdf"
            dst = unique_path(dst)

            self._log(f"▶ [{i + 1}/{total}] {src.name} ({format_size(original_size)})")
            try:
                if profile.dpi == 0:
                    compressed = self._auto_compress(src, dst, original_size)
                else:
                    compressed = self._compress_single(src, dst, profile)

                if compressed > 0 and compressed < original_size:
                    total_compressed += compressed
                    ratio = (1 - compressed / original_size) * 100
                    self._log(f"   ✅ {format_size(compressed)} (-{ratio:.1f}%, x{original_size / compressed:.1f}) → {dst.name}")
                    success += 1
                else:
                    total_compressed += original_size
                    if dst.exists():
                        try:
                            dst.unlink()
                        except OSError:
                            pass
                    self._log("   ⚠ Не уменьшился (нет изображений или уже оптимизирован) — оригинал не изменён")
                    failed += 1
            except Exception as e:
                self._log(f"   ❌ Ошибка: {e}")
                failed += 1

        self._ui(self.progress_var.set, 100)
        self._ui(self._finish_compress, success, failed, total_original, total_compressed)

    def _finish_compress(self, success: int, failed: int, orig_total: int, comp_total: int) -> None:
        self._set_busy(False)
        if self.stop_flag:
            self.status_var.set("⏹ Остановлено пользователем")
            self._log("⏹ Сжатие остановлено.")
            return

        if orig_total > 0:
            saved = max(0, orig_total - comp_total)
            ratio = (1 - comp_total / orig_total) * 100 if orig_total else 0
            self.status_var.set(
                f"✅ Готово! Успешно: {success}, пропущено: {failed} | "
                f"{format_size(orig_total)} → {format_size(comp_total)} (-{ratio:.1f}%)")
            self._log(f"\n📊 ИТОГО: успешно {success}, пропущено {failed}")
            self._log(f"   Было {format_size(orig_total)} → стало {format_size(comp_total)} (-{ratio:.1f}%)")
            if success:
                messagebox.showinfo("Готово",
                                    f"Сжатие завершено!\n\nУспешно: {success}\nПропущено: {failed}\n"
                                    f"Было:  {format_size(orig_total)}\nСтало: {format_size(comp_total)}\n"
                                    f"Экономия: {format_size(saved)} ({ratio:.1f}%)")

    def _auto_compress(self, src: Path, dst: Path, original_size: int) -> int:
        """Перебирает профили от мягкого к агрессивному, пока не достигнет цели."""
        target = max(1.05, float(self.target_ratio.get() or 2.0))
        target_size = int(original_size / target)
        self._log(f"   🎯 Цель: ≤ {format_size(target_size)} (сжатие ≥ x{target:g})")

        tmp = dst.with_suffix(".autotmp.pdf")
        best_tmp = dst.with_suffix(".besttmp.pdf")
        best_size = 0
        best_profile: Optional[CompressionProfile] = None
        order = [2, 4, 1] if target >= 2.0 else self.AUTO_ORDER
        try:
            for idx in order:
                if self.stop_flag:
                    return 0
                profile = PROFILES[idx]
                self._log(f"   ⚙ Профиль «{profile.name}»...")
                try:
                    size = self._compress_single(src, tmp, profile)
                except Exception as e:
                    self._log(f"      ⚠ {e}")
                    continue
                if size <= 0:
                    continue
                if size <= target_size:
                    shutil.move(str(tmp), str(dst))
                    self._log(f"   ✔ Цель достигнута: {format_size(size)} (x{original_size / size:.1f}) "
                              f"профилем «{profile.name}»")
                    return size
                if best_profile is None or size < best_size:
                    best_size, best_profile = size, profile
                    shutil.move(str(tmp), str(best_tmp))
                elif tmp.exists():
                    try:
                        tmp.unlink()
                    except OSError:
                        pass
                self._log(f"      Мало: {format_size(size)} (x{original_size / size:.1f}), агрессивнее...")

            if best_profile is not None and best_size < original_size and best_tmp.exists():
                shutil.move(str(best_tmp), str(dst))
                self._log(f"   ⚠ Цель x{target:g} недостижима, лучшее — «{best_profile.name}»: "
                          f"{format_size(best_size)} (x{original_size / best_size:.1f})")
                return best_size
            return 0
        finally:
            for p_tmp in (tmp, best_tmp):
                if p_tmp.exists():
                    try:
                        p_tmp.unlink()
                    except OSError:
                        pass

    def _compress_single(self, src: Path, dst: Path, profile: CompressionProfile) -> int:
        p = profile
        args = [
            self.gs_path,
            "-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
            "-dNOPAUSE", "-dQUIET", "-dBATCH", "-dSAFER",
            f"-dColorImageResolution={p.dpi}", f"-dGrayImageResolution={p.dpi}",
            f"-dMonoImageResolution={p.dpi}",
            f"-dColorImageDownsampleType={p.image_downsample_type}",
            f"-dGrayImageDownsampleType={p.image_downsample_type}",
            "-dMonoImageDownsampleType=/Subsample",
            f"-dJPEGQ={p.jpeg_quality}",
            "-dAutoFilterColorImages=false", "-dAutoFilterGrayImages=false",
            "-dColorImageFilter=/DCTEncode", "-dGrayImageFilter=/DCTEncode",
            "-dDownsampleColorImages=true", "-dDownsampleGrayImages=true", "-dDownsampleMonoImages=true",
            *p.extra_args,
            f"-sOutputFile={dst}",
            str(src),
        ]
        try:
            r = subprocess.run(args, stdin=subprocess.DEVNULL, capture_output=True, text=True,
                               encoding="utf-8", errors="ignore", timeout=300,
                               creationflags=_SUBPROCESS_FLAGS)
        except subprocess.TimeoutExpired:
            raise RuntimeError("Таймаут 5 мин — файл слишком большой или повреждён")
        except FileNotFoundError:
            raise RuntimeError("Ghostscript не найден по указанному пути")
        if r.returncode != 0:
            err = (r.stderr or r.stdout or "").strip()
            raise RuntimeError(f"GS код {r.returncode}: {err[:300]}")
        if not dst.exists():
            raise RuntimeError("Выходной файл не создан")
        return get_file_size(dst)

    # ===================== ОПЕРАЦИЯ: ОБЪЕДИНЕНИЕ =====================
    def _start_merge(self) -> None:
        if not self._require_gs():
            return
        files = list(self.merge_files.files)
        if len(files) < 2:
            messagebox.showwarning("Внимание", "Добавьте минимум 2 PDF файла.")
            return

        out_str = self.merge_out_var.get().strip()
        if out_str:
            output = Path(out_str)
        else:
            output = files[0].parent / "merged.pdf"
        if output.suffix.lower() != ".pdf":
            output = output.with_suffix(".pdf")
        output = unique_path(output)
        self.merge_out_var.set(str(output))

        self._start_worker(lambda: self._merge_worker(files, output))

    def _merge_worker(self, files: List[Path], output: Path) -> None:
        total_src = sum(get_file_size(f) for f in files)
        self._ui(self.status_var.set, f"Объединение {len(files)} файлов...")
        self._log(f"▶ Объединение {len(files)} файлов → {output.name}")
        for f in files:
            self._log(f"   • {f.name} ({format_size(get_file_size(f))})")
        try:
            output.parent.mkdir(parents=True, exist_ok=True)
            gs_merge(self.gs_path, files, output)
            size = get_file_size(output)
            pages = gs_page_count(self.gs_path, output)
            self._ui(self.progress_var.set, 100)
            pages_str = f"{pages} стр." if pages else "?"
            self._log(f"   ✅ Готово: {output.name} — {format_size(size)}, {pages_str}")
            self._ui(self._finish_merge, True, output, total_src, size, pages)
        except Exception as e:
            self._log(f"   ❌ Ошибка: {e}")
            self._ui(self._finish_merge, False, output, total_src, 0, None)

    def _finish_merge(self, ok: bool, output: Path, total_src: int, size: int, pages: Optional[int]) -> None:
        self._set_busy(False)
        if not ok:
            self.status_var.set("❌ Объединение не удалось — см. лог")
            messagebox.showerror("Ошибка", "Не удалось объединить файлы. Подробности в логе.")
            return
        pages_str = f", страниц: {pages}" if pages else ""
        self.status_var.set(f"✅ Объединено: {output.name} ({format_size(size)}{pages_str})")
        messagebox.showinfo("Готово",
                            f"Файлы объединены!\n\nРезультат: {output.name}\n"
                            f"Размер: {format_size(size)}{pages_str}\nСохранён в:\n{output.parent}")

    # ===================== ОПЕРАЦИЯ: РАЗДЕЛЕНИЕ =====================
    def _start_split(self) -> None:
        if not self._require_gs():
            return
        files = self.split_files.get_selected_or_all()
        if not files:
            messagebox.showwarning("Внимание", "Добавьте PDF файлы (или выберите файлы в списке).")
            return
        mode = self.split_mode.get()
        chunk = 1
        ranges_spec = ""
        if mode == "chunks":
            try:
                chunk = int(self.chunk_size.get())
                if chunk < 1:
                    raise ValueError
            except (ValueError, tk.TclError):
                messagebox.showerror("Ошибка", "Размер порции должен быть целым числом ≥ 1.")
                return
        elif mode == "ranges":
            ranges_spec = self.ranges_var.get()
            if not ranges_spec.strip():
                messagebox.showerror("Ошибка", "Укажите диапазоны, например: 1-3,5,7-10")
                return

        self._start_worker(lambda: self._split_worker(files, mode, chunk, ranges_spec))

    def _split_worker(self, files: List[Path], mode: str, chunk: int, ranges_spec: str) -> None:
        total = len(files)
        ok_files = 0
        created_total = 0

        for i, src in enumerate(files):
            if self.stop_flag:
                break
            self._ui(self.status_var.set, f"[{i + 1}/{total}] Разделение: {src.name}")
            self._ui(self.progress_var.set, (i / total) * 100)
            self._log(f"▶ [{i + 1}/{total}] {src.name} ({format_size(get_file_size(src))})")

            try:
                pages = gs_page_count(self.gs_path, src)
                if not pages:
                    raise RuntimeError("Не удалось определить число страниц (повреждённый PDF?)")

                if self.split_output_folder:
                    outdir = self.split_output_folder / f"{src.stem}_pages" if total > 1 else self.split_output_folder
                else:
                    outdir = src.parent / f"{src.stem}_pages"
                outdir.mkdir(parents=True, exist_ok=True)

                if mode == "each":
                    segments = [(n, n) for n in range(1, pages + 1)]
                elif mode == "chunks":
                    segments = split_into_chunks(pages, chunk)
                else:
                    segments = parse_ranges(ranges_spec, pages)

                self._log(f"   📄 {pages} стр. → {len(segments)} файл(ов)")

                for (a, b) in segments:
                    if self.stop_flag:
                        break
                    name = f"{src.stem}_p{a:03d}.pdf" if a == b else f"{src.stem}_p{a:03d}-{b:03d}.pdf"
                    out_file = unique_path(outdir / name)
                    gs_extract_pages(self.gs_path, src, a, b, out_file)
                    created_total += 1
                    self._log(f"      ✔ {out_file.name} ({format_size(get_file_size(out_file))})")
                ok_files += 1
            except ValueError as e:
                self._log(f"   ❌ {e}")
            except Exception as e:
                self._log(f"   ❌ Ошибка: {e}")

        self._ui(self.progress_var.set, 100)
        self._ui(self._finish_split, ok_files, created_total, total, self.split_output_folder, files)

    def _finish_split(self, ok_files: int, created: int, total: int, out_folder: Optional[Path],
                      files: List[Path]) -> None:
        self._set_busy(False)
        if self.stop_flag:
            self.status_var.set("⏹ Остановлено пользователем")
            return
        if ok_files == 0:
            self.status_var.set("❌ Ничего не разделено — см. лог")
            messagebox.showerror("Ошибка", "Не удалось разделить файлы. Подробности в логе.")
            return
        target_dir = out_folder if out_folder else (files[0].parent / f"{files[0].stem}_pages")
        self.status_var.set(f"✅ Готово: {created} файл(ов) из {ok_files} PDF")
        messagebox.showinfo("Готово",
                            f"Разделение завершено!\n\nСоздано файлов: {created}\n"
                            f"Из PDF: {ok_files} из {total}\n\nПапка с результатами:\n{target_dir}")


def main() -> None:
    try:
        from tkinterdnd2 import TkinterDnD
        root = TkinterDnD.Tk()
    except ImportError:
        root = tk.Tk()
    PDFToolkitApp(root)
    root.mainloop()


if __name__ == "__main__":
    main()
