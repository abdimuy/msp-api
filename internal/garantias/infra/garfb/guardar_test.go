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

func TestGarantiaRepo_GuardarAvanceYEvento(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
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

		now := time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC)

		clienteID := 950001
		ventaID := 950002
		articuloMicrosipID := 950003

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar Guardar",
			Calle:          "Calle 5",
			NumeroExterior: "500",
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

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloMicrosipID,
				Clave:       "GUARDAR-001",
				Description: "Artículo para probar actualización",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Minute),
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, repo.Crear(ctx, g))

		// Reload the aggregate from Firebird.
		// This keeps already persisted events out of the hydrated aggregate
		// so only new pending events are written by Guardar.
		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		articulos := cargada.ArticulosForRepo()
		require.Len(t, articulos, 1)

		articuloID := articulos[0].ID()

		require.Equal(
			t,
			domain.EtapaRegistrado,
			articulos[0].Etapa(),
		)

		// Client warranty transition:
		// registrado -> pendiente_recoleccion.
		err = cargada.AvanzarArticulo(
			articuloID,
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, repo.Guardar(ctx, cargada))

		// Read the aggregate again from Firebird.
		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)

		articulosGuardados := got.ArticulosForRepo()
		require.Len(t, articulosGuardados, 1)

		require.Equal(
			t,
			domain.EtapaPendienteRecoleccion,
			articulosGuardados[0].Etapa(),
		)

		// Expected events:
		// 1. folio_abierto
		// 2. articulo_agregado
		// 3. etapa_avanzada
		eventos, err := eventoRepo.ListarPorGarantia(ctx, g.ID())
		require.NoError(t, err)
		require.Len(t, eventos, 3)

		ultimo := eventos[2]

		require.Equal(
			t,
			domain.TipoEventoEtapaAvanzada,
			ultimo.Tipo(),
		)

		require.NotNil(t, ultimo.ArticuloRef())
		require.Equal(t, articuloID, *ultimo.ArticuloRef())

		require.NotNil(t, ultimo.EtapaDesde())
		require.Equal(
			t,
			domain.EtapaRegistrado,
			*ultimo.EtapaDesde(),
		)

		require.NotNil(t, ultimo.EtapaHasta())
		require.Equal(
			t,
			domain.EtapaPendienteRecoleccion,
			*ultimo.EtapaHasta(),
		)
	})
}

func TestGarantiaRepo_GuardarArticuloNuevoYReemplazo(t *testing.T) {
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

		now := time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC)

		clienteID := 960001
		ventaID := 960002
		articuloMicrosipID := 960003

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar artículo de reemplazo",
			Calle:          "Calle 6",
			NumeroExterior: "600",
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

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloMicrosipID,
				Clave:       "REEMP-001",
				Description: "Artículo original para probar reemplazo",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Minute),
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, repo.Crear(ctx, g))

		// Reload to work with a clean aggregate.
		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		articulos := cargada.ArticulosForRepo()
		require.Len(t, articulos, 1)

		originalID := articulos[0].ID()

		// registrado -> pendiente_recoleccion
		err = cargada.AvanzarArticulo(
			originalID,
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)

		// pendiente_recoleccion -> recolectado
		err = cargada.AvanzarArticulo(
			originalID,
			domain.EtapaRecolectado,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(3 * time.Minute),
			},
			now.Add(3*time.Minute),
		)
		require.NoError(t, err)

		// recolectado -> en_revision
		err = cargada.AvanzarArticulo(
			originalID,
			domain.EtapaEnRevision,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(4 * time.Minute),
			},
			now.Add(4*time.Minute),
		)
		require.NoError(t, err)

		rolTecnica := domain.RolDecisorTecnica

		// en_revision -> en_taller
		err = cargada.RegistrarDiagnostico(
			originalID,
			domain.RutaReparacionTaller,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(5 * time.Minute),
				RolDecisor:        &rolTecnica,
			},
			now.Add(5*time.Minute),
		)
		require.NoError(t, err)

		// The physical replacement must leave the original in standby and create
		// a new replacement article in listo_entrega.
		err = cargada.AutorizarCambioFisico(
			originalID,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(6 * time.Minute),
				RolDecisor:        &rolTecnica,
			},
			now.Add(6*time.Minute),
		)
		require.NoError(t, err)

		antesDeGuardar := cargada.ArticulosForRepo()
		require.Len(t, antesDeGuardar, 2)

		require.NoError(t, repo.Guardar(ctx, cargada))

		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)

		guardados := got.ArticulosForRepo()
		require.Len(t, guardados, 2)

		var original *domain.Articulo
		var reemplazo *domain.Articulo

		for _, a := range guardados {
			switch a.Rol() {
			case domain.RolArticuloOriginal:
				original = a
			case domain.RolArticuloReemplazo:
				reemplazo = a
			}
		}

		require.NotNil(t, original)
		require.NotNil(t, reemplazo)

		require.Equal(t, originalID, original.ID())
		require.Equal(t, domain.EtapaStandby, original.Etapa())

		require.Equal(
			t,
			domain.EtapaListoEntrega,
			reemplazo.Etapa(),
		)

		require.NotNil(t, reemplazo.ReemplazaA())
		require.Equal(
			t,
			original.ID(),
			*reemplazo.ReemplazaA(),
		)

		require.Equal(
			t,
			original.Description(),
			reemplazo.Description(),
		)
	})
}

func TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
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

		now := time.Date(2026, 10, 2, 23, 0, 0, 0, time.UTC)

		clienteID := 980001
		ventaID := 980002
		articuloMicrosipID := 980003

		claveDuplicada := uuid.NewString()

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar rollback",
			Calle:          "Calle 8",
			NumeroExterior: "800",
			Colonia:        "Centro",
			Localidad:      "Puebla",
			Ciudad:         "Puebla",
			CodigoPostal:   "72000",
			AbiertoPor:     "ruben",
			Now:            now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveDuplicada,
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloMicrosipID,
				Clave:       "ROLLBACK-001",
				Description: "Artículo para probar atomicidad",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Minute),
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, repo.Crear(ctx, g))

		q := firebird.GetQuerier(ctx, pool.DB)

		// Savepoint used to verify atomic rollback when Guardar fails.
		_, err = q.ExecContext(ctx, "SAVEPOINT antes_guardar")
		require.NoError(t, err)

		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		articulos := cargada.ArticulosForRepo()
		require.Len(t, articulos, 1)

		err = cargada.AvanzarArticulo(
			articulos[0].ID(),
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario: "ruben",

				// This key already exists on folio_abierto.
				ClaveIdempotencia: claveDuplicada,

				DeviceCreatedAt: now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)

		// Guardar updates the article first, then the duplicate event
		// insert fails.
		err = repo.Guardar(ctx, cargada)
		require.ErrorIs(
			t,
			err,
			domain.ErrClaveIdempotenciaDuplicada,
		)

		antesRollback, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)

		articulosAntesRollback := antesRollback.ArticulosForRepo()
		require.Len(t, articulosAntesRollback, 1)

		require.Equal(
			t,
			domain.EtapaPendienteRecoleccion,
			articulosAntesRollback[0].Etapa(),
		)

		// Roll back only the work performed after the savepoint.
		_, err = q.ExecContext(
			ctx,
			"ROLLBACK TO SAVEPOINT antes_guardar",
		)
		require.NoError(t, err)

		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)

		articulos = got.ArticulosForRepo()
		require.Len(t, articulos, 1)

		// The stage change must not remain after rollback.
		require.Equal(
			t,
			domain.EtapaRegistrado,
			articulos[0].Etapa(),
		)

		// Only the events written by Crear should remain:
		// folio_abierto and articulo_agregado.
		eventos, err := eventoRepo.ListarPorGarantia(ctx, g.ID())
		require.NoError(t, err)
		require.Len(t, eventos, 2)
	})
}

func TestGarantiaRepo_PersisteRutaDictamenYDatosDeEvento(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
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

		now := time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)

		clienteID := 996001
		ventaID := 996002
		articuloMicrosipID := 996003

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar ruta de proveedor",
			Calle:          "Avenida Juárez",
			NumeroExterior: "120",
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

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloMicrosipID,
				Clave:       "PROV-001",
				Description: "Sillón con daño en mecanismo",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Minute),
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, repo.Crear(ctx, g))

		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		articulos := cargada.ArticulosForRepo()
		require.Len(t, articulos, 1)

		id := articulos[0].ID()

		// registrado -> pendiente_recoleccion
		require.NoError(t, cargada.AvanzarArticulo(
			id,
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		))

		// pendiente_recoleccion -> recolectado
		require.NoError(t, cargada.AvanzarArticulo(
			id,
			domain.EtapaRecolectado,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(3 * time.Minute),
			},
			now.Add(3*time.Minute),
		))

		// recolectado -> en_revision
		require.NoError(t, cargada.AvanzarArticulo(
			id,
			domain.EtapaEnRevision,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(4 * time.Minute),
			},
			now.Add(4*time.Minute),
		))

		rol := domain.RolDecisorTecnica
		lat := 19.0414
		lon := -98.2063
		claveDiagnostico := uuid.NewString()

		// en_revision -> orden_generada
		require.NoError(t, cargada.RegistrarDiagnostico(
			id,
			domain.RutaReparacionProveedor,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveDiagnostico,
				DeviceCreatedAt:   now.Add(5 * time.Minute),
				RolDecisor:        &rol,
				GPSLat:            &lat,
				GPSLon:            &lon,
			},
			now.Add(5*time.Minute),
		))

		// orden_generada -> enviado_proveedor
		require.NoError(t, cargada.AvanzarArticulo(
			id,
			domain.EtapaEnviadoProveedor,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(6 * time.Minute),
			},
			now.Add(6*time.Minute),
		))

		// enviado_proveedor -> dictamen_recibido
		require.NoError(t, cargada.AvanzarArticulo(
			id,
			domain.EtapaDictamenRecibido,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(7 * time.Minute),
			},
			now.Add(7*time.Minute),
		))

		// accepted verdict -> reparado_proveedor
		require.NoError(t, cargada.RegistrarDictamen(
			id,
			domain.DictamenAceptada,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(8 * time.Minute),
			},
			now.Add(8*time.Minute),
		))

		require.NoError(t, repo.Guardar(ctx, cargada))

		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)

		guardados := got.ArticulosForRepo()
		require.Len(t, guardados, 1)

		a := guardados[0]

		require.NotNil(t, a.Ruta())
		require.Equal(
			t,
			domain.RutaReparacionProveedor,
			*a.Ruta(),
		)

		require.NotNil(t, a.Dictamen())
		require.Equal(
			t,
			domain.DictamenAceptada,
			*a.Dictamen(),
		)

		require.Equal(
			t,
			domain.EtapaReparadoProveedor,
			a.Etapa(),
		)

		// Verify that RolDecisor and GPS values round-trip through Firebird.
		eventoDiagnostico, err := eventoRepo.ObtenerPorClaveIdempotencia(
			ctx,
			claveDiagnostico,
		)
		require.NoError(t, err)
		require.NotNil(t, eventoDiagnostico)

		require.NotNil(t, eventoDiagnostico.RolDecisor())
		require.Equal(
			t,
			domain.RolDecisorTecnica,
			*eventoDiagnostico.RolDecisor(),
		)

		require.NotNil(t, eventoDiagnostico.GPSLat())
		require.NotNil(t, eventoDiagnostico.GPSLon())

		require.InDelta(t, lat, *eventoDiagnostico.GPSLat(), 1e-9)
		require.InDelta(t, lon, *eventoDiagnostico.GPSLon(), 1e-9)
	})
}

