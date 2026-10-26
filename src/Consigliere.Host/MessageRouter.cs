using System.Text;
using Consigliere.Core.Agent;
using Consigliere.Core.Commands;
using Consigliere.Llm;

namespace Consigliere.Host;

/// <summary>Slash commands are handled deterministically; everything else goes to the LLM.</summary>
public sealed class MessageRouter
{
    private static readonly CommandInfo[] BuiltIns =
    [
        new("/status", "uptime, pause state, token usage"),
        new("/pause", "kill switch: stop scheduled work and LLM calls"),
        new("/resume", "undo /pause"),
        new("/reset", "forget this conversation"),
    ];

    private readonly AgentState _state;
    private readonly ChatService _chat;
    private readonly UsageTracker _usage;
    private readonly Dictionary<string, ICommandHandler> _skillCommands;

    public MessageRouter(AgentState state, ChatService chat, UsageTracker usage, IEnumerable<ICommandHandler> handlers)
    {
        _state = state;
        _chat = chat;
        _usage = usage;
        _skillCommands = handlers
            .SelectMany(h => h.Commands.Select(c => (c.Name, Handler: h)))
            .ToDictionary(x => x.Name, x => x.Handler, StringComparer.OrdinalIgnoreCase);
        Help = string.Join("\n", BuiltIns.Concat(handlers.SelectMany(h => h.Commands)).Select(c => $"{c.Name} - {c.Description}"))
               + "\nAnything else is sent to the assistant.";
    }

    public string Help { get; }

    public async Task<string> HandleAsync(long chatId, string text, CancellationToken ct)
    {
        var parts = text.Trim().Split(' ', 2);
        // Telegram appends @botname to commands tapped in group chats.
        var command = parts[0].Split('@')[0].ToLowerInvariant();
        var args = parts.Length > 1 ? parts[1].Trim() : "";

        switch (command)
        {
            case "/start":
            case "/help":
                return Help;
            case "/status":
                return Status();
            case "/pause":
                _state.Pause();
                return "Paused. Scheduled work and LLM calls are stopped until /resume.";
            case "/resume":
                _state.Resume();
                return "Resumed.";
            case "/reset":
                _chat.Reset(chatId);
                return "Conversation cleared.";
        }

        if (_state.Paused) return "I'm paused. Send /resume first.";
        if (_skillCommands.TryGetValue(command, out var handler)) return await handler.HandleAsync(command, args, chatId, ct);
        if (command.StartsWith('/')) return $"Unknown command {command}.\n\n{Help}";
        return await _chat.ReplyAsync(chatId, text, ct);
    }

    private string Status()
    {
        var uptime = DateTimeOffset.UtcNow - _state.StartedAt;
        var sb = new StringBuilder()
            .AppendLine($"State: {(_state.Paused ? "PAUSED" : "running")}")
            .AppendLine($"Uptime: {(int)uptime.TotalHours}h {uptime.Minutes}m");

        var snapshot = _usage.Snapshot();
        if (snapshot.Count == 0) sb.AppendLine("No LLM calls yet.");
        foreach (var (model, u) in snapshot)
            sb.AppendLine($"{model}: {u.Calls} calls, {u.InputTokens:N0} in / {u.OutputTokens:N0} out ({u.CacheReadTokens:N0} cached)");

        return sb.ToString().TrimEnd();
    }
}
