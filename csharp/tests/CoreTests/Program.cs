// Тесты ядра PDF Toolkit на реальном Ghostscript.
// Запуск: dotnet run -- <путь_к_gswin64c.exe>
// Тестовые PDF (5 и 3 стр., с цветным шумом — как скан) создаются самим Ghostscript из PostScript.
using System.Text;
using PdfToolkit.Core;

Console.OutputEncoding = Encoding.UTF8;

int passed = 0, failed = 0;
void Check(string name, bool ok, string detail = "")
{
    if (ok) passed++; else failed++;
    Console.WriteLine($"  {(ok ? "✅" : "❌")} {name}{(detail.Length > 0 ? " — " + detail : "")}");
}

if (args.Length < 1) { Console.WriteLine("Нужно: <путь к gs>"); return 2; }
var gs = new GhostscriptService(args[0]);
if (gs.GsPath is null || !File.Exists(gs.GsPath)) { Console.WriteLine("Ghostscript не найден: " + args[0]); return 2; }
string work = Path.Combine(Path.GetTempPath(), "pdftk_cs_" + Guid.NewGuid().ToString("N")[..8]);
Directory.CreateDirectory(work);

string pdfA = Path.Combine(work, "a.pdf"), pdfB = Path.Combine(work, "b.pdf");
await MakeNoisyPdf(gs.GsPath, work, pdfA, pages: 5, seed: 7);
await MakeNoisyPdf(gs.GsPath, work, pdfB, pages: 3, seed: 99);

Console.WriteLine("[1] Разбор диапазонов");
Check("'1-3,5,7-' при 10 стр.", PageRanges.Parse("1-3,5,7-", 10).SequenceEqual(new[] { new PageRange(1, 3), new PageRange(5, 5), new PageRange(7, 10) }));
Check("'2' при 5 стр.", PageRanges.Parse("2", 5).SequenceEqual(new[] { new PageRange(2, 2) }));
foreach (var bad in new[] { "0-2", "9-12", "3-1", "abc", "", "x-3" })
{
    bool threw = false;
    try { PageRanges.Parse(bad, 5); } catch (FormatException) { threw = true; }
    Check($"ошибка на «{bad}»", threw);
}
Check("Chunks(7,3)", PageRanges.Chunks(7, 3).SequenceEqual(new[] { new PageRange(1, 3), new PageRange(4, 6), new PageRange(7, 7) }));
Check("EachPage(3)", PageRanges.EachPage(3).Count == 3);

Console.WriteLine("[2] Подсчёт страниц");
int? na = await gs.PageCountAsync(pdfA), nb = await gs.PageCountAsync(pdfB);
Check("A = 5 стр.", na == 5, $"{na}");
Check("B = 3 стр.", nb == 3, $"{nb}");
Check("битый файл → null", await gs.PageCountAsync(MakeBroken(work)) is null);

Console.WriteLine("[3] Объединение");
string merged = Path.Combine(work, "merged.pdf");
await gs.MergeAsync(new[] { pdfA, pdfB }, merged);
Check("8 стр. после объединения", await gs.PageCountAsync(merged) == 8);
long srcSum = new FileInfo(pdfA).Length + new FileInfo(pdfB).Length;
Check("размер ≈ сумма (без перекодирования)", new FileInfo(merged).Length >= srcSum * 0.9,
      $"{new FileInfo(merged).Length:N0} vs {srcSum:N0}");

Console.WriteLine("[4] Извлечение страниц");
string ext = Path.Combine(work, "ext.pdf");
await gs.ExtractAsync(pdfA, new PageRange(2, 4), ext);
Check("2-4 → 3 стр.", await gs.PageCountAsync(ext) == 3);

Console.WriteLine("[5] Сжатие профилями");
long origA = new FileInfo(pdfA).Length;
foreach (int i in new[] { 1, 2, 3, 4 })
{
    string dst = Path.Combine(work, $"p{i}.pdf");
    long sz = await gs.CompressAsync(pdfA, dst, GhostscriptService.Profiles[i]);
    Check($"профиль {i} «{GhostscriptService.Profiles[i].Name}»: x{(double)origA / sz:0.0}, стр. ок",
          await gs.PageCountAsync(dst) == 5);
}

Console.WriteLine("[6] АВТО-режим, цель 2x");
string auto = Path.Combine(work, "auto.pdf");
long autoSize = await gs.AutoCompressAsync(pdfA, auto, 2.0);
Check("цель 2x достигнута", autoSize > 0 && autoSize <= origA / 2.0, $"x{(double)origA / autoSize:0.0}");
Check("страницы на месте", await gs.PageCountAsync(auto) == 5);
Check("временных файлов нет", Directory.GetFiles(work, "*.autotmp.pdf").Length == 0);

Console.WriteLine("[7] Битый вход не роняет АВТО-режим");
long broken = await gs.AutoCompressAsync(MakeBroken(work), Path.Combine(work, "bo.pdf"), 2.0);
Check("вернул 0", broken == 0, $"{broken}");

Console.WriteLine("[8] Отмена работы");
using (var cts = new CancellationTokenSource(TimeSpan.FromMilliseconds(1)))
{
    bool canceled = false;
    try { await gs.AutoCompressAsync(pdfA, Path.Combine(work, "c.pdf"), 2.0, null, cts.Token); }
    catch (OperationCanceledException) { canceled = true; }
    Check("отмена прерывает операцию", canceled);
}

Console.WriteLine($"\nИТОГО: {passed} пройдено, {failed} провалено");
Directory.Delete(work, true);
return failed == 0 ? 0 : 1;

// Генерирует PDF с цветными прямоугольниками (имитация скана с изображениями) через PostScript → pdfwrite.
static async Task MakeNoisyPdf(string gsPath, string dir, string pdfOut, int pages, int seed)
{
    string psPath = Path.ChangeExtension(pdfOut, ".ps");
    // Растровое RGB-изображение со случайными пикселями (как шум скана) на каждую страницу.
    string ps = $@"%!PS
{seed} srand
/W 1500 def /H 2000 def
/row W 3 mul string def
1 1 {pages} {{
  pop
  gsave 0 0 translate 612 792 scale
  W H 8 [W 0 0 H neg 0 H]
  {{ 0 1 row length 1 sub {{ row exch rand 255 mod put }} for row }}
  false 3 colorimage
  grestore
  0 setgray /Helvetica findfont 12 scalefont setfont 40 760 moveto (Test page) show
  showpage
}} for
";
    await File.WriteAllTextAsync(psPath, ps);
    var psi = new System.Diagnostics.ProcessStartInfo(gsPath)
    {
        UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true,
    };
    foreach (var a in new[] { "-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite", $"-sOutputFile={pdfOut}", psPath })
        psi.ArgumentList.Add(a);
    using var p = System.Diagnostics.Process.Start(psi)!;
    await p.WaitForExitAsync();
    if (p.ExitCode != 0 || !File.Exists(pdfOut))
        throw new InvalidOperationException("Не удалось создать тестовый PDF");
}

static string MakeBroken(string dir)
{
    string p = Path.Combine(dir, "broken.pdf");
    File.WriteAllText(p, "%PDF-1.4 garbage");
    return p;
}
