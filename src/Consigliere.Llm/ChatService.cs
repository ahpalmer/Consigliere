using System.Collections.Concurrent;
using Anthropic;
using Anthropic.Models.Beta.Messages;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace Consigliere.Llm;

/// <summary>
/// Conversational turn handling. History is in-memory per conversation and stored as text only,
/// so trimming old turns never edits thinking blocks the API would need to validate.
/// </summary>
public sealed class ChatService(
    AnthropicClient client,
    PromptLibrary prompts,
    UsageTracker usage,
    IOptions<LlmOptions> options,
    ILogger<ChatService> logger)
{
    private readonly ConcurrentDictionary<long, List<BetaMessageParam>> _histories = new();
    private readonly LlmOptions _options = options.Value;

    public async Task<string> ReplyAsync(long conversationId, string userText, CancellationToken ct)
    {
        var history = _histories.GetOrAdd(conversationId, _ => []);
        List<BetaMessageParam> request;
        lock (history)
        {
            history.Add(new BetaUserMessageParam { Content = userText });
            request = [.. history];
        }

        var model = _options.ModelFor(LlmTask.Chat);
        var response = await client.Beta.Messages.Create(new MessageCreateParams
        {
            Model = model,
            MaxTokens = 16000,
            Betas = ["server-side-fallback-2026-07-01"],
            Fallbacks = new Default(),
            System = new List<BetaTextBlockParam>
            {
                new() { Text = prompts.Get("system"), CacheControl = new BetaCacheControlEphemeral() },
            },
            OutputConfig = new BetaOutputConfig { Effort = Effort.Medium },
            Messages = request,
        }, ct);

        usage.Record(model, response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.CacheReadInputTokens ?? 0);

        string reply;
        if (response.StopReason == "refusal")
        {
            logger.LogWarning("Chat request refused: {Category}", response.StopDetails?.Category);
            reply = "I can't help with that one.";
        }
        else
        {
            reply = string.Join("\n", response.Content.Select(b => b.Value).OfType<BetaTextBlock>().Select(t => t.Text)).Trim();
            if (reply.Length == 0) reply = "(no text in reply)";
        }

        lock (history)
        {
            history.Add(new BetaAssistantMessageParam { Content = reply });
            var excess = history.Count - _options.MaxHistoryTurns * 2;
            if (excess > 0) history.RemoveRange(0, excess);
        }

        return reply;
    }

    public void Reset(long conversationId) => _histories.TryRemove(conversationId, out _);
}
