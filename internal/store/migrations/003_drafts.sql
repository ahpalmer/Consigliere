-- Model-written drafts (apply kits, prep packs, thank-you notes), kept so the dashboard can show them again.
CREATE TABLE IF NOT EXISTS "Drafts" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_Drafts" PRIMARY KEY AUTOINCREMENT,
    "PursuitId" INTEGER NULL CONSTRAINT "FK_Drafts_Pursuits" REFERENCES "Pursuits" ("Id") ON DELETE CASCADE,
    -- Set when the draft was written for a radar item (with or without a pursuit).
    "ItemId" INTEGER NULL CONSTRAINT "FK_Drafts_RadarItems" REFERENCES "RadarItems" ("Id"),
    -- kit | prep | thanks
    "Kind" TEXT NOT NULL,
    "Title" TEXT NOT NULL,
    -- pending | ready | failed
    "Status" TEXT NOT NULL,
    "Text" TEXT NOT NULL DEFAULT '',
    "Error" TEXT NULL,
    -- The tailored resume (DOCX) that comes with a kit.
    "FileName" TEXT NULL,
    "File" BLOB NULL,
    "CreatedAt" TEXT NOT NULL,
    "FinishedAt" TEXT NULL
);

CREATE INDEX IF NOT EXISTS "IX_Drafts_PursuitId" ON "Drafts" ("PursuitId");
CREATE INDEX IF NOT EXISTS "IX_Drafts_ItemId" ON "Drafts" ("ItemId");
