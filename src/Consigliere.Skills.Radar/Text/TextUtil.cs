using System.Globalization;
using System.Net;
using System.Security.Cryptography;
using System.Text;
using System.Text.RegularExpressions;

namespace Consigliere.Skills.Radar.Text;

public static partial class TextUtil
{
    public static string HtmlToText(string html)
    {
        if (string.IsNullOrEmpty(html)) return "";
        // Some sources (Greenhouse) entity-escape the HTML itself, so decode before stripping tags.
        var decoded = WebUtility.HtmlDecode(html);
        var withBreaks = BlockTag().Replace(decoded, "\n");
        var stripped = WebUtility.HtmlDecode(AnyTag().Replace(withBreaks, " "));
        var collapsedSpaces = Spaces().Replace(stripped, " ");
        return BlankLines().Replace(collapsedSpaces, "\n\n").Trim();
    }

    public static string Hash(params string[] parts)
    {
        var bytes = SHA256.HashData(Encoding.UTF8.GetBytes(string.Join('\u001f', parts)));
        return Convert.ToHexString(bytes, 0, 16).ToLowerInvariant();
    }

    public static string Normalize(string s) => Spaces().Replace(s.Trim().ToLowerInvariant(), " ");

    /// <summary>Whole-word, case-insensitive match, so "uk" doesn't match "Milwaukee".</summary>
    public static bool ContainsKeyword(string text, string keyword) =>
        !string.IsNullOrWhiteSpace(keyword) &&
        Regex.IsMatch(text, $@"(?<![\p{{L}}\p{{N}}]){Regex.Escape(keyword.Trim())}(?![\p{{L}}\p{{N}}])", RegexOptions.IgnoreCase | RegexOptions.CultureInvariant);

    public static bool ContainsAnyKeyword(string text, IEnumerable<string> keywords) => keywords.Any(k => ContainsKeyword(text, k));

    /// <summary>
    /// Finds the first annual-looking pay range in free text: "$405,000—$485,000", "$211.4K – $290.6K", "$180k to $220k",
    /// "between $132,000 and $198,450", "$196,000.00 - $294,000.00".
    /// Ranges under $20K are ignored as hourly or monthly.
    /// </summary>
    public static (decimal Min, decimal Max)? ExtractPayRange(string text)
    {
        foreach (Match m in PayRange().Matches(text))
        {
            var min = ParseMoney(m.Groups["min"].Value);
            var max = ParseMoney(m.Groups["max"].Value);
            if (min is null || max is null || min < 20_000 || max < min) continue;
            return (min.Value, max.Value);
        }
        return null;
    }

    private static decimal? ParseMoney(string s)
    {
        s = s.Replace(",", "").Replace(" ", "");
        var thousands = s.EndsWith('k') || s.EndsWith('K');
        if (thousands) s = s[..^1];
        if (!decimal.TryParse(s, NumberStyles.Number, CultureInfo.InvariantCulture, out var value)) return null;
        return thousands ? value * 1000 : value;
    }

    [GeneratedRegex(@"<\s*(br|/p|/div|/li|/h\d|li)\b[^>]*>", RegexOptions.IgnoreCase)]
    private static partial Regex BlockTag();

    [GeneratedRegex("<[^>]+>")]
    private static partial Regex AnyTag();

    [GeneratedRegex(@"[ \t ]+")]
    private static partial Regex Spaces();

    [GeneratedRegex(@"\s*\n[\s\n]*")]
    private static partial Regex BlankLines();

    // Amount: "$405,000", "$196,000.00" (cents are dropped), or "$211.4K", optionally followed by "/yr". Separator: dash, en/em dash, "to", or "and".
    [GeneratedRegex(@"\$\s?(?<min>\d{1,3}(?:,\d{3})+|\d{2,3}(?:\.\d+)?\s?[kK])(?:\.\d{2})?(?:\s*/\s*(?:yr|year))?\s*(?:USD)?\s*(?:-|–|—|to|and)\s*\$?\s?(?<max>\d{1,3}(?:,\d{3})+|\d{2,3}(?:\.\d+)?\s?[kK])(?:\.\d{2})?")]
    private static partial Regex PayRange();
}
