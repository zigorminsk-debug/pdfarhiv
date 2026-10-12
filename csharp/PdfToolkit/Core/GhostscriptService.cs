using System.Diagnostics;
using System.Text;
using System.Text.RegularExpressions;

namespace PdfToolkit.Core;

/// <summary>Профиль сжатия (соответствует профилям Python-версии).</summary>
public sealed record CompressionProfile(
    string Name,
    string Description,
    int Dpi,                 // 0 = АВТО
    int JpegQuality,
    string DownsampleType,
    string[] ExtraArgs);

public sealed class GsException : Exception
{
    public GsException(string message) : base(message) { }
}

/// <summary>Обёртка над Ghostscript: сжатие, объединение, извлечение страниц.</summary>
public sealed class GhostscriptService
{
    public static readonly CompressionProfile[] Profiles =
    {
        new("⚡ АВТО — минимум 2x (рекомендую)",
            "Сам подбирает профиль от мягкого к агрессивному, пока не достигнет цели.",
            0, 0, "/Bicubic", Array.Empty<string>()),
        new("🔥 Максимальное (2-10x)",
            "72 DPI, JPEG 30%. Для архива/email.",
            72, 30, "/Bicubic",
            new[] { "-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true", "-dRemoveBookmarks=true" }),
        new("📄 Баланс (2-4x)",
            "150 DPI, JPEG 60%. Для документов.",
            150, 60, "/Bicubic",
            new[] { "-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true" }),
        new("👁 Качество (1.5-2x)",
            "300 DPI, JPEG 85%. Почти без потерь.",
            300, 85, "/Bicubic",
            new[] { "-dDetectDuplicateImages=true", "-dCompressFonts=true", "-dSubsetFonts=true" }),
        new("📱 Только текст (макс. сжатие)",
            "150 DPI, JPEG 40%, Subsample. Для текстовых сканов.",
            150, 40, "/Subsample",
            new[] { "-dColorImageDownsampleType=/Subsample", "-dGrayImageDownsampleType=/Subsample",
                    "-dMonoImageDownsampleType=/Subsample", "-dDetectDuplicateImages=true",
                    "-dCompressFonts=true", "-dSubsetFonts=true" }),
    };

    /// <summary>Порядок перебора в АВТО-режиме: Качество → Баланс → Только текст → Максимальное.</summary>
    public static readonly int[] AutoOrder = { 3, 2, 4, 1 };

    public string? GsPath { get; set; }

    public GhostscriptService(string? gsPath = null)
    {
        GsPath = gsPath ?? FindGhostscript();
    }

    /// <summary>Ищет gswin64c.exe в GS_PATH, стандартных папках и в PATH (самая новая версия).</summary>
    public static string? FindGhostscript()
    {
        var envGs = Environment.GetEnvironmentVariable("GS_PATH");
        if (!string.IsNullOrWhiteSpace(envGs) && File.Exists(envGs))
            return envGs;

        var roots = new List<string>
        {
            Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles),
            Environment.GetFolderPath(Environment.SpecialFolder.ProgramFilesX86),
        };
        foreach (var root in roots.Where(r => !string.IsNullOrEmpty(r)).Distinct())
        {
            string gsDir = Path.Combine(root, "gs");
            if (!Directory.Exists(gsDir)) continue;
            var found = Directory.GetDirectories(gsDir, "gs*")
                .SelectMany(d => new[] { Path.Combine(d, "bin", "gswin64c.exe"), Path.Combine(d, "bin", "gswin32c.exe") })
                .Where(File.Exists)
                .OrderByDescending(p => p, StringComparer.OrdinalIgnoreCase)
                .FirstOrDefault();
            if (found != null) return found;
        }

