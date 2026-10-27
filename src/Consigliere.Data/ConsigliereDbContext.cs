using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Design;
using Microsoft.EntityFrameworkCore.Storage.ValueConversion;

namespace Consigliere.Data;

public sealed class ConsigliereDbContext(DbContextOptions<ConsigliereDbContext> options) : DbContext(options)
{
    public DbSet<RadarItem> RadarItems => Set<RadarItem>();
    public DbSet<SourceRun> SourceRuns => Set<SourceRun>();
    public DbSet<ProcessedMessage> ProcessedMessages => Set<ProcessedMessage>();

    protected override void ConfigureConventions(ModelConfigurationBuilder builder)
    {
        // SQLite can't order or compare DateTimeOffset natively. Store UTC DateTime instead: EF writes it as
        // sortable text ("2026-10-26 21:55:29.7107"), which compares correctly and stays readable in a DB browser.
        builder.Properties<DateTimeOffset>().HaveConversion<UtcDateTimeConverter>();
        builder.Properties<decimal>().HaveConversion<double>();
    }

    protected override void OnModelCreating(ModelBuilder model)
    {
        model.Entity<RadarItem>(e =>
        {
            e.HasIndex(i => new { i.Source, i.Board, i.ExternalId }).IsUnique();
            e.HasIndex(i => i.Fingerprint);
            e.HasIndex(i => i.TitleKey);
            e.HasIndex(i => new { i.Passed, i.DigestedAt });
            e.HasIndex(i => new { i.ContentHash, i.ScoringKey });
        });

        model.Entity<SourceRun>(e => e.HasIndex(r => new { r.Source, r.Board, r.StartedAt }));

        model.Entity<ProcessedMessage>(e => e.HasIndex(m => new { m.Mailbox, m.ProcessedAt }));
    }
}

public sealed class UtcDateTimeConverter() : ValueConverter<DateTimeOffset, DateTime>(
    v => v.UtcDateTime,
    v => new DateTimeOffset(DateTime.SpecifyKind(v, DateTimeKind.Utc)));

/// <summary>Lets `dotnet ef` build the model without starting the host.</summary>
public sealed class DesignTimeDbContextFactory : IDesignTimeDbContextFactory<ConsigliereDbContext>
{
    public ConsigliereDbContext CreateDbContext(string[] args) =>
        new(new DbContextOptionsBuilder<ConsigliereDbContext>().UseSqlite("Data Source=design-time.db").Options);
}
