-- Matches the schema the .NET version's EF Core migrations produced (Initial + ScoringAndReadableDates + MailIngestion),
-- so either implementation can open the same database. Dates are UTC text: "2006-01-02 15:04:05.9999999".
CREATE TABLE IF NOT EXISTS "SourceRuns" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_SourceRuns" PRIMARY KEY AUTOINCREMENT,
    "Board" TEXT NOT NULL,
    "Error" TEXT NULL,
    "Fetched" INTEGER NOT NULL,
    "FinishedAt" TEXT NOT NULL,
    "New" INTEGER NOT NULL,
    "Passed" INTEGER NOT NULL,
    "Source" TEXT NOT NULL,
    "StartedAt" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "RadarItems" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_RadarItems" PRIMARY KEY AUTOINCREMENT,
    "AlertedAt" TEXT NULL,
    "Board" TEXT NOT NULL,
    "Category" TEXT NULL,
    "CompCurrency" TEXT NULL,
    "CompMax" REAL NULL,
    "CompMin" REAL NULL,
    "Company" TEXT NOT NULL,
    "ContentHash" TEXT NOT NULL,
    "Description" TEXT NOT NULL,
    "DigestedAt" TEXT NULL,
    "DuplicateOfId" INTEGER NULL,
    "ExternalId" TEXT NOT NULL,
    "Feedback" INTEGER NULL,
    "FeedbackAt" TEXT NULL,
    "FilterReason" TEXT NULL,
    "Fingerprint" TEXT NOT NULL,
    "FirstSeenAt" TEXT NOT NULL,
    "FitGaps" TEXT NULL,
    "FitReasons" TEXT NULL,
    "FitScore" INTEGER NULL,
    "FitSummary" TEXT NULL,
    "LastSeenAt" TEXT NOT NULL,
    "Locations" TEXT NOT NULL,
    "Passed" INTEGER NOT NULL,
    "PostedAt" TEXT NULL,
    "ScoredAt" TEXT NULL,
    "ScoringKey" TEXT NULL,
    "Source" TEXT NOT NULL,
    "Tier" TEXT NULL,
    "Title" TEXT NOT NULL,
    "Url" TEXT NOT NULL,
    "TitleKey" TEXT NULL
);

CREATE TABLE IF NOT EXISTS "ProcessedMessages" (
    "Id" TEXT NOT NULL CONSTRAINT "PK_ProcessedMessages" PRIMARY KEY,
    "Mailbox" TEXT NOT NULL,
    "From" TEXT NOT NULL,
    "Subject" TEXT NOT NULL,
    "ReceivedAt" TEXT NULL,
    "ProcessedAt" TEXT NOT NULL,
    "ItemCount" INTEGER NOT NULL,
    "Note" TEXT NULL
);

CREATE INDEX IF NOT EXISTS "IX_SourceRuns_Source_Board_StartedAt" ON "SourceRuns" ("Source", "Board", "StartedAt");
CREATE INDEX IF NOT EXISTS "IX_RadarItems_ContentHash_ScoringKey" ON "RadarItems" ("ContentHash", "ScoringKey");
CREATE INDEX IF NOT EXISTS "IX_RadarItems_Fingerprint" ON "RadarItems" ("Fingerprint");
CREATE INDEX IF NOT EXISTS "IX_RadarItems_Passed_DigestedAt" ON "RadarItems" ("Passed", "DigestedAt");
CREATE UNIQUE INDEX IF NOT EXISTS "IX_RadarItems_Source_Board_ExternalId" ON "RadarItems" ("Source", "Board", "ExternalId");
CREATE INDEX IF NOT EXISTS "IX_RadarItems_TitleKey" ON "RadarItems" ("TitleKey");
CREATE INDEX IF NOT EXISTS "IX_ProcessedMessages_Mailbox_ProcessedAt" ON "ProcessedMessages" ("Mailbox", "ProcessedAt");
