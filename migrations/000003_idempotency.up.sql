-- Idempotency keys cache (24h TTL).
--
-- A janitor job in Go deletes rows where expires_at < now(). The DB does NOT
-- expire them automatically (see CLAUDE.md, "No logic in the database").
-- All timestamps are set explicitly by Go.
CREATE TABLE idempotency_keys (
    key             TEXT         NOT NULL PRIMARY KEY,
    user_id         UUID,
    method          TEXT         NOT NULL,
    path            TEXT         NOT NULL,
    request_hash    TEXT         NOT NULL,           -- SHA-256 hex of request body
    response_status INT          NOT NULL,
    response_body   JSONB        NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL,
    expires_at      TIMESTAMPTZ  NOT NULL
);

CREATE INDEX idx_idempotency_expires ON idempotency_keys (expires_at);
