using System.Text;
using Google.Apis.Auth.OAuth2;
using Google.Apis.Auth.OAuth2.Flows;
using Google.Apis.Auth.OAuth2.Responses;
using Google.Apis.Gmail.v1;
using Google.Apis.Gmail.v1.Data;
using Google.Apis.Services;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Mail;

public sealed record MailMessage(string Id, string From, string Subject, DateTimeOffset? ReceivedAt, string? Html, string? Text);

public interface IMailbox
{
    string Name { get; }

    Task<IReadOnlyList<string>> ListMessageIdsAsync(string query, int max, CancellationToken ct);

    Task<MailMessage> GetAsync(string id, CancellationToken ct);
}

/// <summary>Read-only Gmail access (gmail.readonly scope) to the dedicated forwarding mailbox.</summary>
public sealed class GmailMailbox(IOptions<MailOptions> options) : IMailbox, IDisposable
{
    public static readonly string[] Scopes = [GmailService.Scope.GmailReadonly];

    private readonly Lazy<GmailService> _service = new(() => Create(options.Value));

    public string Name => "gmail";

    public async Task<IReadOnlyList<string>> ListMessageIdsAsync(string query, int max, CancellationToken ct)
    {
        var ids = new List<string>();
        string? page = null;
        do
        {
            var request = _service.Value.Users.Messages.List("me");
            request.Q = query;
            request.MaxResults = Math.Min(100, max - ids.Count);
            request.PageToken = page;
            var response = await request.ExecuteAsync(ct);
            ids.AddRange((response.Messages ?? []).Select(m => m.Id));
            page = response.NextPageToken;
        } while (page is not null && ids.Count < max);
        return ids;
    }

    public async Task<MailMessage> GetAsync(string id, CancellationToken ct)
    {
        var request = _service.Value.Users.Messages.Get("me", id);
        request.Format = UsersResource.MessagesResource.GetRequest.FormatEnum.Full;
        var message = await request.ExecuteAsync(ct);

        string Header(string name) => message.Payload?.Headers?.FirstOrDefault(h => h.Name.Equals(name, StringComparison.OrdinalIgnoreCase))?.Value ?? "";
        DateTimeOffset? received = message.InternalDate is { } ms ? DateTimeOffset.FromUnixTimeMilliseconds(ms) : null;
        return new MailMessage(id, Header("From"), Header("Subject"), received, FindBody(message.Payload, "text/html"), FindBody(message.Payload, "text/plain"));
    }

    public void Dispose()
    {
        if (_service.IsValueCreated) _service.Value.Dispose();
    }

    private static string? FindBody(MessagePart? part, string mimeType)
    {
        if (part is null) return null;
        if (part.MimeType == mimeType && part.Body?.Data is { } data) return DecodeBase64Url(data);
        return part.Parts?.Select(p => FindBody(p, mimeType)).FirstOrDefault(b => b is not null);
    }

    private static string DecodeBase64Url(string data)
    {
        var base64 = data.Replace('-', '+').Replace('_', '/');
        base64 = base64.PadRight(base64.Length + (4 - base64.Length % 4) % 4, '=');
        return Encoding.UTF8.GetString(Convert.FromBase64String(base64));
    }

    private static GmailService Create(MailOptions o)
    {
        if (!o.IsConfigured)
            throw new InvalidOperationException("Mail is not configured: set Mail:ClientId and Mail:ClientSecret, then run `auth gmail`.");
        var flow = new GoogleAuthorizationCodeFlow(new GoogleAuthorizationCodeFlow.Initializer
        {
            ClientSecrets = new ClientSecrets { ClientId = o.ClientId, ClientSecret = o.ClientSecret },
            Scopes = Scopes,
        });
        var credential = new UserCredential(flow, "consigliere", new TokenResponse { RefreshToken = o.RefreshToken });
        return new GmailService(new BaseClientService.Initializer { HttpClientInitializer = credential, ApplicationName = "Consigliere" });
    }
}
