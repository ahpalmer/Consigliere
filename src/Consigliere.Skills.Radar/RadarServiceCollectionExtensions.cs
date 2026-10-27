using Consigliere.Core.Channels;
using Consigliere.Core.Commands;
using Consigliere.Core.Scheduling;
using Consigliere.Skills.Radar.Config;
using Consigliere.Skills.Radar.Scoring;
using Consigliere.Skills.Radar.Sources;
using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Http.Resilience;

namespace Consigliere.Skills.Radar;

public static class RadarServiceCollectionExtensions
{
    public static IServiceCollection AddRadar(this IServiceCollection services, IConfiguration configuration)
    {
        services.Configure<RadarOptions>(configuration.GetSection(RadarOptions.Section));
        services.AddSingleton<RadarConfigLoader>();

        services.AddHttpClient<GreenhouseSource>(ConfigureClient).AddStandardResilienceHandler(ConfigureResilience);
        services.AddHttpClient<LeverSource>(ConfigureClient).AddStandardResilienceHandler(ConfigureResilience);
        services.AddHttpClient<AshbySource>(ConfigureClient).AddStandardResilienceHandler(ConfigureResilience);
        services.AddTransient<IItemSource>(sp => sp.GetRequiredService<GreenhouseSource>());
        services.AddTransient<IItemSource>(sp => sp.GetRequiredService<LeverSource>());
        services.AddTransient<IItemSource>(sp => sp.GetRequiredService<AshbySource>());

        services.AddSingleton<RadarScanner>();
        services.AddSingleton<RankingFactory>();
        services.AddSingleton<IItemScorer, ItemScorer>();
        services.AddSingleton<ScoringRunner>();
        services.AddSingleton<RadarAlerts>();
        services.AddSingleton<RadarDigest>();
        services.AddSingleton<RadarFeedback>();
        services.AddSingleton<ICallbackHandler>(sp => sp.GetRequiredService<RadarFeedback>());
        services.AddSingleton<ICommandHandler, RadarCommands>();
        services.AddSingleton<IScheduledJob, RadarScanJob>();
        services.AddSingleton<IScheduledJob, RadarScoreJob>();
        services.AddSingleton<IScheduledJob, RadarDigestJob>();
        return services;
    }

    private static void ConfigureClient(HttpClient client)
    {
        client.DefaultRequestHeaders.UserAgent.ParseAdd("Consigliere/0.1 (personal feed reader)");
        client.Timeout = Timeout.InfiniteTimeSpan; // the resilience pipeline owns timeouts
    }

    // Some boards return 10MB+ of JSON, so allow longer attempts than the defaults.
    private static void ConfigureResilience(HttpStandardResilienceOptions o)
    {
        o.AttemptTimeout.Timeout = TimeSpan.FromSeconds(60);
        o.CircuitBreaker.SamplingDuration = TimeSpan.FromMinutes(2);
        o.TotalRequestTimeout.Timeout = TimeSpan.FromMinutes(3);
    }
}
