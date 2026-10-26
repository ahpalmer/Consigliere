using Anthropic;
using Consigliere.Core.Agent;
using Consigliere.Core.DataPack;
using Consigliere.Host;
using Consigliere.Llm;
using Microsoft.Extensions.Logging.Abstractions;
using Microsoft.Extensions.Options;

namespace Consigliere.Tests;

public sealed class MessageRouterTests
{
    private readonly AgentState _state = new();
    private readonly MessageRouter _router;

    public MessageRouterTests()
    {
        // Commands never reach the LLM, so a client with a dummy key is safe here.
        var pack = new DataPack(Options.Create(new DataPackOptions { Path = Path.GetTempPath() }));
        var usage = new UsageTracker();
        var chat = new ChatService(new AnthropicClient { ApiKey = "test" }, pack, usage,
            Options.Create(new LlmOptions()), NullLogger<ChatService>.Instance);
        _router = new MessageRouter(_state, chat, usage, []);
    }

    [Fact]
    public async Task Pause_and_resume_toggle_kill_switch()
    {
        await _router.HandleAsync(1, "/pause", CancellationToken.None);
        Assert.True(_state.Paused);

        await _router.HandleAsync(1, "/resume", CancellationToken.None);
        Assert.False(_state.Paused);
    }

    [Fact]
    public async Task Free_text_is_blocked_while_paused()
    {
        _state.Pause();
        var reply = await _router.HandleAsync(1, "hello", CancellationToken.None);
        Assert.Contains("paused", reply, StringComparison.OrdinalIgnoreCase);
    }

    [Fact]
    public async Task Status_reports_state()
    {
        var reply = await _router.HandleAsync(1, "/STATUS", CancellationToken.None);
        Assert.Contains("running", reply);
    }
}