        foreach (var name in new[] { "gswin64c.exe", "gswin32c.exe", "gs" })
        {
            var onPath = (Environment.GetEnvironmentVariable("PATH") ?? string.Empty)
                .Split(Path.PathSeparator, StringSplitOptions.RemoveEmptyEntries)
                .Select(d => Path.Combine(d, name))
                .FirstOrDefault(File.Exists);
            if (onPath != null) return onPath;
        }
        return null;
    }

    // ---------- низкоуровневый запуск ----------

    private async Task<(int Code, string Stdout, string Stderr)> RunAsync(IEnumerable<string> args, int timeoutMs, CancellationToken ct)
    {
        if (string.IsNullOrEmpty(GsPath) || !File.Exists(GsPath))
            throw new GsException("Ghostscript не найден. Укажите путь к gswin64c.exe.");

        var psi = new ProcessStartInfo(GsPath)
        {
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            UseShellExecute = false,
            CreateNoWindow = true,
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8,
        };
        foreach (var a in args) psi.ArgumentList.Add(a);

        using var proc = Process.Start(psi) ?? throw new GsException("Не удалось запустить Ghostscript");
        var stdout = proc.StandardOutput.ReadToEndAsync();
        var stderr = proc.StandardError.ReadToEndAsync();

        using var timeout = new CancellationTokenSource(timeoutMs);
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(ct, timeout.Token);
        try
        {
            await proc.WaitForExitAsync(linked.Token);
        }
        catch (OperationCanceledException)
        {
            try { proc.Kill(entireProcessTree: true); } catch { /* уже завершён */ }
            if (ct.IsCancellationRequested) throw;
            throw new GsException("Таймаут — файл слишком большой или повреждён");
        }

        string outText = await stdout;
        string errText = await stderr;
        return (proc.ExitCode, outText, errText);
    }

    private static string Short(string s, int max = 300) =>
        s.Trim().Length <= max ? s.Trim() : s.Trim()[..max];

    /// <summary>Число страниц. Возвращает null, если файл не читается.</summary>
    public async Task<int?> PageCountAsync(string pdf, CancellationToken ct = default)
    {
        // Ghostscript: сначала безопасный режим с разрешением чтения файла, затем -dNOSAFER.
        string ps = $"({EscapePs(pdf)}) (r) file runpdfbegin pdfpagecount = quit";
        foreach (var extra in new[] { $"--permit-file-read={pdf}", "-dNOSAFER" })
        {
            try
            {
                var (code, stdout, _) = await RunAsync(new[] { "-q", "-dNODISPLAY", extra, "-c", ps }, 120_000, ct);
                // Принимаем только строку, состоящую из одного числа (иначе это текст ошибки)
                var m = Regex.Match(stdout, @"^\s*(\d+)\s*$", RegexOptions.Multiline);
                // Ghostscript на битом файле печатает «Error:» и «0» с кодом 0 — это не валидный ответ
                if (code == 0 && !stdout.Contains("Error", StringComparison.OrdinalIgnoreCase)
                    && m.Success && int.TryParse(m.Groups[1].Value, out int n) && n > 0)
                    return n;
            }
            catch (GsException) { /* пробуем следующий способ */ }
        }
        return null;
    }

    private static string EscapePs(string s) =>
        s.Replace("\\", "\\\\").Replace("(", "\\(").Replace(")", "\\)");

    // ---------- операции без потери качества ----------

    /// <summary>Объединение PDF; изображения не перекодируются.</summary>
    public async Task MergeAsync(IReadOnlyList<string> inputs, string output, CancellationToken ct = default)
    {
        if (inputs.Count < 2) throw new GsException("Для объединения нужно минимум 2 файла");
        var args = new List<string>
        {
            "-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
            "-dPassThroughJPEGImages=true", "-dPassThroughJPXImages=true", "-dDetectDuplicateImages=true",
            $"-sOutputFile={output}",
        };
        args.AddRange(inputs);
        await RunChecked(args, output, ct);
    }

    /// <summary>Извлечение страниц first..last (включительно); качество сохраняется.</summary>
    public async Task ExtractAsync(string src, PageRange range, string output, CancellationToken ct = default)
    {
        var args = new[]
        {
            "-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
            $"-dFirstPage={range.First}", $"-dLastPage={range.Last}",
            "-dPassThroughJPEGImages=true", "-dPassThroughJPXImages=true",
            $"-sOutputFile={output}", src,
        };
        await RunChecked(args, output, ct);
    }

    private async Task RunChecked(IEnumerable<string> args, string output, CancellationToken ct)
    {
        var (code, stdout, stderr) = await RunAsync(args, 1_800_000, ct);
        if (code != 0)
        {
            string msg = string.IsNullOrWhiteSpace(stderr) ? stdout : stderr;
            throw new GsException($"Ghostscript вернул код {code}: {Short(msg)}");
        }
        if (!File.Exists(output)) throw new GsException("Выходной файл не создан");
    }

    // ---------- сжатие ----------

    /// <summary>Сжатие одним профилем. Возвращает размер результата в байтах.</summary>
    public async Task<long> CompressAsync(string src, string dst, CompressionProfile p, CancellationToken ct = default)
    {
        var args = new List<string>
        {
            "-sDEVICE=pdfwrite", "-dCompatibilityLevel=1.4",
            "-dNOPAUSE", "-dQUIET", "-dBATCH", "-dSAFER",
            $"-dColorImageResolution={p.Dpi}", $"-dGrayImageResolution={p.Dpi}", $"-dMonoImageResolution={p.Dpi}",
            $"-dColorImageDownsampleType={p.DownsampleType}",
            $"-dGrayImageDownsampleType={p.DownsampleType}",
            $"-dMonoImageDownsampleType={p.DownsampleType}",
            $"-dJPEGQ={p.JpegQuality}",
            "-dAutoFilterColorImages=true", "-dAutoFilterGrayImages=true",
            "-dColorImageFilter=/DCTEncode", "-dGrayImageFilter=/DCTEncode",
            "-dDownsampleColorImages=true", "-dDownsampleGrayImages=true", "-dDownsampleMonoImages=true",
        };
        args.AddRange(p.ExtraArgs);
        args.Add($"-sOutputFile={dst}");
        args.Add(src);

        await RunChecked(args, dst, ct);
        return new FileInfo(dst).Length;
    }

    /// <summary>
    /// АВТО-режим: перебирает профили от мягкого к агрессивному и берёт первый, который даёт
    /// сжатие не меньше targetRatio. Если цель недостижима — лучший результат.
    /// Возвращает размер итогового файла в dst; 0 — если ничего не вышло (dst не создан).
    /// </summary>
    public async Task<long> AutoCompressAsync(string src, string dst, double targetRatio,
        Action<string>? log = null, CancellationToken ct = default)
    {
        log ??= _ => { };
        long originalSize = new FileInfo(src).Length;
        long targetSize = (long)(originalSize / Math.Max(1.05, targetRatio));
        string tmp = Path.ChangeExtension(dst, ".autotmp.pdf");

        long bestSize = 0;
        CompressionProfile? best = null;
        try
        {
            foreach (int idx in AutoOrder)
            {
                ct.ThrowIfCancellationRequested();
                var p = Profiles[idx];
                log($"   ⚙ Профиль «{p.Name}»...");
                long size;
                try
                {
                    size = await CompressAsync(src, tmp, p, ct);
                }
                catch (GsException e)
                {
                    log($"      ⚠ {e.Message}");
                    continue;
                }

                if (size <= 0) continue;

                if (best == null || size < bestSize)
                {
                    bestSize = size;
                    best = p;
                }
                if (size <= targetSize)
                {
                    File.Move(tmp, dst, overwrite: true);
                    log($"   ✔ Цель достигнута: {size:N0} Б (x{(double)originalSize / size:0.0}) — «{p.Name}»");
                    return size;
                }
                log($"      Мало: x{(double)originalSize / size:0.0}, агрессивнее...");
            }

            if (best != null && bestSize < originalSize)
            {
                await CompressAsync(src, dst, best, ct);
                log($"   ⚠ Цель x{targetRatio:0.#} недостижима, лучшее — «{best.Name}» (x{(double)originalSize / bestSize:0.0})");
                return bestSize;
            }
            return 0;
        }
        finally
        {
            try { if (File.Exists(tmp)) File.Delete(tmp); } catch { /* ignore */ }
        }
    }
}
