using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Filtering;
using Consigliere.Skills.Radar.Sources;

namespace Consigliere.Tests.Radar;

public sealed class RuleFilterTests
{
    private static readonly Preferences Prefs = new()
    {
        Titles = new KeywordSet { Include = ["platform engineer", "sre", "software engineer"], Exclude = ["intern", "principal"] },
        ExcludeLocations = ["canada", "uk", "london"],
        DealBreakers = ["security clearance"],
        Tiers =
        [
            new Tier { Name = "local", Locations = ["raleigh", "durham"] },
            new Tier { Name = "remote", Remote = true },
            new Tier { Name = "relocation", MinComp = 250_000 },
        ],
    };

    private static FetchedItem Item(string title = "Senior Platform Engineer", string[]? locations = null, decimal? compMax = null, string description = "") => new()
    {
        ExternalId = "1",
        Company = "Acme",
        Title = title,
        Locations = locations ?? ["Raleigh, NC"],
        Url = "https://example.test/1",
        Description = description,
        CompMax = compMax,
    };

    [Theory]
    [InlineData("Marketing Manager", "title: no include keyword")]
    [InlineData("Software Engineer Intern", "title: excluded 'intern'")]
    [InlineData("Principal Platform Engineer", "title: excluded 'principal'")]
    public void Rejects_on_title(string title, string reason) => Assert.Equal(reason, RuleFilter.Evaluate(Item(title), Prefs).Reason);

    [Fact]
    public void Local_tier_wins_over_remote_when_both_listed() =>
        Assert.Equal("local", RuleFilter.Evaluate(Item(locations: ["Remote (US)", "Durham, NC"]), Prefs).Tier);

    [Fact]
    public void Remote_tier_requires_a_remote_location_after_exclusions()
    {
        Assert.Equal("remote", RuleFilter.Evaluate(Item(locations: ["Remote (US)", "Remote (Canada)"]), Prefs).Tier);

        var canadaOnly = RuleFilter.Evaluate(Item(locations: ["Remote (Canada)", "London, UK"]), Prefs);
        Assert.False(canadaOnly.Passed);
        Assert.Equal("location: all excluded", canadaOnly.Reason);
    }

    [Fact]
    public void Relocation_applies_comp_floor_but_passes_unknown_comp()
    {
        var low = RuleFilter.Evaluate(Item(locations: ["Seattle, WA"], compMax: 200_000), Prefs);
        Assert.False(low.Passed);
        Assert.Equal("relocation", low.Tier);

        var high = RuleFilter.Evaluate(Item(locations: ["Seattle, WA"], compMax: 300_000), Prefs);
        Assert.True(high.Passed);

        var unknown = RuleFilter.Evaluate(Item(locations: ["Seattle, WA"]), Prefs);
        Assert.True(unknown.Passed);
        Assert.Equal("comp unknown", unknown.Reason);
    }

    [Fact]
    public void Comp_floor_does_not_apply_to_local_tier() =>
        Assert.True(RuleFilter.Evaluate(Item(compMax: 120_000), Prefs).Passed);

    [Fact]
    public void Deal_breaker_in_description_rejects() =>
        Assert.Equal("deal-breaker 'security clearance'",
            RuleFilter.Evaluate(Item(description: "Active security clearance required."), Prefs).Reason);

    [Fact]
    public void Empty_preferences_pass_everything() =>
        Assert.True(RuleFilter.Evaluate(Item("Anything"), new Preferences()).Passed);
}
