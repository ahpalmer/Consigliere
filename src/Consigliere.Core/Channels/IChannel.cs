namespace Consigliere.Core.Channels;

/// <summary>A tappable action under a message. <paramref name="Data"/> is routed to the <see cref="ICallbackHandler"/> whose prefix it starts with.</summary>
public sealed record ActionButton(string Label, string Data);

/// <summary>An outbound path to the owner (Telegram today; WhatsApp, email, etc. later).</summary>
public interface IChannel
{
    string Name { get; }

    Task NotifyOwnerAsync(string text, CancellationToken ct = default);

    /// <summary>Sends a message with one row of buttons.</summary>
    Task NotifyOwnerAsync(string text, IReadOnlyList<ActionButton> buttons, CancellationToken ct = default);
}

/// <summary>Handles button taps whose data starts with <see cref="Prefix"/>.</summary>
public interface ICallbackHandler
{
    string Prefix { get; }

    /// <returns>Short confirmation shown to the owner (a toast in Telegram).</returns>
    Task<string> HandleAsync(string data, CancellationToken ct);
}
