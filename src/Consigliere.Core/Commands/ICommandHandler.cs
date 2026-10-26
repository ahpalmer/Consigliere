namespace Consigliere.Core.Commands;

public sealed record CommandInfo(string Name, string Description);

/// <summary>A skill's slash commands. Handled deterministically, without the LLM.</summary>
public interface ICommandHandler
{
    IReadOnlyList<CommandInfo> Commands { get; }

    /// <param name="command">Lower-cased command including the slash, e.g. "/scan".</param>
    /// <param name="args">Everything after the command, trimmed.</param>
    Task<string> HandleAsync(string command, string args, long chatId, CancellationToken ct);
}
