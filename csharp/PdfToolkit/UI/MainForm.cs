using System.Diagnostics;
using PdfToolkit.Core;

namespace PdfToolkit.UI;

/// <summary>Главное окно: три вкладки — Сжать / Объединить / Разделить.</summary>
public sealed class MainForm : Form
{
    private readonly GhostscriptService _gs = new();
    private bool _busy;
    private CancellationTokenSource? _cts;

    // Общие элементы
    private readonly Label _gsLabel = new() { AutoSize = true };
    private readonly ProgressBar _progress = new() { Dock = DockStyle.Fill, Minimum = 0, Maximum = 100 };
    private readonly Label _status = new() { AutoSize = true, Text = "Готов. Добавьте PDF и выберите операцию." };
    private readonly Button _stopBtn = new() { Text = "⏹ Стоп", Enabled = false, AutoSize = true };
    private readonly TextBox _log = new() { Multiline = true, ReadOnly = true, Dock = DockStyle.Fill, ScrollBars = ScrollBars.Vertical, Font = new Font("Consolas", 9f) };
    private readonly TabControl _tabs = new() { Dock = DockStyle.Fill };

    // Вкладка «Сжать»
    private readonly FileListPanel _compressList = new("Файлы для сжатия (перетащите сюда)", false);
    private readonly ComboBox _profileBox = new() { DropDownStyle = ComboBoxStyle.DropDownList, Width = 330 };
    private readonly ComboBox _targetBox = new() { DropDownStyle = ComboBoxStyle.DropDownList, Width = 60 };
    private readonly Label _profileDesc = new() { AutoSize = true, ForeColor = Color.DimGray };
    private readonly TextBox _compressOut = new() { ReadOnly = true, Dock = DockStyle.Fill };
    private string? _compressOutDir;
    private readonly Button _compressStart = new() { Text = "▶  СТАРТ СЖАТИЯ", AutoSize = true, Font = new Font("Segoe UI", 10f, FontStyle.Bold) };

    // Вкладка «Объединить»
    private readonly FileListPanel _mergeList = new("Файлы для объединения (сверху вниз)", true);
    private readonly TextBox _mergeOut = new() { Dock = DockStyle.Fill };
    private readonly Button _mergeStart = new() { Text = "🔗  ОБЪЕДИНИТЬ", AutoSize = true, Font = new Font("Segoe UI", 10f, FontStyle.Bold) };

    // Вкладка «Разделить»
    private readonly FileListPanel _splitList = new("Файлы для разделения", false);
    private readonly RadioButton _rbEach = new() { Text = "Каждая страница — отдельный PDF", Checked = true, AutoSize = true };
    private readonly RadioButton _rbChunks = new() { Text = "Порциями по", AutoSize = true };
    private readonly NumericUpDown _chunkSize = new() { Minimum = 1, Maximum = 10000, Value = 2, Width = 70 };
    private readonly RadioButton _rbRanges = new() { Text = "Только диапазоны:", AutoSize = true };
    private readonly TextBox _rangesBox = new() { Text = "1-3,5,7-10", Width = 220 };
    private readonly TextBox _splitOut = new() { ReadOnly = true, Dock = DockStyle.Fill };
    private string? _splitOutDir;
    private readonly Button _splitStart = new() { Text = "✂  РАЗДЕЛИТЬ", AutoSize = true, Font = new Font("Segoe UI", 10f, FontStyle.Bold) };

    private readonly List<Button> _actionButtons = new();

    public MainForm()
    {
        Text = "PDF Toolkit 2.0 — сжатие · объединение · разделение";
        Size = new Size(1000, 780);
        MinimumSize = new Size(820, 640);
        StartPosition = FormStartPosition.CenterScreen;
        Font = new Font("Segoe UI", 9f);

        BuildLayout();
        WireEvents();
        FillProfiles();
        if (!string.IsNullOrEmpty(_gs.GsPath) && File.Exists(_gs.GsPath))
            CheckGhostscript();
        else
            _gsLabel.Text = "⏳ Подготовка встроенного Ghostscript...";
        Shown += async (_, _) => await InitBundledAsync();
    }

