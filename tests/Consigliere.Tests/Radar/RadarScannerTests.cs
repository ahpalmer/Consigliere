using Consigliere.Skills.Radar;
using Microsoft.EntityFrameworkCore;
using static Consigliere.Tests.Radar.RadarHarness;

namespace Consigliere.Tests.Radar;

public sealed class RadarScannerTests
{
    [Fact]
    public async Task Second_scan_records_nothing_new()
    {
        await using var h = await CreateAsync();
        h.Source.Items["acme"] = [Item("1", "Platform Engineer"), Item("2", "Recruiter")];
        var scanner = h.Get<RadarScanner>();

        var first = await scanner.ScanAsync(CancellationToken.None);
        var second = await scanner.ScanAsync(CancellationToken.None);

        Assert.Equal((2, 1), (first!.New, first.Passed));
        Assert.Equal(0, second!.New);
        Assert.Equal(2, (await h.ItemsAsync()).Count);
    }

    [Fact]
    public async Task Rescan_never_changes_first_seen_or_posted_dates()
    {
        await using var h = await CreateAsync();
        var posted = new DateTimeOffset(2026, 10, 1, 9, 30, 0, TimeSpan.FromHours(-4));
        h.Source.Items["acme"] = [Item("1", "Engineer", posted: posted)];
        var scanner = h.Get<RadarScanner>();
        await scanner.ScanAsync(CancellationToken.None);
        var firstSeen = h.Time.GetUtcNow();

        // Days later the same posting comes back with an edited description.
        h.Time.Advance(TimeSpan.FromDays(3));
        h.Source.Items["acme"] = [Item("1", "Engineer", posted: posted) with { Description = "Edited" }];
        await scanner.ScanAsync(CancellationToken.None);

        var item = Assert.Single(await h.ItemsAsync());
        Assert.Equal("Edited", item.Description);
        Assert.Equal(posted, item.PostedAt);              // the board's date, offset normalized to UTC
        Assert.Equal(firstSeen, item.FirstSeenAt);        // when we first pulled it
        Assert.Equal(h.Time.GetUtcNow(), item.LastSeenAt); // updated each time it's still listed
    }

    [Fact]
    public async Task Changed_posting_is_refreshed_and_refiltered_until_digested()
    {
        await using var h = await CreateAsync();
        var scanner = h.Get<RadarScanner>();
        h.Source.Items["acme"] = [Item("1", "Engineer"), Item("2", "Engineer II")];
        await scanner.ScanAsync(CancellationToken.None);

        // Digest item 1 only, then both postings change.
        await using (var db = await h.DbAsync())
            await db.RadarItems.Where(i => i.ExternalId == "1").ExecuteUpdateAsync(s => s.SetProperty(i => i.DigestedAt, DateTimeOffset.UtcNow));
        h.Source.Items["acme"] = [Item("1", "Recruiter") with { CompMax = 150_000 }, Item("2", "Recruiter") with { CompMax = 150_000 }];
        var rescan = await scanner.ScanAsync(CancellationToken.None);

        Assert.Equal(0, rescan!.New);
        var items = await h.ItemsAsync();
        Assert.All(items, i => Assert.Equal(150_000m, i.CompMax));
        Assert.True(items[0].Passed);  // already digested: filter result kept
        Assert.False(items[1].Passed); // not yet digested: re-filtered, "Recruiter" fails the title rule
    }

    [Fact]
    public async Task Preference_edits_apply_to_existing_unsent_items_on_next_scan()
    {
        await using var h = await CreateAsync();
        var scanner = h.Get<RadarScanner>();
        h.Source.Items["acme"] = [Item("1", "Engineer", location: "Austin, TX")];
        await scanner.ScanAsync(CancellationToken.None);
        Assert.Equal("anywhere", (await h.ItemsAsync())[0].Tier);

        h.WritePreferences(DefaultPreferences.Replace("locations: [raleigh]", "locations: [raleigh, austin]"));
        await scanner.ScanAsync(CancellationToken.None);

        Assert.Equal("local", (await h.ItemsAsync())[0].Tier);
    }

    [Fact]
    public async Task Same_posting_on_two_boards_is_marked_duplicate()
    {
        await using var h = await CreateAsync();
        h.Source.Items["acme"] = [Item("1", "SRE Engineer", company: "Acme")];
        h.Source.Items["globex"] = [Item("x9", "sre  engineer", company: "ACME")];

        await h.Get<RadarScanner>().ScanAsync(CancellationToken.None);

        var items = await h.ItemsAsync();
        Assert.Null(items[0].DuplicateOfId);
        Assert.Equal(items[0].Id, items[1].DuplicateOfId);
    }

    [Fact]
    public async Task Unsupported_source_reports_error_without_failing_scan()
    {
        await using var h = await CreateAsync(watchlist: DefaultWatchlist + "\n  - { name: Initech, ats: workday, board: initech }");

        var summary = await h.Get<RadarScanner>().ScanAsync(CancellationToken.None);

        Assert.Contains(summary!.Sources, s => s.Name == "Initech" && s.Error!.Contains("unsupported"));
        await using var db = await h.DbAsync();
        Assert.Equal(3, await db.SourceRuns.CountAsync());
    }
}

public sealed class AggregatorDedupeTests
{
    private const string Watchlist =
        """
        sources:
          - { name: Acme board, ats: fake, board: acme }
          - { name: Alerts, ats: fakeagg, board: inbox }
        """;

    [Fact]
    public async Task Aggregator_copy_defers_to_existing_primary_item()
    {
        await using var h = await RadarHarness.CreateAsync(Watchlist);
        h.Source.Items["acme"] = [RadarHarness.Item("1", "Platform Engineer", location: "Raleigh, NC")];
        await h.Get<Consigliere.Skills.Radar.RadarScanner>().ScanAsync(CancellationToken.None);

        h.Aggregator.Items["inbox"] = [RadarHarness.Item("linkedin:9", "Platform  engineer", company: "ACME", location: "Raleigh, NC (Hybrid)")];
        await h.Get<Consigliere.Skills.Radar.RadarScanner>().ScanAsync(CancellationToken.None);

        var items = await h.ItemsAsync();
        Assert.Equal(items.Single(i => i.Source == "fake").Id, items.Single(i => i.Source == "fakeagg").DuplicateOfId);
    }

    [Fact]
    public async Task Primary_item_arriving_later_takes_over_from_the_aggregator_copy()
    {
        await using var h = await RadarHarness.CreateAsync(Watchlist);
        h.Aggregator.Items["inbox"] = [RadarHarness.Item("linkedin:9", "Platform Engineer", location: "Raleigh, NC (Hybrid)")];
        await h.Get<Consigliere.Skills.Radar.RadarScanner>().ScanAsync(CancellationToken.None);
        Assert.Null(Assert.Single(await h.ItemsAsync()).DuplicateOfId);

        h.Source.Items["acme"] = [RadarHarness.Item("1", "Platform Engineer", location: "Raleigh, NC")];
        await h.Get<Consigliere.Skills.Radar.RadarScanner>().ScanAsync(CancellationToken.None);

        var items = await h.ItemsAsync();
        Assert.Null(items.Single(i => i.Source == "fake").DuplicateOfId);
        Assert.Equal(items.Single(i => i.Source == "fake").Id, items.Single(i => i.Source == "fakeagg").DuplicateOfId);
    }
}
