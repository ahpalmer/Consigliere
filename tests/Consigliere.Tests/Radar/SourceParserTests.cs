using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Sources;

namespace Consigliere.Tests.Radar;

/// <summary>Fixtures are trimmed copies of real public board responses.</summary>
public sealed class SourceParserTests
{
    private static string Fixture(string name) => File.ReadAllText(Path.Combine(AppContext.BaseDirectory, "Fixtures", name));

    [Fact]
    public void Greenhouse_decodes_escaped_html_and_extracts_pay_range()
    {
        var items = GreenhouseSource.Parse(Fixture("greenhouse.json"), new WatchedSource { Name = "Fallback Co", Board = "anthropic" });

        Assert.Equal(2, items.Count);
        var swe = items[0];
        Assert.Equal("5098025008", swe.ExternalId);
        Assert.Equal("Anthropic", swe.Company); // company_name from the feed wins over the watchlist name
        Assert.Equal(["San Francisco, CA", "New York City, NY"], swe.Locations);
        Assert.Equal(405_000m, swe.CompMin);
        Assert.Equal(485_000m, swe.CompMax);
        Assert.DoesNotContain("<", swe.Description);
        Assert.DoesNotContain("&lt;", swe.Description);
        Assert.StartsWith("https://", swe.Url);
        Assert.NotNull(swe.PostedAt);

        Assert.Null(items[1].CompMax);
    }

    [Fact]
    public void Lever_uses_watchlist_name_and_flags_remote_workplace()
    {
        var items = LeverSource.Parse(Fixture("lever.json"), new WatchedSource { Name = "Palantir", Board = "palantir" });

        Assert.Equal(2, items.Count);
        Assert.All(items, i => Assert.Equal("Palantir", i.Company));
        Assert.Contains("Remote", items[0].Locations); // workplaceType: remote
        Assert.DoesNotContain("Remote", items[1].Locations);
        Assert.Contains("What We Require", items[1].Description); // list sections are folded into the description
    }

    [Fact]
    public void Ashby_reads_structured_salary_and_secondary_locations()
    {
        var items = AshbySource.Parse(Fixture("ashby.json"), new WatchedSource { Name = "Ramp", Board = "ramp" });

        var security = items[0];
        Assert.Equal("Security Engineer, Cloud", security.Title); // trimmed
        Assert.Equal(211_400m, security.CompMin);
        Assert.Equal(290_600m, security.CompMax);
        Assert.Equal("USD", security.CompCurrency);
        Assert.Contains("Remote (US)", security.Locations);
        Assert.Equal("New York, NY (HQ)", security.Locations[0]);
    }
}
