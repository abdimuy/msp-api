//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	flotaapp "github.com/abdimuy/msp-api/internal/flota/app"
	flotadomain "github.com/abdimuy/msp-api/internal/flota/domain"
	"github.com/abdimuy/msp-api/internal/flota/ports/outbound"
)

// t0 is the instant of the first photograph in these tests.
var t0 = time.Date(2026, 8, 28, 14, 30, 0, 0, time.UTC)

// escenario wires a Service over the in-memory fakes and hands back the
// pieces a test needs to assert on.
type escenario struct {
	svc    *flotaapp.Service
	roster *rosterFake
	repo   *repoFake
	tx     *txFake
	reloj  *relojFijo
}

func nuevoEscenario(roster *rosterFake, previas ...*flotadomain.Asignacion) *escenario {
	repo := nuevoRepoFake(previas...)
	tx := &txFake{repo: repo}
	reloj := &relojFijo{t: t0}
	return &escenario{
		svc:    flotaapp.NewService(roster, repo, tx, reloj),
		roster: roster,
		repo:   repo,
		tx:     tx,
		reloj:  reloj,
	}
}

// ── Primera ejecución ────────────────────────────────────────────────────────

// TestTomarFoto_PrimeraEjecucionEsLineaBase is the production-day-one test.
// The first pass photographs everybody, writes the ledger row, and emits not
// one history row — the alternative would be sixty rows claiming people moved
// on the day we merely started watching.
func TestTomarFoto_PrimeraEjecucionEsLineaBase(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		usuario("uid-2", "ana@msp.com", "Ana", intPtr(11342)),
		usuario("uid-3", "oficina@msp.com", "Oficina", nil),
	}))

	res, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	assert.True(t, res.LineaBase)
	assert.Equal(t, 3, res.UsuariosObservados)
	assert.Equal(t, 3, res.Altas)
	assert.Equal(t, 0, res.CambiosDetectados)
	assert.Empty(t, e.repo.Cambios(), "la línea base no inventa cambios")
	assert.Equal(t, []string{"uid-1", "uid-2", "uid-3"}, e.repo.uids())

	require.Len(t, e.repo.Fotos(), 1)
	foto := e.repo.Fotos()[0]
	assert.True(t, foto.EsLineaBase())
	assert.Equal(t, t0, foto.EjecutadoEn())
	assert.Equal(t, 3, foto.UsuariosObservados())
}

// TestTomarFoto_SegundaEjecucionYaNoEsLineaBase verifies the baseline is
// decided by the ledger and only ever happens once.
func TestTomarFoto_SegundaEjecucionYaNoEsLineaBase(t *testing.T) {
	t.Parallel()

	foto := []outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	e := nuevoEscenario(nuevoRosterFake(foto, foto))

	primera, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	require.True(t, primera.LineaBase)

	e.reloj.avanzar(15 * time.Minute)
	segunda, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	assert.False(t, segunda.LineaBase)
	assert.Len(t, e.repo.Fotos(), 2, "cada pasada deja su fila en la bitácora de fotos")
}

// ── Sin cambios ──────────────────────────────────────────────────────────────

// TestTomarFoto_SinCambiosNoEscribeEnElEspejo verifies that an unchanged
// roster costs nothing but the ledger row. That row is deliberate: it is the
// positive control that separates "nothing changed" from "the worker died".
func TestTomarFoto_SinCambiosNoEscribeEnElEspejo(t *testing.T) {
	t.Parallel()

	foto := []outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		usuario("uid-2", "ana@msp.com", "Ana", nil),
	}
	e := nuevoEscenario(nuevoRosterFake(foto, foto, foto))

	for i := range 3 {
		res, err := e.svc.TomarFoto(context.Background())
		require.NoError(t, err)
		if i > 0 {
			assert.Equal(t, 0, res.Altas+res.Actualizaciones+res.Bajas,
				"pasada %d: una foto idéntica no debe tocar el espejo", i+1)
		}
		e.reloj.avanzar(15 * time.Minute)
	}

	assert.Empty(t, e.repo.Cambios(), "tres fotos idénticas no producen bitácora")
	assert.Equal(t, 3, e.roster.Leidas(), "cada pasada cuesta una lectura completa del padrón")
	assert.Len(t, e.repo.Fotos(), 3, "pero sí dejan constancia de que se miró")
	for _, f := range e.repo.Fotos() {
		assert.Equal(t, 0, f.CambiosDetectados())
	}
}

// ── Las cuatro transiciones, extremo a extremo ───────────────────────────────

