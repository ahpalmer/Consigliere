using Consigliere.Skills.Radar;
using Consigliere.Skills.Radar.Scoring;
using static Consigliere.Tests.Radar.RadarHarness;

namespace Consigliere.Tests.Radar;

public sealed class ScoringAndDigestTests
{
    private static async Task ScanAndScoreAsync(RadarHarness h)
    {
        await h.Get<RadarScanner>().ScanAsync(CancellationToken.None);
        await h.Get<ScoringRunner>().RunAsync(CancellationToken.None);
    }

    [Fact]
    public async Task Scores_are_stored_and_not_recomputed_until_the_scoring_key_changes()
    {
        await using var h = await CreateAsync();
        h.Source.Items["acme"] = [Item("1", "Engineer")];
        await ScanAndScoreAsync(h);
        await h.Get<ScoringRunner>().RunAsync(CancellationToken.None);

        Assert.Single(h.Scorer.Calls);
        var item = Assert.Single(await h.ItemsAsync());
        Assert.Equal((60, "software"), (item.FitScore, item.Category));

        h.Scorer.Version = "v2"; // e.g. the profile was edited
        await h.Get<ScoringRunner>().RunAsync(CancellationToken.None);
        Assert.Equal(2, h.Scorer.Calls.Count);
    }

    [Fact]
    public async Task Identical_content_reuses_an_existing_score()
    {
        await using var h = await CreateAsync();
        // Same title + description on two boards, but different locations, so not a duplicate.
        h.Source.Items["acme"] = [Item("1", "Engineer", location: "Raleigh, NC")];
        h.Source.Items["globex"] = [Item("1", "Engineer", location: "Austin, TX")];
        await h.Get<RadarScanner>().ScanAsync(CancellationToken.None);

        var summary = await h.Get<ScoringRunner>().RunAsync(CancellationToken.None);

        Assert.Single(h.Scorer.Calls);
        Assert.Equal((1, 1), (summary!.Scored, summary.Cached));
        Assert.All(await h.ItemsAsync(), i => Assert.Equal(60, i.FitScore));
    }

    [Fact]
    public async Task Stale_items_are_never_scored_or_digested()
    {
        await using var h = await CreateAsync();
        var now = h.Time.GetUtcNow();
        h.Source.Items["acme"] = [Item("1", "Fresh Engineer", posted: now.AddDays(-3)), Item("2", "Old Engineer", posted: now.AddDays(-120))];
        await ScanAndScoreAsync(h);

        Assert.Equal(["Fresh Engineer"], h.Scorer.Calls);
        var digest = await h.Get<RadarDigest>().SendableAsync(CancellationToken.None);
        Assert.Contains("Fresh Engineer", digest.Text);
        Assert.DoesNotContain("Old Engineer", digest.Text);
        Assert.Contains("posted 3d ago", digest.Text);
    }

    [Fact]
    public async Task Declined_items_are_not_retried_or_digested()
    {
        await using var h = await CreateAsync();
        h.Scorer.ByTitle["Weird Engineer"] = null;
        h.Source.Items["acme"] = [Item("1", "Weird Engineer")];
        await ScanAndScoreAsync(h);
        await h.Get<ScoringRunner>().RunAsync(CancellationToken.None);

        Assert.Single(h.Scorer.Calls);
        Assert.Null((await h.Get<RadarDigest>().SendableAsync(CancellationToken.None)).Text);
    }

    [Fact]
    public async Task Digest_orders_by_tier_then_category_then_priority_adjusted_fit()
    {
        await using var h = await CreateAsync(configure: o => o.AlertMinScore = 101); // no alerts in this test
        h.Scorer.ByTitle["Robot Engineer"] = (95, "robotics");
        h.Scorer.ByTitle["Good Engineer"] = (70, "software");
        h.Scorer.ByTitle["Great Engineer"] = (80, "software");
        h.Scorer.ByTitle["Remote Engineer"] = (99, "software");
        h.Source.Items["acme"] =
        [
            Item("1", "Robot Engineer"), Item("2", "Good Engineer"), Item("4", "Remote Engineer", location: "Austin, TX"),
        ];
        // Globex is low priority: 80 - 15 = 65, so it ranks below Acme's 70.
        h.Source.Items["globex"] = [Item("3", "Great Engineer", company: "Globex")];
        await ScanAndScoreAsync(h);

        var text = (await h.Get<RadarDigest>().SendableAsync(CancellationToken.None)).Text!;
        var order = new[] { "Good Engineer", "Great Engineer", "Robot Engineer", "Remote Engineer" }.Select(t => text.IndexOf(t, StringComparison.Ordinal)).ToList();
        Assert.All(order, i => Assert.True(i >= 0));
        Assert.Equal(order.Order(), order);
        Assert.Contains("— local · software (2) —", text);
        Assert.Contains("— local · robotics (1) —", text);
    }