    /// <summary>Распаковка встроенного Ghostscript и, при необходимости, установка VC++.</summary>
    private async Task InitBundledAsync()
    {
        SetBusy(true);
        SetStatus("Подготовка встроенного Ghostscript (первый запуск — несколько секунд)...");
        try
        {
            string? bundled = await Task.Run(GsBundle.EnsureExtracted);
            if (bundled != null)
            {
                _gs.GsPath = bundled;
                Log("Встроенный Ghostscript готов: " + bundled);
            }
        }
        catch (Exception ex)
        {
            Log("⚠ Не удалось распаковать встроенный Ghostscript: " + ex.Message);
        }

        if (!GsBundle.VcRuntimeInstalled())
        {
            Log("⚠ Не найдены библиотеки Microsoft Visual C++ — без них Ghostscript не запустится.");
            var answer = MessageBox.Show(this,
                "Для работы Ghostscript нужен Microsoft Visual C++ Redistributable (x64).\n\n" +
                "Установить сейчас? Windows запросит подтверждение администратора.",
                "Требуется компонент Windows", MessageBoxButtons.YesNo, MessageBoxIcon.Question);
            if (answer == DialogResult.Yes)
            {
                try
                {
                    bool ok = await GsBundle.InstallVcRuntimeAsync();
                    Log(ok ? "✅ Visual C++ установлен." : "❌ Установка Visual C++ не выполнена.");
                }
                catch (Exception ex)
                {
                    Log("❌ Установка Visual C++: " + ex.Message);
                }
            }
        }

        SetBusy(false);
        CheckGhostscript();
        SetStatus("Готов. Добавьте PDF и выберите операцию.");
    }

    // ================= РАЗМЕТКА =================
    private void BuildLayout()
    {
        // Нижняя часть: прогресс, статус, лог
        var bottom = new TableLayoutPanel { Dock = DockStyle.Bottom, AutoSize = true, ColumnCount = 1, Padding = new Padding(10, 0, 10, 8) };
        var progRow = new TableLayoutPanel { Dock = DockStyle.Fill, AutoSize = true, ColumnCount = 2 };
        progRow.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        progRow.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        progRow.Controls.Add(_progress, 0, 0);
        progRow.Controls.Add(_stopBtn, 1, 0);
        var logGroup = new GroupBox { Text = "Лог", Dock = DockStyle.Fill, Height = 150, Padding = new Padding(4) };
        logGroup.Controls.Add(_log);
        bottom.Controls.Add(progRow);
        bottom.Controls.Add(_status);
        bottom.Controls.Add(logGroup);

        // Верхняя панель с Ghostscript
        var top = new FlowLayoutPanel { Dock = DockStyle.Top, AutoSize = true, Padding = new Padding(10, 8, 10, 4) };
        var btnManual = new Button { Text = "🔍 Указать вручную", AutoSize = true };
        var btnDownload = new Button { Text = "📥 Скачать Ghostscript", AutoSize = true };
        btnManual.Click += (_, _) => SelectGhostscript();
        btnDownload.Click += (_, _) => OpenUrl("https://ghostscript.com/releases/gsdnld.html");
        top.Controls.Add(new Label { Text = "Ghostscript:", AutoSize = true, Padding = new Padding(0, 4, 0, 0) });
        top.Controls.Add(_gsLabel);
        top.Controls.Add(btnManual);
        top.Controls.Add(btnDownload);

        // Вкладки
        _tabs.TabPages.Add(BuildCompressTab());
        _tabs.TabPages.Add(BuildMergeTab());
        _tabs.TabPages.Add(BuildSplitTab());

        var tabHolder = new Panel { Dock = DockStyle.Fill, Padding = new Padding(10, 6, 10, 6) };
        tabHolder.Controls.Add(_tabs);

        Controls.Add(tabHolder);
        Controls.Add(bottom);
        Controls.Add(top);

        _stopBtn.Click += (_, _) => StopCurrent();
        _actionButtons.AddRange(new[] { _compressStart, _mergeStart, _splitStart });
    }

