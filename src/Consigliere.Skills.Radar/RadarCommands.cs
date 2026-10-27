using System.Globalization;
using System.Text;
using Consigliere.Core.Commands;
using Consigliere.Data;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Scoring;
using Microsoft.EntityFrameworkCore;

namespace Consigliere.Skills.Radar;

public sealed class RadarCommands(
    RadarScanner scanner,
    ScoringRunner scoring,
    RadarDigest digest,
    RadarFeedback feedback,
    RadarConfigLoader config,
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    TimeProvider time) : ICommandHandler
{
    public IReadOnlyList<CommandInfo> Commands { get; } =
    [
        new("/scan", "check all watched sources now, then score what's new"),
        new("/score", "scoring progress; starts scoring if anything is waiting"),
        new("/digest", "best new items; '/digest reset' starts over from the top"),
        new("/item", "full details for one item, e.g. /item 123"),
        new("/good", "mark item(s) as a good find, e.g. /good 123 456"),
        new("/bad", "mark item(s) as a miss, e.g. /bad 123"),
        new("/sources", "watched sources and their last run"),
    ];

    public Task<string> HandleAsync(string command, string args, long chatId, CancellationToken ct) => command switch
    {
        "/scan" => ScanAsync(ct),
        "/score" => ScoreAsync(ct),
        "/digest" when args.Equals("reset", StringComparison.OrdinalIgnoreCase) => ResetDigestAsync(ct),
        "/digest" => DigestAsync(ct),
        "/item" => ItemAsync(args, ct),
        "/good" => FeedbackAsync(args, 1, ct),
        "/bad" => FeedbackAsync(args, -1, ct),
        "/sources" => SourcesAsync(ct),
        _ => throw new ArgumentOutOfRangeException(nameof(command), command, null),
    };

    private async Task<string> ScanAsync(CancellationToken ct)
    {
        var summary = await scanner.ScanAsync(ct);
        if (summary is null) return "A scan is already running.";
        if (summary.Sources.Count == 0) return $"Nothing to scan. Add sources to {RadarConfigLoader.WatchlistPath} in the data pack.";

        var sb = new StringBuilder($"Scanned {summary.Sources.Count} source(s): {summary.New} new, {summary.Passed} passed the filter.\n");
        foreach (var s in summary.Sources.Where(s => s.Error is not null))
            sb.AppendLine($"⚠ {s.Name}: {s.Error}");

        var pending = await scoring.CountPendingAsync(ct);
        if (pending > 0)
        {
            scoring.RunInBackground();
            sb.AppendLine($"Scoring {pending} item(s) in the background. Strong local matches will alert you; /score shows progress.");
        }
        return sb.ToString().TrimEnd();
    }

    private async Task<string> ScoreAsync(CancellationToken ct)
    {
        var pending = await scoring.CountPendingAsync(ct);
        if (scoring.IsRunning) return $"Scoring in progress; {pending} item(s) left.";
        if (pending == 0) return "Everything is scored.";
        scoring.RunInBackground();
        return $"Started scoring {pending} item(s).";
    }

    private async Task<string> DigestAsync(CancellationToken ct) =>
        (await digest.SendableAsync(ct)).Text ?? "Nothing new since the last digest.";

    private async Task<string> ResetDigestAsync(CancellationToken ct)
    {
        var count = await digest.ResetAsync(ct);
        return count == 0 ? "Nothing to reset; no items have been sent yet." : $"Reset {count} item(s). Send /digest to start from the top.";
    }

    private async Task<string> ItemAsync(string args, CancellationToken ct)
    {
        var ids = ParseIds(args);
        if (ids.Count != 1) return "Usage: /item <id>";
        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var item = await db.RadarItems.Where(i => i.Id == ids[0]).Select(ItemView.Projection).SingleOrDefaultAsync(ct);
        return item is null ? $"No item #{ids[0]}." : RadarFormat.Detail(item, time.GetUtcNow());
    }

    private async Task<string> FeedbackAsync(string args, int value, CancellationToken ct)
    {
        var ids = ParseIds(args);
        if (ids.Count == 0) return $"Usage: {(value > 0 ? "/good" : "/bad")} <id> [id...]";
        var updated = await feedback.SetAsync(ids, value, ct);
        return $"Marked {updated} item(s) {(value > 0 ? "👍" : "👎")}." + (updated < ids.Count ? $" {ids.Count - updated} id(s) not found." : "");
    }

    private async Task<string> SourcesAsync(CancellationToken ct)
    {
        var watched = config.LoadWatchlist().Sources;
        if (watched.Count == 0) return $"No sources. Add some to {RadarConfigLoader.WatchlistPath} in the data pack.";

        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var runs = await db.SourceRuns
            .GroupBy(r => new { r.Source, r.Board })
            .Select(g => g.OrderByDescending(r => r.StartedAt).First())
            .ToListAsync(ct);

        var sb = new StringBuilder();
        foreach (var s in watched)
        {
            var run = runs.FirstOrDefault(r => string.Equals(r.Source, s.Ats, StringComparison.OrdinalIgnoreCase) && r.Board == s.Board);
            var status = run switch
            {
                null => "never run",
                { Error: { } error } => $"FAILED {run.StartedAt.LocalDateTime:g}: {error}",
                _ => $"{run.Fetched} items, {run.New} new at {run.StartedAt.LocalDateTime:g}",
            };
            var priority = string.Equals(s.Priority, "normal", StringComparison.OrdinalIgnoreCase) ? "" : $" [{s.Priority}]";
            sb.AppendLine($"• {s.Name}{priority} ({s.Ats}/{s.Board}): {status}");
        }
        return sb.ToString().TrimEnd();
    }

    private static List<long> ParseIds(string args) =>
        args.Split([' ', ',', '#'], StringSplitOptions.RemoveEmptyEntries)
            .Select(a => long.TryParse(a, NumberStyles.None, CultureInfo.InvariantCulture, out var id) ? id : -1)
            .Where(id => id > 0)
            .Distinct()
            .ToList();
}
