using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection;

namespace Consigliere.Data;

public static class DataServiceCollectionExtensions
{
    /// <param name="databasePath">Resolves the SQLite file path; its directory is created if missing.</param>
    public static IServiceCollection AddConsigliereData(this IServiceCollection services, Func<IServiceProvider, string> databasePath) =>
        services.AddDbContextFactory<ConsigliereDbContext>((sp, options) =>
        {
            var path = databasePath(sp);
            Directory.CreateDirectory(Path.GetDirectoryName(path)!);
            options.UseSqlite($"Data Source={path}");
        });
}
