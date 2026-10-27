using System.Globalization;
using System.Text;
using System.Text.Json;
using Anthropic;
using Anthropic.Models.Messages;
using Consigliere.Core.DataPack;
using Consigliere.Llm;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Text;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Radar.Scoring;

public sealed record ScoreResult(int Fit, string Category, string Summary, IReadOnlyList<string> Reasons, IReadOnlyList<string> Gaps);

/// <summary>What the scorer needs to know about an item.</summary>
public sealed record ScoreInput(string Company, string Title, string Locations, string? Tier, decimal? CompMin, decimal? CompMax,
    DateTimeOffset? PostedAt, string Description);

/// <summary>The stable, cacheable part of every scoring request: rubric + owner profile + categories.</summary>
public sealed record ScoringContext(string Model, string Rubric, string Profile, IReadOnlyList<string> Categories)
{
    /// <summary>Changes whenever the rubric, profile, categories, or model change, which makes existing scores stale.</summary>
    public string Key { get; } = TextUtil.Hash(Model, Rubric, Profile, string.Join("|", Categories));
}

public interface IItemScorer
{
    ScoringContext BuildContext();

    /// <returns>Null if the model declined to score the item.</returns>
    Task<ScoreResult?> ScoreAsync(ScoreInput item, ScoringContext context, CancellationToken ct);
}

/// <summary>Scores one item against the owner's profile with a cheap model and structured output.</summary>
public sealed class ItemScorer(
    AnthropicClient client,
    PromptLibrary prompts,
    DataPack dataPack,
    RadarConfigLoader config,
    UsageTracker usage,
    IOptions<LlmOptions> llmOptions,
    TimeProvider time,
    ILogger<ItemScorer> logger) : IItemScorer
{
    public const string OtherCategory = "other";

    // Long descriptions are mostly boilerplate (benefits, EEO); the first ~12K chars carry the signal.
    private const int MaxDescriptionChars = 12_000;

    public ScoringContext BuildContext()
    {
        var prefs = config.LoadPreferences();
        var categories = prefs.Categories.Select(c => c.Name).Where(n => n.Length > 0).Append(OtherCategory).Distinct().ToList();

        var profile = new StringBuilder();
        var profileDir = dataPack.Resolve("profile");
        if (Directory.Exists(profileDir))
        {
            foreach (var file in Directory.GetFiles(profileDir).Order(StringComparer.Ordinal))
                profile.AppendLine($"## {Path.GetFileName(file)}").AppendLine(File.ReadAllText(file).Trim()).AppendLine();
        }
        profile.AppendLine("## preferences").AppendLine(dataPack.ReadTextOrDefault(RadarConfigLoader.PreferencesPath)?.Trim() ?? "(none)").AppendLine();
        profile.AppendLine("## categories, highest priority first");
        foreach (var c in prefs.Categories) profile.AppendLine($"- {c.Name}: {c.Description}");
        profile.AppendLine($"- {OtherCategory}: anything that fits none of the above");

        return new ScoringContext(llmOptions.Value.ModelFor(LlmTask.Score), prompts.Get("score", typeof(ItemScorer).Assembly), profile.ToString().Trim(), categories);
    }

    public async Task<ScoreResult?> ScoreAsync(ScoreInput item, ScoringContext context, CancellationToken ct)
    {
        var response = await client.Messages.Create(new MessageCreateParams
        {
            Model = context.Model,
            MaxTokens = 8000,
            System = new List<TextBlockParam>
            {
                new() { Text = context.Rubric },
                // Identical across every item in a run, so it's cached after the first call.
                new() { Text = context.Profile, CacheControl = new CacheControlEphemeral() },
            },
            OutputConfig = new OutputConfig
            {
                Effort = Effort.Low,
                Format = new JsonOutputFormat { Schema = Schema(context.Categories) },
            },
            Messages = [new UserMessageParam { Content = Describe(item) }],
        }, ct);

        usage.Record(context.Model, response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.CacheReadInputTokens ?? 0);

        if (response.StopReason == "refusal")
        {
            logger.LogWarning("Scoring refused for {Company} / {Title}: {Category}", item.Company, item.Title, response.StopDetails?.Category);
            return null;
        }

        var json = string.Concat(response.Content.Select(b => b.Value).OfType<TextBlock>().Select(t => t.Text));
        var raw = JsonSerializer.Deserialize<RawScore>(json, JsonOptions)
                  ?? throw new InvalidOperationException("Scorer returned empty JSON.");
        return new ScoreResult(
            Math.Clamp(raw.Fit, 0, 100),
            context.Categories.Contains(raw.Category) ? raw.Category : OtherCategory,
            raw.Summary.Trim(),
            raw.Reasons.Take(3).ToList(),
            raw.Gaps.Take(3).ToList());
    }

    private string Describe(ScoreInput item)
    {
        var today = time.GetUtcNow();
        var age = item.PostedAt is { } posted
            ? $"{posted:yyyy-MM-dd} ({Math.Max(0, (int)(today - posted).TotalDays)} days ago)"
            : "unknown";
        var pay = item.CompMax is null && item.CompMin is null
            ? "not listed"
            : $"{item.CompMin?.ToString("N0", CultureInfo.InvariantCulture) ?? "?"} - {item.CompMax?.ToString("N0", CultureInfo.InvariantCulture) ?? "?"} (listed range, usually base pay)";
        var description = item.Description.Length > MaxDescriptionChars ? item.Description[..MaxDescriptionChars] + "\n[truncated]" : item.Description;

        return $"""
            Today: {today:yyyy-MM-dd}
            Company: {item.Company}
            Title: {item.Title}
            Locations: {item.Locations}
            Location tier (from the rule filter): {item.Tier ?? "none"}
            Pay: {pay}
            Posted: {age}

            Description:
            {description}
            """;
    }

    private static Dictionary<string, JsonElement> Schema(IReadOnlyList<string> categories) => new()
    {
        ["type"] = JsonSerializer.SerializeToElement("object"),
        ["properties"] = JsonSerializer.SerializeToElement(new
        {
            fit = new { type = "integer", description = "0-100" },
            category = new { type = "string", @enum = categories },
            summary = new { type = "string" },
            reasons = new { type = "array", items = new { type = "string" } },
            gaps = new { type = "array", items = new { type = "string" } },
        }),
        ["required"] = JsonSerializer.SerializeToElement(new[] { "fit", "category", "summary", "reasons", "gaps" }),
        ["additionalProperties"] = JsonSerializer.SerializeToElement(false),
    };

    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);

    private sealed record RawScore(int Fit, string Category, string Summary, List<string> Reasons, List<string> Gaps);
}
