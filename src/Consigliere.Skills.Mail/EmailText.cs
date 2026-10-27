using System.Net;
using System.Text.RegularExpressions;
using Consigliere.Skills.Radar.Text;

namespace Consigliere.Skills.Mail;

public static partial class EmailText
{
    /// <summary>
    /// HTML email to plain text that keeps each link as "text [url]", so the extractor can cite real URLs.
    /// Image-only links (logos, tracking pixels) are dropped, and known URLs are shortened to their canonical form.
    /// </summary>
    public static string ToText(string? html, string? plain)
    {
        if (string.IsNullOrWhiteSpace(html)) return CanonicalizeUrlsInText(plain ?? "");

        var withLinks = Anchor().Replace(html, m =>
        {
            var text = TextUtil.HtmlToText(m.Groups["text"].Value).Replace('\n', ' ').Trim();
            if (text.Length == 0) return " ";
            var url = Canonicalize(WebUtility.HtmlDecode(m.Groups["href"].Value));
            return $" {text} [{url}] ";
        });
        return TextUtil.HtmlToText(withLinks);
    }

    /// <summary>Every URL in the text, canonicalized. Extracted items must link to one of these.</summary>
    public static HashSet<string> Urls(string text) =>
        UrlPattern().Matches(text).Select(m => Canonicalize(m.Value.TrimEnd(']', ')', '.', ','))).ToHashSet(StringComparer.OrdinalIgnoreCase);

    /// <summary>Strips tracking so the same job always has the same URL. LinkedIn job links collapse to /jobs/view/{id}/.</summary>
    public static string Canonicalize(string url)
    {
        url = url.Trim();
        if (LinkedInJobId(url) is { } id) return $"https://www.linkedin.com/jobs/view/{id}/";
        return url;
    }

    public static string? LinkedInJobId(string url) => LinkedInJob().Match(url) is { Success: true } m ? m.Groups["id"].Value : null;

    private static string CanonicalizeUrlsInText(string text) => UrlPattern().Replace(text, m => Canonicalize(m.Value));

    [GeneratedRegex("""<a\b[^>]*?href\s*=\s*["'](?<href>[^"']+)["'][^>]*>(?<text>.*?)</a\s*>""", RegexOptions.IgnoreCase | RegexOptions.Singleline)]
    private static partial Regex Anchor();

    [GeneratedRegex(@"https?://[^\s\[\]<>""']+", RegexOptions.IgnoreCase)]
    private static partial Regex UrlPattern();

    [GeneratedRegex(@"linkedin\.com/(?:comm/)?jobs/view/(?<id>\d+)", RegexOptions.IgnoreCase)]
    private static partial Regex LinkedInJob();
}
