using System.Diagnostics;
using System.IO.Compression;
using System.Reflection;

namespace PdfToolkit.Core;

/// <summary>
/// Встроенный в exe Ghostscript (portable-сборка 10.05.1 для Windows x64) и VC++ Redistributable.
/// Ghostscript распаковывается при первом запуске в %LOCALAPPDATA%\PdfToolkit — права администратора не нужны.
/// </summary>
public static class GsBundle
{
    public const string GsVersion = "10.05.1";
    private const string GsResource = "gs.zip";
    /// <summary>Официальная ссылка Microsoft на VC++ Redistributable x64 (скачивается только при необходимости).</summary>
    public const string VcRedistUrl = "https://aka.ms/vs/17/release/vc_redist.x64.exe";

    /// <summary>Папка распаковки, например C:\Users\имя\AppData\Local\PdfToolkit\gs-10.05.1.</summary>
    public static string InstallRoot =>
        Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                     "PdfToolkit", "gs-" + GsVersion);

    /// <summary>
    /// Распаковывает встроенный Ghostscript, если ещё не распакован. Возвращает путь к gswin64c.exe
    /// или null, если архив не встроен. Повторные вызовы быстрые (проверяется маркер).
    /// </summary>
    public static string? EnsureExtracted()
    {
        string gsExe = Path.Combine(InstallRoot, "bin", "gswin64c.exe");
        string marker = Path.Combine(InstallRoot, ".complete");
        if (File.Exists(gsExe) && File.Exists(marker)) return gsExe;

        using var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(GsResource);
        if (stream is null) return null;

        string parent = Path.GetDirectoryName(InstallRoot)!;
        Directory.CreateDirectory(parent);
        string tmp = InstallRoot + ".tmp";
        if (Directory.Exists(tmp)) Directory.Delete(tmp, recursive: true);
        Directory.CreateDirectory(tmp);

        string tmpFull = Path.GetFullPath(tmp) + Path.DirectorySeparatorChar;
        using (var zip = new ZipArchive(stream, ZipArchiveMode.Read))
        {
            foreach (var entry in zip.Entries)
            {
                string dest = Path.GetFullPath(Path.Combine(tmp, entry.FullName));
                if (!dest.StartsWith(tmpFull, StringComparison.OrdinalIgnoreCase)) continue; // защита от zip-slip
                if (string.IsNullOrEmpty(entry.Name))
                {
                    Directory.CreateDirectory(dest);
                    continue;
                }
                Directory.CreateDirectory(Path.GetDirectoryName(dest)!);
                entry.ExtractToFile(dest, overwrite: true);
            }
        }

        if (!File.Exists(Path.Combine(tmp, "bin", "gswin64c.exe")))
            throw new IOException("В архиве нет bin\\gswin64c.exe — сборка повреждена");

        if (Directory.Exists(InstallRoot)) Directory.Delete(InstallRoot, recursive: true);
        Directory.Move(tmp, InstallRoot);
        File.WriteAllText(marker, GsVersion);
        return gsExe;
    }

    /// <summary>Установлены ли системные библиотеки VC++ 2015–2022, нужные gswin64c.exe.</summary>
    public static bool VcRuntimeInstalled()
    {
        if (!OperatingSystem.IsWindows()) return true;
        string sys = Environment.SystemDirectory;
        return File.Exists(Path.Combine(sys, "vcruntime140.dll"))
            && File.Exists(Path.Combine(sys, "msvcp140.dll"));
    }

    /// <summary>
    /// Тихо ставит VC++ Redistributable (официальный установщик Microsoft, встроен в exe).
    /// Windows запросит подтверждение администратора (UAC). Возвращает true при успехе.
    /// </summary>
    public static async Task<bool> InstallVcRuntimeAsync()
    {
        string path = Path.Combine(Path.GetTempPath(), "PdfToolkit_vc_redist.x64.exe");
        using (var http = new HttpClient { Timeout = TimeSpan.FromMinutes(10) })
        using (var resp = await http.GetAsync(VcRedistUrl, HttpCompletionOption.ResponseHeadersRead))
        {
            resp.EnsureSuccessStatusCode();
            using var src = await resp.Content.ReadAsStreamAsync();
            using var fs = File.Create(path);
            await src.CopyToAsync(fs);
        }

        var psi = new ProcessStartInfo(path, "/install /quiet /norestart")
        {
            UseShellExecute = true,
            Verb = "runas",
        };
        using var proc = Process.Start(psi) ?? throw new IOException("Не удалось запустить установщик VC++");
        await proc.WaitForExitAsync();
        try { File.Delete(path); } catch { /* ignore */ }

        // 0 — ок, 3010 — ок, но нужна перезагрузка, 1638 — уже установлено
        return proc.ExitCode is 0 or 3010 or 1638;
    }
}
