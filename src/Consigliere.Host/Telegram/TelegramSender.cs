using Consigliere.Core.Channels;
using Microsoft.Extensions.Options;
using Telegram.Bot;
using Telegram.Bot.Types;
using Telegram.Bot.Types.ReplyMarkups;

namespace Consigliere.Host.Telegram;

/// <summary>Outbound half of the Telegram channel. Depends only on the bot client, so anything can notify the owner.</summary>
public sealed class TelegramSender(ITelegramBotClient bot, IOptions<TelegramOptions> options, ILogger<TelegramSender> logger) : IChannel
{
    // Telegram's hard limit is 4096 characters per message.
    private const int MaxMessageLength = 4000;

    private readonly TelegramOptions _options = options.Value;

    public string Name => "telegram";

    public Task NotifyOwnerAsync(string text, CancellationToken ct = default) => NotifyOwnerAsync(text, [], ct);

    public async Task NotifyOwnerAsync(string text, IReadOnlyList<ActionButton> buttons, CancellationToken ct = default)
    {
        var owner = _options.OwnerChatId ?? _options.AllowedChatIds.FirstOrDefault();
        if (owner == 0)
        {
            logger.LogWarning("No owner chat configured; dropping notification");
            return;
        }
        await SendAsync(owner, text, buttons, ct);
    }

    public async Task SendAsync(long chatId, string text, IReadOnlyList<ActionButton> buttons, CancellationToken ct)
    {
        var chunks = Chunk(text).ToList();
        for (var i = 0; i < chunks.Count; i++)
        {
            // Buttons go on the last chunk so they sit under the whole message.
            var markup = i == chunks.Count - 1 && buttons.Count > 0
                ? new InlineKeyboardMarkup(buttons.Select(b => InlineKeyboardButton.WithCallbackData(b.Label, b.Data)))
                : null;
            await bot.SendMessage(chatId, chunks[i], replyMarkup: markup,
                linkPreviewOptions: new LinkPreviewOptions { IsDisabled = true }, cancellationToken: ct);
        }
    }

    private static IEnumerable<string> Chunk(string text)
    {
        for (var i = 0; i < text.Length; i += MaxMessageLength)
            yield return text.Substring(i, Math.Min(MaxMessageLength, text.Length - i));
    }
}
