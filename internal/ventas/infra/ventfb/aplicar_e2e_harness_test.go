//nolint:misspell // Spanish vocabulary (ventas, aplicar, cajero, etc.) by convention.
package ventfb_test

import (
	"context"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── recordingFakeOutbox ──────────────────────────────────────────────────────

// recordingFakeOutbox is a thread-safe in-memory OutboxEnqueuer that captures
// every Enqueue invocation so tests can inspect event types and payloads.
// Named distinctly from e2eRecordingOutbox (venthttp_test pkg) to avoid
// collision.
type recordingFakeOutbox struct {
	mu    sync.Mutex
	calls []fbRecordedEvent
}

type fbRecordedEvent struct {
	aggregate   string
	aggregateID uuid.UUID
	eventType   string
	payload     any
}

func (o *recordingFakeOutbox) Enqueue(_ context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, fbRecordedEvent{aggregate, aggregateID, eventType, payload})
	return nil
}

// snapshot returns a copy of recorded events.
func (o *recordingFakeOutbox) snapshot() []fbRecordedEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]fbRecordedEvent, len(o.calls))
	copy(out, o.calls)
	return out
}

// hasEvent reports whether any captured event has the given eventType.
func (o *recordingFakeOutbox) hasEvent(eventType string) bool {
	for _, ev := range o.snapshot() {
		if ev.eventType == eventType {
			return true
		}
	}
	return false
}

// hasEventForAggregate reports whether any captured event has the given
// eventType and aggregateID.
func (o *recordingFakeOutbox) hasEventForAggregate(eventType string, aggregateID uuid.UUID) bool {
	for _, ev := range o.snapshot() {
		if ev.eventType == eventType && ev.aggregateID == aggregateID {
			return true
		}
	}
	return false
}

// ─── harnessFixedClock ───────────────────────────────────────────────────────

// harnessFixedClock is an outbound.Clock implementation returning a fixed instant.
type harnessFixedClock struct{ t time.Time }

func (c harnessFixedClock) Now() time.Time { return c.t }

// ─── noopStorage ─────────────────────────────────────────────────────────────

// noopStorage is a StorageProvider that discards all blobs. Suitable for
// AplicarVenta tests that do not exercise image upload.
type noopStorage struct{}

func (noopStorage) Store(_ context.Context, _, _ string, _ int64, _ io.Reader) error {
	return nil
}

func (noopStorage) Get(_ context.Context, _ string) (outbound.StorageObject, error) {
	return outbound.StorageObject{}, nil
}
func (noopStorage) Delete(_ context.Context, _ string) error { return nil }

// ─── aplicarE2EHarness ───────────────────────────────────────────────────────

// aplicarE2EHarness wires a real ventasapp.Service with Firebird-backed repos
// for AplicarVenta E2E tests. All fakes are limited to: outbox (capturing),
// clock (fixed), storage (noop), imageProc (NoOp).
//
// The txMgr is purposely nil: tests run inside WithTestTransaction which
// injects the tx into the context via firebird.InjectTx. The service's
// runInTx wrapper short-circuits to fn(ctx) when txMgr==nil, so all
// reads/writes share the ambient rollback-only tx.
type aplicarE2EHarness struct {
	svc    *ventasapp.Service
	pool   *firebird.Pool
	q      firebird.Querier
	outbox *recordingFakeOutbox
	clock  outbound.Clock
}

// newAplicarE2EHarness builds the harness wired with REAL repos inside the
// given transactional context. Must be called from within WithTestTransaction.
func newAplicarE2EHarness(ctx context.Context, t *testing.T, pool *firebird.Pool) *aplicarE2EHarness {
	t.Helper()
	outbox := &recordingFakeOutbox{}
	clock := harnessFixedClock{t: testNow()}
	repo := ventfb.NewVentaRepo(pool)
	cfg := ventfb.NewAplicarConfigRepo(pool)
	writer := microsip.NewVentaWriter(pool)
	svc := ventasapp.NewService(
		repo, nil, nil,
		noopStorage{},
		clock,
		outbox,
		imageprocessor.NoOpProcessor{},
		nil, // txMgr nil — ambient WithTestTransaction tx is used
		cfg,
		writer,
		nil, // microsipCliente nil — AplicarVenta E2E tests use ventas with known ClienteID
	)
	return &aplicarE2EHarness{
		svc:    svc,
		pool:   pool,
		q:      firebird.GetQuerier(ctx, pool.DB),
		outbox: outbox,
		clock:  clock,
	}
}

// persistAprobada persists a venta that is already in APROBADA state (built
// via one of the buildAplicar* helpers) by writing it to MSP_VENTAS via the
// repo.Save path, then advancing its state through EnviarARevision and
// Aprobar on the in-memory aggregate (state transitions already applied by
// the builder), then calling repo.Update so the persisted row reflects the
// aprobada situacion.
//
// The userID passed to the builder must match a pre-seeded MSP_USUARIOS row
// (use seedUsuarioRow before calling this).
//
// Returns the venta UUID.
func (h *aplicarE2EHarness) persistAprobada(ctx context.Context, t *testing.T, v *domain.Venta) uuid.UUID {
	t.Helper()
	repo := ventfb.NewVentaRepo(h.pool)
	// Save initial state (aggregate already in aprobada after builder called
	// EnviarARevision + Aprobar on the domain object).
	require.NoError(t, repo.Save(ctx, v), "persist venta en MSP_VENTAS")
	// The domain aggregate already has situacion=APROBADA; Update persists the
	// current state (including SITUACION and APROBACION columns).
	require.NoError(t, repo.Update(ctx, v), "update venta situacion a aprobada")
	return v.ID()
}

