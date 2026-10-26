using System.Text.Json;
using System.Text.Json.Serialization;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Text;

namespace Consigliere.Skills.Radar.Sources;

/// <summary>Lever postings API: api.lever.co/v0/postings/{board}?mode=json.</summary>
public sealed class LeverSource(HttpClient http) : IItemSource
{
    public string Kind => "lever";

    public async Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct)
    {
        await using var stream = await http.GetStreamAsync($"https://api.lever.co/v0/postings/{Uri.EscapeDataString(source.Board)}?mode=json", ct);
        return Parse(await JsonSerializer.DeserializeAsync<List<Posting>>(stream, cancellationToken: ct), source);
    }

    public static IReadOnlyList<FetchedItem> Parse(string json, WatchedSource source) => Parse(JsonSerializer.Deserialize<List<Posting>>(json), source);

    private static IReadOnlyList<FetchedItem> Parse(List<Posting>? postings, WatchedSource source) =>
        (postings ?? []).Select(p =>
        {
            var description = string.Join("\n\n", new[]
            {
                p.DescriptionPlain,
                string.Join("\n\n", (p.Lists ?? []).Select(l => $"{l.Text}\n{TextUtil.HtmlToText(l.Content ?? "")}")),
                p.AdditionalPlain,
            }.Where(s => !string.IsNullOrWhiteSpace(s))).Trim();

            // Lever's salaryRange is optional; fall back to scanning the text.
            var pay = p.SalaryRange is { Min: > 0, Max: > 0 } s && s.Interval?.Contains("year", StringComparison.OrdinalIgnoreCase) != false
                ? (s.Min, s.Max)
                : TextUtil.ExtractPayRange(description);

            var locations = p.Categories?.AllLocations is { Count: > 0 } all ? all : [p.Categories?.Location];
            return new FetchedItem
            {
                ExternalId = p.Id,
                Company = source.Name.Trim(),
                Title = p.Text.Trim(),
                Locations = SourceHelpers.Locations(locations, string.Equals(p.WorkplaceType, "remote", StringComparison.OrdinalIgnoreCase)),
                Url = p.HostedUrl,
                Description = description,
                CompMin = pay?.Item1,
                CompMax = pay?.Item2,
                CompCurrency = pay is null ? null : p.SalaryRange?.Currency ?? "USD",
                PostedAt = p.CreatedAt is { } ms ? DateTimeOffset.FromUnixTimeMilliseconds(ms) : null,
            };
        }).ToList();

    private sealed record Posting(
        [property: JsonPropertyName("id")] string Id,
        [property: JsonPropertyName("text")] string Text,
        [property: JsonPropertyName("hostedUrl")] string HostedUrl,
        [property: JsonPropertyName("categories")] Categories? Categories,
        [property: JsonPropertyName("workplaceType")] string? WorkplaceType,
        [property: JsonPropertyName("descriptionPlain")] string? DescriptionPlain,
        [property: JsonPropertyName("additionalPlain")] string? AdditionalPlain,
        [property: JsonPropertyName("lists")] List<ListSection>? Lists,
        [property: JsonPropertyName("salaryRange")] SalaryRange? SalaryRange,
        [property: JsonPropertyName("createdAt")] long? CreatedAt);

    private sealed record Categories(
        [property: JsonPropertyName("location")] string? Location,
        [property: JsonPropertyName("allLocations")] List<string?>? AllLocations);

    private sealed record ListSection(
        [property: JsonPropertyName("text")] string? Text,
        [property: JsonPropertyName("content")] string? Content);

    private sealed record SalaryRange(
        [property: JsonPropertyName("min")] decimal Min,
        [property: JsonPropertyName("max")] decimal Max,
        [property: JsonPropertyName("currency")] string? Currency,
        [property: JsonPropertyName("interval")] string? Interval);
}
