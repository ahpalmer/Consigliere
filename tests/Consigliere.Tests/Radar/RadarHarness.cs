using Consigliere.Core.Agent;
using Consigliere.Core.Channels;
using Consigliere.Core.DataPack;
using Consigliere.Data;
using Consigliere.Skills.Radar;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Scoring;
using Consigliere.Skills.Radar.Sources;
using Microsoft.Data.Sqlite;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Time.Testing;

namespace Consigliere.Tests.Radar;

/// <summary>
/// The Radar pipeline wired up like the host, against a temp data pack and a real SQLite file with the real
/// migrations, but with fake sources, a fake scorer, a fake channel, and a controllable clock.
/// </summary>
public sealed class RadarHarness : IAsyncDisposable
{
    public const string DefaultWatchlist =
        """
        sources:
          - { name: Acme, ats: fake, board: acme }
          - { name: Globex, ats: fake, board: globex, priority: low }
        """;

    public const string DefaultPreferences =
        """
        titles:
          include: [engineer]
        max_posting_age_days: 90
        categories:
          - { name: software, description: software roles }
          - { name: robotics, description: robotics roles }
        tiers:
          - { name: local, locations: [raleigh], alert: instant }
          - { name: anywhere }
        """;

    private readonly string _pack = Directory.CreateTempSubdirectory("radar-").FullName;
    private readonly ServiceProvider _services;

    public FakeSource Source { get; } = new();
    public FakeSource Aggregator { get; } = new("fakeagg", isAggregator: true);
    public FakeScorer Scorer { get; } = new();
    public FakeChannel Channel { get; } = new();
    public FakeTimeProvider Time { get; } = new(new DateTimeOffset(2026, 10, 26, 12, 0, 0, TimeSpan.Zero));

    private RadarHarness(string watchlist, string preferences, Action<RadarOptions>? configure)
    {
        Directory.CreateDirectory(Path.Combine(_pack, "radar"));
        File.WriteAllText(Path.Combine(_pack, "radar", "watchlist.yaml"), watchlist);
        File.WriteAllText(Path.Combine(_pack, "radar", "preferences.yaml"), preferences);

        var services = new ServiceCollection()
            .AddLogging()
            .AddSingleton<TimeProvider>(Time)
            .AddSingleton<IItemSource>(Source)
            .AddSingleton<IItemSource>(Aggregator)
            .AddSingleton<IItemScorer>(Scorer)
            .AddSingleton<IChannel>(Channel)
            .AddSingleton(new AgentState())
            .AddSingleton(new DataPack(Microsoft.Extensions.Options.Options.Create(new DataPackOptions { Path = _pack })))
            .AddConsigliereData(_ => Path.Combine(_pack, "db", "test.db"));
        services.AddSingleton<RadarConfigLoader>();
        services.AddSingleton<RankingFactory>();
        services.AddSingleton<RadarScanner>();
        services.AddSingleton<ScoringRunner>();
        services.AddSingleton<RadarAlerts>();
        services.AddSingleton<RadarDigest>();
        services.AddSingleton<RadarFeedback>();
        services.Configure<RadarOptions>(o => configure?.Invoke(o));
        _services = services.BuildServiceProvider();
    }

    public static async Task<RadarHarness> CreateAsync(string watchlist = DefaultWatchlist, string preferences = DefaultPreferences, Action<RadarOptions>? configure = null)
    {
        var harness = new RadarHarness(watchlist, preferences, configure);
        await using var db = await harness.DbAsync();
        await db.Database.MigrateAsync();
        return harness;
    }

    public T Get<T>() where T : notnull => _services.GetRequiredService<T>();

    public Task<ConsigliereDbContext> DbAsync() => Get<IDbContextFactory<ConsigliereDbContext>>().CreateDbContextAsync();

    public async Task<List<RadarItem>> ItemsAsync()
    {
        await using var db = await DbAsync();
        return await db.RadarItems.OrderBy(i => i.Id).ToListAsync();
    }

    public void WritePreferences(string yaml) => File.WriteAllText(Path.Combine(_pack, "radar", "preferences.yaml"), yaml);

    public static FetchedItem Item(string id, string title, string company = "Acme", string location = "Raleigh, NC", DateTimeOffset? posted = null) => new()
    {
        ExternalId = id,
        Company = company,
        Title = title,
        Locations = [location],
        Url = $"https://example.test/{id}",
        Description = $"About {title}",
        PostedAt = posted,
    };

    public async ValueTask DisposeAsync()
    {
        await _services.DisposeAsync();
        SqliteConnection.ClearAllPools(); // release the file lock on Windows
        Directory.Delete(_pack, recursive: true);
    }

    public sealed class FakeSource(string kind = "fake", bool isAggregator = false) : IItemSource
    {
        public Dictionary<string, IReadOnlyList<FetchedItem>> Items { get; } = [];

        public string Kind => kind;

        public bool IsAggregator => isAggregator;

        public Task<IReadOnlyList<FetchedItem>> FetchAsync(WatchedSource source, CancellationToken ct) =>
            Task.FromResult(Items.TryGetValue(source.Board, out var items) ? items : (IReadOnlyList<FetchedItem>)[]);
    }

    /// <summary>Scores by title lookup; anything unlisted gets 60/software. Changing <see cref="Version"/> invalidates scores.</summary>
    public sealed class FakeScorer : IItemScorer
    {
        public Dictionary<string, (int Fit, string Category)?> ByTitle { get; } = [];
        public List<string> Calls { get; } = [];
        public string Version { get; set; } = "v1";

        public ScoringContext BuildContext() => new("fake-model", "rubric", Version, ["software", "robotics", "other"]);

        public Task<ScoreResult?> ScoreAsync(ScoreInput item, ScoringContext context, CancellationToken ct)
        {
            lock (Calls) Calls.Add(item.Title);
            var hit = ByTitle.TryGetValue(item.Title, out var v) ? v : (60, "software");
            return Task.FromResult(hit is { } s ? new ScoreResult(s.Fit, s.Category, $"{item.Title} summary", ["reason"], ["gap"]) : null);
        }
    }

    public sealed class FakeChannel : IChannel
    {
        public List<(string Text, IReadOnlyList<ActionButton> Buttons)> Sent { get; } = [];

        public string Name => "fake";

        public Task NotifyOwnerAsync(string text, CancellationToken ct = default) => NotifyOwnerAsync(text, [], ct);

        public Task NotifyOwnerAsync(string text, IReadOnlyList<ActionButton> buttons, CancellationToken ct = default)
        {
            Sent.Add((text, buttons));
            return Task.CompletedTask;
        }
    }
}
