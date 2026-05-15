-- Failed-intent capture table.
--
-- Records every mutating HTTP request to the API that responded with 4xx/5xx.
-- Lets ops and end-users inspect "ventas zombies" — requests the mobile app
-- queued offline that the server rejected, plus the reason. An admin can
-- replay an intent from its captured body, or mark it ignored.
--
-- Per CLAUDE.md, all timestamps and ids are produced in Go; the database
-- has no DEFAULT now() or generators.
CREATE TABLE failed_intents (
    id              UUID         NOT NULL PRIMARY KEY,
    received_at     TIMESTAMPTZ  NOT NULL,
    method          TEXT         NOT NULL,                      -- POST | PATCH | PUT | DELETE
    path            TEXT         NOT NULL,                      -- request path verbatim, e.g. /v2/ventas
    firebase_uid    TEXT,                                       -- null when request bypassed authentication
    usuario_id      UUID,                                       -- resolved by the auth middleware when present
    idempotency_key TEXT,                                       -- null when caller did not send the header
    request_id      UUID         NOT NULL,                      -- X-Request-ID for cross-referencing logs
    body            JSONB        NOT NULL,                      -- captured request body (capped; see body_truncated)
    body_truncated  BOOLEAN      NOT NULL,                      -- true when body exceeded the capture cap
    http_status     INT          NOT NULL,                      -- 4xx or 5xx
    error_code      TEXT         NOT NULL,                      -- apperror code, e.g. "validation_failed"
    error_message   TEXT         NOT NULL,                      -- user-facing message (Spanish)
    retry_count     INT          NOT NULL DEFAULT 0,            -- times an admin replayed the intent
    status          TEXT         NOT NULL DEFAULT 'new',        -- new | retried_ok | retried_fail | ignored | resolved_manual
    resolved_at     TIMESTAMPTZ,
    resolved_by     UUID,                                       -- admin usuario_id when status moved away from 'new'
    notes           TEXT
);

-- Listing newest first is the dominant query path.
CREATE INDEX idx_failed_intents_received ON failed_intents (received_at DESC);

-- "Unresolved backlog" dashboards filter by status, then order by time.
CREATE INDEX idx_failed_intents_status_received ON failed_intents (status, received_at DESC);

-- "Carlos's failed intents" — partial index avoids bloat from anonymous rows.
CREATE INDEX idx_failed_intents_firebase ON failed_intents (firebase_uid, received_at DESC)
    WHERE firebase_uid IS NOT NULL;

-- Cross-reference from a log line (request_id) back to its captured payload.
CREATE INDEX idx_failed_intents_request_id ON failed_intents (request_id);
