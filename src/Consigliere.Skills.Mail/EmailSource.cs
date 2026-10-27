using System.Text.RegularExpressions;
using Consigliere.Data;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Sources;
using Consigliere.Skills.Radar.Text;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Mail;

/// <summary>
/// Radar source over a dedicated, forward-only mailbox. Each new message from an allowed sender is extracted once;
/// listings whose URL doesn't appear in the email are discarded. Watchlist entry: { name: ..., ats: email, board: gmail }.
/// </summary>
public sealed partial class EmailSource(
    IMailbox mailbox,
    IEmailExtractor extractor,
    IDbContextFactory<ConsigliereDbContext> dbFactory,
    IOptions<MailOptions> options,
    TimeProvider time,
    ILogger<EmailSource> logger) : IItemSource
{
    public string Kind => "email";

    public bool IsAggregator => true;

    public async Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct)
    {
        var o = options.Value;
        var ids = await mailbox.ListMessageIdsAsync(o.Query, o.MaxMessagesPerScan, ct);

        await using var db = await dbFactory.CreateDbContextAsync(ct);
        var processed = (await db.ProcessedMessages
            .Where(m => m.Mailbox == mailbox.Name && ids.Contains(m.Id))
            .Select(m => m.Id)
            .ToListAsync(ct)).ToHashSet();

        var items = new List<FetchedItem>();
        foreach (var id in ids.Where(id => !processed.Contains(id)))
        {
            var message = await mailbox.GetAsync(id, ct);
            var record = new ProcessedMessage
            {
                Id = id,
                Mailbox = mailbox.Name,
                From = message.From,
                Subject = message.Subject,
                ReceivedAt = message.ReceivedAt,
                ProcessedAt = time.GetUtcNow(),
            };

            if (!IsAllowedSender(message.From, o.AllowedSenderDomains))
            {
                record.Note = "sender not allowed";
                logger.LogInformation("Skipping mail from {From}: not an allowed sender", message.From);
            }
            else
            {
                var found = await ExtractAsync(message, ct);
                items.AddRange(found);
                record.ItemCount = found.Count;
            }

            // Recorded even when nothing was found, so the same message is never paid for twice.
            db.ProcessedMessages.Add(record);
            await db.SaveChangesAsync(ct);
        }
        return items;
    }

    private async Task<List<FetchedItem>> ExtractAsync(MailMessage message, CancellationToken ct)
    {
        var text = EmailText.ToText(message.Html, message.Text);
        var urls = EmailText.Urls(text);
        var listings = await extractor.ExtractAsync($"From: {message.From}\nSubject: {message.Subject}\n\n{text}", ct);

        var items = new List<FetchedItem>();
        foreach (var listing in listings)
        {
            var url = EmailText.Canonicalize(listing.Url);
            if (!urls.Contains(url) || string.IsNullOrWhiteSpace(listing.Title) || string.IsNullOrWhiteSpace(listing.Company))
            {
                logger.LogWarning("Dropping extracted listing '{Title}' at {Company}: URL not found in the email", listing.Title, listing.Company);
                continue;
            }

            var pay = TextUtil.ExtractPayRange(listing.Salary);
            items.Add(new FetchedItem
            {
                // LinkedIn ids are stable across alert emails, so the same job from two alerts dedupes.
                ExternalId = EmailText.LinkedInJobId(url) is { } liId ? $"linkedin:{liId}" : TextUtil.Hash(url),
                Company = listing.Company.Trim(),
                Title = listing.Title.Trim(),
                Locations = string.IsNullOrWhiteSpace(listing.Location) ? [] : [listing.Location.Trim()],
                Url = url,
                Description = string.Join("\n", new[]
                {
                    listing.Snippet,
                    listing.Salary.Length > 0 ? $"Pay: {listing.Salary}" : "",
                    $"(From an alert email: {message.Subject}. Full description not included.)",
                }.Where(s => s.Length > 0)),
                CompMin = pay?.Min,
                CompMax = pay?.Max,
                CompCurrency = pay is null ? null : "USD",
                PostedAt = PostedAt(listing.Posted, message.ReceivedAt),
            });
        }
        return items;
    }

    private static bool IsAllowedSender(string from, IEnumerable<string> domains)
    {
        var address = SenderAddress().Match(from) is { Success: true } m ? m.Groups["addr"].Value : from;
        var domain = address.Split('@').LastOrDefault()?.Trim().ToLowerInvariant() ?? "";
        return domains.Any(d => domain == d.ToLowerInvariant() || domain.EndsWith("." + d.ToLowerInvariant(), StringComparison.Ordinal));
    }

    /// <summary>
    /// "3 days ago" relative to when the alert arrived. Alerts list newly posted jobs, so with no stated age the
    /// email's own date is the best available posting date (it's the source's date, not our clock).
    /// </summary>
    internal static DateTimeOffset? PostedAt(string posted, DateTimeOffset? received)
    {
        if (received is not { } r) return null;
        var m = RelativeAge().Match(posted);
        if (!m.Success) return r;
        var n = int.Parse(m.Groups["n"].Value);
        return m.Groups["unit"].Value.ToLowerInvariant() switch
        {
            "minute" => r.AddMinutes(-n),
            "hour" => r.AddHours(-n),
            "day" => r.AddDays(-n),
            "week" => r.AddDays(-7 * n),
            "month" => r.AddDays(-30 * n),
            _ => r,
        };
    }

    [GeneratedRegex("<(?<addr>[^>]+)>")]
    private static partial Regex SenderAddress();

    [GeneratedRegex(@"(?<n>\d+)\s*(?<unit>minute|hour|day|week|month)s?\s+ago", RegexOptions.IgnoreCase)]
    private static partial Regex RelativeAge();
}
