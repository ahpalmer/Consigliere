using Consigliere.Core.Agent;
using Consigliere.Core.Channels;
using Consigliere.Core.Scheduling;
using Cronos;
using Microsoft.Extensions.Options;

namespace Consigliere.Host.Scheduling;

public sealed class SchedulerOptions
{
    public const string Section = "Scheduler";

    /// <summary>IANA or Windows time zone id that cron expressions are evaluated in.</summary>
    public string TimeZone { get; set; } = "America/New_York";

    /// <summary>
    /// Optional healthchecks.io ping base, e.g. "https://hc-ping.com/<ping-key>". Each job pings "{base}/{job-name}"
    /// on success and ".../fail" on failure, so a check per job goes red if that job stops running. Keep it in secrets.
    /// </summary>
    public string? HealthchecksPingUrl { get; set; }
}

/// <summary>
/// Runs each <see cref="IScheduledJob"/> on its cron schedule inside the host process.
/// Paused agents skip runs; a job that starts failing alerts the owner once until it recovers.
/// With healthchecks configured, every run is also reported externally: the dead-man's switch for a dead host.
/// </summary>
public sealed class JobScheduler(
    IEnumerable<IScheduledJob> jobs,
    AgentState state,
    IChannel channel,
    IOptions<SchedulerOptions> options,
    IHttpClientFactory httpFactory,
    TimeProvider time,
    ILogger<JobScheduler> logger) : BackgroundService
{
    protected override Task ExecuteAsync(CancellationToken stoppingToken)
    {
        var zone = TimeZoneInfo.FindSystemTimeZoneById(options.Value.TimeZone);
        return Task.WhenAll(jobs.Select(job => RunLoopAsync(job, CronExpression.Parse(job.Schedule), zone, stoppingToken)));
    }

    private async Task RunLoopAsync(IScheduledJob job, CronExpression cron, TimeZoneInfo zone, CancellationToken ct)
    {
        var failing = false;
        while (!ct.IsCancellationRequested)
        {
            var next = cron.GetNextOccurrence(time.GetUtcNow(), zone);
            if (next is null) return;
            logger.LogInformation("Next {Job} run at {Next}", job.Name, TimeZoneInfo.ConvertTime(next.Value, zone));

            try
            {
                await Task.Delay(next.Value - time.GetUtcNow(), time, ct);
            }
            catch (OperationCanceledException)
            {
                return;
            }

            if (state.Paused)
            {
                logger.LogInformation("Skipping {Job}: agent is paused", job.Name);
                continue;
            }

            try
            {
                await job.RunAsync(ct);
                await PingAsync(job.Name, success: true, ct);
                if (failing) await channel.NotifyOwnerAsync($"✅ {job.Name} recovered.", ct);
                failing = false;
            }
            catch (Exception ex) when (!ct.IsCancellationRequested)
            {
                logger.LogError(ex, "Scheduled job {Job} failed", job.Name);
                await PingAsync(job.Name, success: false, ct);
                if (!failing) await channel.NotifyOwnerAsync($"⚠ {job.Name} failed: {ex.Message}", ct);
                failing = true;
            }
        }
    }

    private async Task PingAsync(string jobName, bool success, CancellationToken ct)
    {
        if (string.IsNullOrWhiteSpace(options.Value.HealthchecksPingUrl)) return;
        var url = $"{options.Value.HealthchecksPingUrl.TrimEnd('/')}/{jobName}{(success ? "" : "/fail")}?create=1";
        try
        {
            using var client = httpFactory.CreateClient();
            client.Timeout = TimeSpan.FromSeconds(10);
            (await client.GetAsync(url, ct)).EnsureSuccessStatusCode();
        }
        catch (Exception ex) when (!ct.IsCancellationRequested)
        {
            // Monitoring must never break the job itself.
            logger.LogWarning(ex, "Healthchecks ping for {Job} failed", jobName);
        }
    }
}
