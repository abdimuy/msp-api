package firebird_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	idempotencyfb "github.com/abdimuy/msp-api/internal/platform/idempotency/firebird"
)

// TestStore_ImplementsStoreInterface is a compile-time assertion that
// *idempotencyfb.Store satisfies idempotency.Store. The test body is empty;
// a build failure here means the interface contract was broken.
func TestStore_ImplementsStoreInterface(t *testing.T) {
	t.Parallel()
	var _ idempotency.Store = (*idempotencyfb.Store)(nil)
}
