using System.Text.Json;
using System.Text.Json.Nodes;
using Google.Apis.Auth.OAuth2;
using Google.Apis.Util.Store;
using Microsoft.Extensions.Configuration.UserSecrets;

namespace Consigliere.Skills.Mail;

/// <summary>
/// One-time consent for the dedicated mailbox: opens a browser, asks for read-only Gmail access, and saves the
/// refresh token to user-secrets. In the cloud, copy that token into Key Vault instead.
/// </summary>
public static class GmailAuth
{
    public static async Task<string> RunAsync(MailOptions options, string userSecretsId, CancellationToken ct)
    {
        if (string.IsNullOrWhiteSpace(options.ClientId) || string.IsNullOrWhiteSpace(options.ClientSecret))
            throw new InvalidOperationException("Set Mail:ClientId and Mail:ClientSecret in user-secrets first.");

        // NullDataStore: don't cache tokens on disk; force a fresh consent so Google returns a refresh token.
        var credential = await GoogleWebAuthorizationBroker.AuthorizeAsync(
            new ClientSecrets { ClientId = options.ClientId, ClientSecret = options.ClientSecret },
            GmailMailbox.Scopes, "consigliere", ct, new NullDataStore());

        var refreshToken = credential.Token.RefreshToken
            ?? throw new InvalidOperationException("Google returned no refresh token. Remove the app's access at myaccount.google.com/permissions and retry.");

        var path = PathHelper.GetSecretsPathFromSecretsId(userSecretsId);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        var secrets = File.Exists(path) ? JsonNode.Parse(await File.ReadAllTextAsync(path, ct))?.AsObject() ?? [] : new JsonObject();
        secrets[$"{MailOptions.Section}:{nameof(MailOptions.RefreshToken)}"] = refreshToken;
        await File.WriteAllTextAsync(path, secrets.ToJsonString(new JsonSerializerOptions { WriteIndented = true }), ct);
        return path;
    }
}