    private TabPage BuildCompressTab()
    {
        var page = new TabPage(" 🗜 Сжать ") { Padding = new Padding(8) };
        var layout = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 1, RowCount = 4 };
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.Controls.Add(_compressList, 0, 0);

        var prof = new GroupBox { Text = "Профиль сжатия", AutoSize = true, Dock = DockStyle.Fill, Padding = new Padding(8) };
        var profRow = new FlowLayoutPanel { AutoSize = true, Dock = DockStyle.Top };
        profRow.Controls.Add(_profileBox);
        profRow.Controls.Add(new Label { Text = "  Цель:", AutoSize = true, Padding = new Padding(0, 4, 0, 0) });
        profRow.Controls.Add(_targetBox);
        profRow.Controls.Add(new Label { Text = "x", AutoSize = true, Padding = new Padding(0, 4, 0, 0) });
        var profStack = new FlowLayoutPanel { FlowDirection = FlowDirection.TopDown, AutoSize = true, Dock = DockStyle.Fill };
        profStack.Controls.Add(profRow);
        profStack.Controls.Add(_profileDesc);
        prof.Controls.Add(profStack);
        layout.Controls.Add(prof, 0, 1);

        var outGroup = new GroupBox { Text = "Куда сохранять (по умолчанию — рядом с оригиналом, суффикс _compressed)", Dock = DockStyle.Fill, AutoSize = true, Padding = new Padding(8) };
        var outRow = new TableLayoutPanel { Dock = DockStyle.Fill, AutoSize = true, ColumnCount = 3 };
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        var pick = new Button { Text = "📂 Выбрать", AutoSize = true };
        var reset = new Button { Text = "🔄 Сброс", AutoSize = true };
        pick.Click += (_, _) => PickFolder(ref _compressOutDir, _compressOut, "Папка для сжатых файлов", "[Рядом с оригиналом]");
        reset.Click += (_, _) => { _compressOutDir = null; _compressOut.Text = "[Рядом с оригиналом]"; };
        _compressOut.Text = "[Рядом с оригиналом]";
        outRow.Controls.Add(_compressOut, 0, 0);
        outRow.Controls.Add(pick, 1, 0);
        outRow.Controls.Add(reset, 2, 0);
        outGroup.Controls.Add(outRow);
        layout.Controls.Add(outGroup, 0, 2);

        var startRow = new FlowLayoutPanel { AutoSize = true, Dock = DockStyle.Fill, Padding = new Padding(0, 6, 0, 0) };
        startRow.Controls.Add(_compressStart);
        layout.Controls.Add(startRow, 0, 3);

