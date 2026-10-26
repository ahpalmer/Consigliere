using Microsoft.Extensions.Options;

namespace Consigliere.Core.DataPack;

public sealed class DataPackOptions
{
    public const string Section = "DataPack";

    /// <summary>Root of the private data pack. Relative paths resolve against <see cref="BasePath"/>.</summary>
    public string Path { get; set; } = "../../samples/data-pack";

    /// <summary>Directory relative paths resolve from; the host sets this to its content root (the project folder under dotnet run).</summary>
    public string BasePath { get; set; } = Directory.GetCurrentDirectory();
}

/// <summary>
/// Read-only access to the private data pack (profile, preferences, prompts).
/// The pack lives outside the public repo; the code only knows its shape.
/// </summary>
public sealed class DataPack
{
    public string Root { get; }

    public DataPack(IOptions<DataPackOptions> options)
    {
        Root = System.IO.Path.GetFullPath(options.Value.Path, options.Value.BasePath);
        if (!Directory.Exists(Root))
            throw new DirectoryNotFoundException($"Data pack not found at '{Root}'. Set {DataPackOptions.Section}:Path.");
    }

    public string Resolve(string relativePath)
    {
        var full = System.IO.Path.GetFullPath(System.IO.Path.Combine(Root, relativePath));
        var rootWithSeparator = System.IO.Path.EndsInDirectorySeparator(Root) ? Root : Root + System.IO.Path.DirectorySeparatorChar;
        if (!full.StartsWith(rootWithSeparator, StringComparison.OrdinalIgnoreCase))
            throw new ArgumentException($"Path '{relativePath}' escapes the data pack.", nameof(relativePath));
        return full;
    }

    public bool Exists(string relativePath) => File.Exists(Resolve(relativePath));

    public string? ReadTextOrDefault(string relativePath)
    {
        var path = Resolve(relativePath);
        return File.Exists(path) ? File.ReadAllText(path) : null;
    }
}
