using System.Text.Json;
using Anthropic;
using Anthropic.Models.Messages;
using Consigliere.Llm;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Options;

namespace Consigliere.Skills.Mail;

/// <summary>A listing as the email states it. Not trusted until <see cref="EmailSource"/> verifies the URL.</summary>
public sealed record ExtractedListing(string Title, string Company, string Location, string Url, string Salary, string Posted, string Snippet);

public interface IEmailExtractor
{
    Task<IReadOnlyList<ExtractedListing>> ExtractAsync(string emailText, CancellationToken ct);
}

/// <summary>
/// Uses a cheap model with structured output instead of per-sender HTML parsers, which break whenever an email template changes.
/// </summary>
public sealed class LlmEmailExtractor(
    AnthropicClient client,
    PromptLibrary prompts,
    UsageTracker usage,
    IOptions<LlmOptions> llmOptions,
    ILogger<LlmEmailExtractor> logger) : IEmailExtractor
{
    // Alert emails are mostly short; this bounds cost on a pathological one.
    private const int MaxEmailChars = 40_000;

    public async Task<IReadOnlyList<ExtractedListing>> ExtractAsync(string emailText, CancellationToken ct)
    {
        var model = llmOptions.Value.ModelFor(LlmTask.Extract);
        var response = await client.Messages.Create(new MessageCreateParams
        {
            Model = model,
            MaxTokens = 16000,
            System = prompts.Get("extract", typeof(LlmEmailExtractor).Assembly),
            OutputConfig = new OutputConfig
            {
                Effort = Effort.Low,
                Format = new JsonOutputFormat { Schema = Schema },
            },
            Messages = [new UserMessageParam { Content = emailText.Length > MaxEmailChars ? emailText[..MaxEmailChars] : emailText }],
        }, ct);

        usage.Record(model, response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.CacheReadInputTokens ?? 0);
        if (response.StopReason == "refusal")
        {
            logger.LogWarning("Extraction refused: {Category}", response.StopDetails?.Category);
            return [];
        }

        var json = string.Concat(response.Content.Select(b => b.Value).OfType<TextBlock>().Select(t => t.Text));
        return JsonSerializer.Deserialize<Result>(json, JsonOptions)?.Items ?? [];
    }

    private static readonly Dictionary<string, JsonElement> Schema = new()
    {
        ["type"] = JsonSerializer.SerializeToElement("object"),
        ["properties"] = JsonSerializer.SerializeToElement(new
        {
            items = new
            {
                type = "array",
                items = new
                {
                    type = "object",
                    properties = new
                    {
                        title = new { type = "string" },
                        company = new { type = "string" },
                        location = new { type = "string" },
                        url = new { type = "string" },
                        salary = new { type = "string" },
                        posted = new { type = "string" },
                        snippet = new { type = "string" },
                    },
                    required = new[] { "title", "company", "location", "url", "salary", "posted", "snippet" },
                    additionalProperties = false,
                },
            },
        }),
        ["required"] = JsonSerializer.SerializeToElement(new[] { "items" }),
        ["additionalProperties"] = JsonSerializer.SerializeToElement(false),
    };

    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);

    private sealed record Result(List<ExtractedListing> Items);
}
