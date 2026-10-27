//nolint:paralleltest,testpackage // Firebird integration test needs package access and runs serially.
package garfb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nakagami/firebirdsql"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
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

func crearGarantiaParaConteoBandeja(
	ctx context.Context,
	t *testing.T,
	repo *GarantiaRepo,
	folios *FolioGenerator,
	now time.Time,
	articuloBase int,
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
		Description: "Garantía para contar consultas de bandeja",
		AbiertoPor:  "ruben",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	for i := 0; i < 2; i++ { //nolint:intrange
		articuloID := articuloBase + i

		err = g.AgregarArticulo(
			domain.AgregarArticuloParams{
				ArticuloID:  &articuloID,
				Clave:       []string{"COUNT-01", "COUNT-02"}[i],
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

	require.NoError(t, repo.Crear(ctx, g))

	return g
}

func TestBandejaRepo_ListarUsaDosConsultasPorPagina(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantiaRepo := NewGarantiaRepo(pool)
	bandejaRepo := NewBandejaRepo(pool)
	folios := NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(2099, 2, 6, 9, 0, 0, 0, time.UTC)
		desde := base.Add(-time.Second)
		hasta := base.Add(3 * time.Hour)

		for i := 0; i < 3; i++ { //nolint:intrange
			crearGarantiaParaConteoBandeja(
				ctx,
				t,
				garantiaRepo,
				folios,
				base.Add(time.Duration(i)*time.Hour),
				800000+(i*10),
			)
		}

		counter := &countingQuerier{}

		bandejaRepo.decorateQuerier = func(
			q firebird.Querier,
		) firebird.Querier {
			counter.base = q
			return counter
		}

		filtros := outbound.ListarGarantiasFiltros{
			Desde: &desde,
			Hasta: &hasta,
		}

		pagina1, err := bandejaRepo.Listar(
			ctx,
			filtros,
			outbound.Paginacion{
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina1.Items, 2)
		require.NotEmpty(t, pagina1.SiguienteCursor)
		require.Equal(t, 2, counter.queries)

		counter.queries = 0

		pagina2, err := bandejaRepo.Listar(
			ctx,
			filtros,
			outbound.Paginacion{
				Cursor: pagina1.SiguienteCursor,
				Limite: 2,
			},
		)
		require.NoError(t, err)
		require.Len(t, pagina2.Items, 1)
		require.Empty(t, pagina2.SiguienteCursor)
		require.Equal(t, 2, counter.queries)
	})
}

func TestEsClaveDuplicada_DistingueRestriccionReal(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := NewGarantiaRepo(pool)
	folios := NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		now := time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)

		numero, err := folios.Siguiente(ctx)
		require.NoError(t, err)

		folio, err := domain.NewFolio(numero)
		require.NoError(t, err)

		origen, err := domain.ParseOrigenFolio("piso")
		require.NoError(t, err)

		g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Garantía para probar clave duplicada",
			AbiertoPor:  "ruben",
			Now:         now,
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now,
			},
		})
		require.NoError(t, err)

		require.NoError(t, repo.Crear(ctx, g))

		eventos := g.EventosPendientesForRepo()
		require.NotEmpty(t, eventos)

		evento := eventos[0]

		tx, err := firebird.RequireTx(ctx)
		require.NoError(t, err)

		// Mismo CLAVE_IDEMPOTENCIA con otro ID:
		// debe chocar contra UQ_MSP_GA_EVENTO_CLAVE.
		_, err = tx.ExecContext(
			ctx,
			insertarEventoSQL,
			uuid.NewString(),
			evento.GarantiaID().String(),
			uuidPtrArg(evento.ArticuloRef()),
			string(evento.Tipo()),
			nullableString(evento.Description()),
			enumPtrArg(evento.EtapaDesde()),
			enumPtrArg(evento.EtapaHasta()),
			enumPtrArg(evento.UbicacionDesde()),
			enumPtrArg(evento.UbicacionHasta()),
			evento.Usuario(),
			enumPtrArg(evento.RolDecisor()),
			evento.GPSLat(),
			evento.GPSLon(),
			firebird.ToWallClock(evento.CreatedAt()),
			firebird.ToWallClock(evento.DeviceCreatedAt()),
			evento.ClaveIdempotencia(),
		)
		require.Error(t, err)
		require.True(t, esClaveDuplicada(err))

		// Control: otro UNIQUE de la base, el FOLIO.
		// No debe confundirse con la clave de idempotencia.
		g2, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:       folio,
			Origen:      origen,
			Description: "Otra garantía con el mismo folio",
			AbiertoPor:  "ruben",
			Now:         now.Add(time.Hour),
			Actor: domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(time.Hour),
			},
		})
		require.NoError(t, err)

		err = repo.Crear(ctx, g2)
		require.Error(t, err)
		require.False(t, esClaveDuplicada(err))
	})
}

func TestEsClaveDuplicada_RequiereCodigoYRestriccion(t *testing.T) {
	t.Run("codigo_y_restriccion_correctos", func(t *testing.T) {
		err := &firebirdsql.FbError{
			GDSCodes: []int{335544665},
			Message:  "violation of PRIMARY or UNIQUE KEY constraint UQ_MSP_GA_EVENTO_CLAVE",
		}

		require.True(t, esClaveDuplicada(err))
	})

	t.Run("nombre_sin_codigo_no_basta", func(t *testing.T) {
		err := &firebirdsql.FbError{
			GDSCodes: []int{335544347},
			Message:  "violation of PRIMARY or UNIQUE KEY constraint UQ_MSP_GA_EVENTO_CLAVE",
		}

		require.False(t, esClaveDuplicada(err))
	})

	t.Run("codigo_con_otra_restriccion_no_basta", func(t *testing.T) {
		err := &firebirdsql.FbError{
			GDSCodes: []int{335544665},
			Message:  "violation of PRIMARY or UNIQUE KEY constraint PK_MSP_GA_EVENTO",
		}

		require.False(t, esClaveDuplicada(err))
	})
}
