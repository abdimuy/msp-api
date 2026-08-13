//nolint:misspell // Spanish domain vocabulary (recurso, zona, ventas, pagos) per project convention.
package domain

// RecursoSync identifies which incremental-sync stream an epoch applies to.
// The literals match MSP_CFG_SYNC_EPOCH.RECURSO — do not rename without a
// migration.
type RecursoSync string

// Canonical RecursoSync values, one per cursor-based sync endpoint that
// exposes `sync_epoch`.
const (
	// RecursoSyncVentas is the stream behind /cobranza/sync/ventas/zona/{id}.
	RecursoSyncVentas RecursoSync = "ventas"
	// RecursoSyncPagos is the stream behind /cobranza/sync/pagos/zona/{id}.
	RecursoSyncPagos RecursoSync = "pagos"
)

// IsValid reports whether r is one of the canonical values.
func (r RecursoSync) IsValid() bool {
	switch r {
	case RecursoSyncVentas, RecursoSyncPagos:
		return true
	}
	return false
}

// String returns the underlying string.
func (r RecursoSync) String() string { return string(r) }

// ZonaEpochGlobal is the sentinel ZONA_CLIENTE_ID of the row that applies to
// every zone. 0 is safe as a sentinel because Microsip zone IDs live in
// ZONAS_CLIENTES and are always positive (12271, 12272, …).
const ZonaEpochGlobal = 0

// EpochRow is one MSP_CFG_SYNC_EPOCH row, already narrowed to a single
// RECURSO by the repository query. Only the two columns the calculation needs
// are carried.
type EpochRow struct {
	// ZonaClienteID is the row's zone, or ZonaEpochGlobal for the global row.
	ZonaClienteID int
	// Epoch is the row's generation counter.
	Epoch int
}

// EpochEfectivo computes the effective sync epoch for zonaID out of the rows
// of a single recurso:
//
//	epoch_efectivo = EPOCH(global) + EPOCH(zona)
//
// Missing rows count as 0, so an empty table (or a database where the
// migration has not run) yields 0 and the sync keeps working unchanged — the
// epoch is a forcing lever, never a precondition.
//
// Adding instead of taking the max is deliberate: a global bump must move
// every zone forward even if that zone already has a per-zone bump of its
// own, and a per-zone bump must move only that zone. As long as callers only
// ever increment, the sum is monotonically non-decreasing, which is what the
// mobile client's "epoch grew → wipe cursor and resync" rule relies on.
//
// rows may arrive in any order and may contain rows for other zones (they are
// ignored). When zonaID happens to be ZonaEpochGlobal the global row is
// counted exactly once, not twice.
func EpochEfectivo(rows []EpochRow, zonaID int) int {
	total := 0
	for _, r := range rows {
		if r.ZonaClienteID == ZonaEpochGlobal {
			total += r.Epoch
			continue
		}
		if r.ZonaClienteID == zonaID {
			total += r.Epoch
		}
	}
	return total
}