        page.Controls.Add(layout);
        return page;
    }

    private TabPage BuildMergeTab()
    {
        var page = new TabPage(" 🔗 Объединить ") { Padding = new Padding(8) };
        var layout = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 1, RowCount = 4 };
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.Controls.Add(new Label
        {
            AutoSize = true, ForeColor = Color.DimGray,
            Text = "Порядок файлов = порядок страниц в результате (меняйте кнопками ⬆⬇).\n" +
                   "Изображения НЕ перекодируются — качество сохраняется полностью."
        }, 0, 0);
        layout.Controls.Add(_mergeList, 0, 1);

        var outGroup = new GroupBox { Text = "Результат", Dock = DockStyle.Fill, AutoSize = true, Padding = new Padding(8) };
        var outRow = new TableLayoutPanel { Dock = DockStyle.Fill, AutoSize = true, ColumnCount = 2 };
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        var saveAs = new Button { Text = "📂 Сохранить как...", AutoSize = true };
        saveAs.Click += (_, _) =>
        {
            using var dlg = new SaveFileDialog { Filter = "PDF файлы|*.pdf", DefaultExt = "pdf", FileName = "merged.pdf" };
            if (dlg.ShowDialog(this) == DialogResult.OK) _mergeOut.Text = dlg.FileName;
        };
        outRow.Controls.Add(_mergeOut, 0, 0);
        outRow.Controls.Add(saveAs, 1, 0);
        outGroup.Controls.Add(outRow);
        layout.Controls.Add(outGroup, 0, 2);

        var startRow = new FlowLayoutPanel { AutoSize = true, Dock = DockStyle.Fill, Padding = new Padding(0, 6, 0, 0) };
        startRow.Controls.Add(_mergeStart);
        layout.Controls.Add(startRow, 0, 3);

        page.Controls.Add(layout);
        return page;
    }

    private TabPage BuildSplitTab()
    {
        var page = new TabPage(" ✂ Разделить ") { Padding = new Padding(8) };
        var layout = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 1, RowCount = 5 };
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.Controls.Add(new Label
        {
            AutoSize = true, ForeColor = Color.DimGray,
            Text = "Качество не теряется. Результаты — в подпапке «<имя>_pages» рядом с файлом.\n" +
                   "Если выделить файлы в списке — операция затронет только их."
        }, 0, 0);
        layout.Controls.Add(_splitList, 0, 1);

        var mode = new GroupBox { Text = "Как разделять", Dock = DockStyle.Fill, AutoSize = true, Padding = new Padding(8) };
        var modeStack = new FlowLayoutPanel { FlowDirection = FlowDirection.TopDown, AutoSize = true, Dock = DockStyle.Fill };
        modeStack.Controls.Add(_rbEach);
        var r1 = new FlowLayoutPanel { AutoSize = true };
        r1.Controls.Add(_rbChunks);
        r1.Controls.Add(_chunkSize);
        r1.Controls.Add(new Label { Text = "стр.", AutoSize = true, Padding = new Padding(0, 4, 0, 0) });
        modeStack.Controls.Add(r1);
        var r2 = new FlowLayoutPanel { AutoSize = true };
        r2.Controls.Add(_rbRanges);
        r2.Controls.Add(_rangesBox);
        r2.Controls.Add(new Label { Text = "(например 1-3,5,7-)", AutoSize = true, ForeColor = Color.DimGray, Padding = new Padding(0, 4, 0, 0) });
        modeStack.Controls.Add(r2);
        mode.Controls.Add(modeStack);
        layout.Controls.Add(mode, 0, 2);

        var outGroup = new GroupBox { Text = "Куда сохранять (по умолчанию — подпапка рядом с файлом)", Dock = DockStyle.Fill, AutoSize = true, Padding = new Padding(8) };
        var outRow = new TableLayoutPanel { Dock = DockStyle.Fill, AutoSize = true, ColumnCount = 3 };
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        outRow.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        var pick = new Button { Text = "📂 Выбрать", AutoSize = true };
        var reset = new Button { Text = "🔄 Сброс", AutoSize = true };
        _splitOut.Text = "[Подпапка «<имя>_pages» рядом с файлом]";
        pick.Click += (_, _) => PickFolder(ref _splitOutDir, _splitOut, "Папка для разделённых файлов", "[Подпапка «<имя>_pages» рядом с файлом]");
        reset.Click += (_, _) => { _splitOutDir = null; _splitOut.Text = "[Подпапка «<имя>_pages» рядом с файлом]"; };
        outRow.Controls.Add(_splitOut, 0, 0);
        outRow.Controls.Add(pick, 1, 0);
        outRow.Controls.Add(reset, 2, 0);
        outGroup.Controls.Add(outRow);
        layout.Controls.Add(outGroup, 0, 3);

        var startRow = new FlowLayoutPanel { AutoSize = true, Dock = DockStyle.Fill, Padding = new Padding(0, 6, 0, 0) };
        startRow.Controls.Add(_splitStart);
        layout.Controls.Add(startRow, 0, 4);

        page.Controls.Add(layout);
        return page;
    }

    // ================= СОБЫТИЯ =================
    private void WireEvents()
    {
        _profileBox.SelectedIndexChanged += (_, _) =>
            _profileDesc.Text = GhostscriptService.Profiles[_profileBox.SelectedIndex].Description;
        _compressStart.Click += (_, _) => StartCompress();
        _mergeStart.Click += (_, _) => StartMerge();
        _splitStart.Click += (_, _) => StartSplit();

        KeyPreview = true;
        KeyDown += (_, e) =>
        {
            if (e.Control && e.KeyCode == Keys.O) { CurrentList().AddFiles(this); e.Handled = true; }
            else if (e.KeyCode == Keys.F5) { RunCurrentTab(); e.Handled = true; }
            else if (e.KeyCode == Keys.Escape && _busy) { StopCurrent(); e.Handled = true; }
        };
        FormClosing += (_, e) =>
        {
            if (_busy && MessageBox.Show(this, "Идёт операция. Остановить и выйти?", "Подтверждение",
                    MessageBoxButtons.YesNo, MessageBoxIcon.Question) == DialogResult.No)
            {
                e.Cancel = true;
                return;
            }
            _cts?.Cancel();
        };
    }

    private FileListPanel CurrentList() => _tabs.SelectedIndex switch
    {
        0 => _compressList,
        1 => _mergeList,
        _ => _splitList,
    };

    private void RunCurrentTab()
    {
        if (_busy) return;
        switch (_tabs.SelectedIndex)
        {
            case 0: StartCompress(); break;
            case 1: StartMerge(); break;
            default: StartSplit(); break;
        }
    }

    private void FillProfiles()
    {
        foreach (var p in GhostscriptService.Profiles) _profileBox.Items.Add(p.Name);
        _profileBox.SelectedIndex = 0;
        foreach (var t in new[] { 1.5, 2.0, 3.0, 5.0, 10.0 })
            _targetBox.Items.Add(t.ToString("0.0", System.Globalization.CultureInfo.InvariantCulture));
        _targetBox.SelectedIndex = 1;
    }

    // ================= GHOSTSCRIPT =================
    private void CheckGhostscript()
    {
        if (!string.IsNullOrEmpty(_gs.GsPath) && File.Exists(_gs.GsPath))
        {
            _gsLabel.Text = "✅ Найден: " + _gs.GsPath;
            _gsLabel.ForeColor = Color.Green;
            Log("Ghostscript найден: " + _gs.GsPath);
        }
        else
        {
            _gsLabel.Text = "❌ НЕ НАЙДЕН — установите Ghostscript";
            _gsLabel.ForeColor = Color.Red;
            Log("⚠ Ghostscript не найден. Скачайте: https://ghostscript.com/releases/gsdnld.html");
        }
    }

    private void SelectGhostscript()
    {
        using var dlg = new OpenFileDialog { Title = "Выберите gswin64c.exe", Filter = "Ghostscript|gswin*c.exe|Все файлы|*.*" };
        if (dlg.ShowDialog(this) == DialogResult.OK)
        {
            _gs.GsPath = dlg.FileName;
            CheckGhostscript();
        }
    }

    private bool RequireGs()
    {
        if (!string.IsNullOrEmpty(_gs.GsPath) && File.Exists(_gs.GsPath)) return true;
        MessageBox.Show(this, "Ghostscript не найден!\nУстановите его: https://ghostscript.com/releases/gsdnld.html",
            "Ошибка", MessageBoxButtons.OK, MessageBoxIcon.Error);
        return false;
    }

    // ================= ОБЩИЕ УТИЛИТЫ =================
    private void Log(string msg)
    {
        if (IsDisposed) return;
        if (InvokeRequired) { BeginInvoke(() => Log(msg)); return; }
        _log.AppendText(msg + Environment.NewLine);
    }

    private void SetStatus(string msg)
    {
        if (IsDisposed) return;
        if (InvokeRequired) { BeginInvoke(() => SetStatus(msg)); return; }
        _status.Text = msg;
    }

    private void SetProgress(int value)
    {
        if (IsDisposed) return;
        if (InvokeRequired) { BeginInvoke(() => SetProgress(value)); return; }
        _progress.Value = Math.Clamp(value, 0, 100);
    }

    private void SetBusy(bool busy)
    {
        _busy = busy;
        _stopBtn.Enabled = busy;
        foreach (var b in _actionButtons) b.Enabled = !busy;
        _compressList.Enabled = !busy;
        _mergeList.Enabled = !busy;
        _splitList.Enabled = !busy;
    }

    private void StopCurrent()
    {
        if (!_busy || _cts == null) return;
        _cts.Cancel();
        SetStatus("Остановка...");
        Log("🛑 Остановка по запросу...");
    }

    private void PickFolder(ref string? dir, TextBox box, string title, string resetText)
    {
        using var dlg = new FolderBrowserDialog { Description = title, UseDescriptionForTitle = true };
        if (dlg.ShowDialog(this) == DialogResult.OK)
        {
            dir = dlg.SelectedPath;
            box.Text = dir;
        }
    }

    private static void OpenUrl(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); }
        catch { /* браузер не открылся — ссылка уже в логе */ }
    }

    private static string FormatSize(long bytes)
    {
        double b = bytes;
        string[] units = { "Б", "КБ", "МБ", "ГБ" };
        int u = 0;
        while (b >= 1024 && u < units.Length - 1) { b /= 1024; u++; }
        return $"{b:0.0} {units[u]}";
    }

    /// <summary>Запуск операции в фоне: блокирует кнопки, даёт токен отмены.</summary>
    private async void RunBackground(Func<CancellationToken, Task> work)
    {
        if (_busy) return;
        _cts = new CancellationTokenSource();
        _progress.Value = 0;
        SetBusy(true);
        try
        {
            await work(_cts.Token);
        }
        catch (OperationCanceledException)
        {
            Log("⏹ Остановлено.");
            SetStatus("⏹ Остановлено пользователем");
        }
        catch (Exception ex)
        {
            Log("❌ Ошибка: " + ex.Message);
            SetStatus("❌ Ошибка — см. лог");
        }
        finally
        {
            SetBusy(false);
            _cts.Dispose();
            _cts = null;
        }
    }

    // ================= СЖАТИЕ =================
    private void StartCompress()
    {
        if (!RequireGs()) return;
        var files = _compressList.Files.ToList();
        if (files.Count == 0) { MessageBox.Show(this, "Список файлов пуст."); return; }

        int idx = _profileBox.SelectedIndex;
        var profile = GhostscriptService.Profiles[idx];
        double target = double.Parse(_targetBox.SelectedItem?.ToString() ?? "2", System.Globalization.CultureInfo.InvariantCulture);
        RunBackground(ct => CompressWork(files, profile, target, ct));
    }

    private async Task CompressWork(List<string> files, CompressionProfile profile, double target, CancellationToken ct)
    {
        int ok = 0, skipped = 0;
        long totalOrig = 0, totalComp = 0;
        for (int i = 0; i < files.Count; i++)
        {
            ct.ThrowIfCancellationRequested();
            string src = files[i];
            SetStatus($"[{i + 1}/{files.Count}] Сжатие: {Path.GetFileName(src)}");
            SetProgress(i * 100 / files.Count);

            long origSize = new FileInfo(src).Length;
            totalOrig += origSize;
            string dir = _compressOutDir ?? Path.GetDirectoryName(src)!;
            string dst = UniquePath(Path.Combine(dir, Path.GetFileNameWithoutExtension(src) + "_compressed.pdf"));

            Log($"▶ [{i + 1}/{files.Count}] {Path.GetFileName(src)} ({FormatSize(origSize)})");
            try
            {
                long size = profile.Dpi == 0
                    ? await _gs.AutoCompressAsync(src, dst, target, Log, ct)
                    : await _gs.CompressAsync(src, dst, profile, ct);

                if (size > 0 && size < origSize)
                {
                    totalComp += size;
                    Log($"   ✅ {FormatSize(size)} (x{(double)origSize / size:0.0}) → {Path.GetFileName(dst)}");
                    ok++;
                }
                else
                {
                    totalComp += origSize;
                    TryDelete(dst);
                    Log("   ⚠ Не уменьшился — оригинал не изменён");
                    skipped++;
                }
            }
            catch (GsException ex)
            {
                TryDelete(dst);
                totalComp += origSize;
                Log("   ❌ " + ex.Message);
                skipped++;
            }
        }
        SetProgress(100);

        if (totalOrig > 0)
        {
            double pct = (1 - (double)totalComp / totalOrig) * 100;
            SetStatus($"✅ Готово! Успешно: {ok}, пропущено: {skipped} | {FormatSize(totalOrig)} → {FormatSize(totalComp)} (-{pct:0.0}%)");
            Log($"📊 ИТОГО: успешно {ok}, пропущено {skipped}; было {FormatSize(totalOrig)} → стало {FormatSize(totalComp)} (-{pct:0.0}%)");
            if (ok > 0)
                Invoke(() => MessageBox.Show(this,
                    $"Сжатие завершено!\n\nУспешно: {ok}\nПропущено: {skipped}\nБыло:  {FormatSize(totalOrig)}\nСтало: {FormatSize(totalComp)}\nЭкономия: {pct:0.0}%",
                    "Готово", MessageBoxButtons.OK, MessageBoxIcon.Information));
        }
    }

    // ================= ОБЪЕДИНЕНИЕ =================
    private void StartMerge()
    {
        if (!RequireGs()) return;
        var files = _mergeList.Files.ToList();
        if (files.Count < 2) { MessageBox.Show(this, "Добавьте минимум 2 PDF файла."); return; }

        string output = string.IsNullOrWhiteSpace(_mergeOut.Text)
            ? Path.Combine(Path.GetDirectoryName(files[0])!, "merged.pdf")
            : _mergeOut.Text.Trim();
        if (!string.Equals(Path.GetExtension(output), ".pdf", StringComparison.OrdinalIgnoreCase))
            output += ".pdf";
        output = UniquePath(output);
        _mergeOut.Text = output;

        RunBackground(async ct =>
        {
            SetStatus($"Объединение {files.Count} файлов...");
            Log($"▶ Объединение {files.Count} файлов → {Path.GetFileName(output)}");
            foreach (var f in files) Log($"   • {Path.GetFileName(f)} ({FormatSize(new FileInfo(f).Length)})");

            Directory.CreateDirectory(Path.GetDirectoryName(output)!);
            await _gs.MergeAsync(files, output, ct);
            long size = new FileInfo(output).Length;
            int? pages = await _gs.PageCountAsync(output, ct);
            SetProgress(100);
            Log($"   ✅ Готово: {Path.GetFileName(output)} — {FormatSize(size)}, {(pages?.ToString() ?? "?")} стр.");
            SetStatus($"✅ Объединено: {Path.GetFileName(output)} ({FormatSize(size)})");
            Invoke(() => MessageBox.Show(this,
                $"Файлы объединены!\n\nРезультат: {Path.GetFileName(output)}\nРазмер: {FormatSize(size)}" +
                (pages.HasValue ? $", страниц: {pages}" : "") + $"\nПапка: {Path.GetDirectoryName(output)}",
                "Готово", MessageBoxButtons.OK, MessageBoxIcon.Information));
        });
    }

    // ================= РАЗДЕЛЕНИЕ =================
    private void StartSplit()
    {
        if (!RequireGs()) return;
        var files = _splitList.Selected.Count > 0 ? _splitList.Selected : _splitList.Files.ToList();
        if (files.Count == 0) { MessageBox.Show(this, "Добавьте PDF файлы."); return; }

        int chunk = (int)_chunkSize.Value;
        string spec = _rangesBox.Text;
        int mode = _rbEach.Checked ? 0 : _rbChunks.Checked ? 1 : 2;
        if (mode == 2 && string.IsNullOrWhiteSpace(spec))
        {
            MessageBox.Show(this, "Укажите диапазоны, например: 1-3,5,7-10");
            return;
        }

        RunBackground(ct => SplitWork(files, mode, chunk, spec, ct));
    }

    private async Task SplitWork(List<string> files, int mode, int chunk, string spec, CancellationToken ct)
    {
        int okFiles = 0, created = 0;
        string? lastDir = null;
        for (int i = 0; i < files.Count; i++)
        {
            ct.ThrowIfCancellationRequested();
            string src = files[i];
            SetStatus($"[{i + 1}/{files.Count}] Разделение: {Path.GetFileName(src)}");
            SetProgress(i * 100 / files.Count);
            Log($"▶ [{i + 1}/{files.Count}] {Path.GetFileName(src)} ({FormatSize(new FileInfo(src).Length)})");

            try
            {
                int? pages = await _gs.PageCountAsync(src, ct);
                if (pages is null) throw new GsException("Не удалось определить число страниц (повреждённый PDF?)");

                string baseName = Path.GetFileNameWithoutExtension(src);
                string outDir = Path.Combine(_splitOutDir ?? Path.GetDirectoryName(src)!, baseName + "_pages");
                Directory.CreateDirectory(outDir);
                lastDir = outDir;

                List<PageRange> segments = mode switch
                {
                    0 => PageRanges.EachPage(pages.Value),
                    1 => PageRanges.Chunks(pages.Value, chunk),
                    _ => PageRanges.Parse(spec, pages.Value),
                };
                Log($"   📄 {pages} стр. → {segments.Count} файл(ов)");

                foreach (var seg in segments)
                {
                    ct.ThrowIfCancellationRequested();
                    string name = seg.First == seg.Last
                        ? $"{baseName}_p{seg.First:000}.pdf"
                        : $"{baseName}_p{seg.First:000}-{seg.Last:000}.pdf";
                    string outFile = UniquePath(Path.Combine(outDir, name));
                    await _gs.ExtractAsync(src, seg, outFile, ct);
                    created++;
                    Log($"      ✔ {Path.GetFileName(outFile)} ({FormatSize(new FileInfo(outFile).Length)})");
                }
                okFiles++;
            }
            catch (FormatException ex)
            {
                Log("   ❌ " + ex.Message);
            }
            catch (GsException ex)
            {
                Log("   ❌ " + ex.Message);
            }
        }
        SetProgress(100);

        if (okFiles == 0)
        {
            SetStatus("❌ Ничего не разделено — см. лог");
            Invoke(() => MessageBox.Show(this, "Не удалось разделить файлы. Подробности в логе.", "Ошибка",
                MessageBoxButtons.OK, MessageBoxIcon.Error));
            return;
        }
        SetStatus($"✅ Готово: {created} файл(ов) из {okFiles} PDF");
        Invoke(() => MessageBox.Show(this,
            $"Разделение завершено!\n\nСоздано файлов: {created}\nИз PDF: {okFiles} из {files.Count}\n\nПапка:\n{_splitOutDir ?? lastDir}",
            "Готово", MessageBoxButtons.OK, MessageBoxIcon.Information));
    }

    // ================= ФАЙЛОВЫЕ УТИЛИТЫ =================
    public static string UniquePath(string path)
    {
        if (!File.Exists(path)) return path;
        string dir = Path.GetDirectoryName(path)!;
        string stem = Path.GetFileNameWithoutExtension(path);
        string ext = Path.GetExtension(path);
        for (int i = 1; i < 1000; i++)
        {
            string candidate = Path.Combine(dir, $"{stem} ({i}){ext}");
            if (!File.Exists(candidate)) return candidate;
        }
        return Path.Combine(dir, $"{stem}_{Guid.NewGuid():N}{ext}");
    }

    private static void TryDelete(string path)
    {
        try { if (File.Exists(path)) File.Delete(path); } catch { /* ignore */ }
    }
}
