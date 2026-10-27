namespace Consigliere.Skills.Radar;

public sealed class RadarOptions
{
    public const string Section = "Radar";

    /// <summary>Public JSON feeds are cheap to poll; LLM cost only applies to new or changed items.</summary>
    public string ScanCron { get; set; } = "0 * * * *";

    /// <summary>Works through any scoring backlog (e.g. after a profile edit makes every score stale).</summary>
    public string ScoreCron { get; set; } = "*/15 * * * *";

    public string DigestCron { get; set; } = "0 8 * * *";

    public int DigestMaxItems { get; set; } = 25;

    /// <summary>Items ranking below this (fit + source priority) are left out of digests.</summary>
    public int DigestMinScore { get; set; } = 50;

    /// <summary>Instant-alert tiers only alert at or above this rank score.</summary>
    public int AlertMinScore { get; set; } = 75;

    /// <summary>Max items scored per run; the rest wait for the next run.</summary>
    public int ScoreBatchSize { get; set; } = 200;

    public int ScoreConcurrency { get; set; } = 4;

    /// <summary>Location of the SQLite database inside the data pack.</summary>
    public string DatabasePath { get; set; } = "db/consigliere.db";
}
