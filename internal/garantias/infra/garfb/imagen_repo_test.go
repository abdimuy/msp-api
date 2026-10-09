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
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestImagenRepo_RegistrarYListarPorEvento_UTF8(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para prueba de imágenes",
			AbiertoPor:  "ruben",
			Now:         now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		eventos := g.EventosPendientesForRepo()
		require.NotEmpty(t, eventos)

		eventoID := eventos[0].ID()

		img, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    eventoID,
			Ruta:        "garantías/niño/sillón-daño-ñ.jpg",
			Description: "Daño en el sillón: raspón, piñón y área húmeda",
			SubidaPor:   "Rubén Carrera",
			CreatedAt:   now.Add(time.Minute),
		})
		require.NoError(t, err)

		require.NoError(t, imagenRepo.Registrar(ctx, img))

		imagenes, err := imagenRepo.ListarPorEvento(ctx, eventoID)
		require.NoError(t, err)
		require.Len(t, imagenes, 1)

		got := imagenes[0]

		require.Equal(t, img.ID(), got.ID())
		require.Equal(t, img.EventoID(), got.EventoID())
		require.Equal(t, img.Ruta(), got.Ruta())
		require.Equal(t, img.Description(), got.Description())
		require.Equal(t, img.SubidaPor(), got.SubidaPor())
		require.Equal(t, img.CreatedAt(), got.CreatedAt())
	})
}

func TestImagenRepo_ListarPorEvento_FiltraOrdenaYVacio(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para listar imágenes",
			AbiertoPor:  "ruben",
			Now:         base,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   base,
			},
		})
		require.NoError(t, err)

		articuloID := 820001

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloID,
				Clave:       "IMG-001",
				Description: "Artículo para prueba de imágenes",
			},
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   base.Add(time.Minute),
			},
			base.Add(time.Minute),
		)
		require.NoError(t, err)

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		eventos := g.EventosPendientesForRepo()
		require.Len(t, eventos, 2)

		eventoObjetivo := eventos[0].ID()
		otroEvento := eventos[1].ID()

		imgTarde, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    eventoObjetivo,
			Ruta:        "garantias/img-tarde.jpg",
			Description: "Imagen tarde",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(30 * time.Minute),
		})
		require.NoError(t, err)

		imgTemprana, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    eventoObjetivo,
			Ruta:        "garantias/img-temprana.jpg",
			Description: "Imagen temprana",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(10 * time.Minute),
		})
		require.NoError(t, err)

		imgMedia, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    eventoObjetivo,
			Ruta:        "garantias/img-media.jpg",
			Description: "Imagen media",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(20 * time.Minute),
		})
		require.NoError(t, err)

		imgOtroEvento, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    otroEvento,
			Ruta:        "garantias/otro-evento.jpg",
			Description: "Imagen de otro evento",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(5 * time.Minute),
		})
		require.NoError(t, err)

		// Se registran fuera de orden para comprobar el ORDER BY.
		require.NoError(t, imagenRepo.Registrar(ctx, imgTarde))
		require.NoError(t, imagenRepo.Registrar(ctx, imgTemprana))
		require.NoError(t, imagenRepo.Registrar(ctx, imgMedia))
		require.NoError(t, imagenRepo.Registrar(ctx, imgOtroEvento))

		imagenes, err := imagenRepo.ListarPorEvento(ctx, eventoObjetivo)
		require.NoError(t, err)
		require.Len(t, imagenes, 3)

		require.Equal(t, imgTemprana.ID(), imagenes[0].ID())
		require.Equal(t, imgMedia.ID(), imagenes[1].ID())
		require.Equal(t, imgTarde.ID(), imagenes[2].ID())

		vacias, err := imagenRepo.ListarPorEvento(ctx, uuid.New())
		require.NoError(t, err)
		require.Empty(t, vacias)
	})
}

func TestImagenRepo_Registrar_EventoInexistente(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	imagenRepo := garfb.NewImagenRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)

		img, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    uuid.New(),
			Ruta:        "garantias/evento-inexistente.jpg",
			Description: "Imagen con evento inexistente",
			SubidaPor:   "ruben",
			CreatedAt:   now,
		})
		require.NoError(t, err)

		err = imagenRepo.Registrar(ctx, img)
		require.Error(t, err)

		appErr, ok := apperror.As(err)
		require.True(t, ok)
		require.Equal(t, "firebird_fk_violation", appErr.Code)
	})
}

func TestImagenRepo_Registrar_SinTransaccion(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para probar imagen sin transacción",
			AbiertoPor:  "ruben",
			Now:         now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		eventos := g.EventosPendientesForRepo()
		require.NotEmpty(t, eventos)

		img, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:    eventos[0].ID(),
			Ruta:        "garantias/sin-transaccion.jpg",
			Description: "No debe guardarse",
			SubidaPor:   "ruben",
			CreatedAt:   now.Add(time.Minute),
		})
		require.NoError(t, err)

		// context.Background no contiene la transacción activa.
		err = imagenRepo.Registrar(context.Background(), img)
		require.ErrorIs(t, err, firebird.ErrNoTx)

		tx, err := firebird.RequireTx(ctx)
		require.NoError(t, err)

		var total int

		err = tx.QueryRowContext(
			ctx,
			`SELECT COUNT(*)
FROM MSP_GA_IMAGEN
WHERE ID = ?`,
			img.ID().String(),
		).Scan(&total)
		require.NoError(t, err)

		require.Equal(t, 0, total)
	})
}
