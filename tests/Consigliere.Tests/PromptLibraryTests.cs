using Consigliere.Llm;
using Microsoft.Extensions.Options;

namespace Consigliere.Tests;

public sealed class PromptLibraryTests
{
    [Fact]
    public void System_prompt_is_embedded_and_owner_is_substituted()
    {
        var prompt = new PromptLibrary(Options.Create(new AgentOptions { OwnerName = "Vito" })).Get("system");

        Assert.Contains("Vito", prompt);
        Assert.DoesNotContain("{{owner}}", prompt);
    }

    [Fact]
    public void Missing_prompt_fails_loudly() =>
        Assert.Throws<InvalidOperationException>(() => new PromptLibrary(Options.Create(new AgentOptions())).Get("nope"));
}
