//nolint:paralleltest // Firebird integration tests must run serially.
package garfb_test

import (
	"context"
	"sort"
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

		const huella = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

		img, err := domain.NewImagen(domain.NewImagenParams{
			Tipo:        domain.TipoImagenFirma,
			EventoID:    eventoID,
			Ruta:        "garantías/niño/sillón-daño-ñ.jpg",
			Description: "Daño en el sillón: raspón, piñón y área húmeda",
			SubidaPor:   "Rubén Carrera",
			Huella:      huella,
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
		require.Equal(t, domain.TipoImagenFirma, got.Tipo())
		require.Equal(t, huella, got.Huella())
		require.Equal(t, img.Ruta(), got.Ruta())
		require.Equal(t, img.Description(), got.Description())
		require.Equal(t, img.SubidaPor(), got.SubidaPor())
		require.Equal(t, img.CreatedAt(), got.CreatedAt())

		obtenida, err := imagenRepo.Obtener(ctx, img.ID())
		require.NoError(t, err)
		require.Equal(t, img.ID(), obtenida.ID())
		require.Equal(t, img.EventoID(), obtenida.EventoID())
		require.Equal(t, domain.TipoImagenFirma, obtenida.Tipo())
		require.Equal(t, huella, obtenida.Huella())
		require.Equal(t, img.Ruta(), obtenida.Ruta())
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
			Tipo:        domain.TipoImagenEvidencia,
			EventoID:    eventoObjetivo,
			Ruta:        "garantias/img-tarde.jpg",
			Description: "Imagen tarde",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(30 * time.Minute),
		})
		require.NoError(t, err)

		imgTemprana, err := domain.NewImagen(domain.NewImagenParams{
			Tipo:        domain.TipoImagenEvidencia,
			EventoID:    eventoObjetivo,
			Ruta:        "garantias/img-temprana.jpg",
			Description: "Imagen temprana",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(10 * time.Minute),
		})
		require.NoError(t, err)

		imgMedia, err := domain.NewImagen(domain.NewImagenParams{
			Tipo:        domain.TipoImagenEvidencia,
			EventoID:    eventoObjetivo,
			Ruta:        "garantias/img-media.jpg",
			Description: "Imagen media",
			SubidaPor:   "ruben",
			CreatedAt:   base.Add(20 * time.Minute),
		})
		require.NoError(t, err)

		imgOtroEvento, err := domain.NewImagen(domain.NewImagenParams{
			Tipo:        domain.TipoImagenEvidencia,
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
		require.NotNil(t, vacias)
		require.Empty(t, vacias)
	})
}

func TestImagenRepo_Registrar_EventoInexistente(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	imagenRepo := garfb.NewImagenRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)

		img, err := domain.NewImagen(domain.NewImagenParams{
			Tipo:        domain.TipoImagenEvidencia,
			EventoID:    uuid.New(),
			Ruta:        "garantias/evento-inexistente.jpg",
			Description: "Imagen con evento inexistente",
			SubidaPor:   "ruben",
			CreatedAt:   now,
		})
		require.NoError(t, err)

		err = imagenRepo.Registrar(ctx, img)
		require.Error(t, err)
		require.NotErrorIs(t, err, domain.ErrImagenDuplicada)

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
			Tipo:        domain.TipoImagenEvidencia,
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

func TestImagenRepo_ListarPorEvento_DesempataPorID(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2099, 2, 8, 12, 0, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para desempate de imágenes",
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
		mismoCreatedAt := now.Add(time.Minute)

		imagenes := []*domain.Imagen{
			domain.HydrateImagen(domain.HydrateImagenParams{
				ID: uuid.MustParse(
					"00000000-0000-0000-0000-000000000003",
				),
				EventoID:    eventoID,
				Ruta:        "garantias/03.jpg",
				Description: "Imagen tres",
				SubidaPor:   "ruben",
				CreatedAt:   mismoCreatedAt,
			}),
			domain.HydrateImagen(domain.HydrateImagenParams{
				ID: uuid.MustParse(
					"00000000-0000-0000-0000-000000000001",
				),
				EventoID:    eventoID,
				Ruta:        "garantias/01.jpg",
				Description: "Imagen uno",
				SubidaPor:   "ruben",
				CreatedAt:   mismoCreatedAt,
			}),
			domain.HydrateImagen(domain.HydrateImagenParams{
				ID: uuid.MustParse(
					"00000000-0000-0000-0000-000000000002",
				),
				EventoID:    eventoID,
				Ruta:        "garantias/02.jpg",
				Description: "Imagen dos",
				SubidaPor:   "ruben",
				CreatedAt:   mismoCreatedAt,
			}),
		}

		for _, img := range imagenes {
			require.NoError(t, imagenRepo.Registrar(ctx, img))
		}

		obtenidas, err := imagenRepo.ListarPorEvento(ctx, eventoID)
		require.NoError(t, err)
		require.Len(t, obtenidas, 3)

		require.Equal(
			t,
			"00000000-0000-0000-0000-000000000001",
			obtenidas[0].ID().String(),
		)
		require.Equal(
			t,
			"00000000-0000-0000-0000-000000000002",
			obtenidas[1].ID().String(),
		)
		require.Equal(
			t,
			"00000000-0000-0000-0000-000000000003",
			obtenidas[2].ID().String(),
		)
	})
}

