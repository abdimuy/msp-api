package outbound

import (
	"context"
	"time"
)

// CursorRepo remembers how far the payment changelog has been walked.
//
// The cursor is the guarantee, not the notification: POST_EVENT only wakes
// the worker earlier. If the cursor is lost or rolled back, receipts are
// re-enqueued, never skipped.
type CursorRepo interface {
	Leer(ctx context.Context) (int64, error)
	Guardar(ctx context.Context, seqID int64, now time.Time) error
}