    [Fact]
    public async Task Digest_skips_low_scores_and_pages_then_resets()
    {
        await using var h = await CreateAsync(configure: o => { o.DigestMaxItems = 2; o.AlertMinScore = 101; });
        h.Scorer.ByTitle["Meh Engineer"] = (20, "software");
        h.Source.Items["acme"] = [Item("1", "A Engineer"), Item("2", "B Engineer"), Item("3", "C Engineer"), Item("4", "Meh Engineer")];
        await ScanAndScoreAsync(h);
        var digest = h.Get<RadarDigest>();

        var page1 = await digest.SendableAsync(CancellationToken.None);
        var page2 = await digest.SendableAsync(CancellationToken.None);
        var page3 = await digest.SendableAsync(CancellationToken.None);

        Assert.Equal((2, 1), (page1.Shown, page1.Remaining));
        Assert.Equal((1, 0), (page2.Shown, page2.Remaining));
        Assert.Null(page3.Text);
        Assert.DoesNotContain("Meh Engineer", page1.Text + page2.Text);

        await digest.ResetAsync(CancellationToken.None);
        Assert.Equal(page1.Text, (await digest.SendableAsync(CancellationToken.None)).Text);
    }

    [Fact]
    public async Task Strong_local_match_alerts_once_with_feedback_buttons_and_skips_the_digest()
    {
        await using var h = await CreateAsync(configure: o => o.AlertMinScore = 75);
        h.Scorer.ByTitle["Dream Engineer"] = (90, "software");
        h.Scorer.ByTitle["Far Engineer"] = (95, "software");
        h.Source.Items["acme"] = [Item("1", "Dream Engineer"), Item("2", "Far Engineer", location: "Austin, TX")];
        await ScanAndScoreAsync(h);
        await h.Get<ScoringRunner>().RunAsync(CancellationToken.None);

        var (text, buttons) = Assert.Single(h.Channel.Sent); // "anywhere" tier isn't instant
        Assert.Contains("Dream Engineer", text);
        Assert.Equal(["👍 Good", "👎 Bad"], buttons.Select(b => b.Label));

        var digest = (await h.Get<RadarDigest>().SendableAsync(CancellationToken.None)).Text!;
        Assert.DoesNotContain("Dream Engineer", digest);
        Assert.Contains("Far Engineer", digest);

        var itemId = (await h.ItemsAsync()).Single(i => i.Title == "Dream Engineer").Id;
        Assert.Equal("Noted 👍", await h.Get<RadarFeedback>().HandleAsync(buttons[0].Data, CancellationToken.None));
        Assert.Equal(1, (await h.ItemsAsync()).Single(i => i.Id == itemId).Feedback);
    }

    [Theory]
    [InlineData("GitHub, Inc.", "github")]
    [InlineData("Google LLC", "google")]
    [InlineData("Sierra Nevada Corporation", "sierra nevada")]
    [InlineData("Co", "co")]
    public void Company_names_normalize_for_exact_matching(string name, string expected) =>
        Assert.Equal(expected, Ranking.NormalizeCompany(name));

    [Fact]
    public async Task High_priority_companies_lead_the_digest_and_alert_in_any_tier()
    {
        var prefs = DefaultPreferences + "\ncompany_priority:\n  high: [Sierra, \"GitHub\"]\n  low: [Acme]\n";
        await using var h = await CreateAsync(preferences: prefs, configure: o => o.AlertMinScore = 75);
        h.Scorer.ByTitle["Local Engineer"] = (70, "software");
        h.Scorer.ByTitle["Agent Engineer"] = (65, "software");    // 65 + 15 = 80: alerts even though "anywhere" isn't instant
        h.Scorer.ByTitle["Defense Engineer"] = (70, "software");  // Sierra Nevada must not match "Sierra"
        h.Source.Items["acme"] =
        [
            Item("1", "Local Engineer", company: "Initech"),
            Item("2", "Agent Engineer", company: "Sierra", location: "Austin, TX"),
            Item("3", "Defense Engineer", company: "Sierra Nevada Corporation", location: "Austin, TX"),
        ];
        await ScanAndScoreAsync(h);

        var (alert, _) = Assert.Single(h.Channel.Sent);
        Assert.StartsWith("★ Agent Engineer", alert);

        await h.Get<RadarDigest>().ResetAsync(CancellationToken.None);
        var text = (await h.Get<RadarDigest>().SendableAsync(CancellationToken.None)).Text!;
        Assert.Contains("— ★ priority · anywhere · software (1) —", text);
        Assert.True(text.IndexOf("Agent Engineer", StringComparison.Ordinal) < text.IndexOf("Local Engineer", StringComparison.Ordinal));
        Assert.DoesNotContain("★ priority · anywhere · software (2)", text);
    }

    [Fact]
    public async Task Paused_agent_stops_scoring()
    {
        await using var h = await CreateAsync();
        h.Get<Consigliere.Core.Agent.AgentState>().Pause();
        h.Source.Items["acme"] = [Item("1", "Engineer")];
        await ScanAndScoreAsync(h);

        Assert.Empty(h.Scorer.Calls);
    }
}
