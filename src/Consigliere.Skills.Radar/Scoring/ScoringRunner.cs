using Consigliere.Core.Agent;
using Consigliere.Data;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Radar.Scoring;

public sealed record ScoringSummary(int Scored, int Cached, int Declined, int Failed, int Remaining);

/// <summary>
/// Scores items that passed the rule filter and whose score is missing or stale (rubric/profile/model changed).
/// Fresh-enough items only, highest-ranked tiers first, a batch at a time. Then sends any instant alerts.
/// </summary>
public sealed class ScoringRunner(
    IItemScorer scorer,
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    RankingFactory rankingFactory,
    RadarAlerts alerts,
    AgentState state,
    IOptions<RadarOptions> options,
    TimeProvider time,
    ILogger<ScoringRunner> logger)
{
    private readonly SemaphoreSlim _gate = new(1, 1);

    public bool IsRunning => _gate.CurrentCount == 0;

    /// <summary>Items that still need a score under the current rubric/profile.</summary>
    public async Task<int> CountPendingAsync(CancellationToken ct)
    {
        var key = scorer.BuildContext().Key;
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        return await Pending(db, key, rankingFactory.Create().StaleBefore(time.GetUtcNow())).CountAsync(ct);
    }

    /// <summary>Fire-and-forget for chat commands, so Telegram isn't blocked while a batch scores.</summary>
    public void RunInBackground() => _ = Task.Run(async () =>
    {
        try { await RunAsync(CancellationToken.None); }
        catch (Exception ex) { logger.LogError(ex, "Background scoring failed"); }
    });

    /// <returns>Null when a run is already in progress.</returns>
    public async Task<ScoringSummary?> RunAsync(CancellationToken ct)
    {
        if (!await _gate.WaitAsync(0, ct)) return null;
        try
        {
            var summary = await ScoreBatchAsync(ct);
            await alerts.SendAsync(ct);
            return summary;
        }
        finally
        {
            _gate.Release();
        }
    }

    private async Task<ScoringSummary> ScoreBatchAsync(CancellationToken ct)
    {
        var context = scorer.BuildContext();
        var ranking = rankingFactory.Create();
        var staleBefore = ranking.StaleBefore(time.GetUtcNow());

        List<Candidate> candidates;
        await using (var db = await dbFactory.CreateDbContextAsync(ct))
            candidates = await Pending(db, context.Key, staleBefore)
                .Select(i => new Candidate(i.Id, i.ContentHash, i.Tier, i.PostedAt ?? i.FirstSeenAt))
                .ToListAsync(ct);

        // Best tiers first so the useful stuff is ready soonest (new items haven't been categorized yet).
        var batch = candidates
            .OrderBy(i => ranking.TierRank(i.Tier))
            .ThenByDescending(i => i.Recency)
            .Take(options.Value.ScoreBatchSize)
            .ToList();
        if (batch.Count == 0) return new ScoringSummary(0, 0, 0, 0, 0);

        // Identical content scores once: the first item in each group calls the model, the rest hit the cache.
        int scored = 0, cached = 0, declined = 0, failed = 0;
        var groups = batch.GroupBy(c => c.ContentHash).ToList();
        await Parallel.ForEachAsync(groups, new ParallelOptions { MaxDegreeOfParallelism = options.Value.ScoreConcurrency, CancellationToken = ct }, async (group, token) =>
        {
            foreach (var candidate in group)
            {
                if (state.Paused) return; // kill switch stops spend mid-batch
                try
                {
                    switch (await ScoreOneAsync(candidate.Id, context, token))
                    {
                        case Outcome.Scored: Interlocked.Increment(ref scored); break;
                        case Outcome.Cached: Interlocked.Increment(ref cached); break;
                        case Outcome.Declined: Interlocked.Increment(ref declined); break;
                    }
                }
                catch (Exception ex) when (!token.IsCancellationRequested)
                {
                    Interlocked.Increment(ref failed);
                    logger.LogWarning(ex, "Scoring item {Id} failed", candidate.Id);
                }
            }
        });

        var remaining = candidates.Count - scored - cached - declined;
        logger.LogInformation("Scoring: {Scored} scored, {Cached} from cache, {Declined} declined, {Failed} failed, {Remaining} remaining",
            scored, cached, declined, failed, remaining);
        if (failed > 0 && scored + cached + declined == 0)
            throw new InvalidOperationException($"All {failed} scoring attempts failed; see logs.");
        return new ScoringSummary(scored, cached, declined, failed, remaining);
    }

    private enum Outcome { Scored, Cached, Declined }

    private sealed record Candidate(long Id, string ContentHash, string? Tier, DateTimeOffset Recency);

    private async Task<Outcome> ScoreOneAsync(long id, ScoringContext context, CancellationToken ct)
    {
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var item = await db.RadarItems.SingleAsync(i => i.Id == id, ct);
        var now = time.GetUtcNow();

        // Same content already scored under the same rubric/profile (e.g. the posting changed back, or a re-post).
        var hit = await db.RadarItems
            .Where(i => i.Id != id && i.ContentHash == item.ContentHash && i.ScoringKey == context.Key && i.FitScore != null)
            .Select(i => new { i.FitScore, i.Category, i.FitSummary, i.FitReasons, i.FitGaps })
            .FirstOrDefaultAsync(ct);
        if (hit is not null)
        {
            (item.FitScore, item.Category, item.FitSummary, item.FitReasons, item.FitGaps) = (hit.FitScore, hit.Category, hit.FitSummary, hit.FitReasons, hit.FitGaps);
            item.ScoringKey = context.Key;
            item.ScoredAt = now;
            await db.SaveChangesAsync(ct);
            return Outcome.Cached;
        }

        var result = await scorer.ScoreAsync(
            new ScoreInput(item.Company, item.Title, item.Locations, item.Tier, item.CompMin, item.CompMax, item.PostedAt, item.Description),
            context, ct);

        // A declined item keeps a null score (so it never digests) but records the key so it isn't retried every run.
        item.FitScore = result?.Fit;
        item.Category = result?.Category;
        item.FitSummary = result?.Summary ?? "Declined to score.";
        item.FitReasons = result is null ? null : string.Join('\n', result.Reasons);
        item.FitGaps = result is null ? null : string.Join('\n', result.Gaps);
        item.ScoringKey = context.Key;
        item.ScoredAt = now;
        await db.SaveChangesAsync(ct);
        return result is null ? Outcome.Declined : Outcome.Scored;
    }

    private static IQueryable<Data.RadarItem> Pending(ConsigliereDbContext db, string key, DateTimeOffset? staleBefore) =>
        db.RadarItems.Where(i => i.Passed && i.DuplicateOfId == null
                                 && (i.ScoringKey == null || i.ScoringKey != key)
                                 && (staleBefore == null || i.PostedAt == null || i.PostedAt >= staleBefore));
}
