//nolint:paralleltest,testpackage // Firebird integration test needs package access and runs serially.
package garfb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

type countingQuerier struct {
	base    firebird.Querier
	queries int
}

func (q *countingQuerier) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	q.queries++

	return q.base.ExecContext(ctx, query, args...)
}

func (q *countingQuerier) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (*sql.Rows, error) {
	q.queries++

	return q.base.QueryContext(ctx, query, args...)
}

func (q *countingQuerier) QueryRowContext(
	ctx context.Context,
	query string,
	args ...any,
) *sql.Row {
	q.queries++

	return q.base.QueryRowContext(ctx, query, args...)
}

func TestGarantiaRepo_ObtenerUsaDosConsultas(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := NewGarantiaRepo(pool)
	folios := NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("cliente")
		require.NoError(t, err)

		estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
		require.NoError(t, err)

		now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

		clienteID := 992001
		ventaID := 992002

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para contar consultas",
			Calle:          "Avenida Reforma",
			NumeroExterior: "20",
			Colonia:        "Centro",
			Localidad:      "Puebla",
			Ciudad:         "Puebla",
			CodigoPostal:   "72000",
			AbiertoPor:     "ruben",
			Now:            now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		for i, articuloID := range []int{992003, 992004, 992005} {
			err = g.AgregarArticulo(
				domain.AgregarArticuloParams{
					ArticuloID:  &articuloID,
					Clave:       []string{"Q-001", "Q-002", "Q-003"}[i],
					Description: []string{"Artículo uno", "Artículo dos", "Artículo tres"}[i],
				},
				domain.ActorParams{
					Usuario:           "ruben",
					ClaveIdempotencia: uuid.NewString(),
					DeviceCreatedAt:   now.Add(time.Duration(i+1) * time.Minute),
				},
				now.Add(time.Duration(i+1)*time.Minute),
			)
			require.NoError(t, err)
		}

		require.NoError(t, repo.Crear(ctx, g))

		tx, err := firebird.RequireTx(ctx)
		require.NoError(t, err)

		counter := &countingQuerier{base: tx}

		got, err := obtenerGarantia(
			ctx,
			counter,
			obtenerGarantiaSQL,
			g.ID().String(),
		)
		require.NoError(t, err)
		require.Len(t, got.ArticulosForRepo(), 3)

		require.Equal(t, 2, counter.queries)
	})
}
