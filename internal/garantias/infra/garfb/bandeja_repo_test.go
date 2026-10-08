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
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

func crearGarantiaParaBandeja(
	t *testing.T,
	ctx context.Context,
	repo *garfb.GarantiaRepo,
	folios *garfb.FolioGenerator,
	now time.Time,
	enProceso bool,
) *domain.Garantia {
	t.Helper()

	numero, err := folios.Siguiente(ctx)
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	origen, err := domain.ParseOrigenFolio("piso")
	require.NoError(t, err)

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       folio,
		Origen:      origen,
		Description: "Garantía para prueba de bandeja",
		AbiertoPor:  "ruben",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	articuloID := 700000 + numero

	err = g.AgregarArticulo(
		domain.AgregarArticuloParams{
			ArticuloID:  &articuloID,
			Clave:       "BANDEJA",
			Description: "Artículo para prueba de bandeja",
		},
		domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Minute),
		},
		now.Add(time.Minute),
	)
	require.NoError(t, err)

	if enProceso {
		err = g.IniciarProceso(
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)
	}

	require.NoError(t, repo.Crear(ctx, g))

	return g
}

func crearGarantiaClienteParaBandeja(
	t *testing.T,
	ctx context.Context,
	repo *garfb.GarantiaRepo,
	folios *garfb.FolioGenerator,
	now time.Time,
	clienteID int,
) *domain.Garantia {
	t.Helper()

	numero, err := folios.Siguiente(ctx)
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	origen, err := domain.ParseOrigenFolio("cliente")
	require.NoError(t, err)

	estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
	require.NoError(t, err)

	ventaID := clienteID + 1

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:          folio,
		Origen:         origen,
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estadoCuenta,
		Description:    "Garantía de cliente para bandeja",
		Calle:          "Avenida Juárez",
		NumeroExterior: "100",
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

	articuloID := clienteID + 100

	err = g.AgregarArticulo(
		domain.AgregarArticuloParams{
			ArticuloID:  &articuloID,
			Clave:       "CLIENTE-BANDEJA",
			Description: "Artículo de cliente para bandeja",
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

	return g
}

func crearGarantiaConEtapaParaBandeja(
	t *testing.T,
	ctx context.Context,
	repo *garfb.GarantiaRepo,
	folios *garfb.FolioGenerator,
	now time.Time,
	etapa domain.Etapa,
) *domain.Garantia {
	t.Helper()

	numero, err := folios.Siguiente(ctx)
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	origen, err := domain.ParseOrigenFolio("piso")
	require.NoError(t, err)

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       folio,
		Origen:      origen,
		Description: "Garantía para filtro por etapa",
		AbiertoPor:  "ruben",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	articuloID := 730000 + numero

	err = g.AgregarArticulo(
		domain.AgregarArticuloParams{
			ArticuloID:  &articuloID,
			Clave:       "ETAPA-BANDEJA",
			Description: "Artículo para filtro por etapa",
		},
		domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Minute),
		},
		now.Add(time.Minute),
	)
	require.NoError(t, err)

	if etapa != domain.EtapaRegistrado {
		articulo := g.ArticulosForRepo()[0]

		err = g.AvanzarArticulo(
			articulo.ID(),
			etapa,
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Minute),
			},
			now.Add(2*time.Minute),
		)
		require.NoError(t, err)
	}

	require.NoError(t, repo.Crear(ctx, g))

	return g
}

func TestBandejaRepo_Listar_FiltroEstado(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

		esperada := crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base,
			true,
		)

		_ = crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base.Add(time.Hour),
			false,
		)

		estado := domain.EstadoFolioEnProceso

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Estado: &estado,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())
		require.Len(t, pagina.Items[0].ArticulosForRepo(), 1)
		require.Empty(t, pagina.SiguienteCursor)
	})
}

func TestBandejaRepo_Listar_FiltroOrigen(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)

		esperada := crearGarantiaClienteParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base,
			710001,
		)

		_ = crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base.Add(time.Hour),
			false,
		)

		origen := domain.OrigenFolio("cliente")

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Origen: &origen,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())
		require.Equal(t, origen, pagina.Items[0].Origen())
	})
}

func TestBandejaRepo_Listar_FiltroClienteID(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC)

		clienteBuscado := 720001

		esperada := crearGarantiaClienteParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base,
			clienteBuscado,
		)

		_ = crearGarantiaClienteParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base.Add(time.Hour),
			720010,
		)

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				ClienteID: &clienteBuscado,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())

		require.NotNil(t, pagina.Items[0].ClienteID())
		require.Equal(
			t,
			clienteBuscado,
			*pagina.Items[0].ClienteID(),
		)
	})
}

func TestBandejaRepo_Listar_FiltroEtapa(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

		etapaBuscada := domain.EtapaPendienteRecoleccion

		esperada := crearGarantiaConEtapaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base,
			etapaBuscada,
		)

		_ = crearGarantiaConEtapaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base.Add(time.Hour),
			domain.EtapaRegistrado,
		)

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Etapa: &etapaBuscada,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())

		articulos := pagina.Items[0].ArticulosForRepo()
		require.Len(t, articulos, 1)
		require.Equal(t, etapaBuscada, articulos[0].Etapa())
	})
}

func TestBandejaRepo_Listar_FiltroUbicacion(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC)

		esperada := crearGarantiaClienteParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base,
			740001,
		)

		_ = crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			base.Add(time.Hour),
			false,
		)

		ubicacion := domain.UbicacionDomicilioCliente

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Ubicacion: &ubicacion,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())

		articulos := pagina.Items[0].ArticulosForRepo()
		require.Len(t, articulos, 1)
		require.Equal(
			t,
			domain.UbicacionDomicilioCliente,
			articulos[0].Ubicacion(),
		)
	})
}

