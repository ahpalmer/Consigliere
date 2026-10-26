using Consigliere.Core.DataPack;
using Consigliere.Data;
using Consigliere.Skills.Radar;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Sources;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging.Abstractions;
using Microsoft.Extensions.Options;

namespace Consigliere.Tests.Radar;

/// <summary>Runs the scanner against a real SQLite file with the real migrations applied.</summary>
public sealed class RadarScannerTests : IAsyncLifetime
{
    private readonly string _pack = Directory.CreateTempSubdirectory("radar-").FullName;
    private readonly FakeSource _fake = new();
    private ServiceProvider _services = null!;

    public async Task InitializeAsync()
    {
        Directory.CreateDirectory(Path.Combine(_pack, "radar"));
        await File.WriteAllTextAsync(Path.Combine(_pack, "radar", "watchlist.yaml"),
            """
            sources:
              - name: Acme
                ats: fake
                board: acme
              - name: Globex
                ats: fake
                board: globex
              - name: Initech
                ats: workday
                board: initech
            """);
        await File.WriteAllTextAsync(Path.Combine(_pack, "radar", "preferences.yaml"),
            """
            titles:
              include: [engineer]
            tiers:
              - name: local
                locations: [raleigh]
              - name: anywhere
            """);

        var services = new ServiceCollection()
            .AddLogging()
            .AddSingleton(TimeProvider.System)
            .AddSingleton<IItemSource>(_fake)
            .AddSingleton(new DataPack(Options.Create(new DataPackOptions { Path = _pack })))
            .AddConsigliereData(_ => Path.Combine(_pack, "db", "test.db"));
        services.AddSingleton<RadarConfigLoader>();
        services.AddSingleton<RadarScanner>();
        services.AddSingleton<RadarDigest>();
        services.Configure<RadarOptions>(o => o.DigestMaxItems = 2);
        _services = services.BuildServiceProvider();

        await using var db = await Db();
        await db.Database.MigrateAsync();
    }

    public async Task DisposeAsync()
    {
        await _services.DisposeAsync();
        SqliteConnection.ClearAllPools(); // release the file lock on Windows
        Directory.Delete(_pack, recursive: true);
    }

    private Task<ConsigliereDbContext> Db() => _services.GetRequiredService<IDbContextFactory<ConsigliereDbContext>>().CreateDbContextAsync();

    [Fact]
    public async Task Second_scan_records_nothing_new()
    {
        _fake.Items["acme"] = [Fake("1", "Platform Engineer"), Fake("2", "Recruiter")];
        var scanner = _services.GetRequiredService<RadarScanner>();

        var first = await scanner.ScanAsync(CancellationToken.None);
        var second = await scanner.ScanAsync(CancellationToken.None);

        Assert.Equal(2, first!.New);
        Assert.Equal(1, first.Passed);
        Assert.Equal(0, second!.New);
        await using var db = await Db();
        Assert.Equal(2, await db.RadarItems.CountAsync());
    }

    [Fact]
    public async Task Changed_posting_is_refreshed_and_refiltered_until_digested()
    {
        var scanner = _services.GetRequiredService<RadarScanner>();
        _fake.Items["acme"] = [Fake("1", "Engineer"), Fake("2", "Engineer II")];
        await scanner.ScanAsync(CancellationToken.None);

        // Digest item 1 only, then both postings change.
        await using (var db = await Db())
            await db.RadarItems.Where(i => i.ExternalId == "1").ExecuteUpdateAsync(s => s.SetProperty(i => i.DigestedAt, DateTimeOffset.UtcNow));
        _fake.Items["acme"] = [Fake("1", "Recruiter") with { CompMax = 150_000 }, Fake("2", "Recruiter") with { CompMax = 150_000 }];
        var rescan = await scanner.ScanAsync(CancellationToken.None);

        Assert.Equal(0, rescan!.New);
        await using var after = await Db();
        var items = await after.RadarItems.OrderBy(i => i.ExternalId).ToListAsync();
        Assert.All(items, i => Assert.Equal(150_000m, i.CompMax));
        Assert.True(items[0].Passed);  // already digested: filter result kept
        Assert.False(items[1].Passed); // not yet digested: re-filtered, "Recruiter" fails the title rule
    }

    [Fact]
    public async Task Same_posting_on_two_boards_is_marked_duplicate()
    {
        _fake.Items["acme"] = [Fake("1", "SRE Engineer", company: "Acme")];
        _fake.Items["globex"] = [Fake("x9", "sre  engineer", company: "ACME")];

        await _services.GetRequiredService<RadarScanner>().ScanAsync(CancellationToken.None);

        await using var db = await Db();
        var items = await db.RadarItems.OrderBy(i => i.Id).ToListAsync();
        Assert.Null(items[0].DuplicateOfId);
        Assert.Equal(items[0].Id, items[1].DuplicateOfId);
    }

    [Fact]
    public async Task Unsupported_source_reports_error_without_failing_scan()
    {
        var summary = await _services.GetRequiredService<RadarScanner>().ScanAsync(CancellationToken.None);

        Assert.Contains(summary!.Sources, s => s.Name == "Initech" && s.Error!.Contains("unsupported"));
        await using var db = await Db();
        Assert.Equal(3, await db.SourceRuns.CountAsync());
    }

    [Fact]
    public async Task Digest_pages_through_items_and_puts_local_tier_first()
    {
        _fake.Items["acme"] = [Fake("1", "Engineer A", location: "Austin, TX"), Fake("2", "Engineer B", location: "Raleigh, NC"), Fake("3", "Engineer C", location: "Boise, ID")];
        await _services.GetRequiredService<RadarScanner>().ScanAsync(CancellationToken.None);
        var digest = _services.GetRequiredService<RadarDigest>();

        var page1 = await digest.SendableAsync(CancellationToken.None);
        var page2 = await digest.SendableAsync(CancellationToken.None);
        var page3 = await digest.SendableAsync(CancellationToken.None);

        Assert.Equal((2, 1), (page1.Shown, page1.Remaining));
        Assert.True(page1.Text!.IndexOf("Engineer B", StringComparison.Ordinal) < page1.Text.IndexOf("Engineer", page1.Text.IndexOf("— anywhere", StringComparison.Ordinal), StringComparison.Ordinal));
        Assert.Equal((1, 0), (page2.Shown, page2.Remaining));
        Assert.Null(page3.Text);
    }

    private static FetchedItem Fake(string id, string title, string company = "Acme", string location = "Raleigh, NC") => new()
    {
        ExternalId = id,
        Company = company,
        Title = title,
        Locations = [location],
        Url = $"https://example.test/{id}",
    };

    private sealed class FakeSource : IItemSource
    {
        public Dictionary<string, IReadOnlyList<FetchedItem>> Items { get; } = [];

        public string Kind => "fake";

        public Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct) =>
            Task.FromResult(Items.TryGetValue(source.Board, out var items) ? items : (IReadOnlyList<FetchedItem>)[]);
    }
}
