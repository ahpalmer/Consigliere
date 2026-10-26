using System.Globalization;
using System.Text;
using Consigliere.Data;
using Consigliere.Skills.Radar.Config;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Radar;

public sealed record DigestResult(string? Text, int Shown, int Remaining);

/// <summary>Formats items that passed the filter and haven't been sent yet, then marks them sent.</summary>
public sealed class RadarDigest(
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    RadarConfigLoader config,
    IOptions<RadarOptions> options,
    TimeProvider time)
{
    public async Task<DigestResult> SendableAsync(CancellationToken ct)
    {
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var pending = await db.RadarItems
            .Where(i => i.Passed && i.DuplicateOfId == null && i.DigestedAt == null)
            .Select(i => new { i.Id, i.Title, i.Company, i.Locations, i.Url, i.CompMin, i.CompMax, i.Tier, i.FirstSeenAt })
            .ToListAsync(ct);
        if (pending.Count == 0) return new DigestResult(null, 0, 0);

        var tierOrder = config.LoadPreferences().Tiers.Select(t => t.Name).ToList();
        int Rank(string? tier) => tier is null || tierOrder.IndexOf(tier) is var i && i < 0 ? int.MaxValue : i;

        var shown = pending
            .OrderBy(i => Rank(i.Tier))
            .ThenByDescending(i => i.FirstSeenAt)
            .Take(options.Value.DigestMaxItems)
            .ToList();
        var remaining = pending.Count - shown.Count;

        var sb = new StringBuilder($"Radar: {shown.Count} of {pending.Count} new\n");
        foreach (var group in shown.GroupBy(i => i.Tier ?? "untiered"))
        {
            sb.AppendLine().AppendLine($"— {group.Key} ({group.Count()}) —");
            foreach (var i in group)
            {
                sb.AppendLine($"• {i.Title} · {i.Company}");
                var details = new[] { Shorten(i.Locations, 60), FormatComp(i.CompMin, i.CompMax) }.Where(s => s.Length > 0);
                sb.AppendLine($"  {string.Join(" · ", details)}");
                sb.AppendLine($"  {i.Url}");
            }
        }
        if (remaining > 0) sb.AppendLine().AppendLine($"{remaining} more — send /digest again.");

        var ids = shown.Select(i => i.Id).ToList();
        var now = time.GetUtcNow();
        await db.RadarItems.Where(i => ids.Contains(i.Id)).ExecuteUpdateAsync(s => s.SetProperty(i => i.DigestedAt, now), ct);

        return new DigestResult(sb.ToString().TrimEnd(), shown.Count, remaining);
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

    internal static string FormatComp(decimal? min, decimal? max)
    {
        static string K(decimal v) => "$" + (v / 1000m).ToString("0.#", CultureInfo.InvariantCulture) + "K";
        return (min, max) switch
        {
            ({ } lo, { } hi) when lo != hi => $"{K(lo)}–{K(hi)}",
            ({ } lo, _) => K(lo),
            (_, { } hi) => K(hi),
            _ => "",
        };
    }

    private static string Shorten(string s, int max) => s.Length <= max ? s : s[..(max - 1)] + "…";
}
