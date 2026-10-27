using System;
using Microsoft.EntityFrameworkCore.Migrations;

#nullable disable

namespace Consigliere.Data.Migrations
{
    /// <inheritdoc />
    public partial class ScoringAndReadableDates : Migration
    {
        /// <inheritdoc />
        protected override void Up(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.AlterColumn<DateTime>(
                name: "StartedAt",
                table: "SourceRuns",
                type: "TEXT",
                nullable: false,
                oldClrType: typeof(long),
                oldType: "INTEGER");

            migrationBuilder.AlterColumn<DateTime>(
                name: "FinishedAt",
                table: "SourceRuns",
                type: "TEXT",
                nullable: false,
                oldClrType: typeof(long),
                oldType: "INTEGER");

            migrationBuilder.AlterColumn<DateTime>(
                name: "PostedAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: true,
                oldClrType: typeof(long),
                oldType: "INTEGER",
                oldNullable: true);

            migrationBuilder.AlterColumn<DateTime>(
                name: "LastSeenAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: false,
                oldClrType: typeof(long),
                oldType: "INTEGER");

            migrationBuilder.AlterColumn<DateTime>(
                name: "FirstSeenAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: false,
                oldClrType: typeof(long),
                oldType: "INTEGER");

            migrationBuilder.AlterColumn<DateTime>(
                name: "DigestedAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: true,
                oldClrType: typeof(long),
                oldType: "INTEGER",
                oldNullable: true);

            migrationBuilder.AddColumn<DateTime>(
                name: "AlertedAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "Category",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<int>(
                name: "Feedback",
                table: "RadarItems",
                type: "INTEGER",
                nullable: true);

            migrationBuilder.AddColumn<DateTime>(
                name: "FeedbackAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "FitGaps",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "FitReasons",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<int>(
                name: "FitScore",
                table: "RadarItems",
                type: "INTEGER",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "FitSummary",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<DateTime>(
                name: "ScoredAt",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.AddColumn<string>(
                name: "ScoringKey",
                table: "RadarItems",
                type: "TEXT",
                nullable: true);

            migrationBuilder.CreateIndex(
                name: "IX_RadarItems_ContentHash_ScoringKey",
                table: "RadarItems",
                columns: new[] { "ContentHash", "ScoringKey" });

            // Convert existing values from DateTimeOffsetToBinaryConverter's packed format to UTC text.
            foreach (var (table, column) in DateColumns)
                migrationBuilder.Sql(BinaryToUtcText(table, column));
        }

        private static readonly (string Table, string Column)[] DateColumns =
        [
            ("RadarItems", "PostedAt"), ("RadarItems", "FirstSeenAt"), ("RadarItems", "LastSeenAt"), ("RadarItems", "DigestedAt"),
            ("SourceRuns", "StartedAt"), ("SourceRuns", "FinishedAt"),
        ];

        // Packed format: ((clockTicks / 1000) << 11) | (offsetMinutes & 0x7FF), the offset as 11-bit two's complement.
        // The table rebuild above copies the numbers into TEXT columns, so CAST them back before decoding.
        // Down() does not convert data back; rolling back loses these dates.
        private static string BinaryToUtcText(string table, string column) =>
            $"""
            UPDATE "{table}" SET "{column}" = strftime('%Y-%m-%d %H:%M:%f',
                (CAST("{column}" AS INTEGER) >> 11) * 1000 / 10000000.0 - 62135596800
                - 60 * (CASE WHEN (CAST("{column}" AS INTEGER) & 2047) >= 1024
                             THEN (CAST("{column}" AS INTEGER) & 2047) - 2048
                             ELSE (CAST("{column}" AS INTEGER) & 2047) END),
                'unixepoch')
            WHERE "{column}" IS NOT NULL AND "{column}" NOT LIKE '%-%';
            """;

        /// <inheritdoc />
        protected override void Down(MigrationBuilder migrationBuilder)
        {
            migrationBuilder.DropIndex(
                name: "IX_RadarItems_ContentHash_ScoringKey",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "AlertedAt",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "Category",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "Feedback",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "FeedbackAt",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "FitGaps",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "FitReasons",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "FitScore",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "FitSummary",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "ScoredAt",
                table: "RadarItems");

            migrationBuilder.DropColumn(
                name: "ScoringKey",
                table: "RadarItems");

            migrationBuilder.AlterColumn<long>(
                name: "StartedAt",
                table: "SourceRuns",
                type: "INTEGER",
                nullable: false,
                oldClrType: typeof(DateTime),
                oldType: "TEXT");

            migrationBuilder.AlterColumn<long>(
                name: "FinishedAt",
                table: "SourceRuns",
                type: "INTEGER",
                nullable: false,
                oldClrType: typeof(DateTime),
                oldType: "TEXT");

            migrationBuilder.AlterColumn<long>(
                name: "PostedAt",
                table: "RadarItems",
                type: "INTEGER",
                nullable: true,
                oldClrType: typeof(DateTime),
                oldType: "TEXT",
                oldNullable: true);

            migrationBuilder.AlterColumn<long>(
                name: "LastSeenAt",
                table: "RadarItems",
                type: "INTEGER",
                nullable: false,
                oldClrType: typeof(DateTime),
                oldType: "TEXT");

            migrationBuilder.AlterColumn<long>(
                name: "FirstSeenAt",
                table: "RadarItems",
                type: "INTEGER",
                nullable: false,
                oldClrType: typeof(DateTime),
                oldType: "TEXT");

            migrationBuilder.AlterColumn<long>(
                name: "DigestedAt",
                table: "RadarItems",
                type: "INTEGER",
                nullable: true,
                oldClrType: typeof(DateTime),
                oldType: "TEXT",
                oldNullable: true);
        }
    }
}
