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
    private const string MarkerName = ".complete_v2";
    /// <summary>Официальная ссылка Microsoft на VC++ Redistributable x64 (скачивается только при необходимости).</summary>
    public const string VcRedistUrl = "https://aka.ms/vs/17/release/vc_redist.x64.exe";

    /// <summary>Папка распаковки, например C:\Users\имя\AppData\Local\PdfToolkit\gs-10.05.1.</summary>
    public static string InstallRoot =>
        Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                     "PdfToolkit", "gs-" + GsVersion);

    private static readonly HashSet<string> NeededFiles = new(StringComparer.OrdinalIgnoreCase)
    {
        "bin/gswin64c.exe",
        "bin/gsdll64.dll",
        "COPYING.txt",
    };

    /// <summary>
    /// Распаковывает только необходимые файлы Ghostscript (gswin64c.exe, gsdll64.dll, COPYING.txt).
    /// Все ресурсы (%rom%Resource, %rom%iccprofiles) уже встроены внутри gsdll64.dll,
    /// поэтому распаковка 3 файлов вместо 535 занимает доли секунды и не вызывает зависания антивируса.
    /// </summary>
    public static string? EnsureExtracted()
    {
        string gsExe = Path.Combine(InstallRoot, "bin", "gswin64c.exe");
        string gsDll = Path.Combine(InstallRoot, "bin", "gsdll64.dll");
        string marker = Path.Combine(InstallRoot, MarkerName);
        if (File.Exists(gsExe) && File.Exists(gsDll) && File.Exists(marker))
            return gsExe;

        using var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(GsResource);
        if (stream is null) return null;

        Directory.CreateDirectory(Path.Combine(InstallRoot, "bin"));

        using (var zip = new ZipArchive(stream, ZipArchiveMode.Read))
        {
            foreach (var entry in zip.Entries)
            {
                string norm = entry.FullName.Replace('\\', '/');
                if (!NeededFiles.Contains(norm)) continue;

                string dest = Path.Combine(InstallRoot, entry.FullName.Replace('/', Path.DirectorySeparatorChar));
                Directory.CreateDirectory(Path.GetDirectoryName(dest)!);
                string tmpFile = dest + ".tmp";
                entry.ExtractToFile(tmpFile, overwrite: true);
                File.Move(tmpFile, dest, overwrite: true);
            }
        }

        if (!File.Exists(gsExe))
            throw new IOException("В архиве нет bin\\gswin64c.exe — сборка повреждена");

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
    /// Тихо ставит VC++ Redistributable (официальный установщик Microsoft).
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
