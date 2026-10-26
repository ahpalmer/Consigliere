using System.Collections.Concurrent;
using Microsoft.Extensions.Options;

namespace Consigliere.Llm;

public sealed class AgentOptions
{
    public const string Section = "Agent";

    /// <summary>Substituted for {{owner}} in prompts. Set it in user-secrets / Key Vault, not appsettings.json.</summary>
    public string OwnerName { get; set; } = "the owner";
}

/// <summary>
/// Prompts ship with the code as embedded resources (src/Consigliere.Llm/Prompts/*.md),
/// so they're versioned and reviewed like code. Personal details come in through placeholders.
/// </summary>
public sealed class PromptLibrary(IOptions<AgentOptions> options)
{
    private readonly ConcurrentDictionary<string, string> _cache = new();

    public string Get(string name) =>
        _cache.GetOrAdd(name, Load).Replace("{{owner}}", options.Value.OwnerName);

    private static string Load(string name)
    {
        var resource = $"Consigliere.Llm.Prompts.{name}.md";
        using var stream = typeof(PromptLibrary).Assembly.GetManifestResourceStream(resource)
            ?? throw new InvalidOperationException($"Prompt '{name}' not found (expected embedded resource {resource}).");
        using var reader = new StreamReader(stream);
        return reader.ReadToEnd().Trim();
    }
}
