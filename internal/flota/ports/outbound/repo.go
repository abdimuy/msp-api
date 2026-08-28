package outbound

import (
	"context"
	"time"

	flotadomain "github.com/abdimuy/msp-api/internal/flota/domain"
)

// Repo persists the flota module's three tables: the mirror of the last
// observed roster (MSP_FLOTA_ASIGNACION_ACTUAL), the append-only history
// (MSP_FLOTA_ASIGNACION_CAMBIOS) and the snapshots ledger (MSP_FLOTA_FOTOS).
//
// There is no read method for the history on purpose. The module's query
// surface is SQL — see the package doc of internal/flota/app — so a
// ListarCambios here would be code with no caller, and code with no caller
// stops being tested honestly the day someone changes it.
type Repo interface {
	// UltimaFoto returns when the most recent snapshot ran. The bool is false
	// when the ledger is empty, which is the module's definition of "we have
	// never looked" and therefore of the baseline run.
	UltimaFoto(ctx context.Context) (time.Time, bool, error)
	// ListarAsignaciones returns the whole mirror — the state the incoming
	// photograph is compared against.
	ListarAsignaciones(ctx context.Context) ([]*flotadomain.Asignacion, error)
	// InsertarAsignaciones adds people the mirror had never seen.
	InsertarAsignaciones(ctx context.Context, altas []*flotadomain.Asignacion) error
	// ActualizarAsignaciones overwrites the mirror for people whose observed
	// state changed.
	ActualizarAsignaciones(ctx context.Context, cambiadas []*flotadomain.Asignacion) error
	// EliminarAsignaciones removes the people who left the roster. Their
	// history rows are untouched — that is why there is no foreign key.
	EliminarAsignaciones(ctx context.Context, uids []string) error
	// InsertarCambios appends history rows.
	InsertarCambios(ctx context.Context, cambios []*flotadomain.Cambio) error
	// RegistrarFoto appends the snapshots-ledger row for this pass.
	RegistrarFoto(ctx context.Context, foto *flotadomain.Foto) error
}

// TxRunner is the transaction boundary. A snapshot writes the mirror, the
// history and the ledger row in ONE transaction: applying the mirror without
// its history would advance the "before" past a change nobody recorded, and
// that change is then unrecoverable — Firestore keeps no history, which is
// the entire reason this module exists.
type TxRunner interface {
	// RunInTx runs fn inside a transaction, committing on success and rolling
	// back on error.
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock abstracts time.Now for deterministic tests. Implementations return
// UTC, per docs/module-standards/DATETIME_HANDLING.md.
type Clock interface {
	// Now returns the current instant in UTC.
	Now() time.Time
}

// ProductionClock is the real-world Clock.
type ProductionClock struct{}

// Now returns time.Now() in UTC.
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
