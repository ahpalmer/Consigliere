using System.Globalization;
using System.Text;

namespace Consigliere.Skills.Radar;

public static class RadarFormat
{
    /// <summary>Compact entry for digests.</summary>
    public static string Line(ItemView i, DateTimeOffset now)
    {
        var sb = new StringBuilder($"• {i.FitScore?.ToString(CultureInfo.InvariantCulture) ?? "–"} · {i.Title} · {i.Company} #{i.Id}\n");
        sb.Append("  ").AppendLine(string.Join(" · ", new[] { Shorten(i.Locations, 50), Comp(i.CompMin, i.CompMax), Age(i.PostedAt, now) }.Where(s => s.Length > 0)));
        if (!string.IsNullOrWhiteSpace(i.FitSummary)) sb.Append("  ").AppendLine(i.FitSummary);
        sb.Append("  ").Append(i.Url);
        return sb.ToString();
    }

    /// <summary>Full view for alerts and /item.</summary>
    public static string Detail(ItemView i, DateTimeOffset now)
    {
        var sb = new StringBuilder()
            .AppendLine($"{i.Title} · {i.Company}  #{i.Id}")
            .AppendLine($"Fit {i.FitScore?.ToString(CultureInfo.InvariantCulture) ?? "not scored"} · {i.Category ?? "uncategorized"} · {i.Tier ?? "no tier"}")
            .AppendLine(string.Join(" · ", new[] { Shorten(i.Locations, 80), Comp(i.CompMin, i.CompMax) }.Where(s => s.Length > 0)))
            .AppendLine($"Posted: {(i.PostedAt is { } p ? $"{p:yyyy-MM-dd} ({Age(p, now)})" : "unknown")} · first seen {i.FirstSeenAt:yyyy-MM-dd}");
        if (!string.IsNullOrWhiteSpace(i.FitSummary)) sb.AppendLine().AppendLine(i.FitSummary);
        AppendList(sb, "Why", i.FitReasons);
        AppendList(sb, "Gaps", i.FitGaps);
        if (i.Feedback is { } f) sb.AppendLine().AppendLine(f > 0 ? "You marked this 👍" : "You marked this 👎");
        return sb.AppendLine().Append(i.Url).ToString();
    }

    public static string Age(DateTimeOffset? posted, DateTimeOffset now)
    {
        if (posted is not { } p) return "";
        var days = Math.Max(0, (int)(now - p).TotalDays);
        return days switch
        {
            0 => "posted today",
            < 14 => $"posted {days}d ago",
            < 60 => $"posted {days / 7}w ago",
            _ => $"posted {days / 30}mo ago",
        };
    }

    public static string Comp(decimal? min, decimal? max)
    {
        static string K(decimal v) => "$" + (v / 1000m).ToString("0.#", CultureInfo.InvariantCulture) + "K";
        return (min, max) switch
        {
            ({ } lo, { } hi) when lo != hi => $"{K(lo)}–{K(hi)}",
            ({ } lo, _) => K(lo),
            (_, { } hi) => K(hi),
            _ => "",
        };
    }

    private static void AppendList(StringBuilder sb, string label, string? newlineSeparated)
    {
        if (string.IsNullOrWhiteSpace(newlineSeparated)) return;
        sb.AppendLine().AppendLine($"{label}:");
        foreach (var line in newlineSeparated.Split('\n', StringSplitOptions.RemoveEmptyEntries)) sb.AppendLine($"- {line}");
    }

    private static string Shorten(string s, int max) => s.Length <= max ? s : s[..(max - 1)] + "…";
}
