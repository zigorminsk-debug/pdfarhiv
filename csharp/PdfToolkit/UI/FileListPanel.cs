namespace PdfToolkit.UI;

/// <summary>Список PDF-файлов с добавлением, drag&amp;drop, сортировкой и удалением.</summary>
public sealed class FileListPanel : UserControl
{
    private readonly List<string> _files = new();
    private readonly ListBox _list = new()
    {
        SelectionMode = SelectionMode.MultiExtended,
        IntegralHeight = false,
        Dock = DockStyle.Fill,
        HorizontalScrollbar = true,
        Font = new Font("Consolas", 9f),
    };
    private readonly bool _allowReorder;

    public FileListPanel(string title, bool allowReorder)
    {
        _allowReorder = allowReorder;
        AllowDrop = true;
        Dock = DockStyle.Fill;

        var group = new GroupBox { Text = title, Dock = DockStyle.Fill, Padding = new Padding(8) };
        group.Controls.Add(_list);

        var buttons = new FlowLayoutPanel { Dock = DockStyle.Bottom, AutoSize = true, Padding = new Padding(0, 6, 0, 0) };
        buttons.Controls.Add(MakeButton("➕ Добавить файлы", (_, _) => AddFiles(FindForm())));
        buttons.Controls.Add(MakeButton("📁 Добавить папку", (_, _) => AddFolder()));
        if (allowReorder)
        {
            buttons.Controls.Add(MakeButton("⬆ Вверх", (_, _) => MoveSelected(-1)));
            buttons.Controls.Add(MakeButton("⬇ Вниз", (_, _) => MoveSelected(1)));
        }
        buttons.Controls.Add(MakeButton("🗑 Удалить", (_, _) => RemoveSelected()));
        buttons.Controls.Add(MakeButton("🧹 Очистить", (_, _) => { _files.Clear(); Refresh_(); }));
        group.Controls.Add(buttons);

        Controls.Add(group);

        _list.KeyDown += (_, e) =>
        {
            if (e.KeyCode == Keys.Delete) { RemoveSelected(); e.Handled = true; }
        };

        group.AllowDrop = true;
        _list.AllowDrop = true;

        DragEventHandler onDragEnter = (_, e) =>
        {
            e.Effect = e.Data?.GetDataPresent(DataFormats.FileDrop) == true ? DragDropEffects.Copy : DragDropEffects.None;
        };
        DragEventHandler onDragDrop = (_, e) =>
        {
            if (e.Data?.GetData(DataFormats.FileDrop) is not string[] paths) return;
            foreach (var p in paths)
            {
                if (Directory.Exists(p))
                    AddFolderContents(p);
                else
                    TryAdd(p);
            }
            Refresh_();
        };

        DragEnter += onDragEnter;
        DragDrop += onDragDrop;
        group.DragEnter += onDragEnter;
        group.DragDrop += onDragDrop;
        _list.DragEnter += onDragEnter;
        _list.DragDrop += onDragDrop;
    }

    private static Button MakeButton(string text, EventHandler onClick)
    {
        var b = new Button { Text = text, AutoSize = true, Margin = new Padding(0, 0, 6, 0) };
        b.Click += onClick;
        return b;
    }

    /// <summary>Все файлы списка (полные пути), в текущем порядке.</summary>
    public List<string> Files => new(_files);

    /// <summary>Выделенные в списке файлы (пусто, если ничего не выделено).</summary>
    public List<string> Selected =>
        _list.SelectedIndices.Cast<int>().Select(i => _files[i]).ToList();

    public void AddFiles(IWin32Window? owner)
    {
        using var dlg = new OpenFileDialog
        {
            Title = "Выберите PDF файлы",
            Filter = "PDF файлы|*.pdf|Все файлы|*.*",
            Multiselect = true,
        };
        if (dlg.ShowDialog(owner) != DialogResult.OK) return;
        foreach (var f in dlg.FileNames) TryAdd(f);
        Refresh_();
    }

    private void AddFolder()
    {
        using var dlg = new FolderBrowserDialog { Description = "Выберите папку с PDF", UseDescriptionForTitle = true };
        if (dlg.ShowDialog(FindForm()) != DialogResult.OK) return;
        AddFolderContents(dlg.SelectedPath);
        Refresh_();
    }

    private void AddFolderContents(string folder)
    {
        foreach (var f in Directory.EnumerateFiles(folder, "*.pdf", SearchOption.AllDirectories).OrderBy(x => x, StringComparer.OrdinalIgnoreCase))
            TryAdd(f);
    }

    private void TryAdd(string path)
    {
        if (!string.Equals(Path.GetExtension(path), ".pdf", StringComparison.OrdinalIgnoreCase)) return;
        if (!File.Exists(path)) return;
        string full = Path.GetFullPath(path);
        if (!_files.Contains(full, StringComparer.OrdinalIgnoreCase)) _files.Add(full);
    }

    private void RemoveSelected()
    {
        var idx = _list.SelectedIndices.Cast<int>().OrderByDescending(i => i).ToList();
        foreach (var i in idx) _files.RemoveAt(i);
        Refresh_();
    }

    private void MoveSelected(int delta)
    {
        if (!_allowReorder) return;
        var sel = _list.SelectedIndices.Cast<int>().OrderBy(i => i).ToList();
        if (sel.Count == 0) return;
        if (delta < 0 && sel[0] == 0) return;
        if (delta > 0 && sel[^1] == _files.Count - 1) return;

        var order = delta < 0 ? sel : sel.AsEnumerable().Reverse().ToList();
        foreach (var i in order)
        {
            int j = i + delta;
            (_files[i], _files[j]) = (_files[j], _files[i]);
        }
        var newSel = sel.Select(i => i + delta).ToList();
        Refresh_();
        foreach (var i in newSel) _list.SelectedIndices.Add(i);
    }

    private void Refresh_()
    {
        _list.BeginUpdate();
        _list.Items.Clear();
        for (int i = 0; i < _files.Count; i++)
        {
            string f = _files[i];
            string prefix = _allowReorder ? $"{i + 1}. " : string.Empty;
            long size = File.Exists(f) ? new FileInfo(f).Length : 0;
            _list.Items.Add($"{prefix}{Path.GetFileName(f)}   [{FormatBytes(size)}]   —   {Path.GetDirectoryName(f)}");
        }
        _list.EndUpdate();
    }

    private static string FormatBytes(long bytes)
    {
        double b = bytes;
        string[] units = { "Б", "КБ", "МБ", "ГБ" };
        int u = 0;
        while (b >= 1024 && u < units.Length - 1) { b /= 1024; u++; }
        return $"{b:0.0} {units[u]}";
    }
}
