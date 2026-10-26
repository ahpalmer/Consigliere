namespace Consigliere.Core.Agent;

/// <summary>Process-wide runtime state. Paused is the kill switch: schedulers and outbound LLM calls check it.</summary>
public sealed class AgentState
{
    private volatile bool _paused;

    public DateTimeOffset StartedAt { get; } = DateTimeOffset.UtcNow;

    public bool Paused => _paused;

    public void Pause() => _paused = true;

    public void Resume() => _paused = false;
}
