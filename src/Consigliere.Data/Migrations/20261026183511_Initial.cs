using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace Consigliere.Data.Migrations
{
    /// <inheritdoc />
    public partial class Initial : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.CreateTable(
                name: "RadarItems",
                columns: table => new
                {
                    Id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    Source = table.Column<string>(type: "TEXT", nullable: false),
                    Board = table.Column<string>(type: "TEXT", nullable: false),
                    ExternalId = table.Column<string>(type: "TEXT", nullable: false),
                    Company = table.Column<string>(type: "TEXT", nullable: false),
                    Title = table.Column<string>(type: "TEXT", nullable: false),
                    Locations = table.Column<string>(type: "TEXT", nullable: false),
                    Url = table.Column<string>(type: "TEXT", nullable: false),
                    Description = table.Column<string>(type: "TEXT", nullable: false),
                    CompMin = table.Column<double>(type: "REAL", nullable: true),
                    CompMax = table.Column<double>(type: "REAL", nullable: true),
                    CompCurrency = table.Column<string>(type: "TEXT", nullable: true),
                    PostedAt = table.Column<long>(type: "INTEGER", nullable: true),
                    FirstSeenAt = table.Column<long>(type: "INTEGER", nullable: false),
                    LastSeenAt = table.Column<long>(type: "INTEGER", nullable: false),
                    ContentHash = table.Column<string>(type: "TEXT", nullable: false),
                    Fingerprint = table.Column<string>(type: "TEXT", nullable: false),
                    DuplicateOfId = table.Column<long>(type: "INTEGER", nullable: true),
                    Passed = table.Column<bool>(type: "INTEGER", nullable: false),
                    Tier = table.Column<string>(type: "TEXT", nullable: true),
                    FilterReason = table.Column<string>(type: "TEXT", nullable: true),
                    DigestedAt = table.Column<long>(type: "INTEGER", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_RadarItems", x => x.Id);
                });

            migrationBuilder.CreateTable(
                name: "SourceRuns",
                columns: table => new
                {
                    Id = table.Column<long>(type: "INTEGER", nullable: false)
                        .Annotation("Sqlite:Autoincrement", true),
                    Source = table.Column<string>(type: "TEXT", nullable: false),
                    Board = table.Column<string>(type: "TEXT", nullable: false),
                    StartedAt = table.Column<long>(type: "INTEGER", nullable: false),
                    FinishedAt = table.Column<long>(type: "INTEGER", nullable: false),
                    Fetched = table.Column<int>(type: "INTEGER", nullable: false),
                    New = table.Column<int>(type: "INTEGER", nullable: false),
                    Passed = table.Column<int>(type: "INTEGER", nullable: false),
                    Error = table.Column<string>(type: "TEXT", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_SourceRuns", x => x.Id);
                });

            migrationBuilder.CreateIndex(
                name: "IX_RadarItems_Fingerprint",
                table: "RadarItems",
                column: "Fingerprint");

            migrationBuilder.CreateIndex(
                name: "IX_RadarItems_Passed_DigestedAt",
                table: "RadarItems",
                columns: new[] { "Passed", "DigestedAt" });

            migrationBuilder.CreateIndex(
                name: "IX_RadarItems_Source_Board_ExternalId",
                table: "RadarItems",
                columns: new[] { "Source", "Board", "ExternalId" },
                unique: true);

            migrationBuilder.CreateIndex(
                name: "IX_SourceRuns_Source_Board_StartedAt",
                table: "SourceRuns",
                columns: new[] { "Source", "Board", "StartedAt" });
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "RadarItems");

            migrationBuilder.DropTable(
                name: "SourceRuns");
        }
    }
}
