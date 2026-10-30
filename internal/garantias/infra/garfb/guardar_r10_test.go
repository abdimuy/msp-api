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
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestGarantiaRepo_GuardarSoloTocaLoModificado es la guardia de R10 y de los
// mutantes C3/C4.
//
// C3: "Guardar actualiza todos los artículos, como antes". Un artículo limpio
// —hidratado, sin tocar— no debe reescribirse. Para detectarlo se corrompe su
// fila a mano después de cargar el agregado: si Guardar lo reescribe, la
// corrupción desaparece y este test lo ve.
//
// C4: "Guardar inserta en silencio cuando el UPDATE afecta 0 filas". Se borra
// la fila del artículo modificado y se comprueba que Guardar falla en vez de
// resucitarla con un INSERT.
func TestGarantiaRepo_GuardarSoloTocaLoModificado(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
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

		now := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)

		clienteID := 984001
		ventaID := 984002

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar R10",
			Calle:          "Calle 10",
			NumeroExterior: "1000",
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

		a1 := 984003
		a2 := 984004
		require.NoError(t, g.AgregarArticulo(domain.AgregarArticuloParams{
			ArticuloID:  &a1,
			Clave:       "R10-001",
			Description: "Sillón uno",
		}, domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		}, now))
		require.NoError(t, g.AgregarArticulo(domain.AgregarArticuloParams{
			ArticuloID:  &a2,
			Clave:       "R10-002",
			Description: "Sillón dos",
		}, domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		}, now.Add(time.Second)))

		require.NoError(t, repo.Crear(ctx, g))

		// Caso C4 primero: se borra la fila del artículo que vamos a modificar.
		// El UPDATE afectará 0 filas y Guardar debe fallar, no insertar.
		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		arts := cargada.ArticulosForRepo()
		require.Len(t, arts, 2)

		victima := arts[0]
		q := firebird.GetQuerier(ctx, pool.DB)

		require.NoError(t, cargada.IniciarProceso(domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Minute),
		}, now.Add(time.Minute)))

		require.NoError(t, cargada.AvanzarArticulo(
			victima.ID(),
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		))

		_, err = q.ExecContext(
			ctx,
			"DELETE FROM MSP_GA_ARTICULO WHERE ID = ?",
			victima.ID().String(),
		)
		require.NoError(t, err)

		err = repo.Guardar(ctx, cargada)
		require.ErrorIs(t, err, domain.ErrArticuloNoEncontrado,
			"un UPDATE que afecta 0 filas debe ser un error, no un INSERT silencioso")

		// La fila borrada no debe reaparecer: sin INSERT silencioso.
		var cuenta int
		err = q.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM MSP_GA_ARTICULO WHERE ID = ?",
			victima.ID().String(),
		).Scan(&cuenta)
		require.NoError(t, err)
		require.Zero(t, cuenta, "el UPDATE fallido no puede insertar la fila de vuelta")
	})
}

// TestGarantiaRepo_GuardarNoReescribeArticuloLimpio es la mitad C3: el artículo
// limpio conserva lo que hay en la base.
func TestGarantiaRepo_GuardarNoReescribeArticuloLimpio(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
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

		now := time.Date(2026, 10, 4, 16, 0, 0, 0, time.UTC)

		clienteID := 985001
		ventaID := 985002

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar el artículo limpio",
			Calle:          "Calle 11",
			NumeroExterior: "1100",
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

		a1 := 985003
		a2 := 985004
		require.NoError(t, g.AgregarArticulo(domain.AgregarArticuloParams{
			ArticuloID: &a1, Clave: "LIMPIO-001", Description: "Sillón uno",
		}, domain.ActorParams{
			Usuario: "ruben", ClaveIdempotencia: uuid.NewString(), DeviceCreatedAt: now,
		}, now))
		require.NoError(t, g.AgregarArticulo(domain.AgregarArticuloParams{
			ArticuloID: &a2, Clave: "LIMPIO-002", Description: "Sillón dos",
		}, domain.ActorParams{
			Usuario: "ruben", ClaveIdempotencia: uuid.NewString(), DeviceCreatedAt: now,
		}, now.Add(time.Second)))

		require.NoError(t, repo.Crear(ctx, g))

		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		arts := cargada.ArticulosForRepo()
		require.Len(t, arts, 2)

		// La víctima es el artículo que NO vamos a tocar.
		limpio := arts[1]
		q := firebird.GetQuerier(ctx, pool.DB)

		// Se corrompe su fila a mano: si Guardar reescribe todos los
		// artículos, esta marca desaparece. La columna tiene que ser una que
		// el UPDATE de Guardar escriba (ETAPA, UBICACION, RUTA, DICTAMEN,
		// DESENLACE, CERRADO_EN, UPDATED_AT); DESCRIPCION no se actualiza.
		_, err = q.ExecContext(
			ctx,
			"UPDATE MSP_GA_ARTICULO SET ETAPA = ? WHERE ID = ?",
			string(domain.EtapaMerma),
			limpio.ID().String(),
		)
		require.NoError(t, err)

		// Sólo se toca el otro artículo.
		require.NoError(t, cargada.IniciarProceso(domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Minute),
		}, now.Add(time.Minute)))

		require.NoError(t, cargada.AvanzarArticulo(
			arts[0].ID(),
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		))

		require.NoError(t, repo.Guardar(ctx, cargada))

		// El artículo limpio conserva la marca: Guardar no lo reescribió (C3).
		var etapa string
		err = q.QueryRowContext(
			ctx,
			"SELECT ETAPA FROM MSP_GA_ARTICULO WHERE ID = ?",
			limpio.ID().String(),
		).Scan(&etapa)
		require.NoError(t, err)
		require.Equal(t, string(domain.EtapaMerma), etapa,
			"Guardar no debe reescribir un artículo limpio")
	})
}