// ─── seedClienteFixture ──────────────────────────────────────────────────────

// seedClienteFixture guarantees that testClienteID exists in CLIENTES with its
// CLAVES_CLIENTES row, inserting it when missing.
//
// It must run inside the test transaction, so nothing persists.
//
// WHY: testClienteID used to be a row of the production padrón. The test
// artifact no longer ships that padrón (docs/base-de-datos-de-pruebas.md), and
// without the row the writer fails with clave_cliente_not_found — the CLAVE is
// read from CLAVES_CLIENTES before the venta is inserted.
//
// It is idempotent on purpose: against the full dev DB the row is already
// there and this is a no-op, so the same test passes against both databases.
func seedClienteFixture(t *testing.T, q firebird.Querier) {
	t.Helper()
	ctx := context.Background()

	var n int
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM CLIENTES WHERE CLIENTE_ID = ?`, testClienteID,
		).Scan(&n),
		"contar CLIENTES fixture")
	if n == 0 {
		var condPagoID int
		require.NoError(t,
			q.QueryRowContext(ctx,
				`SELECT FIRST 1 COND_PAGO_ID FROM CONDICIONES_PAGO ORDER BY COND_PAGO_ID`,
			).Scan(&condPagoID),
			"CONDICIONES_PAGO debe tener al menos una fila")

		_, err := q.ExecContext(ctx,
			`INSERT INTO CLIENTES
			   (CLIENTE_ID, NOMBRE, SUJETO_IEPS, DIFERIR_CFDI_COBROS,
			    MONEDA_ID, COND_PAGO_ID, ESTATUS, ZONA_CLIENTE_ID)
			 VALUES (?, 'CLIENTE FIXTURE E2E', 'N', FALSE, 1, ?, 'A', ?)`,
			testClienteID, condPagoID, testZonaID)
		require.NoError(t, err, "insertar CLIENTES fixture")
	}

	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM CLAVES_CLIENTES WHERE CLIENTE_ID = ?`, testClienteID,
		).Scan(&n),
		"contar CLAVES_CLIENTES fixture")
	if n == 0 {
		var rolClaveID int
		require.NoError(t,
			q.QueryRowContext(ctx,
				`SELECT FIRST 1 ROL_CLAVE_CLI_ID FROM ROLES_CLAVES_CLIENTES ORDER BY ROL_CLAVE_CLI_ID`,
			).Scan(&rolClaveID),
			"ROLES_CLAVES_CLIENTES debe tener al menos una fila")

		_, err := q.ExecContext(ctx,
			`INSERT INTO CLAVES_CLIENTES
			   (CLAVE_CLIENTE_ID, CLAVE_CLIENTE, CLIENTE_ID, ROL_CLAVE_CLI_ID)
			 VALUES (-1, ?, ?, ?)`,
			strconv.Itoa(testClienteID), testClienteID, rolClaveID)
		require.NoError(t, err, "insertar CLAVES_CLIENTES fixture")
	}
}

// ─── requireCatalog ──────────────────────────────────────────────────────────

// requireCatalog verifies that the catalog IDs hardcoded in
// e2e_firebird_aplicar_test.go exist in the current DB snapshot and that the
// MSP_CFG_* config tables are populated. If any check fails, the calling test
// is skipped with a clear reason.
//
// Call at the top of every test that depends on these constants.
func requireCatalog(t *testing.T, q firebird.Querier) {
	t.Helper()
	ctx := context.Background()

	// El cliente ya no es un catálogo: se siembra. Ver seedClienteFixture.
	seedClienteFixture(t, q)

	checks := []struct {
		label string
		query string
		args  []any
	}{
		{
			label: "ARTICULOS testArticuloID",
			query: `SELECT COUNT(*) FROM ARTICULOS WHERE ARTICULO_ID = ?`,
			args:  []any{testArticuloID},
		},
		{
			label: "ARTICULOS testArticuloID16Pct",
			query: `SELECT COUNT(*) FROM ARTICULOS WHERE ARTICULO_ID = ?`,
			args:  []any{testArticuloID16Pct},
		},
		{
			label: "MSP_CFG_ZONA_CAJA testZonaID",
			query: `SELECT COUNT(*) FROM MSP_CFG_ZONA_CAJA WHERE ZONA_CLIENTE_ID = ?`,
			args:  []any{testZonaID},
		},
		{
			label: "MSP_CFG_APLICAR singleton",
			query: `SELECT COUNT(*) FROM MSP_CFG_APLICAR WHERE ID = 1`,
			args:  nil,
		},
	}

	for _, c := range checks {
		var n int
		var err error
		if len(c.args) > 0 {
			err = q.QueryRowContext(ctx, c.query, c.args...).Scan(&n)
		} else {
			err = q.QueryRowContext(ctx, c.query).Scan(&n)
		}
		if err != nil {
			t.Skipf("catalog drift: cannot query %s: %v", c.label, err)
		}
		if n == 0 {
			t.Skipf("catalog drift: %s missing from DB snapshot; refresh DB", c.label)
		}
	}
}