// TestTomarFoto_CambioDeCamioneta is the module's reason to exist, exercised
// through the service: the window bounds must come from the previous ledger
// row, not from anywhere else.
func TestTomarFoto_CambioDeCamioneta(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))},
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11342))},
	))

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	e.reloj.avanzar(15 * time.Minute)
	res, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, res.CambiosDetectados)
	require.Len(t, e.repo.Cambios(), 1)
	c := e.repo.Cambios()[0]
	assert.Equal(t, flotadomain.TipoReasignacion, c.Tipo())
	assert.Equal(t, 11341, c.Anterior().ID())
	assert.Equal(t, 11342, c.Nueva().ID())
	assert.Equal(t, t0, c.VentanaDesde(), "el límite inferior es cuándo corrió la foto anterior")
	assert.Equal(t, t0.Add(15*time.Minute), c.DetectadoEn())

	cam, ok := e.repo.camionetaDe("uid-1")
	require.True(t, ok)
	assert.Equal(t, 11342, cam, "el espejo avanza en la misma pasada")
}

// TestTomarFoto_UsuarioNuevo covers a person appearing in the roster with a
// truck already assigned.
func TestTomarFoto_UsuarioNuevo(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))},
		[]outbound.RosterUsuario{
			usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
			usuario("uid-9", "nuevo@msp.com", "Nuevo", intPtr(51057)),
		},
	))

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	e.reloj.avanzar(15 * time.Minute)
	res, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, res.Altas)
	require.Len(t, e.repo.Cambios(), 1)
	assert.Equal(t, flotadomain.TipoAsignacion, e.repo.Cambios()[0].Tipo())
	assert.Equal(t, "uid-9", e.repo.Cambios()[0].UsuarioUID())
	assert.Equal(t, []string{"uid-1", "uid-9"}, e.repo.uids())
}

// TestTomarFoto_PierdeElCampo covers CAMIONETA_ASIGNADA disappearing from the
// document. It arrives at the service as a nil pointer, exactly as the
// Firestore adapter maps an absent field, an explicit null, and a zero.
func TestTomarFoto_PierdeElCampo(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))},
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", nil)},
	))

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	e.reloj.avanzar(15 * time.Minute)
	_, err = e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	require.Len(t, e.repo.Cambios(), 1)
	assert.Equal(t, flotadomain.TipoRetiro, e.repo.Cambios()[0].Tipo())
	assert.False(t, e.repo.Cambios()[0].Nueva().Asignada())

	cam, ok := e.repo.camionetaDe("uid-1")
	require.True(t, ok, "la persona sigue en el espejo: no se fue, le quitaron la camioneta")
	assert.Equal(t, 0, cam)
}

// TestTomarFoto_UsuarioBorrado covers the whole document disappearing, and
// pins that the history row survives the mirror delete — which is why
// MSP_FLOTA_ASIGNACION_CAMBIOS carries no foreign key.
func TestTomarFoto_UsuarioBorrado(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{
			usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
			usuario("uid-2", "ana@msp.com", "Ana", intPtr(11342)),
		},
		[]outbound.RosterUsuario{usuario("uid-2", "ana@msp.com", "Ana", intPtr(11342))},
	))

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	e.reloj.avanzar(15 * time.Minute)
	res, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, res.Bajas)
	assert.Equal(t, []string{"uid-2"}, e.repo.uids())

	require.Len(t, e.repo.Cambios(), 1)
	c := e.repo.Cambios()[0]
	assert.Equal(t, flotadomain.TipoBajaUsuario, c.Tipo())
	assert.Equal(t, "uid-1", c.UsuarioUID())
	assert.Equal(t, "eliseo@msp.com", c.Email(),
		"la bitácora sobrevive al borrado del espejo y conserva la identidad")
}

// ── Dos cambios en la misma ventana ──────────────────────────────────────────

// TestTomarFoto_DosCambiosEnLaMismaVentanaSePierdeElIntermedio is the honest
// statement of the method's limit, asserted through the service: the roster
// moved twice between photographs and only the endpoints are recorded.
func TestTomarFoto_DosCambiosEnLaMismaVentanaSePierdeElIntermedio(t *testing.T) {
	t.Parallel()

	roster := nuevoRosterFake(
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))},
		// 11342 ocurrió aquí dentro y nunca se fotografió.
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11343))},
	)
	e := nuevoEscenario(roster)

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	e.reloj.avanzar(15 * time.Minute)
	_, err = e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	require.Len(t, e.repo.Cambios(), 1, "una sola fila para dos movimientos")
	assert.Equal(t, 11341, e.repo.Cambios()[0].Anterior().ID())
	assert.Equal(t, 11343, e.repo.Cambios()[0].Nueva().ID())
}

// ── Fallos ───────────────────────────────────────────────────────────────────

