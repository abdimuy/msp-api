// Package flotafb_test contains the Firebird integration tests for the flota
// repo. They skip cleanly when FB_DATABASE is unset (that is how CI stays
// green without a database), and every write runs inside
// fbtestutil.WithTestTransaction, which always rolls back — nothing this file
// does can survive into the shared dev database.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/flota/infra/flotafb/...
//
//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package flotafb_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	flotadomain "github.com/abdimuy/msp-api/internal/flota/domain"
	flotafb "github.com/abdimuy/msp-api/internal/flota/infra/flotafb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireFBEnv skips when there is no Firebird to talk to.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// Instants used throughout. Deliberately NOT time.Now(): the point of half
// these assertions is that the value read back is the value written, and a
// moving clock cannot prove that.
var (
	tPrevia = time.Date(2026, 8, 28, 14, 30, 0, 0, time.UTC)
	tActual = time.Date(2026, 8, 28, 14, 45, 0, 0, time.UTC)
)

// uidPrueba prefixes every uid this file writes, so a query can tell test
// rows apart from anything else even in the unlikely event a rollback failed.
const uidPrueba = "test-flota-"

func intPtr(n int) *int { return &n }

// observacion is the shorthand these tests build entities from.
func observacion(t *testing.T, uid, email, nombre string, camioneta *int) flotadomain.Observacion {
	t.Helper()
	o, err := flotadomain.NuevaObservacion(
		uidPrueba+uid, email, nombre, flotadomain.CamionetaDesdeRoster(camioneta),
	)
	require.NoError(t, err)
	return o
}

// TestRepo_EspejoIdaYVuelta writes the mirror and reads it back, which is the
// only way to prove the nullable camioneta column round-trips: an unassigned
// person must come back unassigned, not as camioneta zero.
func TestRepo_EspejoIdaYVuelta(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := flotafb.New(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		conCamioneta := flotadomain.NuevaAsignacion(
			observacion(t, "1", "eliseo.ramirez@muebleriamsp.mx", "Eliseo Ramírez", intPtr(11341)),
			tPrevia,
		)
		sinCamioneta := flotadomain.NuevaAsignacion(
			observacion(t, "2", "lucia.hernandez@muebleriamsp.mx", "Lucía Hernández", nil),
			tPrevia,
		)
		require.NoError(t, repo.InsertarAsignaciones(ctx,
			[]*flotadomain.Asignacion{conCamioneta, sinCamioneta}))

		leidas := leerEspejoDePrueba(ctx, t, repo)
		require.Len(t, leidas, 2)

		a := leidas[uidPrueba+"1"]
		require.NotNil(t, a)
		assert.True(t, a.Camioneta().Asignada())
		assert.Equal(t, 11341, a.Camioneta().ID())
		assert.Equal(t, "eliseo.ramirez@muebleriamsp.mx", a.Email())
		assert.Equal(t, "Eliseo Ramírez", a.Nombre(), "el acento sobrevive el viaje en UTF8")
		// The timestamp contract: written through ToWallClock, read back
		// through ScanUTCTime, and equal to the UTC instant we started with.
		assert.True(t, a.Audit().CreatedAt().Equal(tPrevia),
			"created_at leído %s, escrito %s", a.Audit().CreatedAt(), tPrevia)
		assert.Equal(t, time.UTC, a.Audit().CreatedAt().Location())

		b := leidas[uidPrueba+"2"]
		require.NotNil(t, b)
		assert.False(t, b.Camioneta().Asignada(),
			"NULL debe volver como 'sin camioneta', no como camioneta 0")
	})
}

