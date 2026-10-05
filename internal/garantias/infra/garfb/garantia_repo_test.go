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

func TestGarantiaRepo_CrearYObtener_Cliente(t *testing.T) {
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

		now := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
		vigencia := time.Date(2027, 10, 2, 0, 0, 0, 0, time.UTC)

		clienteID := 900001
		ventaID := 900002

		lat := 19.0414
		lon := -98.2063

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:         folio,
			Origen:        origen,
			ClienteID:     &clienteID,
			VentaID:       &ventaID,
			EstadoCuenta:  &estadoCuenta,
			Description:   "Sillón con daño en la unión y tela café",
			VigenciaHasta: &vigencia,

			Calle:          "Avenida Juárez",
			NumeroExterior: "125",
			Colonia:        "Centro",
			Localidad:      "Heroica Puebla de Zaragoza",
			Ciudad:         "Puebla",
			CodigoPostal:   "72000",

			GPSLat:     &lat,
			GPSLon:     &lon,
			AbiertoPor: "ruben",

			Now: now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
				GPSLat:            &lat,
				GPSLon:            &lon,
			},
		})
		require.NoError(t, err)

		articuloID1 := 800001

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloID1,
				Clave:       "SILLON-001",
				Description: "Sillón pequeño color café con daño en unión",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Minute),
				GPSLat:            &lat,
				GPSLon:            &lon,
			},
			now.Add(time.Minute),
		)
		require.NoError(t, err)

		articuloID2 := 800002

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloID2,
				Clave:       "MESA-002",
				Description: "Mesa de madera con rayón y pequeña fisura",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
				GPSLat:            &lat,
				GPSLon:            &lon,
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, repo.Crear(ctx, g))

		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)
		require.NotNil(t, got)

		require.Equal(t, g.ID(), got.ID())
		require.Equal(t, g.Folio(), got.Folio())
		require.Equal(t, g.Origen(), got.Origen())

		require.NotNil(t, got.ClienteID())
		require.Equal(t, clienteID, *got.ClienteID())

		require.NotNil(t, got.VentaID())
		require.Equal(t, ventaID, *got.VentaID())

		require.NotNil(t, got.EstadoCuenta())
		require.Equal(t, estadoCuenta, *got.EstadoCuenta())

		require.Equal(t, g.Estado(), got.Estado())
		require.Equal(t, g.Description(), got.Description())

		require.NotNil(t, got.VigenciaHasta())
		require.Equal(t, vigencia.Year(), got.VigenciaHasta().Year())
		require.Equal(t, vigencia.Month(), got.VigenciaHasta().Month())
		require.Equal(t, vigencia.Day(), got.VigenciaHasta().Day())

		require.Equal(t, g.Calle(), got.Calle())
		require.Equal(t, g.NumeroExterior(), got.NumeroExterior())
		require.Equal(t, g.Colonia(), got.Colonia())
		require.Equal(t, g.Localidad(), got.Localidad())
		require.Equal(t, g.Ciudad(), got.Ciudad())
		require.Equal(t, g.CodigoPostal(), got.CodigoPostal())

		require.NotNil(t, got.GPSLat())
		require.NotNil(t, got.GPSLon())
		require.Equal(t, lat, *got.GPSLat())
		require.Equal(t, lon, *got.GPSLon())

		require.Equal(t, g.AbiertoPor(), got.AbiertoPor())
		require.Equal(t, g.ArticulosCount(), got.ArticulosCount())

		esperados := make(map[uuid.UUID]*domain.Articulo)
		for _, a := range g.ArticulosForRepo() {
			esperados[a.ID()] = a
		}

		for _, obtenido := range got.ArticulosForRepo() {
			esperado, ok := esperados[obtenido.ID()]
			require.True(t, ok)

			require.Equal(t, esperado.GarantiaID(), obtenido.GarantiaID())
			require.Equal(t, esperado.Rol(), obtenido.Rol())
			require.Equal(t, esperado.ReemplazaA(), obtenido.ReemplazaA())

			require.NotNil(t, obtenido.ArticuloID())
			require.Equal(t, *esperado.ArticuloID(), *obtenido.ArticuloID())

			require.Equal(t, esperado.Clave(), obtenido.Clave())
			require.Equal(t, esperado.Description(), obtenido.Description())
			require.Equal(t, esperado.Ruta(), obtenido.Ruta())
			require.Equal(t, esperado.Etapa(), obtenido.Etapa())
			require.Equal(t, esperado.Ubicacion(), obtenido.Ubicacion())
			require.Equal(t, esperado.Dictamen(), obtenido.Dictamen())
			require.Equal(t, esperado.Desenlace(), obtenido.Desenlace())
			require.Equal(t, esperado.CerradoEn(), obtenido.CerradoEn())
		}
	})
}
func TestGarantiaRepo_ClaveIdempotenciaDuplicada(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		claveDuplicada := uuid.NewString()
		now := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)

		numero1, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio1, err := domain.NewFolio(numero1)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("cliente")
		require.NoError(t, err)

		estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
		require.NoError(t, err)

		clienteID1 := 920001
		ventaID1 := 920002

		g1, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio1,
			Origen:         origen,
			ClienteID:      &clienteID1,
			VentaID:        &ventaID1,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Primera garantía para probar idempotencia",
			Calle:          "Avenida 1",
			NumeroExterior: "100",
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

		// Positive control: the first warranty must be saved successfully.
		require.NoError(t, repo.Crear(ctx, g1))

		numero2, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio2, err := domain.NewFolio(numero2)
		require.NoError(t, err)

		clienteID2 := 920003
		ventaID2 := 920004

		g2, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio2,
			Origen:         origen,
			ClienteID:      &clienteID2,
			VentaID:        &ventaID2,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Segunda garantía con clave repetida",
			Calle:          "Avenida 2",
			NumeroExterior: "200",
			Colonia:        "Centro",
			Localidad:      "Puebla",
			Ciudad:         "Puebla",
			CodigoPostal:   "72000",
			AbiertoPor:     "ruben",
			Now:            now.Add(time.Minute),
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: claveDuplicada,
				DeviceCreatedAt:   now.Add(time.Minute),
			},
		})
		require.NoError(t, err)

		err = repo.Crear(ctx, g2)

		require.Error(t, err)
		require.ErrorIs(t, err, domain.ErrClaveIdempotenciaDuplicada)
	})
}
func TestGarantiaRepo_NoEncontrada(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	// Obtener with an ID that does not exist.
	_, err := repo.Obtener(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)

	// ObtenerPorFolio with a valid folio that was never persisted.
	numero, err := folios.Siguiente(context.Background())
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	_, err = repo.ObtenerPorFolio(context.Background(), folio)
	require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)

	// These two operations require a transaction.
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.ObtenerParaActualizar(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)

		origen, err := domain.ParseOrigenFolio("cliente")
		require.NoError(t, err)

		estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
		require.NoError(t, err)

		numeroGuardar, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folioGuardar, err := domain.NewFolio(numeroGuardar)
		require.NoError(t, err)

		clienteID := 940001
		ventaID := 940002
		now := time.Date(2026, 10, 2, 19, 0, 0, 0, time.UTC)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folioGuardar,
			Origen:         origen,
			ClienteID:      &clienteID,
			VentaID:        &ventaID,
			EstadoCuenta:   &estadoCuenta,
			Description:    "Garantía que no se guardará",
			Calle:          "Calle 4",
			NumeroExterior: "400",
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

		// Crear was never called, so the UPDATE must affect zero rows.
		err = repo.Guardar(ctx, g)
		require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)
	})
}
func TestGarantiaRepo_CrearYObtener_Piso(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		now := time.Date(2026, 10, 2, 22, 0, 0, 0, time.UTC)
		vigencia := time.Date(2027, 10, 2, 0, 0, 0, 0, time.UTC)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:         folio,
			Origen:        origen,
			Description:   "Mueble de exhibición con pequeña raspadura",
			VigenciaHasta: &vigencia,
			AbiertoPor:    "ruben",
			Now:           now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		articuloID := 970001

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloID,
				Clave:       "PISO-001",
				Description: "Cómoda de exhibición color café con daño pequeño",
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

		got, err := repo.Obtener(ctx, g.ID())
		require.NoError(t, err)
		require.NotNil(t, got)

		require.Equal(t, g.ID(), got.ID())
		require.Equal(t, g.Folio(), got.Folio())
		require.Equal(t, g.Origen(), got.Origen())
		require.Equal(t, g.Estado(), got.Estado())
		require.Equal(t, g.Description(), got.Description())
		require.Equal(t, g.AbiertoPor(), got.AbiertoPor())

		// Floor-stock warranties do not store customer or address data.
		require.Nil(t, got.ClienteID())
		require.Nil(t, got.VentaID())
		require.Nil(t, got.EstadoCuenta())

		require.Empty(t, got.Calle())
		require.Empty(t, got.NumeroExterior())
		require.Empty(t, got.Colonia())
		require.Empty(t, got.Localidad())
		require.Empty(t, got.Ciudad())
		require.Empty(t, got.CodigoPostal())

		require.Nil(t, got.GPSLat())
		require.Nil(t, got.GPSLon())

		require.NotNil(t, got.VigenciaHasta())
		require.Equal(t, vigencia.Year(), got.VigenciaHasta().Year())
		require.Equal(t, vigencia.Month(), got.VigenciaHasta().Month())
		require.Equal(t, vigencia.Day(), got.VigenciaHasta().Day())

		articulos := got.ArticulosForRepo()
		require.Len(t, articulos, 1)

		require.Equal(t, "PISO-001", articulos[0].Clave())
		require.Equal(
			t,
			"Cómoda de exhibición color café con daño pequeño",
			articulos[0].Description(),
		)

		require.Equal(
			t,
			domain.EtapaRegistrado,
			articulos[0].Etapa(),
		)

		require.Equal(
			t,
			domain.UbicacionAlmacenRevision,
			articulos[0].Ubicacion(),
		)
	})
}
