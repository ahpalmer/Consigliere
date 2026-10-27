using Consigliere.Core.Channels;
using Microsoft.Extensions.Options;
using Telegram.Bot;
using Telegram.Bot.Types;
using Telegram.Bot.Types.Enums;

namespace Consigliere.Host.Telegram;

/// <summary>
/// Inbound half of the Telegram channel. Long-polls (outbound-only, so the host needs no inbound ports)
/// and routes allowlisted messages to the router and button taps to callback handlers.
/// </summary>
public sealed class TelegramChannel(
    ITelegramBotClient bot,
    TelegramSender sender,
    MessageRouter router,
    IEnumerable<ICallbackHandler> callbackHandlers,
    IOptions<TelegramOptions> options,
    ILogger<TelegramChannel> logger) : BackgroundService
{
    private readonly TelegramOptions _options = options.Value;
    private readonly ICallbackHandler[] _callbackHandlers = [.. callbackHandlers];

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
                updates = await bot.GetUpdates(offset, timeout: 50, allowedUpdates: [UpdateType.Message, UpdateType.CallbackQuery], cancellationToken: stoppingToken);
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
                if (update.Message is { } message) await HandleMessageAsync(message, stoppingToken);
                else if (update.CallbackQuery is { } callback) await HandleCallbackAsync(callback, stoppingToken);
            }
        }
    }

    private bool IsAllowed(long chatId, string? username)
    {
        if (_options.AllowedChatIds.Contains(chatId)) return true;
        logger.LogWarning("Ignoring update from non-allowlisted chat {ChatId} (@{Username})", chatId, username);
        return false;
    }

    private async Task HandleMessageAsync(Message message, CancellationToken ct)
    {
        if (message.Text is not { } text) return;
        var chatId = message.Chat.Id;
        if (!IsAllowed(chatId, message.From?.Username)) return;

        try
        {
            await bot.SendChatAction(chatId, ChatAction.Typing, cancellationToken: ct);
            var reply = await router.HandleAsync(chatId, text, ct);
            await sender.SendAsync(chatId, reply, [], ct);
        }
        catch (Exception ex) when (!ct.IsCancellationRequested)
        {
            logger.LogError(ex, "Failed handling message in chat {ChatId}", chatId);
            await sender.SendAsync(chatId, $"Something broke: {ex.GetType().Name}. Check the logs.", [], ct);
        }
    }

    private async Task HandleCallbackAsync(CallbackQuery callback, CancellationToken ct)
    {
        // Taps arrive from the chat the button was sent to; check the tapping user too.
        if (!IsAllowed(callback.From.Id, callback.From.Username)) return;

        string toast;
        try
        {
            var handler = _callbackHandlers.FirstOrDefault(h => callback.Data?.StartsWith(h.Prefix, StringComparison.Ordinal) == true);
            toast = handler is null ? "That button no longer does anything." : await handler.HandleAsync(callback.Data!, ct);
        }
        catch (Exception ex) when (!ct.IsCancellationRequested)
        {
            logger.LogError(ex, "Failed handling callback {Data}", callback.Data);
            toast = $"Something broke: {ex.GetType().Name}";
        }
        await bot.AnswerCallbackQuery(callback.Id, toast, cancellationToken: ct);
    }
}
