using Consigliere.Skills.Radar.Sources;
using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;

namespace Consigliere.Skills.Mail;

public static class MailServiceCollectionExtensions
{
    /// <summary>Adds the "email" Radar source. It only runs if the watchlist has an entry with ats: email.</summary>
    public static IServiceCollection AddMail(this IServiceCollection services, IConfiguration configuration)
    {
        services.Configure<MailOptions>(configuration.GetSection(MailOptions.Section));
        services.AddSingleton<IMailbox, GmailMailbox>();
        services.AddSingleton<IEmailExtractor, LlmEmailExtractor>();
        services.AddSingleton<EmailSource>();
        services.AddTransient<IItemSource>(sp => sp.GetRequiredService<EmailSource>());
        return services;
    }
}