func TestImagenRepo_Obtener_TipoNullEsEvidencia(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 30, 13, 20, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para imagen legacy",
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

		imagenID := uuid.New()
		q := firebird.GetQuerier(ctx, pool.DB)

		_, err = q.ExecContext(
			ctx,
			`INSERT INTO MSP_GA_IMAGEN (
				ID,
				EVENTO_ID,
				RUTA,
				DESCRIPCION,
				SUBIDA_POR,
				CREATED_AT
			) VALUES (?, ?, ?, ?, ?, ?)`,
			imagenID.String(),
			eventos[0].ID().String(),
			"garantias/legacy.jpg",
			"Imagen anterior a migración 71",
			"ruben",
			firebird.ToWallClock(now.Add(time.Minute)),
		)
		require.NoError(t, err)

		img, err := imagenRepo.Obtener(ctx, imagenID)
		require.NoError(t, err)

		require.Equal(t, imagenID, img.ID())
		require.Equal(t, domain.TipoImagenEvidencia, img.Tipo())
		require.Empty(t, img.Huella())
		require.Equal(t, "garantias/legacy.jpg", img.Ruta())
	})
}

func TestImagenRepo_Obtener_NoEncontrada(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	imagenRepo := garfb.NewImagenRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := imagenRepo.Obtener(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	})
}

func TestImagenRepo_Registrar_ImagenDuplicada(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 30, 13, 30, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para imagen duplicada",
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

		imagenID := uuid.New()

		primera, err := domain.NewImagen(domain.NewImagenParams{
			ID:        &imagenID,
			EventoID:  eventos[0].ID(),
			Tipo:      domain.TipoImagenEvidencia,
			Ruta:      "garantias/duplicada-1.jpg",
			SubidaPor: "ruben",
			CreatedAt: now.Add(time.Minute),
		})
		require.NoError(t, err)

		require.NoError(t, imagenRepo.Registrar(ctx, primera))

		segunda, err := domain.NewImagen(domain.NewImagenParams{
			ID:        &imagenID,
			EventoID:  eventos[0].ID(),
			Tipo:      domain.TipoImagenEvidencia,
			Ruta:      "garantias/duplicada-2.jpg",
			SubidaPor: "ruben",
			CreatedAt: now.Add(2 * time.Minute),
		})
		require.NoError(t, err)

		err = imagenRepo.Registrar(ctx, segunda)
		require.ErrorIs(t, err, domain.ErrImagenDuplicada)
	})
}

func TestImagenRepo_ListarPorEventos_FiltraOrdenaYDeduplicaIDs(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	imagenRepo := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para listar imágenes por eventos",
			AbiertoPor:  "ruben",
			Now:         base,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   base,
			},
		})
		require.NoError(t, err)

		for i, articuloID := range []int{891001, 891002} {
			require.NoError(t, g.AgregarArticulo(
				domain.AgregarArticuloParams{
					ArticuloID:  &articuloID,
					Clave:       []string{"MULTI-001", "MULTI-002"}[i],
					Description: "Artículo para imágenes",
				},
				domain.ActorParams{
					Usuario:           "ruben",
					ClaveIdempotencia: uuid.NewString(),
					DeviceCreatedAt:   base.Add(time.Duration(i+1) * time.Minute),
				},
				base.Add(time.Duration(i+1)*time.Minute),
			))
		}

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		eventos := g.EventosPendientesForRepo()
		require.Len(t, eventos, 3)

		eventoA := eventos[0].ID()
		eventoB := eventos[1].ID()
		eventoAjeno := eventos[2].ID()

		nueva := func(
			eventoID uuid.UUID,
			ruta string,
			createdAt time.Time,
		) *domain.Imagen {
			t.Helper()

			img, err := domain.NewImagen(domain.NewImagenParams{
				EventoID:  eventoID,
				Tipo:      domain.TipoImagenEvidencia,
				Ruta:      ruta,
				SubidaPor: "ruben",
				CreatedAt: createdAt,
			})
			require.NoError(t, err)

			return img
		}

		imgATarde := nueva(eventoA, "garantias/a-tarde.jpg", base.Add(20*time.Minute))
		imgATemprana := nueva(eventoA, "garantias/a-temprana.jpg", base.Add(10*time.Minute))
		imgB := nueva(eventoB, "garantias/b.jpg", base.Add(5*time.Minute))
		imgAjena := nueva(eventoAjeno, "garantias/ajena.jpg", base.Add(time.Minute))

		for _, img := range []*domain.Imagen{
			imgATarde,
			imgAjena,
			imgB,
			imgATemprana,
		} {
			require.NoError(t, imagenRepo.Registrar(ctx, img))
		}

		got, err := imagenRepo.ListarPorEventos(
			ctx,
			[]uuid.UUID{eventoB, eventoA, eventoB},
		)
		require.NoError(t, err)
		require.Len(t, got, 3)

		idsEventos := []string{eventoA.String(), eventoB.String()}
		sort.Strings(idsEventos)

		esperadas := map[string][]uuid.UUID{
			eventoA.String(): {imgATemprana.ID(), imgATarde.ID()},
			eventoB.String(): {imgB.ID()},
		}

		pos := 0
		for _, eventoID := range idsEventos {
			for _, imagenID := range esperadas[eventoID] {
				require.Equal(t, eventoID, got[pos].EventoID().String())
				require.Equal(t, imagenID, got[pos].ID())
				require.NotEqual(t, imgAjena.ID(), got[pos].ID())
				pos++
			}
		}
	})
}
