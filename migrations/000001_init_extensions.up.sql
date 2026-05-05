-- Index-only Postgres extension. We do NOT install pgcrypto / uuid-ossp:
-- UUIDs and timestamps are generated in Go (see CLAUDE.md, "No logic in
-- the database"). btree_gin only adds index types, not behavior.
CREATE EXTENSION IF NOT EXISTS "btree_gin";
