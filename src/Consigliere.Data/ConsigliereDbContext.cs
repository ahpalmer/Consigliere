using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Design;
using Microsoft.EntityFrameworkCore.Storage.ValueConversion;

namespace Consigliere.Data;

public sealed class ConsigliereDbContext(DbContextOptions<ConsigliereDbContext> options) : DbContext(options)
{
    public DbSet<RadarItem> RadarItems => Set<RadarItem>();
    public DbSet<SourceRun> SourceRuns => Set<SourceRun>();

    protected override void ConfigureConventions(ModelConfigurationBuilder builder)
    {
        // SQLite can't order or compare DateTimeOffset natively; store UTC ticks instead.
        builder.Properties<DateTimeOffset>().HaveConversion<DateTimeOffsetToBinaryConverter>();
        builder.Properties<decimal>().HaveConversion<double>();
    }

    protected override void OnModelCreating(ModelBuilder model)
    {
        model.Entity<RadarItem>(e =>
        {
            e.HasIndex(i => new { i.Source, i.Board, i.ExternalId }).IsUnique();
            e.HasIndex(i => i.Fingerprint);
            e.HasIndex(i => new { i.Passed, i.DigestedAt });
        });

        model.Entity<SourceRun>(e => e.HasIndex(r => new { r.Source, r.Board, r.StartedAt }));
    }
}

/// <summary>Lets `dotnet ef` build the model without starting the host.</summary>
public sealed class DesignTimeDbContextFactory : IDesignTimeDbContextFactory<ConsigliereDbContext>
{
    public ConsigliereDbContext CreateDbContext(string[] args) =>
        new(new DbContextOptionsBuilder<ConsigliereDbContext>().UseSqlite("Data Source=design-time.db").Options);
}
