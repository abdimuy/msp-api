//nolint:paralleltest // Firebird integration tests must run serially.
package garfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

func TestEventoRepo_ListarYObtenerPorClave(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	eventoRepo := garfb.NewEventoRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("cliente")
		require.NoError(t, err)

		estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
		require.NoError(t, err)

		now := time.Date(2026, 10, 2, 16, 0, 0, 0, time.UTC)

		clienteID := 910001
		ventaID := 910002

		claveApertura := uuid.NewString()

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar eventos",
			Calle:          "Calle Reforma",
			NumeroExterior: "10",
			Colonia:        "Centro",
			Localidad:      "Puebla",
			Ciudad:         "Puebla",
			CodigoPostal:   "72000",
			AbiertoPor:     "ruben",
			Now:            now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveApertura,
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		articuloID := 910003
		claveArticulo := uuid.NewString()

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloID,
				Clave:       "TEST-EVENTO",
				Description: "Artículo para probar timeline",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveArticulo,
				DeviceCreatedAt:   now.Add(time.Minute),
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		eventos, err := eventoRepo.ListarPorGarantia(ctx, g.ID())
		require.NoError(t, err)
		require.Len(t, eventos, 2)

		require.Equal(t, claveApertura, eventos[0].ClaveIdempotencia())
		require.Equal(t, claveArticulo, eventos[1].ClaveIdempotencia())

		encontrado, err := eventoRepo.ObtenerPorClaveIdempotencia(
			ctx,
			claveArticulo,
		)
		require.NoError(t, err)
		require.NotNil(t, encontrado)

		require.Equal(t, g.ID(), encontrado.GarantiaID())
		require.Equal(t, claveArticulo, encontrado.ClaveIdempotencia())

		inexistente, err := eventoRepo.ObtenerPorClaveIdempotencia(
			ctx,
			uuid.NewString(),
		)
		require.NoError(t, err)
		require.Nil(t, inexistente)
	})
}

func TestEventoRepo_ListarOrdenadoPorDeviceCreatedAt(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	eventoRepo := garfb.NewEventoRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("cliente")
		require.NoError(t, err)

		estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
		require.NoError(t, err)

		now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)

		clienteID := 990001
		ventaID := 990002
		articuloMicrosipID := 990003

		claveFolio := uuid.NewString()
		claveArticulo := uuid.NewString()

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar orden de eventos",
			Calle:          "Calle 9",
			NumeroExterior: "900",
			Colonia:        "Centro",
			Localidad:      "Puebla",
			Ciudad:         "Puebla",
			CodigoPostal:   "72000",
			AbiertoPor:     "ruben",
			Now:            now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveFolio,

				// This event is created first,
				// but the device reports a later timestamp.
				DeviceCreatedAt: now.Add(3 * time.Minute),
			},
		})
		require.NoError(t, err)

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloMicrosipID,
				Clave:       "ORDEN-001",
				Description: "Artículo para probar orden",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveArticulo,

				// This event is created later but has an earlier device timestamp.
				DeviceCreatedAt: now.Add(time.Minute),
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		// Reload before generating a third event.
		cargada, err := garantiaRepo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		articulos := cargada.ArticulosForRepo()
		require.Len(t, articulos, 1)

		claveAvance := uuid.NewString()

		err = cargada.AvanzarArticulo(
			articulos[0].ID(),
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveAvance,

				// Its device timestamp should place it between the other two.
				DeviceCreatedAt: now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, garantiaRepo.Guardar(ctx, cargada))

		eventos, err := eventoRepo.ListarPorGarantia(ctx, g.ID())
		require.NoError(t, err)
		require.Len(t, eventos, 3)

		// Expected order by DEVICE_CREATED_AT:
		// 1 minute -> article
		// 2 minutes -> stage advance
		// 3 minutes -> folio opened.
		require.Equal(
			t,
			claveArticulo,
			eventos[0].ClaveIdempotencia(),
		)

		require.Equal(
			t,
			claveAvance,
			eventos[1].ClaveIdempotencia(),
		)

		require.Equal(
			t,
			claveFolio,
			eventos[2].ClaveIdempotencia(),
		)

		require.True(
			t,
			eventos[0].DeviceCreatedAt().Before(
				eventos[1].DeviceCreatedAt(),
			),
		)

		require.True(
			t,
			eventos[1].DeviceCreatedAt().Before(
				eventos[2].DeviceCreatedAt(),
			),
		)
	})
}