func TestBandejaRepo_Listar_FiltroDesde(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		desde := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)

		_ = crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			desde.Add(-time.Hour),
			false,
		)

		esperada := crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			desde,
			false,
		)

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Desde: &desde,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())
	})
}

func TestBandejaRepo_Listar_FiltroHasta(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		hasta := time.Date(2026, 10, 8, 20, 0, 0, 0, time.UTC)

		esperada := crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			hasta.Add(-time.Hour),
			false,
		)

		_ = crearGarantiaParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			hasta,
			false,
		)

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Hasta: &hasta,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())
	})
}

func TestBandejaRepo_Listar_TresFiltrosCombinados(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		desde := time.Date(2026, 10, 8, 21, 0, 0, 0, time.UTC)
		clienteBuscado := 750001

		// Cumple origen + cliente + fecha.
		esperada := crearGarantiaClienteParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			desde.Add(time.Hour),
			clienteBuscado,
		)

		// Cumple origen y cliente, pero NO cumple Desde.
		_ = crearGarantiaClienteParaBandeja(
			t,
			ctx,
			garantiaRepo,
			folios,
			desde.Add(-time.Hour),
			clienteBuscado,
		)

		origen := domain.OrigenFolio("cliente")

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Origen:    &origen,
				ClienteID: &clienteBuscado,
				Desde:     &desde,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		require.Len(t, pagina.Items, 1)
		require.Equal(t, esperada.ID(), pagina.Items[0].ID())
	})
}

func TestBandejaRepo_Listar_PaginacionSinRepetirNiSaltar(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2026, 10, 8, 22, 0, 0, 0, time.UTC)

		creadas := make([]*domain.Garantia, 0, 5)

		for i := 0; i < 5; i++ {
			g := crearGarantiaParaBandeja(
				t,
				ctx,
				garantiaRepo,
				folios,
				base.Add(time.Duration(i)*time.Minute),
				false,
			)

			creadas = append(creadas, g)
		}

		// El listado es CREATED_AT DESC, así que la más nueva sale primero.
		esperados := []uuid.UUID{
			creadas[4].ID(),
			creadas[3].ID(),
			creadas[2].ID(),
			creadas[1].ID(),
			creadas[0].ID(),
		}

		pagina1, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{},
			outbound.Paginacion{
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina1.Items, 2)
		require.NotEmpty(t, pagina1.SiguienteCursor)

		pagina2, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{},
			outbound.Paginacion{
				Cursor: pagina1.SiguienteCursor,
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina2.Items, 2)
		require.NotEmpty(t, pagina2.SiguienteCursor)

		pagina3, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{},
			outbound.Paginacion{
				Cursor: pagina2.SiguienteCursor,
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina3.Items, 1)
		require.Empty(t, pagina3.SiguienteCursor)

		obtenidos := []uuid.UUID{
			pagina1.Items[0].ID(),
			pagina1.Items[1].ID(),
			pagina2.Items[0].ID(),
			pagina2.Items[1].ID(),
			pagina3.Items[0].ID(),
		}

		require.Equal(t, esperados, obtenidos)
	})
}

func TestBandejaRepo_Listar_PaginacionConCreatedAtIgual(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		mismoCreatedAt := time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC)

		creadas := make([]*domain.Garantia, 0, 4)

		for i := 0; i < 4; i++ {
			g := crearGarantiaParaBandeja(
				t,
				ctx,
				garantiaRepo,
				folios,
				mismoCreatedAt,
				false,
			)

			creadas = append(creadas, g)
		}

		// Cuando CREATED_AT empata, la bandeja desempata por ID DESC.
		esperados := make([]string, 0, len(creadas))
		for _, g := range creadas {
			esperados = append(esperados, g.ID().String())
		}

		sort.Sort(sort.Reverse(sort.StringSlice(esperados)))

		pagina1, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{},
			outbound.Paginacion{
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina1.Items, 2)
		require.NotEmpty(t, pagina1.SiguienteCursor)

		pagina2, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{},
			outbound.Paginacion{
				Cursor: pagina1.SiguienteCursor,
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina2.Items, 2)
		require.Empty(t, pagina2.SiguienteCursor)

		obtenidos := []string{
			pagina1.Items[0].ID().String(),
			pagina1.Items[1].ID().String(),
			pagina2.Items[0].ID().String(),
			pagina2.Items[1].ID().String(),
		}

		require.Equal(t, esperados, obtenidos)
	})
}

func TestBandejaRepo_Listar_NoDuplicaGarantiaConDosArticulos(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := garfb.NewGarantiaRepo(pool)
	bandejaRepo := garfb.NewBandejaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía con dos artículos",
			AbiertoPor:  "ruben",
			Now:         now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		for i, articuloID := range []int{760001, 760002} {
			err = g.AgregarArticulo(
				domain.AgregarArticuloParams{
					ArticuloID:  &articuloID,
					Clave:       []string{"DUP-001", "DUP-002"}[i],
					Description: []string{"Artículo uno", "Artículo dos"}[i],
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

		require.NoError(t, garantiaRepo.Crear(ctx, g))

		etapa := domain.EtapaRegistrado

		pagina, err := bandejaRepo.Listar(
			ctx,
			outbound.ListarGarantiasFiltros{
				Etapa: &etapa,
			},
			outbound.Paginacion{
				Limite: 20,
			},
		)
		require.NoError(t, err)

		// Aunque los dos artículos coincidan con el filtro,
		// el folio debe aparecer una sola vez.
		require.Len(t, pagina.Items, 1)
		require.Equal(t, g.ID(), pagina.Items[0].ID())
		require.Len(t, pagina.Items[0].ArticulosForRepo(), 2)
	})
}
