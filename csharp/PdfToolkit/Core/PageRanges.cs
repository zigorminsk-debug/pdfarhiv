namespace PdfToolkit.Core;

/// <summary>Диапазон страниц (1-based, включительно).</summary>
public readonly record struct PageRange(int First, int Last);

/// <summary>Разбор строк вида "1-3,5,7-" и нарезка на порции.</summary>
public static class PageRanges
{
    public static List<PageRange> Parse(string spec, int maxPages)
    {
        spec = (spec ?? string.Empty).Replace(" ", "").Trim();
        if (spec.Length == 0)
            throw new FormatException("Укажите диапазоны, например: 1-3,5,7-10");

        var result = new List<PageRange>();
        foreach (var token in spec.Split(',', StringSplitOptions.RemoveEmptyEntries))
        {
            int start, end;
            int dash = token.IndexOf('-');
            if (dash >= 0)
            {
                string a = token[..dash];
                string b = token[(dash + 1)..];
                if (!int.TryParse(a, out start))
                    throw new FormatException($"Не понял диапазон: «{token}»");
                end = b.Length == 0 ? maxPages : (int.TryParse(b, out var e) ? e : throw new FormatException($"Не понял диапазон: «{token}»"));
            }
            else
            {
                if (!int.TryParse(token, out start))
                    throw new FormatException($"Не понял диапазон: «{token}»");
                end = start;
            }

            if (start < 1 || end < start)
                throw new FormatException($"Некорректный диапазон: «{token}» (страницы с 1, начало ≤ конца)");
            if (start > maxPages)
                throw new FormatException($"Диапазон «{token}» за пределами документа (всего {maxPages} стр.)");

            result.Add(new PageRange(start, Math.Min(end, maxPages)));
        }

        if (result.Count == 0)
            throw new FormatException("Не найдено ни одного диапазона");
        return result;
    }

    /// <summary>Нарезка на порции: 7 стр. по 3 → (1-3),(4-6),(7-7).</summary>
    public static List<PageRange> Chunks(int total, int step)
    {
        if (step < 1) step = 1;
        var result = new List<PageRange>();
        for (int i = 1; i <= total; i += step)
            result.Add(new PageRange(i, Math.Min(i + step - 1, total)));
        return result;
    }

    /// <summary>Каждая страница отдельно.</summary>
    public static List<PageRange> EachPage(int total)
    {
        var result = new List<PageRange>(total);
        for (int i = 1; i <= total; i++)
            result.Add(new PageRange(i, i));
        return result;
    }
}
