using Consigliere.Core.Channels;
using Consigliere.Core.Scheduling;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Radar;

public sealed class RadarScanJob(RadarScanner scanner, IOptions<RadarOptions> options, ILogger<RadarScanJob> logger) : IScheduledJob
{
    public string Name => "radar-scan";

    public string Schedule => options.Value.ScanCron;

    public async Task RunAsync(CancellationToken ct)
    {
        var summary = await scanner.ScanAsync(ct);
        if (summary is null)
        {
            logger.LogInformation("Skipping scheduled scan; one is already running");
            return;
        }
        // Surface total outages to the scheduler's failure alert; single-source errors show in /sources.
        if (summary.Sources.Count > 0 && summary.Sources.All(s => s.Error is not null))
            throw new InvalidOperationException($"All {summary.Sources.Count} sources failed; first error: {summary.Sources[0].Error}");
    }
}

public sealed class RadarDigestJob(RadarDigest digest, IChannel channel, IOptions<RadarOptions> options) : IScheduledJob
{
    public string Name => "radar-digest";

    public string Schedule => options.Value.DigestCron;

    public async Task RunAsync(CancellationToken ct)
    {
        var result = await digest.SendableAsync(ct);
        if (result.Text is not null) await channel.NotifyOwnerAsync(result.Text, ct);
    }
}
