using System.Text.Json;
using System.Text.Json.Serialization;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Text;

namespace Consigliere.Skills.Radar.Sources;

/// <summary>Ashby public job board API: api.ashbyhq.com/posting-api/job-board/{board}.</summary>
public sealed class AshbySource(HttpClient http) : IItemSource
{
    public string Kind => "ashby";

    public async Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct)
    {
        await using var stream = await http.GetStreamAsync($"https://api.ashbyhq.com/posting-api/job-board/{Uri.EscapeDataString(source.Board)}?includeCompensation=true", ct);
        return Parse(await JsonSerializer.DeserializeAsync<Response>(stream, cancellationToken: ct), source);
    }

    public static IReadOnlyList<FetchedItem> Parse(string json, WatchedSource source) => Parse(JsonSerializer.Deserialize<Response>(json), source);

    private static IReadOnlyList<FetchedItem> Parse(Response? response, WatchedSource source) =>
        (response?.Jobs ?? []).Where(j => j.IsListed != false).Select(j =>
        {
            var description = j.DescriptionPlain ?? TextUtil.HtmlToText(j.DescriptionHtml ?? "");
            var salary = j.Compensation?.SummaryComponents?.FirstOrDefault(c =>
                c.CompensationType == "Salary" && c is { MinValue: > 0, MaxValue: > 0 } && c.Interval?.Contains("YEAR") != false);
            (decimal Min, decimal Max)? pay = salary is not null ? (salary.MinValue!.Value, salary.MaxValue!.Value) : TextUtil.ExtractPayRange(description);

            return new FetchedItem
            {
                ExternalId = j.Id,
                Company = source.Name.Trim(),
                Title = j.Title.Trim(),
                Locations = SourceHelpers.Locations([j.Location, .. (j.SecondaryLocations ?? []).Select(l => l.Location)], j.IsRemote == true),
                Url = j.JobUrl,
                Description = description,
                CompMin = pay?.Min,
                CompMax = pay?.Max,
                CompCurrency = pay is null ? null : salary?.CurrencyCode ?? "USD",
                PostedAt = j.PublishedAt,
            };
        }).ToList();

    private sealed record Response([property: JsonPropertyName("jobs")] List<Job>? Jobs);

    private sealed record Job(
        [property: JsonPropertyName("id")] string Id,
        [property: JsonPropertyName("title")] string Title,
        [property: JsonPropertyName("location")] string? Location,
        [property: JsonPropertyName("secondaryLocations")] List<SecondaryLocation>? SecondaryLocations,
        [property: JsonPropertyName("isRemote")] bool? IsRemote,
        [property: JsonPropertyName("isListed")] bool? IsListed,
        [property: JsonPropertyName("jobUrl")] string JobUrl,
        [property: JsonPropertyName("descriptionPlain")] string? DescriptionPlain,
        [property: JsonPropertyName("descriptionHtml")] string? DescriptionHtml,
        [property: JsonPropertyName("publishedAt")] DateTimeOffset? PublishedAt,
        [property: JsonPropertyName("compensation")] Compensation? Compensation);

    private sealed record SecondaryLocation([property: JsonPropertyName("location")] string? Location);

    private sealed record Compensation([property: JsonPropertyName("summaryComponents")] List<Component>? SummaryComponents);

    private sealed record Component(
        [property: JsonPropertyName("compensationType")] string? CompensationType,
        [property: JsonPropertyName("interval")] string? Interval,
        [property: JsonPropertyName("currencyCode")] string? CurrencyCode,
        [property: JsonPropertyName("minValue")] decimal? MinValue,
        [property: JsonPropertyName("maxValue")] decimal? MaxValue);
}
