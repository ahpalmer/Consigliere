namespace Consigliere.Data;

/// <summary>Something a Radar source surfaced: normalized, deduped, and evaluated by the rule filter.</summary>
public sealed class RadarItem
{
    public long Id { get; set; }

    // Identity within the source: unique on (Source, Board, ExternalId).
    public required string Source { get; set; }
    public required string Board { get; set; }
    public required string ExternalId { get; set; }

    public required string Company { get; set; }
    public required string Title { get; set; }
    /// <summary>All listed locations joined with "; ".</summary>
    public string Locations { get; set; } = "";
    public required string Url { get; set; }
    public string Description { get; set; } = "";

    public decimal? CompMin { get; set; }
    public decimal? CompMax { get; set; }
    public string? CompCurrency { get; set; }

    public DateTimeOffset? PostedAt { get; set; }
    public DateTimeOffset FirstSeenAt { get; set; }
    public DateTimeOffset LastSeenAt { get; set; }

    /// <summary>Hash of title + description; keys LLM score caching.</summary>
    public required string ContentHash { get; set; }
    /// <summary>Hash of normalized company + title + locations; detects the same item from another source.</summary>
    public required string Fingerprint { get; set; }
    public long? DuplicateOfId { get; set; }

    public bool Passed { get; set; }
    public string? Tier { get; set; }
    public string? FilterReason { get; set; }

    public DateTimeOffset? DigestedAt { get; set; }
}

/// <summary>One fetch of one source, for /sources and failure tracking.</summary>
public sealed class SourceRun
{
    public long Id { get; set; }
    public required string Source { get; set; }
    public required string Board { get; set; }
    public DateTimeOffset StartedAt { get; set; }
    public DateTimeOffset FinishedAt { get; set; }
    public int Fetched { get; set; }
    public int New { get; set; }
    public int Passed { get; set; }
    public string? Error { get; set; }
}
