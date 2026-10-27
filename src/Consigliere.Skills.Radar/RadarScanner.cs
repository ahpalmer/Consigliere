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

    // Set at the start of each scan (under _gate) from the registered sources.
    private List<string> _aggregatorKinds = [];

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
            _aggregatorKinds = sources.Values.Where(s => s.IsAggregator).Select(s => s.Kind).ToList();
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
                result = await PersistAsync(source, watched, fetched, prefs, ct);
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

    private async Task<SourceResult> PersistAsync(IItemSource source, WatchedSource watched, IReadOnlyList<FetchedItem> fetched, Preferences prefs, CancellationToken ct)
    {
        var kind = source.Kind;
        var now = time.GetUtcNow();
        var items = fetched.DistinctBy(i => i.ExternalId).ToList();
        var ids = items.Select(i => i.ExternalId).ToList();

        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var known = await db.RadarItems
            .Where(i => i.Source == kind && i.Board == watched.Board && ids.Contains(i.ExternalId))
            .Select(i => new KnownItem(i.Id, i.ExternalId, i.ContentHash, i.CompMin, i.CompMax, i.DigestedAt, i.Passed, i.Tier, i.FilterReason, i.TitleKey))
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

        // Aggregator items defer to an existing primary item for the same role (location text often differs).
        var primaryByTitleKey = new Dictionary<string, long>();
        if (source.IsAggregator)
        {
            var titleKeys = fresh.Select(f => TitleKey(f.Item)).Distinct().ToList();
            primaryByTitleKey = (await db.RadarItems
                    .Where(i => i.TitleKey != null && titleKeys.Contains(i.TitleKey) && i.DuplicateOfId == null && !_aggregatorKinds.Contains(i.Source))
                    .Select(i => new { i.TitleKey, i.Id })
                    .ToListAsync(ct))
                .GroupBy(o => o.TitleKey!).ToDictionary(g => g.Key, g => g.Min(o => o.Id));
        }

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
                TitleKey = TitleKey(item),
                DuplicateOfId = originalByFingerprint.TryGetValue(fingerprint, out var originalId) ? originalId
                    : primaryByTitleKey.TryGetValue(TitleKey(item), out var primaryId) ? primaryId : null,
                Passed = outcome.Passed,
                Tier = outcome.Tier,
                FilterReason = outcome.Reason,
            });
        }
        await db.SaveChangesAsync(ct);
        if (!source.IsAggregator) await AdoptAggregatorCopiesAsync(db, ct);

        logger.LogInformation("{Source}: {Fetched} fetched, {New} new, {Passed} passed filter", watched.Name, items.Count, fresh.Count, passed);
        return new SourceResult(watched.Name, kind, items.Count, fresh.Count, passed, null);
    }

    /// <summary>
    /// Picks up edited postings, parser improvements, and preference edits. Items not yet digested are re-filtered
    /// against current preferences; digested ones keep their filter result so nothing already sent is re-sent or dropped.
    /// PostedAt and FirstSeenAt are never touched here.
    /// </summary>
    private async Task RefreshChangedAsync(ConsigliereDbContext db, IEnumerable<FetchedItem> items, Dictionary<string, KnownItem> known, Preferences prefs, CancellationToken ct)
    {
        var refreshed = 0;
        foreach (var item in items)
        {
            var old = known[item.ExternalId];
            var hash = TextUtil.Hash(item.Title, item.Description);
            var contentChanged = hash != old.ContentHash || item.CompMin != old.CompMin || item.CompMax != old.CompMax || old.TitleKey is null;
            var outcome = old.DigestedAt is null ? RuleFilter.Evaluate(item, prefs) : null;
            var filterChanged = outcome is not null && (outcome.Passed != old.Passed || outcome.Tier != old.Tier || outcome.Reason != old.FilterReason);
            if (!contentChanged && !filterChanged) continue;

            var locations = string.Join("; ", item.Locations);
            await db.RadarItems.Where(i => i.Id == old.Id).ExecuteUpdateAsync(s =>
            {
                if (contentChanged)
                {
                    s.SetProperty(i => i.Title, item.Title)
                     .SetProperty(i => i.Description, item.Description)
                     .SetProperty(i => i.Locations, locations)
                     .SetProperty(i => i.CompMin, item.CompMin)
                     .SetProperty(i => i.CompMax, item.CompMax)
                     .SetProperty(i => i.CompCurrency, item.CompCurrency)
                     .SetProperty(i => i.ContentHash, hash)
                     .SetProperty(i => i.TitleKey, TitleKey(item));
                }
                if (outcome is not null)
                {
                    s.SetProperty(i => i.Passed, outcome.Passed)
                     .SetProperty(i => i.Tier, outcome.Tier)
                     .SetProperty(i => i.FilterReason, outcome.Reason);
                }
            }, ct);
            refreshed++;
        }
        if (refreshed > 0) logger.LogInformation("Refreshed {Count} changed item(s)", refreshed);
    }

    private sealed record KnownItem(long Id, string ExternalId, string ContentHash, decimal? CompMin, decimal? CompMax,
        DateTimeOffset? DigestedAt, bool Passed, string? Tier, string? FilterReason, string? TitleKey);

    /// <summary>
    /// The reverse case: a primary item arrived after an aggregator already reported the same role.
    /// The aggregator copy (less detail) becomes the duplicate, unless it was already sent to the owner.
    /// </summary>
    private async Task AdoptAggregatorCopiesAsync(ConsigliereDbContext db, CancellationToken ct)
    {
        if (_aggregatorKinds.Count == 0) return;
        var copies = await db.RadarItems
            .Where(a => _aggregatorKinds.Contains(a.Source) && a.DuplicateOfId == null && a.DigestedAt == null && a.TitleKey != null)
            .Select(a => new
            {
                a.Id,
                PrimaryId = db.RadarItems
                    .Where(p => p.TitleKey == a.TitleKey && p.DuplicateOfId == null && !_aggregatorKinds.Contains(p.Source))
                    .Min(p => (long?)p.Id),
            })
            .Where(x => x.PrimaryId != null)
            .ToListAsync(ct);
        foreach (var copy in copies)
            await db.RadarItems.Where(i => i.Id == copy.Id).ExecuteUpdateAsync(s => s.SetProperty(i => i.DuplicateOfId, copy.PrimaryId), ct);
        if (copies.Count > 0) logger.LogInformation("{Count} aggregator item(s) matched a primary source", copies.Count);
    }

    private static string TitleKey(FetchedItem item) => TextUtil.Hash(TextUtil.Normalize(item.Company), TextUtil.Normalize(item.Title));

    private static string Fingerprint(FetchedItem item) =>
        TextUtil.Hash(
            TextUtil.Normalize(item.Company),
            TextUtil.Normalize(item.Title),
            string.Join("|", item.Locations.Select(TextUtil.Normalize).Order()));
}
