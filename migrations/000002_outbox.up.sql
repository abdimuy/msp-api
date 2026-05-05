-- Transactional outbox: durable record of events that need to be propagated
-- to external systems (Microsip, etc.). Inserted in the same tx as the
-- business write; processed by the outbox dispatcher with at-least-once
-- semantics.
--
-- IMPORTANT: this table has NO column defaults. id, created_at, processed_at,
-- failed_at, attempts are all set explicitly by Go (see CLAUDE.md).
CREATE TABLE outbox_events (
    id           UUID         NOT NULL PRIMARY KEY,
    aggregate    TEXT         NOT NULL,             -- 'cliente', 'venta_local', 'pago', ...
    aggregate_id UUID         NOT NULL,
    event_type   TEXT         NOT NULL,             -- 'push_to_microsip', 'mark_synced', ...
    payload      JSONB        NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL,
    processed_at TIMESTAMPTZ,
    failed_at    TIMESTAMPTZ,
    error        TEXT,
    attempts     INT          NOT NULL
);

-- Pending events queue index. Partial index keeps it tiny once events are
-- processed.
CREATE INDEX idx_outbox_pending
    ON outbox_events (created_at)
    WHERE processed_at IS NULL AND failed_at IS NULL;

-- For dead-letter inspection.
CREATE INDEX idx_outbox_failed
    ON outbox_events (failed_at DESC)
    WHERE failed_at IS NOT NULL;

-- For aggregate-level debugging.
CREATE INDEX idx_outbox_aggregate
    ON outbox_events (aggregate, aggregate_id);