func TestGarantiaRepo_PersisteDesenlaceYCierre(t *testing.T) {
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

		now := time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)

		clienteID := 997001
		ventaID := 997002
		articuloMicrosipID := 997003

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía para probar desenlace y cierre",
			Calle:          "Calle Reforma",
			NumeroExterior: "321",
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

		require.NoError(t, g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloMicrosipID,
				Clave:       "CIERRE-001",
				Description: "Sillón para probar cierre",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Minute),
			},
			now.Add(time.Minute),
		))

		require.NoError(t, repo.Crear(ctx, g))

		cargada, err := repo.ObtenerParaActualizar(ctx, g.ID())
		require.NoError(t, err)

		articulos := cargada.ArticulosForRepo()
		require.Len(t, articulos, 1)

		originalID := articulos[0].ID()

		require.NoError(t, cargada.AvanzarArticulo(
			originalID,
			domain.EtapaPendienteRecoleccion,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		))

		require.NoError(t, cargada.AvanzarArticulo(
			originalID,
			domain.EtapaRecolectado,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(3 * time.Minute),
			},
			now.Add(3*time.Minute),
		))

		require.NoError(t, cargada.AvanzarArticulo(
			originalID,
			domain.EtapaEnRevision,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(4 * time.Minute),
			},
			now.Add(4*time.Minute),
		))

		rol := domain.RolDecisorTecnica

		require.NoError(t, cargada.RegistrarDiagnostico(
			originalID,
			domain.RutaReparacionTaller,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(5 * time.Minute),
				RolDecisor:        &rol,
			},
			now.Add(5*time.Minute),
		))

		// Original -> standby and create a replacement in listo_entrega.
		require.NoError(t, cargada.AutorizarCambioFisico(
			originalID,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(6 * time.Minute),
				RolDecisor:        &rol,
			},
			now.Add(6*time.Minute),
		))

		// standby -> segunda_mano.
		require.NoError(t, cargada.RegistrarDesenlace(
			originalID,
			domain.DesenlaceSegundaMano,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(7 * time.Minute),
				RolDecisor:        &rol,
			},
			now.Add(7*time.Minute),
		))

		// Close the folio as well to persist CERRADO_EN.
		require.NoError(t, cargada.IniciarProceso(
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(8 * time.Minute),
			},
			now.Add(8*time.Minute),
		))

		require.NoError(t, cargada.MarcarListoEntrega(
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(9 * time.Minute),
			},
			now.Add(9*time.Minute),
		))

		require.NoError(t, cargada.Entregar(
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(10 * time.Minute),
			},
			now.Add(10*time.Minute),
		))

		horaCierre := now.Add(11 * time.Minute)

		require.NoError(t, cargada.Cerrar(
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   horaCierre,
			},
			horaCierre,
		))

		require.NoError(t, repo.Guardar(ctx, cargada))

		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)

		require.Equal(t, domain.EstadoFolioCerrado, got.Estado())

		require.NotNil(t, got.CerradoEn())
		require.True(t, got.CerradoEn().Equal(horaCierre))

		guardados := got.ArticulosForRepo()
		require.Len(t, guardados, 2)

		var original *domain.Articulo

		for _, a := range guardados {
			if a.ID() == originalID {
				original = a
				break
			}
		}

		require.NotNil(t, original)

		require.Equal(
			t,
			domain.EtapaSegundaMano,
			original.Etapa(),
		)

		require.NotNil(t, original.Desenlace())
		require.Equal(
			t,
			domain.DesenlaceSegundaMano,
			*original.Desenlace(),
		)
	})
}
