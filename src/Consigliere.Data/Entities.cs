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

    /// <summary>When the source says the item was published. Null if the source doesn't say; never our own clock.</summary>
    public DateTimeOffset? PostedAt { get; set; }
    /// <summary>When we first fetched the item. Set once on insert; rescans never change it.</summary>
    public DateTimeOffset FirstSeenAt { get; set; }
    /// <summary>Most recent scan that still listed the item.</summary>
    public DateTimeOffset LastSeenAt { get; set; }

    /// <summary>Hash of title + description; keys LLM score caching.</summary>
    public required string ContentHash { get; set; }
    /// <summary>Hash of normalized company + title + locations; detects the same item from another source.</summary>
    public required string Fingerprint { get; set; }
    /// <summary>Hash of normalized company + title only; matches the same role reported by an aggregator (e.g. an alert email)
    /// whose location text differs from the company board's.</summary>
    public string? TitleKey { get; set; }
    public long? DuplicateOfId { get; set; }

    public bool Passed { get; set; }
    public string? Tier { get; set; }
    public string? FilterReason { get; set; }

    // LLM fit score. ScoringKey identifies the rubric + profile + model that produced it;
    // when any of those change, the item is due for re-scoring.
    public int? FitScore { get; set; }
    /// <summary>One of the owner's configured categories, assigned by the scorer.</summary>
    public string? Category { get; set; }
    public string? FitSummary { get; set; }
    /// <summary>Newline-separated.</summary>
    public string? FitReasons { get; set; }
    /// <summary>Newline-separated.</summary>
    public string? FitGaps { get; set; }
    public string? ScoringKey { get; set; }
    public DateTimeOffset? ScoredAt { get; set; }

    public DateTimeOffset? DigestedAt { get; set; }
    public DateTimeOffset? AlertedAt { get; set; }

    /// <summary>Owner's verdict: +1 good, -1 bad. Feeds rubric tuning.</summary>
    public int? Feedback { get; set; }
    public DateTimeOffset? FeedbackAt { get; set; }
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

/// <summary>An email the Mail skill has already read, so it's never extracted (or paid for) twice.</summary>
public sealed class ProcessedMessage
{
    /// <summary>Provider's message id (e.g. Gmail's).</summary>
    public required string Id { get; set; }
    public required string Mailbox { get; set; }
    public string From { get; set; } = "";
    public string Subject { get; set; } = "";
    public DateTimeOffset? ReceivedAt { get; set; }
    public DateTimeOffset ProcessedAt { get; set; }
    public int ItemCount { get; set; }
    /// <summary>Why it was skipped or failed, if it was.</summary>
    public string? Note { get; set; }
}
