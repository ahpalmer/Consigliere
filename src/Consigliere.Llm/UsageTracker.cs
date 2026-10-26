using System.Collections.Concurrent;

namespace Consigliere.Llm;

/// <summary>In-memory token accounting per model since process start. Persisted to the DB in a later milestone.</summary>
public sealed class UsageTracker
{
    private readonly ConcurrentDictionary<string, ModelUsage> _byModel = new();

    public void Record(string model, long inputTokens, long outputTokens, long cacheReadTokens)
    {
        _byModel.AddOrUpdate(
            model,
            _ => new ModelUsage(1, inputTokens, outputTokens, cacheReadTokens),
            (_, u) => new ModelUsage(u.Calls + 1, u.InputTokens + inputTokens, u.OutputTokens + outputTokens, u.CacheReadTokens + cacheReadTokens));
    }

    public IReadOnlyDictionary<string, ModelUsage> Snapshot() => new Dictionary<string, ModelUsage>(_byModel);
}

public sealed record ModelUsage(long Calls, long InputTokens, long OutputTokens, long CacheReadTokens);
