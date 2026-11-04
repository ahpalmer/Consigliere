-- Answers for application forms that come with an apply kit: a JSON list of {"label", "text"}.
ALTER TABLE "Drafts" ADD COLUMN "Answers" TEXT NULL;

-- Small key/value state, e.g. one-time jobs that have already run.
CREATE TABLE IF NOT EXISTS "AppState" (
    "Key" TEXT NOT NULL CONSTRAINT "PK_AppState" PRIMARY KEY,
    "Value" TEXT NOT NULL
);
