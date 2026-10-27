using System.Text;
using Consigliere.Data;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Radar;

public sealed record DigestResult(string? Text, int Shown, int Remaining);

/// <summary>
/// Formats scored items that haven't been sent yet, best first (priority companies → tier → category → adjusted fit),
/// then marks them sent. Unscored, stale, and low-ranking items are held back.
/// </summary>
public sealed class RadarDigest(
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    RankingFactory rankingFactory,
    IOptions<RadarOptions> options,
    TimeProvider time)
{
    public async Task<DigestResult> SendableAsync(CancellationToken ct)
    {
        var ranking = rankingFactory.Create();
        var now = time.GetUtcNow();
        var staleBefore = ranking.StaleBefore(now);

        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var unsent = db.RadarItems.Where(i => i.Passed && i.DuplicateOfId == null && i.DigestedAt == null
                                              && (staleBefore == null || i.PostedAt == null || i.PostedAt >= staleBefore));
        var awaitingScore = await unsent.CountAsync(i => i.FitScore == null && i.ScoredAt == null, ct);
        var scored = await unsent.Where(i => i.FitScore != null).Select(ItemView.Projection).ToListAsync(ct);

        var pending = ranking.Order(scored.Where(i => ranking.RankScore(i) >= options.Value.DigestMinScore)).ToList();
        if (pending.Count == 0)
            return new DigestResult(awaitingScore > 0 ? $"Nothing scored yet; {awaitingScore} item(s) still being scored." : null, 0, 0);

        var shown = pending.Take(options.Value.DigestMaxItems).ToList();
        var remaining = pending.Count - shown.Count;

        var sb = new StringBuilder($"Radar: {shown.Count} of {pending.Count} new\n");
        foreach (var group in shown.GroupBy(i => (Starred: ranking.PriorityOf(i) == Priority.High, Tier: i.Tier ?? "untiered", Category: i.Category ?? "uncategorized")))
        {
            var star = group.Key.Starred ? "★ priority · " : "";
            sb.AppendLine().AppendLine($"— {star}{group.Key.Tier} · {group.Key.Category} ({group.Count()}) —");
            foreach (var item in group) sb.AppendLine(RadarFormat.Line(item, now));
        }
        sb.AppendLine();
        if (remaining > 0) sb.AppendLine($"{remaining} more — send /digest again.");
        if (awaitingScore > 0) sb.AppendLine($"{awaitingScore} still being scored.");
        sb.Append("/item <id> for details · /good or /bad <id> to rate");

        var ids = shown.Select(i => i.Id).ToList();
        await db.RadarItems.Where(i => ids.Contains(i.Id)).ExecuteUpdateAsync(s => s.SetProperty(i => i.DigestedAt, now), ct);

        return new DigestResult(sb.ToString(), shown.Count, remaining);
    }

    /// <summary>Marks every sent item as unsent, so the next digest starts again from the top.</summary>
    /// <returns>Number of items put back in the queue.</returns>
    public async Task<int> ResetAsync(CancellationToken ct)
    {
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        return await db.RadarItems
            .Where(i => i.Passed && i.DuplicateOfId == null && i.DigestedAt != null)
            .ExecuteUpdateAsync(s => s.SetProperty(i => i.DigestedAt, (DateTimeOffset?)null), ct);
    }
}
