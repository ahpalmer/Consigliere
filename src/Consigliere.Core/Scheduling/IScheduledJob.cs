namespace Consigliere.Core.Scheduling;

/// <summary>Recurring work run by the host's scheduler. Skipped while the agent is paused.</summary>
public interface IScheduledJob
{
    string Name { get; }

    /// <summary>Standard 5-field cron expression, evaluated in the scheduler's configured time zone.</summary>
    string Schedule { get; }

    Task RunAsync(CancellationToken ct);
}
