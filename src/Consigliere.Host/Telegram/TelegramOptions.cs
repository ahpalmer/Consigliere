namespace Consigliere.Host.Telegram;

public sealed class TelegramOptions
{
    public const string Section = "Telegram";

    public string BotToken { get; set; } = "";

    /// <summary>Chat IDs allowed to talk to the agent. Everyone else is ignored (and logged, so you can find your own ID).</summary>
    public long[] AllowedChatIds { get; set; } = [];

    /// <summary>Where proactive notifications go. Defaults to the first allowed chat.</summary>
    public long? OwnerChatId { get; set; }
}
