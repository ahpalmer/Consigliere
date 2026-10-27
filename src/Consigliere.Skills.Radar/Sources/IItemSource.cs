using Consigliere.Skills.Radar.Config;

namespace Consigliere.Skills.Radar.Sources;

/// <summary>A source item normalized to one shape, before persistence.</summary>
public sealed record FetchedItem
{
    public required string ExternalId { get; init; }
    public required string Company { get; init; }
    public required string Title { get; init; }
    /// <summary>Every listed location. Sources that flag remote work add a "Remote" entry if none mentions it.</summary>
    public IReadOnlyList<string> Locations { get; init; } = [];
    public required string Url { get; init; }
    public string Description { get; init; } = "";
    public decimal? CompMin { get; init; }
    public decimal? CompMax { get; init; }
    public string? CompCurrency { get; init; }
    public DateTimeOffset? PostedAt { get; init; }
}

public interface IItemSource
{
    /// <summary>Matches <see cref="WatchedSource.Ats"/>.</summary>
    string Kind { get; }

    /// <summary>
    /// Aggregators (alert emails, job sites) report items that usually also exist on a primary source, with less detail.
    /// Their items defer to a primary item with the same company + title.
    /// </summary>
    bool IsAggregator => false;

    Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct);
}

internal static class SourceHelpers
{
    public static IReadOnlyList<string> Locations(IEnumerable<string?> raw, bool flaggedRemote)
    {
        var list = raw.Where(l => !string.IsNullOrWhiteSpace(l)).Select(l => l!.Trim()).Distinct(StringComparer.OrdinalIgnoreCase).ToList();
        if (flaggedRemote && !list.Any(l => l.Contains("remote", StringComparison.OrdinalIgnoreCase)))
            list.Add("Remote");
        return list;
    }
}
