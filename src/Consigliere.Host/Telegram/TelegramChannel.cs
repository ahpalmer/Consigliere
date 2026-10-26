using Consigliere.Core.Channels;
using Microsoft.Extensions.Options;
using Telegram.Bot;
using Telegram.Bot.Types;
using Telegram.Bot.Types.Enums;

namespace Consigliere.Host.Telegram;

/// <summary>
/// Long-polls Telegram (outbound-only, so the host needs no inbound ports) and routes allowlisted messages.
/// </summary>
public sealed class TelegramChannel(
    ITelegramBotClient bot,
    MessageRouter router,
    IOptions<TelegramOptions> options,
    ILogger<TelegramChannel> logger) : BackgroundService, IChannel
{
    // Telegram's hard limit is 4096 characters per message.
    private const int MaxMessageLength = 4000;

    private readonly TelegramOptions _options = options.Value;

    public string Name => "telegram";

    public async Task NotifyOwnerAsync(string text, CancellationToken ct = default)
    {
        var owner = _options.OwnerChatId ?? _options.AllowedChatIds.FirstOrDefault();
        if (owner == 0)
        {
            logger.LogWarning("No owner chat configured; dropping notification");
            return;
        }
        await SendAsync(owner, text, ct);
    }

    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        var me = await bot.GetMe(stoppingToken);
        logger.LogInformation("Telegram connected as @{Username}; {Count} allowed chat(s)", me.Username, _options.AllowedChatIds.Length);

        int? offset = null;
        while (!stoppingToken.IsCancellationRequested)
        {
            Update[] updates;
            try
            {
                updates = await bot.GetUpdates(offset, timeout: 50, allowedUpdates: [UpdateType.Message], cancellationToken: stoppingToken);
            }
            catch (OperationCanceledException) when (stoppingToken.IsCancellationRequested)
            {
                break;
            }
            catch (Exception ex)
            {
                logger.LogWarning(ex, "Telegram polling failed; retrying");
                await Task.Delay(TimeSpan.FromSeconds(5), stoppingToken);
                continue;
            }

            foreach (var update in updates)
            {
                offset = update.Id + 1;
                await HandleAsync(update, stoppingToken);
            }
        }
    }

    private async Task HandleAsync(Update update, CancellationToken ct)
    {
        if (update.Message is not { Text: { } text } message) return;

        var chatId = message.Chat.Id;
        if (!_options.AllowedChatIds.Contains(chatId))
        {
            logger.LogWarning("Ignoring message from non-allowlisted chat {ChatId} (@{Username})", chatId, message.From?.Username);
            return;
        }

        try
        {
            await bot.SendChatAction(chatId, ChatAction.Typing, cancellationToken: ct);
            var reply = await router.HandleAsync(chatId, text, ct);
            await SendAsync(chatId, reply, ct);
        }
        catch (Exception ex) when (!ct.IsCancellationRequested)
        {
            logger.LogError(ex, "Failed handling message in chat {ChatId}", chatId);
            await SendAsync(chatId, $"Something broke: {ex.GetType().Name}. Check the logs.", ct);
        }
    }

    private async Task SendAsync(long chatId, string text, CancellationToken ct)
    {
        for (var i = 0; i < text.Length; i += MaxMessageLength)
            await bot.SendMessage(chatId, text.Substring(i, Math.Min(MaxMessageLength, text.Length - i)),
                linkPreviewOptions: new LinkPreviewOptions { IsDisabled = true }, cancellationToken: ct);
    }
}
