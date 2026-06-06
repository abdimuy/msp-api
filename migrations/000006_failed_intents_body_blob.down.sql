-- Drops the blob-pointer columns. DESTRUCTIVE for multipart captures: any
-- row whose body lived on disk loses its filesystem pointer and the on-disk
-- blob becomes an orphan that the boot-time sweep will clean up.
ALTER TABLE failed_intents
    DROP COLUMN body_blob_path,
    DROP COLUMN body_content_type;
