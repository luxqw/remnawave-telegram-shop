ALTER TABLE customer ADD COLUMN tribute_autorenew_paused BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE customer ADD COLUMN tribute_autorenew_streak INTEGER NOT NULL DEFAULT 0;