// TestRepo_ActualizarYEliminar exercises the other two mirror operations.
func TestRepo_ActualizarYEliminar(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := flotafb.New(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		a := flotadomain.NuevaAsignacion(
			observacion(t, "1", "eliseo.ramirez@muebleriamsp.mx", "Eliseo Ramírez", intPtr(11341)),
			tPrevia,
		)
		require.NoError(t, repo.InsertarAsignaciones(ctx, []*flotadomain.Asignacion{a}))

		// Moved to another truck.
		a.Observar(observacion(t, "1", "eliseo.ramirez@muebleriamsp.mx", "Eliseo Ramírez", intPtr(11342)), tActual)
		require.NoError(t, repo.ActualizarAsignaciones(ctx, []*flotadomain.Asignacion{a}))

		leidas := leerEspejoDePrueba(ctx, t, repo)
		require.Len(t, leidas, 1)
		assert.Equal(t, 11342, leidas[uidPrueba+"1"].Camioneta().ID())
		assert.True(t, leidas[uidPrueba+"1"].Audit().UpdatedAt().Equal(tActual))

		// Truck taken away — the update has to be able to write NULL back.
		a.Observar(observacion(t, "1", "eliseo.ramirez@muebleriamsp.mx", "Eliseo Ramírez", nil), tActual)
		require.NoError(t, repo.ActualizarAsignaciones(ctx, []*flotadomain.Asignacion{a}))
		leidas = leerEspejoDePrueba(ctx, t, repo)
		assert.False(t, leidas[uidPrueba+"1"].Camioneta().Asignada())

		// Gone from the roster.
		require.NoError(t, repo.EliminarAsignaciones(ctx, []string{uidPrueba + "1"}))
		assert.Empty(t, leerEspejoDePrueba(ctx, t, repo))
	})
}

// TestRepo_UltimaFotoYLineaBase verifies the signal the whole baseline
// decision rests on. Note this test cannot assert "empty ledger" against the
// dev database, because a previous run of the worker may have left rows — so
// it asserts the ordering contract instead, which is what UltimaFoto is for.
func TestRepo_UltimaFotoYLineaBase(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := flotafb.New(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Two future-dated rows: whatever the table already holds, these are
		// the newest, so the ordering contract is testable in isolation.
		vieja := tPrevia.AddDate(50, 0, 0)
		nueva := tActual.AddDate(50, 0, 0)
		require.NoError(t, repo.RegistrarFoto(ctx, flotadomain.NuevaFoto(vieja, 62, 0, true)))
		require.NoError(t, repo.RegistrarFoto(ctx, flotadomain.NuevaFoto(nueva, 62, 3, false)))

		ultima, hay, err := repo.UltimaFoto(ctx)
		require.NoError(t, err)
		require.True(t, hay)
		assert.True(t, ultima.Equal(nueva),
			"UltimaFoto debe devolver la MÁS RECIENTE (%s), devolvió %s", nueva, ultima)
		assert.Equal(t, time.UTC, ultima.Location())
	})
}

// TestRepo_InsertarCambiosGuardaLaVentanaCompleta is the test that protects
// the module's honesty at the storage layer: the two bounds and the tipo have
// to survive the round trip, because a history row without them says nothing.
func TestRepo_InsertarCambiosGuardaLaVentanaCompleta(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := flotafb.New(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		previa := flotadomain.HidratarAsignacion(flotadomain.HidratarAsignacionParams{
			UsuarioUID: uidPrueba + "1",
			Email:      "eliseo.ramirez@muebleriamsp.mx",
			Nombre:     "Eliseo Ramírez",
			Camioneta:  flotadomain.CamionetaDesdeRoster(intPtr(11341)),
			CreatedAt:  tPrevia,
			UpdatedAt:  tPrevia,
		})
		plan, err := flotadomain.Comparar(
			[]*flotadomain.Asignacion{previa},
			[]flotadomain.Observacion{
				observacion(t, "1", "eliseo.ramirez@muebleriamsp.mx", "Eliseo Ramírez", intPtr(11342)),
			},
			flotadomain.ParametrosComparacion{VentanaDesde: tPrevia, DetectadoEn: tActual},
		)
		require.NoError(t, err)
		require.Len(t, plan.Cambios, 1)
		require.NoError(t, repo.InsertarCambios(ctx, plan.Cambios))

		fila := leerCambio(ctx, t, pool, plan.Cambios[0].ID().String())
		assert.Equal(t, uidPrueba+"1", fila.uid)
		assert.Equal(t, string(flotadomain.TipoReasignacion), fila.tipo)
		require.True(t, fila.anterior.Valid)
		assert.Equal(t, int64(11341), fila.anterior.Int64)
		require.True(t, fila.nueva.Valid)
		assert.Equal(t, int64(11342), fila.nueva.Int64)
		assert.True(t, fila.ventanaDesde.Equal(tPrevia), "ventana_desde: %s", fila.ventanaDesde)
		assert.True(t, fila.detectadoEn.Equal(tActual), "detectado_en: %s", fila.detectadoEn)
		assert.True(t, fila.detectadoEn.After(fila.ventanaDesde),
			"la ventana siempre avanza: no se puede detectar antes de la foto previa")
	})
}

