using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Sources;
using Consigliere.Skills.Radar.Text;

namespace Consigliere.Skills.Radar.Filtering;

public sealed record FilterOutcome(bool Passed, string? Tier, string? Reason)
{
    public static FilterOutcome Reject(string reason, string? tier = null) => new(false, tier, reason);
}

/// <summary>Cheap, deterministic first pass. Anything that survives goes on to scoring.</summary>
public static class RuleFilter
{
    public static FilterOutcome Evaluate(FetchedItem item, Preferences prefs)
    {
        var titles = prefs.Titles;
        if (titles.Include.Count > 0 && !TextUtil.ContainsAnyKeyword(item.Title, titles.Include))
            return FilterOutcome.Reject("title: no include keyword");

        var excludedTitle = titles.Exclude.FirstOrDefault(k => TextUtil.ContainsKeyword(item.Title, k));
        if (excludedTitle is not null)
            return FilterOutcome.Reject($"title: excluded '{excludedTitle}'");

        var dealBreaker = prefs.DealBreakers.FirstOrDefault(k => TextUtil.ContainsKeyword(item.Title, k) || TextUtil.ContainsKeyword(item.Description, k));
        if (dealBreaker is not null)
            return FilterOutcome.Reject($"deal-breaker '{dealBreaker}'");

        var locations = item.Locations.Where(l => !TextUtil.ContainsAnyKeyword(l, prefs.ExcludeLocations)).ToList();
        if (item.Locations.Count > 0 && locations.Count == 0)
            return FilterOutcome.Reject("location: all excluded");

        if (prefs.Tiers.Count == 0)
            return new FilterOutcome(true, null, null);

        var tier = prefs.Tiers.FirstOrDefault(t => Matches(t, locations));
        if (tier is null)
            return FilterOutcome.Reject("location: no matching tier");

        var topOfRange = item.CompMax ?? item.CompMin;
        if (tier.MinComp is { } floor && topOfRange is { } top && top < floor)
            return FilterOutcome.Reject($"comp: {top:N0} below {tier.Name} floor {floor:N0}", tier.Name);

        if (topOfRange is null && tier.MinComp is not null)
            return new FilterOutcome(true, tier.UnknownCompTier ?? tier.Name, "comp unknown");

        return new FilterOutcome(true, tier.Name, null);
    }

    private static bool Matches(Tier tier, IReadOnlyList<string> locations) =>
        tier.IsCatchAll
        || locations.Any(l => TextUtil.ContainsAnyKeyword(l, tier.Locations))
        || (tier.Remote && locations.Any(l => TextUtil.ContainsKeyword(l, "remote")));
}
