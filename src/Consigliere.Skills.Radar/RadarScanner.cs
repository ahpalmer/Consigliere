using Consigliere.Data;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Filtering;
using Consigliere.Skills.Radar.Sources;
using Consigliere.Skills.Radar.Text;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging;

namespace Consigliere.Skills.Radar;

public sealed record SourceResult(string Name, string Kind, int Fetched, int New, int Passed, string? Error);

public sealed record ScanSummary(IReadOnlyList<SourceResult> Sources)
{
    public int New => Sources.Sum(s => s.New);
    public int Passed => Sources.Sum(s => s.Passed);
}

/// <summary>Fetches every watched source, records new items, and runs the rule filter on them.</summary>
public sealed class RadarScanner(
    IServiceScopeFactory scopeFactory,
    RadarConfigLoader config,
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    TimeProvider time,
    ILogger<RadarScanner> logger)
{
    private readonly SemaphoreSlim _gate = new(1, 1);

    /// <returns>Null when a scan is already running.</returns>
    public async Task<ScanSummary?> ScanAsync(CancellationToken ct)
    {
        if (!await _gate.WaitAsync(0, ct)) return null;
        try
        {
            var watchlist = config.LoadWatchlist();
            var prefs = config.LoadPreferences();
            // Sources wrap typed HttpClients, which are meant to be short-lived; resolve them per scan.
            using var scope = scopeFactory.CreateScope();
            var sources = scope.ServiceProvider.GetServices<IItemSource>().ToDictionary(s => s.Kind, StringComparer.OrdinalIgnoreCase);
            var results = new List<SourceResult>();
            foreach (var watched in watchlist.Sources)
                results.Add(await ScanSourceAsync(watched, sources, prefs, ct));
            return new ScanSummary(results);
        }
        finally
        {
            _gate.Release();
        }
    }

    private async Task<SourceResult> ScanSourceAsync(WatchedSource watched, Dictionary<string, IItemSource> sources, Preferences prefs, CancellationToken ct)
    {
        var started = time.GetUtcNow();
        SourceResult result;
        if (!sources.TryGetValue(watched.Ats, out var source))
        {
            result = new SourceResult(watched.Name, watched.Ats, 0, 0, 0, $"unsupported source '{watched.Ats}'");
        }
        else
        {
            try
            {
                var fetched = await source.FetchAsync(watched, ct);
                result = await PersistAsync(source.Kind, watched, fetched, prefs, ct);
            }
            catch (Exception ex) when (ex is not OperationCanceledException || !ct.IsCancellationRequested)
            {
                logger.LogWarning(ex, "Scan of {Source} ({Kind}/{Board}) failed", watched.Name, watched.Ats, watched.Board);
                result = new SourceResult(watched.Name, source.Kind, 0, 0, 0, ex.Message);
            }
        }

        await using var db = await dbFactory.CreateDbContextAsync(ct);
        db.SourceRuns.Add(new SourceRun
        {
            Source = result.Kind,
            Board = watched.Board,
            StartedAt = started,
            FinishedAt = time.GetUtcNow(),
            Fetched = result.Fetched,
            New = result.New,
            Passed = result.Passed,
            Error = result.Error,
        });
        await db.SaveChangesAsync(ct);
        return result;
    }

    private async Task<SourceResult> PersistAsync(string kind, WatchedSource watched, IReadOnlyList<FetchedItem> fetched, Preferences prefs, CancellationToken ct)
    {
        var now = time.GetUtcNow();
        var items = fetched.DistinctBy(i => i.ExternalId).ToList();
        var ids = items.Select(i => i.ExternalId).ToList();

        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var known = await db.RadarItems
            .Where(i => i.Source == kind && i.Board == watched.Board && ids.Contains(i.ExternalId))
            .Select(i => new KnownItem(i.Id, i.ExternalId, i.ContentHash, i.CompMin, i.CompMax, i.DigestedAt))
            .ToDictionaryAsync(i => i.ExternalId, ct);

        if (known.Count > 0)
        {
            await db.RadarItems
                .Where(i => i.Source == kind && i.Board == watched.Board && ids.Contains(i.ExternalId))
                .ExecuteUpdateAsync(s => s.SetProperty(i => i.LastSeenAt, now), ct);
            await RefreshChangedAsync(db, items.Where(i => known.ContainsKey(i.ExternalId)), known, prefs, ct);
        }

        var fresh = items.Where(i => !known.ContainsKey(i.ExternalId)).Select(i => (Item: i, Fingerprint: Fingerprint(i))).ToList();
        var fingerprints = fresh.Select(f => f.Fingerprint).ToList();
        var originals = await db.RadarItems
            .Where(i => fingerprints.Contains(i.Fingerprint) && i.DuplicateOfId == null)
            .Select(i => new { i.Fingerprint, i.Id })
            .ToListAsync(ct);
        var originalByFingerprint = originals.GroupBy(o => o.Fingerprint).ToDictionary(g => g.Key, g => g.Min(o => o.Id));

        var passed = 0;
        foreach (var (item, fingerprint) in fresh)
        {
            var outcome = RuleFilter.Evaluate(item, prefs);
            if (outcome.Passed) passed++;
            db.RadarItems.Add(new RadarItem
            {
                Source = kind,
                Board = watched.Board,
                ExternalId = item.ExternalId,
                Company = item.Company,
                Title = item.Title,
                Locations = string.Join("; ", item.Locations),
                Url = item.Url,
                Description = item.Description,
                CompMin = item.CompMin,
                CompMax = item.CompMax,
                CompCurrency = item.CompCurrency,
                PostedAt = item.PostedAt,
                FirstSeenAt = now,
                LastSeenAt = now,
                ContentHash = TextUtil.Hash(item.Title, item.Description),
                Fingerprint = fingerprint,
                DuplicateOfId = originalByFingerprint.TryGetValue(fingerprint, out var originalId) ? originalId : null,
                Passed = outcome.Passed,
                Tier = outcome.Tier,
                FilterReason = outcome.Reason,
            });
        }
        await db.SaveChangesAsync(ct);

        logger.LogInformation("{Source}: {Fetched} fetched, {New} new, {Passed} passed filter", watched.Name, items.Count, fresh.Count, passed);
        return new SourceResult(watched.Name, kind, items.Count, fresh.Count, passed, null);
    }

    /// <summary>
    /// Picks up edited postings and parser improvements. Items not yet digested are re-filtered;
    /// digested ones keep their filter result so nothing already sent gets re-sent or silently dropped.
    /// </summary>
    private async Task RefreshChangedAsync(ConsigliereDbContext db, IEnumerable<FetchedItem> items, Dictionary<string, KnownItem> known, Preferences prefs, CancellationToken ct)
    {
        var refreshed = 0;
        foreach (var item in items)
        {
            var old = known[item.ExternalId];
            var hash = TextUtil.Hash(item.Title, item.Description);
            if (hash == old.ContentHash && item.CompMin == old.CompMin && item.CompMax == old.CompMax) continue;

            var locations = string.Join("; ", item.Locations);
            var rows = db.RadarItems.Where(i => i.Id == old.Id);
            if (old.DigestedAt is null)
            {
                var outcome = RuleFilter.Evaluate(item, prefs);
                await rows.ExecuteUpdateAsync(s => s
                    .SetProperty(i => i.Title, item.Title)
                    .SetProperty(i => i.Description, item.Description)
                    .SetProperty(i => i.Locations, locations)
                    .SetProperty(i => i.CompMin, item.CompMin)
                    .SetProperty(i => i.CompMax, item.CompMax)
                    .SetProperty(i => i.CompCurrency, item.CompCurrency)
                    .SetProperty(i => i.ContentHash, hash)
                    .SetProperty(i => i.Passed, outcome.Passed)
                    .SetProperty(i => i.Tier, outcome.Tier)
                    .SetProperty(i => i.FilterReason, outcome.Reason), ct);
            }
            else
            {
                await rows.ExecuteUpdateAsync(s => s
                    .SetProperty(i => i.Title, item.Title)
                    .SetProperty(i => i.Description, item.Description)
                    .SetProperty(i => i.Locations, locations)
                    .SetProperty(i => i.CompMin, item.CompMin)
                    .SetProperty(i => i.CompMax, item.CompMax)
                    .SetProperty(i => i.CompCurrency, item.CompCurrency)
                    .SetProperty(i => i.ContentHash, hash), ct);
            }
            refreshed++;
        }
        if (refreshed > 0) logger.LogInformation("Refreshed {Count} changed item(s)", refreshed);
    }

    private sealed record KnownItem(long Id, string ExternalId, string ContentHash, decimal? CompMin, decimal? CompMax, DateTimeOffset? DigestedAt);

    private static string Fingerprint(FetchedItem item) =>
        TextUtil.Hash(
            TextUtil.Normalize(item.Company),
            TextUtil.Normalize(item.Title),
            string.Join("|", item.Locations.Select(TextUtil.Normalize).Order()));
}
