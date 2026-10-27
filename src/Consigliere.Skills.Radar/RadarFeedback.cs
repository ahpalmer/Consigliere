using System.Globalization;
using Consigliere.Core.Channels;
using Consigliere.Data;
using Microsoft.EntityFrameworkCore;

namespace Consigliere.Skills.Radar;

/// <summary>👍/👎 on items, from buttons or /good and /bad. Stored for rubric tuning.</summary>
public sealed class RadarFeedback(IDbContextFactory<ConsigliereDbContext> dbFactory, TimeProvider time) : ICallbackHandler
{
    private const string CallbackPrefix = "rf:";

    public string Prefix => CallbackPrefix;

    public static IReadOnlyList<ActionButton> Buttons(long itemId) =>
    [
        new("👍 Good", $"{CallbackPrefix}+:{itemId}"),
        new("👎 Bad", $"{CallbackPrefix}-:{itemId}"),
    ];

    public async Task<string> HandleAsync(string data, CancellationToken ct)
    {
        // rf:+:123 / rf:-:123
        var parts = data.Split(':');
        if (parts.Length != 3 || !long.TryParse(parts[2], NumberStyles.None, CultureInfo.InvariantCulture, out var id))
            return "Unrecognized button.";
        var value = parts[1] == "+" ? 1 : -1;
        return await SetAsync([id], value, ct) == 1 ? (value > 0 ? "Noted 👍" : "Noted 👎") : $"Item #{id} not found.";
    }

    /// <returns>Number of items updated.</returns>
    public async Task<int> SetAsync(IReadOnlyCollection<long> ids, int value, CancellationToken ct)
    {
        var now = time.GetUtcNow();
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        return await db.RadarItems.Where(i => ids.Contains(i.Id))
            .ExecuteUpdateAsync(s => s.SetProperty(i => i.Feedback, value).SetProperty(i => i.FeedbackAt, now), ct);
    }
}