// TestRepo_CambioDeBajaGuardaNuevaEnNull verifies the nullable end of a
// transition. 'retiro' and 'baja_usuario' both land with CAMIONETA_NUEVA
// NULL, and a column that silently stored 0 instead would make them
// indistinguishable from a real almacén id.
func TestRepo_CambioDeBajaGuardaNuevaEnNull(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := flotafb.New(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		previa := flotadomain.HidratarAsignacion(flotadomain.HidratarAsignacionParams{
			UsuarioUID: uidPrueba + "9",
			Email:      "sofia.mendoza@muebleriamsp.mx",
			Nombre:     "Sofía Mendoza",
			Camioneta:  flotadomain.CamionetaDesdeRoster(intPtr(3051068)),
			CreatedAt:  tPrevia,
			UpdatedAt:  tPrevia,
		})
		plan, err := flotadomain.Comparar(
			[]*flotadomain.Asignacion{previa},
			// La persona desapareció del padrón por completo.
			[]flotadomain.Observacion{observacion(t, "otro", "otro@muebleriamsp.mx", "Otro", nil)},
			flotadomain.ParametrosComparacion{VentanaDesde: tPrevia, DetectadoEn: tActual},
		)
		require.NoError(t, err)
		require.Len(t, plan.Cambios, 1)
		require.NoError(t, repo.InsertarCambios(ctx, plan.Cambios))

		fila := leerCambio(ctx, t, pool, plan.Cambios[0].ID().String())
		assert.Equal(t, string(flotadomain.TipoBajaUsuario), fila.tipo)
		assert.True(t, fila.anterior.Valid)
		assert.False(t, fila.nueva.Valid, "sin camioneta se guarda como NULL, nunca como 0")
		assert.Equal(t, "sofia.mendoza@muebleriamsp.mx", fila.email,
			"la identidad se conserva aunque el usuario ya no exista")
	})
}

// TestRepo_LaBitacoraSobreviveAlBorradoDelEspejo pins the reason
// MSP_FLOTA_ASIGNACION_CAMBIOS carries no foreign key. If it did, deleting
// the mirror row of somebody who left would take their history with it —
// destroying the record of the very event we are trying to capture.
func TestRepo_LaBitacoraSobreviveAlBorradoDelEspejo(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := flotafb.New(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		a := flotadomain.NuevaAsignacion(
			observacion(t, "1", "eliseo.ramirez@muebleriamsp.mx", "Eliseo Ramírez", intPtr(11341)),
			tPrevia,
		)
		require.NoError(t, repo.InsertarAsignaciones(ctx, []*flotadomain.Asignacion{a}))

		plan, err := flotadomain.Comparar(
			[]*flotadomain.Asignacion{a},
			[]flotadomain.Observacion{observacion(t, "otro", "otro@muebleriamsp.mx", "Otro", nil)},
			flotadomain.ParametrosComparacion{VentanaDesde: tPrevia, DetectadoEn: tActual},
		)
		require.NoError(t, err)
		require.NoError(t, repo.InsertarCambios(ctx, plan.Cambios))
		require.NoError(t, repo.EliminarAsignaciones(ctx, plan.Bajas))

		assert.Empty(t, leerEspejoDePrueba(ctx, t, repo), "el espejo sí se borra")
		fila := leerCambio(ctx, t, pool, plan.Cambios[0].ID().String())
		assert.Equal(t, uidPrueba+"1", fila.uid, "la bitácora NO se borra con él")
	})
}

