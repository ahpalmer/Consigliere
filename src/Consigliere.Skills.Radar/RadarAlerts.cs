using Consigliere.Core.Channels;
using Consigliere.Data;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Radar;

/// <summary>
/// Sends strong matches right away, one message each with feedback buttons: anything in an "instant" tier,
/// plus high-priority companies in any tier.
/// </summary>
public sealed class RadarAlerts(
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    RankingFactory rankingFactory,
    IChannel channel,
    IOptions<RadarOptions> options,
    TimeProvider time)
{
    public async Task<int> SendAsync(CancellationToken ct)
    {
        var ranking = rankingFactory.Create();
        var instantTiers = ranking.Preferences.Tiers
            .Where(t => string.Equals(t.Alert, "instant", StringComparison.OrdinalIgnoreCase))
            .Select(t => t.Name)
            .ToList();
        var now = time.GetUtcNow();
        var staleBefore = ranking.StaleBefore(now);
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        // Company priority needs name normalization, so the tier/priority test runs in memory on the (small) unsent set.
        var candidates = await db.RadarItems
            .Where(i => i.Passed && i.DuplicateOfId == null && i.FitScore != null && i.AlertedAt == null && i.DigestedAt == null
                        && (staleBefore == null || i.PostedAt == null || i.PostedAt >= staleBefore))
            .Select(ItemView.Projection)
            .ToListAsync(ct);

        var alertable = candidates.Where(i =>
            (instantTiers.Contains(i.Tier ?? "", StringComparer.OrdinalIgnoreCase) || ranking.PriorityOf(i) == Priority.High)
            && ranking.RankScore(i) >= options.Value.AlertMinScore);

        var sent = 0;
        foreach (var item in ranking.Order(alertable))
        {
            var marker = ranking.PriorityOf(item) == Priority.High ? "★ " : "🔔 ";
            await channel.NotifyOwnerAsync(marker + RadarFormat.Detail(item, now), RadarFeedback.Buttons(item.Id), ct);
            // Alerted items count as digested so they don't show up again in the next digest.
            await db.RadarItems.Where(i => i.Id == item.Id)
                .ExecuteUpdateAsync(s => s.SetProperty(i => i.AlertedAt, now).SetProperty(i => i.DigestedAt, now), ct);
            sent++;
        }
        return sent;
    }
}
