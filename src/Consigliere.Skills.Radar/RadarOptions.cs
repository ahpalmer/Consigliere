namespace Consigliere.Skills.Radar;

public sealed class RadarOptions
{
    public const string Section = "Radar";

    /// <summary>Public JSON feeds are cheap to poll; LLM cost only applies to new items.</summary>
    public string ScanCron { get; set; } = "0 * * * *";

    public string DigestCron { get; set; } = "0 8 * * *";

    public int DigestMaxItems { get; set; } = 25;

    /// <summary>Location of the SQLite database inside the data pack.</summary>
    public string DatabasePath { get; set; } = "db/consigliere.db";
}
