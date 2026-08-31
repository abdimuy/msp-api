-- Schema for the canal module's durable SQLite mailbox
-- (internal/canal/infra/canalsqlite). Embedded into the binary via
-- //go:embed and executed by Repo's constructors on every start — every
-- statement is idempotent so re-running it against a database that already
-- has the schema (the VPS restarting with the file still on disk) is a
-- no-op.
--
-- Structural only, by design (docs/adr/0010, global-constraints.md #10):
-- no AUTOINCREMENT, no DEFAULT, no triggers, no computed columns. The id
-- comes from uuid.New() in Go and every timestamp from the injected Clock —
-- never from SQLite itself.
--
-- Every *_en / *_at column holds RFC3339Nano UTC as TEXT. This is a
-- deliberate deviation from docs/module-standards/DATETIME_HANDLING.md's
-- firebird.ToWallClock/ScanUTCTime convention: those exist only for the
-- Delphi/Microsip client reading Firebird wall-clock CDMX, and applying
-- them to this SQLite file would be an active bug (global-constraints.md,
-- "Desviaciones conscientes").

CREATE TABLE IF NOT EXISTS mensajes_entrantes (
    id              TEXT NOT NULL PRIMARY KEY,
    wamid           TEXT NOT NULL,
    remitente       TEXT NOT NULL,
    phone_number_id TEXT NOT NULL,
    tipo            TEXT NOT NULL,
    contenido       TEXT NOT NULL,
    timestamp_meta  TEXT NOT NULL,
    recibido_en     TEXT NOT NULL,
    estado          TEXT NOT NULL,
    motivo_fallo    TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

-- The storage-level dedup Guardar is built on: Meta redelivers webhooks
-- at-least-once, keyed by its own message id (wamid), and an INSERT that
-- does nothing on conflict is what makes Guardar idempotent.
CREATE UNIQUE INDEX IF NOT EXISTS ux_mensajes_entrantes_wamid
    ON mensajes_entrantes (wamid);

-- Serves ListarPendientes: a WHERE on estado ordered by recibido_en.
CREATE INDEX IF NOT EXISTS ix_mensajes_entrantes_estado_recibido_en
    ON mensajes_entrantes (estado, recibido_en);
