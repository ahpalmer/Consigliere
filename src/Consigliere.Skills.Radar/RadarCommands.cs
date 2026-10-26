using System.Text;
using Consigliere.Core.Commands;
using Consigliere.Data;
using Consigliere.Skills.Radar.Config;
using Microsoft.EntityFrameworkCore;

namespace Consigliere.Skills.Radar;

public sealed class RadarCommands(
    RadarScanner scanner,
    RadarDigest digest,
    RadarConfigLoader config,
    IDbContextFactory<ConsigliereDbContext> dbFactory) : ICommandHandler
{
    public IReadOnlyList<CommandInfo> Commands { get; } =
    [
        new("/scan", "check all watched sources now"),
        new("/digest", "show new items that passed the filter"),
        new("/sources", "watched sources and their last run"),
    ];

    public Task<string> HandleAsync(string command, string args, long chatId, CancellationToken ct) => command switch
    {
        "/scan" => ScanAsync(ct),
        "/digest" => DigestAsync(ct),
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
        if (summary.Passed > 0) sb.AppendLine("Send /digest to see them.");
        return sb.ToString().TrimEnd();
    }

    private async Task<string> DigestAsync(CancellationToken ct) =>
        (await digest.SendableAsync(ct)).Text ?? "Nothing new since the last digest.";

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
            sb.AppendLine($"• {s.Name} ({s.Ats}/{s.Board}): {status}");
        }
        return sb.ToString().TrimEnd();
    }
}
