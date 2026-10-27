namespace Consigliere.Llm;

public sealed class LlmOptions
{
    public const string Section = "Llm";

    /// <summary>Model per task, so each route can be tuned independently.</summary>
    public Dictionary<string, string> Models { get; set; } = new()
    {
        [LlmTask.Chat] = "claude-opus-5-5",
        [LlmTask.Draft] = "claude-opus-5-5",
        [LlmTask.Score] = "claude-haiku-5-5",
        [LlmTask.Extract] = "claude-haiku-5-5",
    };

    /// <summary>User/assistant turn pairs kept per conversation.</summary>
    public int MaxHistoryTurns { get; set; } = 20;

    public string ModelFor(string task) =>
        Models.TryGetValue(task, out var model) ? model : throw new InvalidOperationException($"No model configured for task '{task}'.");
}

public static class LlmTask
{
    public const string Chat = "chat";
    public const string Draft = "draft";
    public const string Score = "score";
    public const string Extract = "extract";
}
