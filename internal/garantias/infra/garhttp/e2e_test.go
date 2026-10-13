//nolint:paralleltest // Firebird integration test uses a shared rollback transaction.
package garhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

func seedHTTPGarantia(
	ctx context.Context,
	t *testing.T,
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

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       folio,
		Origen:      domain.OrigenFolioPiso,
		Description: "Garantía E2E de bandeja",
		AbiertoPor:  "ruben",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	articuloID := 900000 + numero

	err = g.AgregarArticulo(
		domain.AgregarArticuloParams{
			ArticuloID:  &articuloID,
			Clave:       "E2E-BANDEJA",
			Description: "Artículo para prueba HTTP E2E",
		},
		domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Second),
		},
		now.Add(time.Second),
	)
	require.NoError(t, err)

	if enProceso {
		err = g.IniciarProceso(
			domain.ActorParams{
				Usuario:           "ruben",
				ClaveIdempotencia: uuid.NewString(),
				DeviceCreatedAt:   now.Add(2 * time.Second),
			},
			now.Add(2*time.Second),
		)
		require.NoError(t, err)
	}

	require.NoError(t, repo.Crear(ctx, g))

	return g
}

func TestGarHTTP_E2E_BandejaFiltradaConFirebird(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	garantias := garfb.NewGarantiaRepo(pool)
	bandeja := garfb.NewBandejaRepo(pool)
	eventos := garfb.NewEventoRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		base := time.Date(
			2026, time.October, 13,
			16, 0, 0, 0,
			time.UTC,
		)

		primera := seedHTTPGarantia(
			ctx,
			t,
			garantias,
			folios,
			base,
			true,
		)

		_ = seedHTTPGarantia(
			ctx,
			t,
			garantias,
			folios,
			base.Add(time.Minute),
			false,
		)

		ultima := seedHTTPGarantia(
			ctx,
			t,
			garantias,
			folios,
			base.Add(2*time.Minute),
			true,
		)

		identity := &fakeHTTPIdentity{
			usuario: outbound.Usuario{
				ID:     "usuario-1",
				Nombre: "Ruben",
			},
			permitido: true,
		}

		router := buildHTTPRouter(
			identity,
			garantias,
			bandeja,
			eventos,
		)

		req := httptest.NewRequest(
			http.MethodGet,
			"/garantias?estado=en_proceso&limit=20",
			nil,
		).WithContext(ctx)

		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(
			t,
			http.StatusOK,
			rec.Code,
			rec.Body.String(),
		)

		var body struct {
			Items []struct {
				ID     string `json:"id"`
				Estado string `json:"estado"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}

		require.NoError(
			t,
			json.Unmarshal(rec.Body.Bytes(), &body),
		)

		require.Len(t, body.Items, 2)

		require.Equal(
			t,
			[]string{
				ultima.ID().String(),
				primera.ID().String(),
			},
			[]string{
				body.Items[0].ID,
				body.Items[1].ID,
			},
		)

		require.Equal(t, "en_proceso", body.Items[0].Estado)
		require.Equal(t, "en_proceso", body.Items[1].Estado)
		require.Empty(t, body.NextCursor)
		require.False(t, body.HasMore)
	})
}
