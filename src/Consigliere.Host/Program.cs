using Anthropic;
using Consigliere.Core.Agent;
using Consigliere.Core.Channels;
using Consigliere.Core.DataPack;
using Consigliere.Data;
using Consigliere.Host;
using Consigliere.Host.Scheduling;
using Consigliere.Host.Telegram;
using Consigliere.Llm;
using Consigliere.Skills.Mail;
using Consigliere.Skills.Radar;
using System.Reflection;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Configuration.UserSecrets;
using Microsoft.Extensions.Options;
using Telegram.Bot;

var builder = Host.CreateApplicationBuilder(args);

// One-time setup commands, e.g. `dotnet run --project src/Consigliere.Host -- auth gmail`.
if (args is ["auth", "gmail"])
{
    var secretsId = typeof(Program).Assembly.GetCustomAttribute<UserSecretsIdAttribute>()!.UserSecretsId;
    var saved = await GmailAuth.RunAsync(builder.Configuration.GetSection(MailOptions.Section).Get<MailOptions>() ?? new(), secretsId, CancellationToken.None);
    Console.WriteLine($"Gmail authorized. Refresh token saved to {saved} as Mail:RefreshToken.");
    return;
}

// Secrets come from user-secrets locally and Key Vault on the VM; never from appsettings.json.
builder.Services.Configure<DataPackOptions>(o => o.BasePath = builder.Environment.ContentRootPath);
builder.Services.Configure<DataPackOptions>(builder.Configuration.GetSection(DataPackOptions.Section));
builder.Services.Configure<LlmOptions>(builder.Configuration.GetSection(LlmOptions.Section));
builder.Services.Configure<AgentOptions>(builder.Configuration.GetSection(AgentOptions.Section));
builder.Services.Configure<TelegramOptions>(builder.Configuration.GetSection(TelegramOptions.Section));

builder.Services.AddSingleton<DataPack>();
builder.Services.AddSingleton<AgentState>();
builder.Services.AddSingleton<UsageTracker>();
builder.Services.AddSingleton<PromptLibrary>();

builder.Services.AddSingleton(_ => new AnthropicClient
{
    ApiKey = builder.Configuration["Anthropic:ApiKey"]
        ?? throw new InvalidOperationException("Anthropic:ApiKey is not configured."),
});
builder.Services.AddSingleton<ChatService>();
builder.Services.AddSingleton<MessageRouter>();

builder.Services.AddSingleton(TimeProvider.System);
builder.Services.AddConsigliereData(sp =>
    sp.GetRequiredService<DataPack>().Resolve(sp.GetRequiredService<IOptions<RadarOptions>>().Value.DatabasePath));
builder.Services.AddRadar(builder.Configuration);
builder.Services.AddMail(builder.Configuration);

builder.Services.Configure<SchedulerOptions>(builder.Configuration.GetSection(SchedulerOptions.Section));
builder.Services.AddHttpClient();
builder.Services.AddHostedService<JobScheduler>();

builder.Services.AddSingleton<ITelegramBotClient>(sp =>
{
    var token = sp.GetRequiredService<IOptions<TelegramOptions>>().Value.BotToken;
    if (string.IsNullOrWhiteSpace(token)) throw new InvalidOperationException("Telegram:BotToken is not configured.");
    return new TelegramBotClient(token);
});
builder.Services.AddSingleton<TelegramSender>();
builder.Services.AddSingleton<IChannel>(sp => sp.GetRequiredService<TelegramSender>());
builder.Services.AddHostedService<TelegramChannel>();

var host = builder.Build();

// Fail fast on a bad data pack path rather than on the first message.
var pack = host.Services.GetRequiredService<DataPack>();
host.Services.GetRequiredService<ILogger<Program>>().LogInformation("Data pack: {Root}", pack.Root);

await using (var db = await host.Services.GetRequiredService<IDbContextFactory<ConsigliereDbContext>>().CreateDbContextAsync())
    await db.Database.MigrateAsync();

await host.RunAsync();
