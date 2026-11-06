//nolint:paralleltest // prueba de integración Firebird; usa filas reales y cleanup.
package garfb

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

type detalleQueryIdentity struct{}

func (detalleQueryIdentity) UsuarioActual(
	_ context.Context,
) (outbound.Usuario, error) {
	return outbound.Usuario{
		ID:     "query-count",
		Nombre: "Ruben",
	}, nil
}

func (detalleQueryIdentity) TienePermiso(
	_ context.Context,
	_ domain.Permiso,
) (bool, error) {
	return true, nil
}

func TestConsultas_DetalleUsaCuatroConsultasConVariasImagenes(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantias := NewGarantiaRepo(pool)
	imagenes := NewImagenRepo(pool)
	folios := NewFolioGenerator(pool)
	txManager := firebird.NewTxManager(pool.DB)

	ctx := context.Background()
	now := time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)

	numero, err := folios.Siguiente(ctx)
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       folio,
		Origen:      domain.OrigenFolioPiso,
		Description: "Garantía para contar detalle completo",
		AbiertoPor:  "ruben",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	articuloID := 991071

	err = g.AgregarArticulo(
		domain.AgregarArticuloParams{
			ArticuloID:  &articuloID,
			Clave:       "COUNT-D5",
			Description: "Artículo para contar consultas",
		},
		domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Minute),
		},
		now.Add(time.Minute),
	)
	require.NoError(t, err)

	eventos := g.EventosPendientesForRepo()
	require.GreaterOrEqual(t, len(eventos), 2)

	imagenUno, err := domain.NewImagen(domain.NewImagenParams{
		EventoID:    eventos[0].ID(),
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        "garantias/query-count/uno.png",
		Description: "Primera evidencia",
		SubidaPor:   "ruben",
		CreatedAt:   now.Add(2 * time.Minute),
	})
	require.NoError(t, err)

	imagenDos, err := domain.NewImagen(domain.NewImagenParams{
		EventoID:    eventos[1].ID(),
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        "garantias/query-count/dos.jpg",
		Description: "Segunda evidencia",
		SubidaPor:   "ruben",
		CreatedAt:   now.Add(3 * time.Minute),
	})
	require.NoError(t, err)

	err = txManager.RunInTx(ctx, func(txCtx context.Context) error {
		if err := garantias.Crear(txCtx, g); err != nil {
			return err
		}
		if err := imagenes.Registrar(txCtx, imagenUno); err != nil {
			return err
		}
		return imagenes.Registrar(txCtx, imagenDos)
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupErr := txManager.RunInTx(
			context.Background(),
			func(cleanCtx context.Context) error {
				q := firebird.GetQuerier(cleanCtx, pool.DB)

				if _, err := q.ExecContext(
					cleanCtx,
					`DELETE FROM MSP_GA_IMAGEN
					 WHERE EVENTO_ID IN (
					   SELECT ID FROM MSP_GA_EVENTO WHERE GARANTIA_ID = ?
					 )`,
					g.ID().String(),
				); err != nil {
					return err
				}

				if _, err := q.ExecContext(
					cleanCtx,
					"DELETE FROM MSP_GA_EVENTO WHERE GARANTIA_ID = ?",
					g.ID().String(),
				); err != nil {
					return err
				}

				if _, err := q.ExecContext(
					cleanCtx,
					"DELETE FROM MSP_GA_ARTICULO WHERE GARANTIA_ID = ?",
					g.ID().String(),
				); err != nil {
					return err
				}

				_, err := q.ExecContext(
					cleanCtx,
					"DELETE FROM MSP_GA_GARANTIA WHERE ID = ?",
					g.ID().String(),
				)
				return err
			},
		)

		require.NoError(t, cleanupErr)
	})

	contadorGarantia := &countingQuerier{}
	contadorImagenes := &countingQuerier{}

	garantias.decorateQuerier = func(q firebird.Querier) firebird.Querier {
		contadorGarantia.base = q
		return contadorGarantia
	}

	imagenes.decorateQuerier = func(q firebird.Querier) firebird.Querier {
		contadorImagenes.base = q
		return contadorImagenes
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Imagenes:  imagenes,
		Identity:  detalleQueryIdentity{},
	})

	detalle, err := consultas.Detalle(ctx, g.ID())
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(detalle.Eventos), 2)
	require.Len(t, detalle.ImagenesPorEvento[eventos[0].ID()], 1)
	require.Len(t, detalle.ImagenesPorEvento[eventos[1].ID()], 1)

	// El contrato de lectura debe mantenerse fijo:
	// garantía + artículos + eventos + todas las imágenes.
	require.Equal(
		t,
		4,
		contadorGarantia.queries+contadorImagenes.queries,
		"el detalle debe mantenerse en cuatro consultas fijas",
	)

	// Garantía + artículos + eventos.
	require.Equal(t, 3, contadorGarantia.queries)

	// Todas las imágenes de todos los eventos en una sola consulta.
	require.Equal(t, 1, contadorImagenes.queries)
}
