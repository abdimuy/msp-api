//nolint:paralleltest // Firebird integration tests must run serially.
package garfb_test

import (
	"context"
	"fmt"
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

		claveFolio := uuid.NewString()

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
				DeviceCreatedAt:   now.Add(20 * time.Minute),
			},
		})
		require.NoError(t, err)

		const totalEmpatados = 8

		claves := make([]string, 0, totalEmpatados)
		deviceCreatedAt := now.Add(time.Minute)

		for i := range totalEmpatados {
			articuloID := 990100 + i
			claveEvento := uuid.NewString()

			claves = append(claves, claveEvento)

			err = g.AgregarArticulo(
				domain.AgregarArticuloParams{
					ArticuloID:  &articuloID,
					Clave:       fmt.Sprintf("ORDEN-%02d", i+1),
					Description: "Artículo para probar desempate",
				},
				domain.ActorParams{
					Usuario:           "ruben",
					ClaveIdempotencia: claveEvento,
					DeviceCreatedAt:   deviceCreatedAt,
				},
				now.Add(time.Duration(i+1)*time.Minute),
			)
			require.NoError(t, err)
		}

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		eventos, err := eventoRepo.ListarPorGarantia(ctx, g.ID())
		require.NoError(t, err)
		require.Len(t, eventos, totalEmpatados+1)

		for i := range totalEmpatados {
			require.Equal(
				t,
				deviceCreatedAt,
				eventos[i].DeviceCreatedAt(),
			)

			require.Equal(
				t,
				claves[i],
				eventos[i].ClaveIdempotencia(),
			)

			if i > 0 {
				require.True(
					t,
					eventos[i-1].CreatedAt().Before(
						eventos[i].CreatedAt(),
					),
				)
			}
		}

		require.Equal(
			t,
			claveFolio,
			eventos[totalEmpatados].ClaveIdempotencia(),
		)
	})
}
