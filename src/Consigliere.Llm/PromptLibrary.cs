using System.Collections.Concurrent;
using System.Reflection;
using Microsoft.Extensions.Options;

namespace Consigliere.Llm;

public sealed class AgentOptions
{
    public const string Section = "Agent";

    /// <summary>Substituted for {{owner}} in prompts. Set it in user-secrets / Key Vault, not appsettings.json.</summary>
    public string OwnerName { get; set; } = "the owner";
}

/// <summary>
/// Prompts ship with the code as embedded resources (each project's Prompts/*.md, named
/// "{AssemblyName}.Prompts.{name}.md"), so they're versioned and reviewed like code. Personal details come in through placeholders.
/// </summary>
public sealed class PromptLibrary(IOptions<AgentOptions> options)
{
    private readonly ConcurrentDictionary<string, string> _cache = new();

    /// <param name="source">Assembly that embeds the prompt; defaults to this one.</param>
    public string Get(string name, Assembly? source = null)
    {
        var assembly = source ?? typeof(PromptLibrary).Assembly;
        var resource = $"{assembly.GetName().Name}.Prompts.{name}.md";
        return _cache.GetOrAdd(resource, r => Load(assembly, r)).Replace("{{owner}}", options.Value.OwnerName);
    }

    private static string Load(Assembly assembly, string resource)
    {
        using var stream = assembly.GetManifestResourceStream(resource)
            ?? throw new InvalidOperationException($"Prompt not found: expected embedded resource {resource}.");
        using var reader = new StreamReader(stream);
        return reader.ReadToEnd().Trim();
    }
}
