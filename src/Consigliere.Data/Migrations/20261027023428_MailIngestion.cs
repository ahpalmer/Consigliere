using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace Consigliere.Data.Migrations
{
    /// <inheritdoc />
    public partial class MailIngestion : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AddColumn<string>(
                name: "TitleKey",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.CreateTable(
                name: "ProcessedMessages",
                columns: table => new
                {
                    Id = table.Column<string>(type: "TEXT", nullable: false),
                    Mailbox = table.Column<string>(type: "TEXT", nullable: false),
                    From = table.Column<string>(type: "TEXT", nullable: false),
                    Subject = table.Column<string>(type: "TEXT", nullable: false),
                    ReceivedAt = table.Column<DateTime>(type: "TEXT", nullable: true),
                    ProcessedAt = table.Column<DateTime>(type: "TEXT", nullable: false),
                    ItemCount = table.Column<int>(type: "INTEGER", nullable: false),
                    Note = table.Column<string>(type: "TEXT", nullable: true)
                },
                constraints: table =>
                {
                    table.PrimaryKey("PK_ProcessedMessages", x => x.Id);
                });

            migrationBuilder.CreateIndex(
                name: "IX_RadarItems_TitleKey",
                table: "RadarItems",
                column: "TitleKey");

            migrationBuilder.CreateIndex(
                name: "IX_ProcessedMessages_Mailbox_ProcessedAt",
                table: "ProcessedMessages",
                columns: new[] { "Mailbox", "ProcessedAt" });
        }

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropTable(
                name: "ProcessedMessages");

            migrationBuilder.DropIndex(
                name: "IX_RadarItems_TitleKey",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "TitleKey",
                table: "RadarItems");
        }
    }
}
