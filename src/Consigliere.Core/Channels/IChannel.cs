namespace Consigliere.Core.Channels;

/// <summary>An outbound path to the owner (Telegram today; WhatsApp, email, etc. later).</summary>
public interface IChannel
{
    string Name { get; }

    Task NotifyOwnerAsync(string text, CancellationToken ct = default);
}
