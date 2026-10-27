using System.Text.Json;
using System.Text.Json.Serialization;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Text;

namespace Consigliere.Skills.Radar.Sources;

/// <summary>Greenhouse public job board API: boards-api.greenhouse.io/v1/boards/{board}/jobs.</summary>
public sealed class GreenhouseSource(HttpClient http) : IItemSource
{
    public string Kind => "greenhouse";

    public async Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct)
    {
        await using var stream = await http.GetStreamAsync($"https://boards-api.greenhouse.io/v1/boards/{Uri.EscapeDataString(source.Board)}/jobs?content=true", ct);
        return Parse(await JsonSerializer.DeserializeAsync<Response>(stream, cancellationToken: ct), source);
    }

    public static IReadOnlyList<FetchedItem> Parse(string json, WatchedSource source) => Parse(JsonSerializer.Deserialize<Response>(json), source);

    private static IReadOnlyList<FetchedItem> Parse(Response? response, WatchedSource source) =>
        (response?.Jobs ?? []).Select(j =>
        {
            var description = TextUtil.HtmlToText(j.Content ?? "");
            var pay = TextUtil.ExtractPayRange(description);
            var remote = j.Metadata?.Any(m => m.Name == "Location Type" && m.Value is { ValueKind: JsonValueKind.String } v &&
                                              v.GetString()!.Contains("remote", StringComparison.OrdinalIgnoreCase)) == true;
            return new FetchedItem
            {
                ExternalId = j.Id.ToString(),
                Company = (string.IsNullOrWhiteSpace(j.CompanyName) ? source.Name : j.CompanyName).Trim(),
                Title = j.Title.Trim(),
                Locations = SourceHelpers.Locations((j.Location?.Name ?? "").Split('|', ';'), remote),
                Url = j.AbsoluteUrl,
                Description = description,
                CompMin = pay?.Min,
                CompMax = pay?.Max,
                CompCurrency = pay is null ? null : "USD",
                PostedAt = j.FirstPublished, // not updated_at: an edit date is not a posting date
            };
        }).ToList();

    private sealed record Response([property: JsonPropertyName("jobs")] List<Job>? Jobs);

    private sealed record Job(
        [property: JsonPropertyName("id")] long Id,
        [property: JsonPropertyName("title")] string Title,
        [property: JsonPropertyName("absolute_url")] string AbsoluteUrl,
        [property: JsonPropertyName("company_name")] string? CompanyName,
        [property: JsonPropertyName("location")] JobLocation? Location,
        [property: JsonPropertyName("content")] string? Content,
        [property: JsonPropertyName("metadata")] List<Metadata>? Metadata,
        [property: JsonPropertyName("first_published")] DateTimeOffset? FirstPublished);

    private sealed record JobLocation([property: JsonPropertyName("name")] string? Name);

    private sealed record Metadata(
        [property: JsonPropertyName("name")] string? Name,
        [property: JsonPropertyName("value")] JsonElement? Value);
}
