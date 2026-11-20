-- Incoming pay packages pasted in for a pursuit (M9): the text as pasted, and the package read from it as YAML,
-- which the owner can correct. Comparisons are worked out on read, against the data pack's current package.
CREATE TABLE IF NOT EXISTS "Offers" (
    "Id" INTEGER NOT NULL CONSTRAINT "PK_Offers" PRIMARY KEY AUTOINCREMENT,
    "PursuitId" INTEGER NOT NULL CONSTRAINT "FK_Offers_Pursuits" REFERENCES "Pursuits" ("Id") ON DELETE CASCADE,
    "Text" TEXT NOT NULL,
    "Package" TEXT NOT NULL,
    "CreatedAt" TEXT NOT NULL,
    "UpdatedAt" TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS "IX_Offers_PursuitId" ON "Offers" ("PursuitId");
