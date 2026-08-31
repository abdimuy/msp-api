//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"context"
	"database/sql"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Compile-time assertion: RutasRepo satisfies the outbound port.
var _ outbound.RutasRepo = (*RutasRepo)(nil)

// RutasRepo is the Firebird-backed implementation of outbound.RutasRepo.
// Reads ZONAS_CLIENTES, COBRADORES, CLIENTES, MSP_CFG_ZONA_CAJA, and
// MSP_SALDOS_VENTAS — no MSP_* tables are written.
type RutasRepo struct {
	pool *firebird.Pool
}

// NewRutasRepo builds a RutasRepo wired to the given pool.
func NewRutasRepo(pool *firebird.Pool) *RutasRepo {
	return &RutasRepo{pool: pool}
}

// ListarRutas returns all zonas ordered by name with cobrador, active client
// count, and total outstanding balance.
func (r *RutasRepo) ListarRutas(ctx context.Context) ([]rutasdomain.RutaResumen, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, queryListarRutas)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []rutasdomain.RutaResumen
	for rows.Next() {
		item, serr := scanRutaResumen(rows)
		if serr != nil {
			return nil, firebird.MapError(serr)
		}
		result = append(result, item)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return result, nil
}

// scannable abstracts *sql.Row and *sql.Rows so scan helpers work for both.
type scannable interface {
	Scan(dest ...any) error
}

// rutaRowRaw holds the raw scan targets for one rutas row.
//
// The two name columns do NOT share a charset: ZONAS_CLIENTES.NOMBRE is
// CHARACTER SET NONE (raw Windows-1252 bytes, decoded here) while
// COBRADORES.NOMBRE is CHARACTER SET ISO8859_1 (already transliterated to
// UTF-8 by Firebird for the charset=UTF8 connection).
type rutaRowRaw struct {
	zonaID        int
	zonaNombreRaw firebird.Win1252 // ZONAS_CLIENTES.NOMBRE — NONE
	cobradorIDRaw sql.NullInt64
	cobrNombre    sql.NullString // COBRADORES.NOMBRE — ISO8859_1; LEFT JOIN → nullable
	numClientes   int
	saldoTotalRaw any // NUMERIC(18,2) after CAST; use firebird.ScanDecimal.
}

func scanRutaResumen(s scannable) (rutasdomain.RutaResumen, error) {
	var raw rutaRowRaw
	if err := s.Scan(
		&raw.zonaID,
		&raw.zonaNombreRaw,
		&raw.cobradorIDRaw,
		&raw.cobrNombre,
		&raw.numClientes,
		&raw.saldoTotalRaw,
	); err != nil {
		return rutasdomain.RutaResumen{}, err
	}

	saldo, err := firebird.ScanDecimal(raw.saldoTotalRaw, 2)
	if err != nil {
		return rutasdomain.RutaResumen{}, err
	}

	// COBRADOR_ID is nullable (LEFT JOIN on cfg) and -1 means "sin asignar"
	// in Microsip convention.
	var cobradorID *int
	cobradorNombre := ""
	if raw.cobradorIDRaw.Valid && raw.cobradorIDRaw.Int64 != -1 {
		v := int(raw.cobradorIDRaw.Int64)
		cobradorID = &v
		cobradorNombre = raw.cobrNombre.String
	}

	return rutasdomain.RutaResumen{
		ZonaID:         raw.zonaID,
		ZonaNombre:     string(raw.zonaNombreRaw),
		CobradorID:     cobradorID,
		CobradorNombre: cobradorNombre,
		NumClientes:    raw.numClientes,
		SaldoTotal:     saldo,
	}, nil
}
