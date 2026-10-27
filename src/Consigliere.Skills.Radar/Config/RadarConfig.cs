using Consigliere.Core.DataPack;
using YamlDotNet.Serialization;
using YamlDotNet.Serialization.NamingConventions;

namespace Consigliere.Skills.Radar.Config;

/// <summary>radar/watchlist.yaml in the data pack.</summary>
public sealed class Watchlist
{
    public List<WatchedSource> Sources { get; set; } = [];
}

public sealed class WatchedSource
{
    public string Name { get; set; } = "";
    /// <summary>Source kind, e.g. greenhouse | lever | ashby.</summary>
    public string Ats { get; set; } = "";
    /// <summary>Board token / company slug as it appears in the source URL.</summary>
    public string Board { get; set; } = "";
    /// <summary>low | normal | high. Nudges ranking (see <see cref="Ranking"/>); never hides items.</summary>
    public string Priority { get; set; } = "normal";
}

/// <summary>radar/preferences.yaml in the data pack.</summary>
public sealed class Preferences
{
    public KeywordSet Titles { get; set; } = new();
    /// <summary>Locations dropped before tiering; an item with no locations left is filtered out.</summary>
    public List<string> ExcludeLocations { get; set; } = [];
    /// <summary>Keywords that reject an item when found in its title or description.</summary>
    public List<string> DealBreakers { get; set; } = [];
    /// <summary>Evaluated in order; the first match wins. A tier with no locations and remote: false is a catch-all.</summary>
    public List<Tier> Tiers { get; set; } = [];
    /// <summary>Items posted longer ago than this are hidden from digests/alerts and never scored. Null = no limit.</summary>
    public int? MaxPostingAgeDays { get; set; }
    /// <summary>Ordered by priority, highest first. The scorer assigns each item one of these.</summary>
    public List<Category> Categories { get; set; } = [];
    /// <summary>Company-level priority, matched on the item's company name, so it applies whatever source reported the item.</summary>
    public CompanyPriority CompanyPriority { get; set; } = new();
}

public sealed class CompanyPriority
{
    /// <summary>Boosted, listed in a priority section at the top of digests, and alerted instantly in any tier.</summary>
    public List<string> High { get; set; } = [];
    public List<string> Low { get; set; } = [];
}

public sealed class Category
{
    public string Name { get; set; } = "";
    /// <summary>Tells the scorer what belongs here.</summary>
    public string Description { get; set; } = "";
}

public sealed class KeywordSet
{
    public List<string> Include { get; set; } = [];
    public List<string> Exclude { get; set; } = [];
}

public sealed class Tier
{
    public string Name { get; set; } = "";
    public List<string> Locations { get; set; } = [];
    public bool Remote { get; set; }
    /// <summary>instant | daily. Consumed by notifications.</summary>
    public string Alert { get; set; } = "daily";
    /// <summary>Reject items whose listed range tops out below this. Listed ranges are usually base pay.</summary>
    public decimal? MinComp { get; set; }
    /// <summary>When <see cref="MinComp"/> is set and an item lists no pay, file it under this tier name instead.
    /// Tiers not listed in preferences rank last.</summary>
    public string? UnknownCompTier { get; set; }

    public bool IsCatchAll => Locations.Count == 0 && !Remote;
}

/// <summary>Loads Radar config from the data pack on each call, so edits apply on the next scan.</summary>
public sealed class RadarConfigLoader(DataPack dataPack)
{
    public const string WatchlistPath = "radar/watchlist.yaml";
    public const string PreferencesPath = "radar/preferences.yaml";

    private static readonly IDeserializer Yaml = new DeserializerBuilder()
        .WithNamingConvention(UnderscoredNamingConvention.Instance)
        .IgnoreUnmatchedProperties()
        .Build();

    public Watchlist LoadWatchlist() => Load<Watchlist>(WatchlistPath);

    public Preferences LoadPreferences() => Load<Preferences>(PreferencesPath);

    private T Load<T>(string path) where T : new()
    {
        var text = dataPack.ReadTextOrDefault(path);
        return string.IsNullOrWhiteSpace(text) ? new T() : Yaml.Deserialize<T>(text) ?? new T();
    }
}
