-- Pipeline: items the owner is actively pursuing, moving through active → waiting → completed.
CREATE TABLE IF NOT EXISTS "Pursuits" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_Pursuits" PRIMARY KEY AUTOINCREMENT,
    -- Null for pursuits added by hand (something seen outside the radar).
    "ItemId" INTEGER NULL CONSTRAINT "FK_Pursuits_RadarItems" REFERENCES "RadarItems" ("Id"),
    "Company" TEXT NOT NULL,
    "Title" TEXT NOT NULL,
    "Url" TEXT NOT NULL DEFAULT '',
    -- active | waiting | completed
    "Stage" TEXT NOT NULL,
    -- Set when completed: offer | rejected | ghosted | withdrawn | dropped
    "Outcome" TEXT NULL,
    -- How far it got, set when completed: active | applied | interview | offer
    "StageReached" TEXT NULL,
    "Notes" TEXT NOT NULL DEFAULT '',
    "ActivatedAt" TEXT NOT NULL,
    "AppliedAt" TEXT NULL,
    "InterviewAt" TEXT NULL,
    -- Next follow-up reminder; null when none is due.
    "FollowUpAt" TEXT NULL,
    -- When to propose "ghosted" if nothing has happened.
    "GhostCheckAt" TEXT NULL,
    "CompletedAt" TEXT NULL,
    "UpdatedAt" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "PursuitSteps" (
    "PursuitId" INTEGER NOT NULL CONSTRAINT "FK_PursuitSteps_Pursuits" REFERENCES "Pursuits" ("Id") ON DELETE CASCADE,
    "Step" TEXT NOT NULL,
    "DoneAt" TEXT NOT NULL,
    CONSTRAINT "PK_PursuitSteps" PRIMARY KEY ("PursuitId", "Step")
);

-- Every stage change, note, and (later) email update, oldest first.
CREATE TABLE IF NOT EXISTS "PursuitEvents" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_PursuitEvents" PRIMARY KEY AUTOINCREMENT,
    "PursuitId" INTEGER NOT NULL CONSTRAINT "FK_PursuitEvents_Pursuits" REFERENCES "Pursuits" ("Id") ON DELETE CASCADE,
    "At" TEXT NOT NULL,
    "Kind" TEXT NOT NULL,
    "Detail" TEXT NULL
);

CREATE INDEX IF NOT EXISTS "IX_Pursuits_Stage" ON "Pursuits" ("Stage");
CREATE INDEX IF NOT EXISTS "IX_Pursuits_ItemId" ON "Pursuits" ("ItemId");
CREATE INDEX IF NOT EXISTS "IX_PursuitEvents_PursuitId_At" ON "PursuitEvents" ("PursuitId", "At");
