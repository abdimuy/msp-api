package outbound

import (
	"context"
	"time"
)

// ChangelogEntry is one row from MSP_PAGOS_CHANGELOG or MSP_SALDOS_CHANGELOG.
// PK refers to IMPTE_DOCTO_CC_ID (pagos) or DOCTO_CC_ID (saldos).
type ChangelogEntry struct {
	SeqID    int64
	PK       int
	TxID     int64
	CommitAt time.Time
}

// PagosChangelogRepo reads / prunes MSP_PAGOS_CHANGELOG.
//
// Cursor protocol: callers pass sinceSeq (exclusive lower bound on SEQ_ID)
// and watermark (exclusive upper bound on TX_ID). The returned entries are
// strictly ordered by SEQ_ID ascending and capped at limit.
//
// The watermark excludes rows written by in-flight transactions, which lets
// callers safely advance their lastSeenSeq without losing rows from
// long-running Microsip GUI transactions that haven't committed yet.
type PagosChangelogRepo interface {
	// Since returns up to limit rows where SEQ_ID > sinceSeq AND TX_ID < watermark,
	// ordered by SEQ_ID ascending. limit must be > 0.
	Since(ctx context.Context, sinceSeq, watermark int64, limit int) ([]ChangelogEntry, error)

	// DeleteOlderThan removes rows where COMMIT_AT < cutoff, capped at maxDelete
	// per call to avoid lock escalation. Returns the number of rows deleted.
	DeleteOlderThan(ctx context.Context, cutoff time.Time, maxDelete int) (int, error)

	// MaxSeqID returns the highest SEQ_ID where TX_ID < watermark, or 0 if the
	// table is empty / all rows are above watermark. Used at listener startup
	// to position the cursor at "everything visible right now".
	MaxSeqID(ctx context.Context, watermark int64) (int64, error)
}

// SaldosChangelogRepo is the analogous port for MSP_SALDOS_CHANGELOG.
type SaldosChangelogRepo interface {
	Since(ctx context.Context, sinceSeq, watermark int64, limit int) ([]ChangelogEntry, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time, maxDelete int) (int, error)
	MaxSeqID(ctx context.Context, watermark int64) (int64, error)
}
