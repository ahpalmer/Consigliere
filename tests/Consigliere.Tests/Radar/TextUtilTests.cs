using Consigliere.Skills.Radar.Text;

namespace Consigliere.Tests.Radar;

public sealed class TextUtilTests
{
    [Theory]
    [InlineData("Annual Salary: $405,000 — $485,000 USD", 405_000, 485_000)]
    [InlineData("Range: $211.4K – $290.6K • Offers Equity", 211_400, 290_600)]
    [InlineData("pays $180k to $220k plus equity", 180_000, 220_000)]
    [InlineData("$150,000 USD - $190,000 USD", 150_000, 190_000)]
    [InlineData("the reasonably expected base pay range is between $132,000 and $198,450 for the level", 132_000, 198_450)]
    [InlineData("The base pay range for this role is $196,000.00 - $294,000.00. Actual salary", 196_000, 294_000)]
    [InlineData("Salary Range $250,000.00 - $263,000 USD", 250_000, 263_000)]
    [InlineData("$160K/yr - $200K/yr · Actively recruiting", 160_000, 200_000)]
    public void Extracts_annual_pay_ranges(string text, int min, int max) =>
        Assert.Equal((min, max), TextUtil.ExtractPayRange(text) is { } r ? ((int)r.Min, (int)r.Max) : default);

    [Theory]
    [InlineData("$45 - $60 per hour")]
    [InlineData("We raised $100M - $200M")]
    [InlineData("Over $1B raised (including our $500M Series F)")]
    [InlineData("a $5,000 stipend and $1,000 learning budget")]
    [InlineData("no numbers here")]
    public void Ignores_non_annual_or_missing_ranges(string text) => Assert.Null(TextUtil.ExtractPayRange(text));

    [Theory]
    [InlineData("Milwaukee, WI", "uk", false)]
    [InlineData("London, UK", "uk", true)]
    [InlineData("Senior SRE", "sre", true)]
    [InlineData("Measured results", "sre", false)]
    [InlineData("Site Reliability Engineer", "site reliability", true)]
    public void Keyword_matching_is_whole_word(string text, string keyword, bool expected) =>
        Assert.Equal(expected, TextUtil.ContainsKeyword(text, keyword));

    [Fact]
    public void Html_to_text_keeps_line_structure()
    {
        var text = TextUtil.HtmlToText("&lt;p&gt;Hello&amp;nbsp;world&lt;/p&gt;&lt;ul&gt;&lt;li&gt;One&lt;/li&gt;&lt;li&gt;Two&lt;/li&gt;&lt;/ul&gt;");
        Assert.Equal("Hello world\n\nOne\n\nTwo", text);
    }
}