// TestTomarFoto_FirestoreFallaNoEscribeNada verifies the module's reaction to
// the roster being unreadable: return the error, touch nothing, and — this is
// the part that matters — do not even open a transaction. A Firestore call
// can hang for its whole deadline, and holding a Firebird transaction across
// it would pin the oldest active transaction, which is the exact shape of the
// bug that froze the cobranza watermark.
func TestTomarFoto_FirestoreFallaNoEscribeNada(t *testing.T) {
	t.Parallel()

	roster := nuevoRosterFake()
	roster.fallarCon(errFirestore)
	e := nuevoEscenario(roster, flotadomain.HidratarAsignacion(flotadomain.HidratarAsignacionParams{
		UsuarioUID: "uid-1", Email: "eliseo@msp.com", Nombre: "Eliseo",
		Camioneta: flotadomain.CamionetaDesdeRoster(intPtr(11341)),
		CreatedAt: t0, UpdatedAt: t0,
	}))

	_, err := e.svc.TomarFoto(context.Background())
	require.ErrorIs(t, err, errFirestore)

	assert.Equal(t, 0, e.tx.Ejecutada, "no se abre transacción si no hay foto que aplicar")
	assert.Empty(t, e.repo.Cambios())
	assert.Empty(t, e.repo.Fotos(), "sin fila de foto: el hueco es la evidencia de la falla")
	assert.Equal(t, []string{"uid-1"}, e.repo.uids(), "el espejo queda intacto")
}

// TestTomarFoto_FirestoreFallaYSeRecupera verifies a failed pass is not
// terminal: the next one simply covers a longer window, which is why the
// worker logs and keeps ticking.
func TestTomarFoto_FirestoreFallaYSeRecupera(t *testing.T) {
	t.Parallel()

	roster := nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	})
	roster.fallarCon(errFirestore)
	e := nuevoEscenario(roster)

	_, err := e.svc.TomarFoto(context.Background())
	require.ErrorIs(t, err, errFirestore)

	roster.fallarCon(nil)
	res, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)
	assert.True(t, res.LineaBase, "la pasada fallida no consumió la línea base")
	assert.Len(t, e.repo.Fotos(), 1)
}

// TestTomarFoto_FalloAMitadDelPlanRevierteTodo is the "does not leave the
// table half written" test. The history insert succeeds and the mirror update
// fails; the transaction rolls back and BOTH have to be gone. A mirror that
// advanced past a change whose row was lost would make that change
// unrecoverable — Firestore keeps no history, which is the whole premise.
func TestTomarFoto_FalloAMitadDelPlanRevierteTodo(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))},
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11342))},
	))

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	e.repo.romperEn("ActualizarAsignaciones")
	e.reloj.avanzar(15 * time.Minute)
	_, err = e.svc.TomarFoto(context.Background())
	require.ErrorIs(t, err, errEscritura)

	assert.Equal(t, 1, e.tx.Revertida)
	assert.Empty(t, e.repo.Cambios(), "la fila de bitácora también se revierte")
	cam, ok := e.repo.camionetaDe("uid-1")
	require.True(t, ok)
	assert.Equal(t, 11341, cam, "el espejo NO puede quedar adelantado sin su bitácora")
	assert.Len(t, e.repo.Fotos(), 1, "la pasada fallida no registra foto")
}

// TestTomarFoto_FalloAlRegistrarLaFotoRevierteTodo covers the last statement
// of the pass. If the ledger row cannot be written, the whole pass must go
// back: a mirror that moved without a ledger row would leave the next window
// starting before changes that were already consumed.
func TestTomarFoto_FalloAlRegistrarLaFotoRevierteTodo(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))},
	))
	e.repo.romperEn("RegistrarFoto")

	_, err := e.svc.TomarFoto(context.Background())
	require.ErrorIs(t, err, errEscritura)

	assert.Empty(t, e.repo.uids(), "el espejo vuelve a estar vacío")
	assert.Empty(t, e.repo.Fotos())
}

// TestTomarFoto_FotoVaciaConEspejoLlenoAborta exercises the mass-deletion
// rail through the service. A Firestore that answers successfully with zero
// documents — which is what a revoked credential or a plan downgrade can look
// like — must not be allowed to wipe the mirror.
func TestTomarFoto_FotoVaciaConEspejoLlenoAborta(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake(
		[]outbound.RosterUsuario{
			usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
			usuario("uid-2", "ana@msp.com", "Ana", intPtr(11342)),
		},
		[]outbound.RosterUsuario{}, // el roster contesta bien, pero vacío
	))

	_, err := e.svc.TomarFoto(context.Background())
	require.NoError(t, err)

	e.reloj.avanzar(15 * time.Minute)
	_, err = e.svc.TomarFoto(context.Background())
	require.ErrorIs(t, err, flotadomain.ErrFotoVacia)

	assert.Equal(t, []string{"uid-1", "uid-2"}, e.repo.uids(), "nadie se borra por una lectura vacía")
	assert.Empty(t, e.repo.Cambios(), "y no se emite un aluvión de bajas")
	assert.Len(t, e.repo.Fotos(), 1)
}

// TestTomarFoto_EntradaSinUIDAborta verifies the service refuses a roster
// entry with no identifier instead of skipping it. Skipping would make that
// person look like a baja on this pass and an alta on the next.
func TestTomarFoto_EntradaSinUIDAborta(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		usuario("", "roto@msp.com", "Roto", intPtr(11342)),
	}))

	_, err := e.svc.TomarFoto(context.Background())
	require.ErrorIs(t, err, flotadomain.ErrUsuarioUIDRequerido)
	assert.Equal(t, 0, e.tx.Ejecutada)
	assert.Empty(t, e.repo.Fotos())
}
