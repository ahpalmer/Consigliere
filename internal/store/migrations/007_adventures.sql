-- Adventures (M13): an idea bank, and adventures planned from it (or from scratch), with reminders and a rating after.
CREATE TABLE IF NOT EXISTS "AdventureIdeas" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_AdventureIdeas" PRIMARY KEY AUTOINCREMENT,
    "Title" TEXT NOT NULL,
    "Description" TEXT NOT NULL DEFAULT '',
    "Place" TEXT NOT NULL DEFAULT '',
    -- "$" to "$$$$", or '' when unknown.
    "Cost" TEXT NOT NULL DEFAULT '',
    -- Free text: "an evening", "a day", "a weekend".
    "Duration" TEXT NOT NULL DEFAULT '',
    -- Comma-separated.
    "Tags" TEXT NOT NULL DEFAULT '',
    -- manual | generated
    "Source" TEXT NOT NULL DEFAULT 'manual',
    "Favorite" INTEGER NOT NULL DEFAULT 0,
    "CreatedAt" TEXT NOT NULL,
    "ArchivedAt" TEXT NULL
);

CREATE TABLE IF NOT EXISTS "Adventures" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_Adventures" PRIMARY KEY AUTOINCREMENT,
    "IdeaId" INTEGER NULL CONSTRAINT "FK_Adventures_AdventureIdeas" REFERENCES "AdventureIdeas" ("Id") ON DELETE SET NULL,
    "Title" TEXT NOT NULL,
    "Place" TEXT NOT NULL DEFAULT '',
    "Notes" TEXT NOT NULL DEFAULT '',
    -- The day, YYYY-MM-DD in the owner's time zone, and an optional start time, HH:MM.
    "Date" TEXT NOT NULL,
    "Time" TEXT NOT NULL DEFAULT '',
    -- Something to do beforehand (book, buy tickets) and the day to do it by.
    "PrepNote" TEXT NOT NULL DEFAULT '',
    "PrepDue" TEXT NOT NULL DEFAULT '',
    -- planned | done | skipped
    "Status" TEXT NOT NULL DEFAULT 'planned',
    -- 1-5 stars, and a line about how it went.
    "Rating" INTEGER NULL,
    "Review" TEXT NOT NULL DEFAULT '',
    "CreatedAt" TEXT NOT NULL,
    "CompletedAt" TEXT NULL,
    -- The days each reminder went out, so each goes once.
    "PrepRemindedOn" TEXT NULL,
    "DayBeforeRemindedOn" TEXT NULL,
    "DayOfRemindedOn" TEXT NULL,
    "RatingAskedOn" TEXT NULL
);

CREATE INDEX IF NOT EXISTS "IX_Adventures_Date" ON "Adventures" ("Status", "Date");
