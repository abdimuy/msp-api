-- failed_intents: optional pointer columns for bodies that are too large or
-- too binary to store inline.
--
-- When CaptureMiddleware sees a multipart/form-data request (the /v2/ventas
-- handler accepts images alongside the JSON payload), the body cannot fit in
-- the inline `body` TEXT column — it would either truncate over the cap or
-- pollute the row with base64 noise. Instead, the middleware streams the body
-- to a sibling blob under STORAGE_DIR/failed-intents/<id>.bin and records the
-- absolute path in body_blob_path, plus the original Content-Type (with the
-- multipart boundary preserved) in body_content_type.
--
-- Both columns are nullable so the legacy JSON-inline capture path is
-- untouched. Per CLAUDE.md the schema is structural only: the rule "body or
-- body_blob_path must be populated" lives in Go (failedintent.Intent
-- validation + middleware branch).
ALTER TABLE failed_intents
    ADD COLUMN body_blob_path     TEXT,
    ADD COLUMN body_content_type  TEXT;
