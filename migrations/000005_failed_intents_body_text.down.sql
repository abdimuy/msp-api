-- Reverts failed_intents.body from TEXT back to JSONB. The USING cast
-- will fail if any row carries non-JSON bytes — only run this rollback
-- when the application is on a binary that still produces JSON-valid
-- bodies (which is the case as long as normaliseBody is in place).
ALTER TABLE failed_intents
    ALTER COLUMN body TYPE JSONB USING body::jsonb;
