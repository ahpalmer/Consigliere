using Consigliere.Core.DataPack;
using Microsoft.Extensions.Options;

namespace Consigliere.Tests;

public sealed class DataPackTests : IDisposable
{
    private readonly string _root = Directory.CreateTempSubdirectory("pack-").FullName;

    public void Dispose() => Directory.Delete(_root, recursive: true);

    private DataPack Pack() => new(Options.Create(new DataPackOptions { Path = _root }));

    [Fact]
    public void Reads_file_inside_pack()
    {
        Directory.CreateDirectory(Path.Combine(_root, "prompts"));
        File.WriteAllText(Path.Combine(_root, "prompts", "system.md"), "hello");

        Assert.Equal("hello", Pack().ReadTextOrDefault("prompts/system.md"));
    }

    [Fact]
    public void Missing_file_returns_null() => Assert.Null(Pack().ReadTextOrDefault("nope.md"));

    [Theory]
    [InlineData("../outside.md")]
    [InlineData("prompts/../../outside.md")]
    public void Rejects_paths_that_escape_the_pack(string path) =>
        Assert.Throws<ArgumentException>(() => Pack().Resolve(path));

    [Fact]
    public void Rejects_sibling_directory_with_shared_prefix() =>
        Assert.Throws<ArgumentException>(() => Pack().Resolve($"../{Path.GetFileName(_root)}-evil/x.md"));

    [Fact]
    public void Missing_root_fails_fast() =>
        Assert.Throws<DirectoryNotFoundException>(() => new DataPack(Options.Create(new DataPackOptions { Path = Path.Combine(_root, "missing") })));
}