// TestRepo_TipoInvalidoLoRechazaLaBase is the defence-in-depth check. The
// canonical rule lives in TipoCambio.Valido; this proves the CHECK constraint
// of migration 000062 mirrors it, so a future code path that invents a tipo
// cannot quietly write it.
func TestRepo_TipoInvalidoLoRechazaLaBase(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		_, err := q.ExecContext(ctx, `
			INSERT INTO MSP_FLOTA_ASIGNACION_CAMBIOS (
				ID, USUARIO_UID, EMAIL, NOMBRE, CAMIONETA_ANTERIOR, CAMIONETA_NUEVA,
				TIPO, VENTANA_DESDE, DETECTADO_EN, CREATED_AT
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"00000000-0000-0000-0000-0000000000ff", uidPrueba+"x", nil, nil, nil, nil,
			"cambiado", // no está en el catálogo
			firebird.ToWallClock(tPrevia), firebird.ToWallClock(tActual),
			firebird.ToWallClock(tActual),
		)
		require.Error(t, err, "la base debe rechazar un tipo fuera del catálogo")
	})
}

// ── helpers de lectura ───────────────────────────────────────────────────────

// leerEspejoDePrueba returns only the rows this file created, keyed by uid.
// Reading through the repo (rather than raw SQL) is deliberate: it exercises
// ListarAsignaciones and its ScanUTCTime path on every call.
func leerEspejoDePrueba(
	ctx context.Context, t *testing.T, repo *flotafb.Repo,
) map[string]*flotadomain.Asignacion {
	t.Helper()
	todas, err := repo.ListarAsignaciones(ctx)
	require.NoError(t, err)
	out := make(map[string]*flotadomain.Asignacion)
	for _, a := range todas {
		if len(a.UsuarioUID()) >= len(uidPrueba) && a.UsuarioUID()[:len(uidPrueba)] == uidPrueba {
			out[a.UsuarioUID()] = a
		}
	}
	return out
}

// filaCambio is one raw history row, read with SQL. The repo has no read
// method for the history on purpose — the module's query surface IS SQL — so
// the verification here uses the same tool an operator would.
type filaCambio struct {
	uid                       string
	email                     string
	tipo                      string
	anterior, nueva           sql.NullInt64
	ventanaDesde, detectadoEn time.Time
}

func leerCambio(ctx context.Context, t *testing.T, pool *firebird.Pool, id string) filaCambio {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var (
		f                    filaCambio
		email                sql.NullString
		ventanaRaw, detecRaw any
	)
	err := q.QueryRowContext(ctx, `
		SELECT USUARIO_UID, EMAIL, TIPO, CAMIONETA_ANTERIOR, CAMIONETA_NUEVA,
		       VENTANA_DESDE, DETECTADO_EN
		FROM MSP_FLOTA_ASIGNACION_CAMBIOS
		WHERE ID = ?`, id).
		Scan(&f.uid, &email, &f.tipo, &f.anterior, &f.nueva, &ventanaRaw, &detecRaw)
	require.NoError(t, err)
	f.email = email.String

	f.ventanaDesde, err = firebird.ScanUTCTime(ventanaRaw)
	require.NoError(t, err)
	f.detectadoEn, err = firebird.ScanUTCTime(detecRaw)
	require.NoError(t, err)
	return f
}
