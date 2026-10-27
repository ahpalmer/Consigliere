namespace Consigliere.Skills.Mail;

public sealed class MailOptions
{
    public const string Section = "Mail";

    // OAuth for the dedicated, read-only mailbox. All three belong in user-secrets / Key Vault.
    public string ClientId { get; set; } = "";
    public string ClientSecret { get; set; } = "";
    /// <summary>Written by `Consigliere.Host auth gmail`.</summary>
    public string RefreshToken { get; set; } = "";

    /// <summary>Gmail search query for candidate messages. Already-processed messages are skipped regardless.</summary>
    public string Query { get; set; } = "newer_than:14d";

    public int MaxMessagesPerScan { get; set; } = 50;

    /// <summary>Only mail from these domains is read. Anything else in the mailbox is ignored and never sent to the LLM.</summary>
    public List<string> AllowedSenderDomains { get; set; } =
        ["linkedin.com", "indeed.com", "glassdoor.com", "wellfound.com", "ziprecruiter.com", "dice.com", "builtin.com"];

    public bool IsConfigured =>
        !string.IsNullOrWhiteSpace(ClientId) && !string.IsNullOrWhiteSpace(ClientSecret) && !string.IsNullOrWhiteSpace(RefreshToken);
}
