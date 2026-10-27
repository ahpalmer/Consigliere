using System.Linq.Expressions;
using System.Text.RegularExpressions;
using Consigliere.Data;
using Consigliere.Skills.Radar.Config;

namespace Consigliere.Skills.Radar;

/// <summary>A RadarItem without its (large) description, for listing and ranking.</summary>
public sealed record ItemView(
    long Id, string Source, string Board, string Company, string Title, string Locations, string Url,
    decimal? CompMin, decimal? CompMax, DateTimeOffset? PostedAt, DateTimeOffset FirstSeenAt,
    string? Tier, string? Category, int? FitScore, string? FitSummary, string? FitReasons, string? FitGaps, int? Feedback)
{
    public static readonly Expression<Func<RadarItem, ItemView>> Projection = i => new ItemView(
        i.Id, i.Source, i.Board, i.Company, i.Title, i.Locations, i.Url, i.CompMin, i.CompMax, i.PostedAt, i.FirstSeenAt,
        i.Tier, i.Category, i.FitScore, i.FitSummary, i.FitReasons, i.FitGaps, i.Feedback);
}

public enum Priority { Low, Normal, High }

/// <summary>
/// Owner-configured ordering: high-priority companies first, then location tier, then category (both in preferences
/// order), then fit adjusted by priority. Priority nudges rather than hides, so a great low-priority item still surfaces.
/// </summary>
public sealed partial class Ranking
{
    public const int LowPriorityAdjustment = -15;
    public const int HighPriorityAdjustment = 15;

    private readonly Preferences _prefs;
    private readonly Dictionary<(string, string), Priority> _sourcePriorities;
    private readonly HashSet<string> _highCompanies;
    private readonly HashSet<string> _lowCompanies;

    public Ranking(Preferences prefs, Watchlist watchlist)
    {
        _prefs = prefs;
        _sourcePriorities = watchlist.Sources
            .GroupBy(s => (s.Ats.ToLowerInvariant(), s.Board.ToLowerInvariant()))
            .ToDictionary(g => g.Key, g => Parse(g.First().Priority));
        _highCompanies = prefs.CompanyPriority.High.Select(NormalizeCompany).ToHashSet();
        _lowCompanies = prefs.CompanyPriority.Low.Select(NormalizeCompany).ToHashSet();
    }

    public Preferences Preferences => _prefs;

    public int TierRank(string? tier) => RankOf(_prefs.Tiers.Select(t => t.Name), tier);

    public int CategoryRank(string? category) => RankOf(_prefs.Categories.Select(c => c.Name), category);

    /// <summary>Company priority (any source) wins over the watchlist source's priority.</summary>
    public Priority PriorityOf(ItemView item)
    {
        var company = NormalizeCompany(item.Company);
        if (_highCompanies.Contains(company)) return Priority.High;
        if (_lowCompanies.Contains(company)) return Priority.Low;
        return _sourcePriorities.GetValueOrDefault((item.Source.ToLowerInvariant(), item.Board.ToLowerInvariant()), Priority.Normal);
    }

    /// <summary>Fit plus priority adjustment; what thresholds and ordering compare against.</summary>
    public int RankScore(ItemView item) => (item.FitScore ?? 0) + PriorityOf(item) switch
    {
        Priority.Low => LowPriorityAdjustment,
        Priority.High => HighPriorityAdjustment,
        _ => 0,
    };

    public IOrderedEnumerable<ItemView> Order(IEnumerable<ItemView> items) => items
        .OrderBy(i => PriorityOf(i) == Priority.High ? 0 : 1)
        .ThenBy(i => TierRank(i.Tier))
        .ThenBy(i => CategoryRank(i.Category))
        .ThenByDescending(RankScore)
        .ThenByDescending(i => i.PostedAt ?? i.FirstSeenAt);

    /// <summary>Items posted before this are stale. Null when there's no age limit.</summary>
    public DateTimeOffset? StaleBefore(DateTimeOffset now) =>
        _prefs.MaxPostingAgeDays is { } days ? now.AddDays(-days) : null;

    /// <summary>"GitHub, Inc." → "github". Exact match after this, so "Sierra" doesn't catch "Sierra Nevada Corporation".</summary>
    public static string NormalizeCompany(string name)
    {
        var words = NonWord().Replace(name.ToLowerInvariant(), " ").Split(' ', StringSplitOptions.RemoveEmptyEntries).ToList();
        while (words.Count > 1 && LegalSuffixes.Contains(words[^1])) words.RemoveAt(words.Count - 1);
        return string.Join(' ', words);
    }

    private static readonly HashSet<string> LegalSuffixes = ["inc", "llc", "ltd", "corp", "corporation", "co", "company", "plc", "gmbh"];

    private static Priority Parse(string? priority) => priority?.ToLowerInvariant() switch
    {
        "low" => Priority.Low,
        "high" => Priority.High,
        _ => Priority.Normal,
    };

    private static int RankOf(IEnumerable<string> ordered, string? name)
    {
        var index = ordered.ToList().FindIndex(n => string.Equals(n, name, StringComparison.OrdinalIgnoreCase));
        return index < 0 ? int.MaxValue : index;
    }

    [GeneratedRegex(@"[^\p{L}\p{N}]+")]
    private static partial Regex NonWord();
}

public sealed class RankingFactory(RadarConfigLoader config)
{
    /// <summary>Reads config fresh each time so edits to priorities and categories apply immediately.</summary>
    public Ranking Create() => new(config.LoadPreferences(), config.LoadWatchlist());
}
