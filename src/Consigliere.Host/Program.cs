using Anthropic;
using Consigliere.Core.Agent;
using Consigliere.Core.Channels;
using Consigliere.Core.DataPack;
using Consigliere.Data;
using Consigliere.Host;
using Consigliere.Host.Scheduling;
using Consigliere.Host.Telegram;
using Consigliere.Llm;
using Consigliere.Skills.Radar;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;
using Telegram.Bot;

var builder = Host.CreateApplicationBuilder(args);

// Secrets come from user-secrets locally and Key Vault on the VM; never from appsettings.json.
builder.Services.Configure<DataPackOptions>(o => o.BasePath = builder.Environment.ContentRootPath);
builder.Services.Configure<DataPackOptions>(builder.Configuration.GetSection(DataPackOptions.Section));
builder.Services.Configure<LlmOptions>(builder.Configuration.GetSection(LlmOptions.Section));
builder.Services.Configure<TelegramOptions>(builder.Configuration.GetSection(TelegramOptions.Section));

builder.Services.AddSingleton<DataPack>();
builder.Services.AddSingleton<AgentState>();
builder.Services.AddSingleton<UsageTracker>();

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

builder.Services.Configure<SchedulerOptions>(builder.Configuration.GetSection(SchedulerOptions.Section));
builder.Services.AddHostedService<JobScheduler>();

builder.Services.AddSingleton<ITelegramBotClient>(sp =>
{
    var token = sp.GetRequiredService<IOptions<TelegramOptions>>().Value.BotToken;
    if (string.IsNullOrWhiteSpace(token)) throw new InvalidOperationException("Telegram:BotToken is not configured.");
    return new TelegramBotClient(token);
});
builder.Services.AddSingleton<TelegramChannel>();
builder.Services.AddSingleton<IChannel>(sp => sp.GetRequiredService<TelegramChannel>());
builder.Services.AddHostedService(sp => sp.GetRequiredService<TelegramChannel>());

var host = builder.Build();

// Fail fast on a bad data pack path rather than on the first message.
var pack = host.Services.GetRequiredService<DataPack>();
host.Services.GetRequiredService<ILogger<Program>>().LogInformation("Data pack: {Root}", pack.Root);

await using (var db = await host.Services.GetRequiredService<IDbContextFactory<ConsigliereDbContext>>().CreateDbContextAsync())
    await db.Database.MigrateAsync();

await host.RunAsync();
