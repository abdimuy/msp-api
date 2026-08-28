package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/flota/domain"
)

// The two ends of every detection window used in these tests. They are real
// instants rather than time.Now() so an assertion about VentanaDesde /
// DetectadoEn is checkable instead of approximate.
var (
	fotoAnterior = time.Date(2026, 8, 28, 14, 30, 0, 0, time.UTC)
	fotoActual   = time.Date(2026, 8, 28, 14, 45, 0, 0, time.UTC)
)

// paramsNormales frames a regular (non-baseline) comparison.
func paramsNormales() domain.ParametrosComparacion {
	return domain.ParametrosComparacion{
		LineaBase:    false,
		VentanaDesde: fotoAnterior,
		DetectadoEn:  fotoActual,
	}
}

// obs builds an observation, failing the test if it does not validate.
func obs(t *testing.T, uid, email, nombre string, camioneta *int) domain.Observacion {
	t.Helper()
	o, err := domain.NuevaObservacion(uid, email, nombre, domain.CamionetaDesdeRoster(camioneta))
	require.NoError(t, err)
	return o
}

// espejo builds a mirror row as the repository would have hydrated it.
func espejo(uid, email, nombre string, camioneta *int) *domain.Asignacion {
	return domain.HidratarAsignacion(domain.HidratarAsignacionParams{
		UsuarioUID: uid,
		Email:      email,
		Nombre:     nombre,
		Camioneta:  domain.CamionetaDesdeRoster(camioneta),
		CreatedAt:  fotoAnterior,
		UpdatedAt:  fotoAnterior,
	})
}

// ── Primera ejecución ────────────────────────────────────────────────────────

// TestComparar_LineaBaseNoInventaCambios is the test that protects the first
// day in production. The baseline pass must record everybody in the mirror
// and emit ZERO history rows: nobody moved, we merely started looking.
func TestComparar_LineaBaseNoInventaCambios(t *testing.T) {
	t.Parallel()

	observadas := []domain.Observacion{
		obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		obs(t, "uid-2", "ana@msp.com", "Ana", intPtr(11342)),
		obs(t, "uid-3", "luis@msp.com", "Luis", nil),
	}

	plan, err := domain.Comparar(nil, observadas, domain.ParametrosComparacion{
		LineaBase:   true,
		DetectadoEn: fotoActual,
	})
	require.NoError(t, err)

	assert.Len(t, plan.Altas, 3, "los tres quedan retratados en el espejo")
	assert.Empty(t, plan.Cambios,
		"la primera foto no tiene con qué comparar: no puede afirmar que alguien se movió")
	assert.Empty(t, plan.Bajas)
	assert.Empty(t, plan.Actualizaciones)
}

// TestComparar_LineaBaseConEspejoSucioTampocoInventa covers the awkward state
// where the mirror holds rows but the ledger says we have never run (somebody
// truncated MSP_FLOTA_FOTOS, or restored a partial backup). The diff still
// runs — the mirror has to converge — but no history is asserted, because the
// module genuinely does not know when those rows were observed.
func TestComparar_LineaBaseConEspejoSucioTampocoInventa(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(99999))}

	plan, err := domain.Comparar(previas, observadas, domain.ParametrosComparacion{
		LineaBase:   true,
		DetectadoEn: fotoActual,
	})
	require.NoError(t, err)

	assert.Len(t, plan.Actualizaciones, 1, "el espejo sí se pone al día")
	assert.Empty(t, plan.Cambios, "pero no se afirma un cambio que no se puede fechar")
}

// ── Sin cambios ──────────────────────────────────────────────────────────────

// TestComparar_SinCambiosNoEscribeNada is the cadence test: 96 passes a day
// against an unchanging roster must touch zero rows.
func TestComparar_SinCambiosNoEscribeNada(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{
		espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		espejo("uid-2", "ana@msp.com", "Ana", nil),
	}
	observadas := []domain.Observacion{
		obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		obs(t, "uid-2", "ana@msp.com", "Ana", nil),
	}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	assert.True(t, plan.SinEscrituras(), "una foto idéntica no debe producir ninguna escritura")
}

// TestComparar_SinCambiosToleraDiferenciasDeFormato verifies that casing and
// surrounding whitespace do not count as a change. Without the normalisation
// in NuevaObservacion this would rewrite the mirror on every pass.
func TestComparar_SinCambiosToleraDiferenciasDeFormato(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{obs(t, "uid-1", "  Eliseo@MSP.com ", " Eliseo ", intPtr(11341))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)
	assert.True(t, plan.SinEscrituras())
}

// ── Las cuatro transiciones ──────────────────────────────────────────────────

// TestComparar_ReasignacionDeCamioneta is the case that motivated the module:
// somebody was moved from one truck to another between two photographs.
func TestComparar_ReasignacionDeCamioneta(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11342))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	require.Len(t, plan.Cambios, 1)
	c := plan.Cambios[0]
	assert.Equal(t, domain.TipoReasignacion, c.Tipo())
	assert.Equal(t, "uid-1", c.UsuarioUID())
	assert.Equal(t, 11341, c.Anterior().ID())
	assert.Equal(t, 11342, c.Nueva().ID())
	assert.Equal(t, "eliseo@msp.com", c.Email())

	// The two bounds are the whole honesty of the module: all it can say is
	// that the move happened inside this window.
	assert.Equal(t, fotoAnterior, c.VentanaDesde())
	assert.Equal(t, fotoActual, c.DetectadoEn())
	assert.NotEqual(t, uuidNil, c.ID().String(), "el id lo genera Go, no la base")

	assert.Len(t, plan.Actualizaciones, 1, "el espejo avanza junto con la bitácora")
	assert.Equal(t, 11342, plan.Actualizaciones[0].Camioneta().ID())
	assert.Equal(t, fotoActual, plan.Actualizaciones[0].Audit().UpdatedAt())
}

// uuidNil is the zero UUID string, used to assert an id was actually minted.
const uuidNil = "00000000-0000-0000-0000-000000000000"

// TestComparar_UsuarioNuevoConCamioneta covers somebody who appears in the
// roster already driving. It is an 'asignacion', not a special "new user"
// type: from the camioneta's point of view a truck went from nobody to them.
func TestComparar_UsuarioNuevoConCamioneta(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{
		obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		obs(t, "uid-9", "nuevo@msp.com", "Nuevo", intPtr(51057)),
	}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	require.Len(t, plan.Altas, 1)
	assert.Equal(t, "uid-9", plan.Altas[0].UsuarioUID())
	assert.Equal(t, fotoActual, plan.Altas[0].Audit().CreatedAt())

	require.Len(t, plan.Cambios, 1)
	assert.Equal(t, domain.TipoAsignacion, plan.Cambios[0].Tipo())
	assert.False(t, plan.Cambios[0].Anterior().Asignada())
	assert.Equal(t, 51057, plan.Cambios[0].Nueva().ID())
}

// TestComparar_UsuarioNuevoSinCamionetaNoEsBitacora covers the other half of
// "a new user": an office hire with no truck. The mirror records them so the
// day they get one it reads as an assignment, but nothing goes in the history
// — the bitácora is about camionetas, and no camioneta moved.
func TestComparar_UsuarioNuevoSinCamionetaNoEsBitacora(t *testing.T) {
	t.Parallel()

	observadas := []domain.Observacion{obs(t, "uid-9", "oficina@msp.com", "Oficina", nil)}

	plan, err := domain.Comparar(nil, observadas, paramsNormales())
	require.NoError(t, err)

	assert.Len(t, plan.Altas, 1, "se retrata para poder comparar la próxima vez")
	assert.Empty(t, plan.Cambios, "nadie tocó ninguna camioneta")
}

// TestComparar_LeQuitanLaCamioneta covers the field disappearing from the
// document (or being nulled or zeroed) while the person stays in the roster.
func TestComparar_LeQuitanLaCamioneta(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{obs(t, "uid-1", "eliseo@msp.com", "Eliseo", nil)}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	require.Len(t, plan.Cambios, 1)
	assert.Equal(t, domain.TipoRetiro, plan.Cambios[0].Tipo(),
		"la persona sigue en el padrón: es retiro, no baja")
	assert.Equal(t, 11341, plan.Cambios[0].Anterior().ID())
	assert.False(t, plan.Cambios[0].Nueva().Asignada())

	assert.Empty(t, plan.Bajas, "el usuario no se fue")
	require.Len(t, plan.Actualizaciones, 1)
	assert.False(t, plan.Actualizaciones[0].Camioneta().Asignada())
}

// TestComparar_UsuarioBorrado covers the document disappearing entirely. It
// is typed differently from a retiro on purpose: nobody took the truck away,
// the person stopped existing in the roster, and an investigation asks a
// different question of each.
func TestComparar_UsuarioBorrado(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{
		espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		espejo("uid-2", "ana@msp.com", "Ana", intPtr(11342)),
	}
	observadas := []domain.Observacion{obs(t, "uid-2", "ana@msp.com", "Ana", intPtr(11342))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	assert.Equal(t, []string{"uid-1"}, plan.Bajas)
	require.Len(t, plan.Cambios, 1)
	c := plan.Cambios[0]
	assert.Equal(t, domain.TipoBajaUsuario, c.Tipo())
	assert.Equal(t, "uid-1", c.UsuarioUID())
	assert.Equal(t, 11341, c.Anterior().ID())
	assert.False(t, c.Nueva().Asignada())
	assert.Equal(t, "eliseo@msp.com", c.Email(),
		"la identidad sólo puede venir del espejo: el documento ya no existe")
}

// TestComparar_UsuarioBorradoSinCamionetaNoEsBitacora is the mirror image of
// the new-hire case: somebody who never had a truck leaving is a mirror
// delete and nothing more.
func TestComparar_UsuarioBorradoSinCamionetaNoEsBitacora(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{
		espejo("uid-7", "oficina@msp.com", "Oficina", nil),
		espejo("uid-2", "ana@msp.com", "Ana", intPtr(11342)),
	}
	observadas := []domain.Observacion{obs(t, "uid-2", "ana@msp.com", "Ana", intPtr(11342))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	assert.Equal(t, []string{"uid-7"}, plan.Bajas)
	assert.Empty(t, plan.Cambios, "quien nunca tuvo camioneta se va sin dejar fila en la bitácora")
}

// TestComparar_CambioDeNombreNoEsCambioDeCamioneta verifies the mirror moves
// while the history stays quiet. A rename is not a fleet event, and letting
// it into the bitácora would bury the real ones.
func TestComparar_CambioDeNombreNoEsCambioDeCamioneta(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{obs(t, "uid-1", "eliseo@msp.com", "Eliseo Ramírez", intPtr(11341))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	require.Len(t, plan.Actualizaciones, 1)
	assert.Equal(t, "Eliseo Ramírez", plan.Actualizaciones[0].Nombre())
	assert.Empty(t, plan.Cambios)
}

// ── La pérdida que el método tiene y no puede evitar ─────────────────────────

// TestComparar_DosCambiosEnLaMismaVentanaColapsan documents a real limitation
// rather than a behaviour anybody wants: if Eliseo goes 11341 → 11342 → 11343
// between two photographs, the module sees 11341 → 11343 and the stop at
// 11342 is gone forever. There is no way for a polling design to recover it;
// a shorter interval lowers the odds and never removes them.
//
// The test exists so nobody later reads a single 'reasignacion' row and
// concludes the truck was only ever changed once.
func TestComparar_DosCambiosEnLaMismaVentanaColapsan(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	// 11342 happened inside the window and was never photographed.
	observadas := []domain.Observacion{obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11343))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	require.Len(t, plan.Cambios, 1, "dos movimientos en la misma ventana producen UNA sola fila")
	assert.Equal(t, 11341, plan.Cambios[0].Anterior().ID())
	assert.Equal(t, 11343, plan.Cambios[0].Nueva().ID())
	assert.NotEqual(t, 11342, plan.Cambios[0].Nueva().ID(),
		"el paso intermedio no existe para este sistema y no se puede recuperar")
}

// TestComparar_IdaYVueltaEnLaMismaVentanaEsInvisible is the worst shape of
// the same limitation: 11341 → 11342 → 11341 leaves the roster exactly as it
// was, so the photograph shows nothing and no row is written at all.
func TestComparar_IdaYVueltaEnLaMismaVentanaEsInvisible(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}
	observadas := []domain.Observacion{obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341))}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)
	assert.True(t, plan.SinEscrituras(),
		"un cambio que se revierte dentro de la ventana es indetectable por construcción")
}

// ── Rieles de seguridad ──────────────────────────────────────────────────────

// TestComparar_FotoVaciaConEspejoLlenoAborta protects against the failure the
// project has actually lived through: Firestore going quiet (revoked
// credential, silent downgrade to the free plan) and returning zero
// documents. Applying that would delete every mirror row and emit a flood of
// bajas. The pass fails instead and writes nothing.
func TestComparar_FotoVaciaConEspejoLlenoAborta(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{
		espejo("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		espejo("uid-2", "ana@msp.com", "Ana", intPtr(11342)),
	}

	plan, err := domain.Comparar(previas, nil, paramsNormales())
	require.ErrorIs(t, err, domain.ErrFotoVacia)
	assert.True(t, plan.SinEscrituras(), "un plan abortado no puede traer escrituras")
}

// TestComparar_FotoVaciaConEspejoVacioEsLegitima verifies the rail does not
// fire on a genuinely empty start — an empty roster and an empty mirror agree
// with each other.
func TestComparar_FotoVaciaConEspejoVacioEsLegitima(t *testing.T) {
	t.Parallel()

	plan, err := domain.Comparar(nil, nil, paramsNormales())
	require.NoError(t, err)
	assert.True(t, plan.SinEscrituras())
}

// TestComparar_VentanaInvertidaAborta guards against a clock that went
// backwards. History rows whose window ends before it starts are
// uninterpretable, so they are never produced.
func TestComparar_VentanaInvertidaAborta(t *testing.T) {
	t.Parallel()

	p := domain.ParametrosComparacion{
		VentanaDesde: fotoActual,
		DetectadoEn:  fotoAnterior,
	}
	_, err := domain.Comparar(nil, []domain.Observacion{obs(t, "uid-1", "", "", nil)}, p)
	require.ErrorIs(t, err, domain.ErrVentanaInvalida)

	// Equal instants are rejected too: a zero-length window bounds nothing.
	p.VentanaDesde = fotoActual
	p.DetectadoEn = fotoActual
	_, err = domain.Comparar(nil, []domain.Observacion{obs(t, "uid-1", "", "", nil)}, p)
	require.ErrorIs(t, err, domain.ErrVentanaInvalida)
}

// TestComparar_UsuarioRepetidoAborta guards against a broken adapter handing
// the same person twice. Firestore document ids are unique, so this can only
// be a defect — and letting it through would insert a duplicate row and hide
// one of the two states.
func TestComparar_UsuarioRepetidoAborta(t *testing.T) {
	t.Parallel()

	observadas := []domain.Observacion{
		obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
		obs(t, "uid-1", "eliseo@msp.com", "Eliseo", intPtr(11342)),
	}
	_, err := domain.Comparar(nil, observadas, paramsNormales())
	require.ErrorIs(t, err, domain.ErrObservacionDuplicada)
}

// TestComparar_BajasSonDeterministas pins the ordering of the bajas so the
// produced plan does not depend on Go's randomised map iteration.
func TestComparar_BajasSonDeterministas(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{
		espejo("uid-c", "c@msp.com", "C", nil),
		espejo("uid-a", "a@msp.com", "A", nil),
		espejo("uid-b", "b@msp.com", "B", nil),
	}
	observadas := []domain.Observacion{obs(t, "uid-z", "z@msp.com", "Z", nil)}

	for range 20 {
		plan, err := domain.Comparar(previas, observadas, paramsNormales())
		require.NoError(t, err)
		assert.Equal(t, []string{"uid-a", "uid-b", "uid-c"}, plan.Bajas)
	}
}

// TestTipoCambio_Valido pins the canonical set, which the CHECK constraint in
// migration 000062 only mirrors.
func TestTipoCambio_Valido(t *testing.T) {
	t.Parallel()

	for _, tipo := range []domain.TipoCambio{
		domain.TipoAsignacion, domain.TipoReasignacion,
		domain.TipoRetiro, domain.TipoBajaUsuario,
	} {
		assert.True(t, tipo.Valido(), "%s", tipo)
	}
	assert.False(t, domain.TipoCambio("cambiado").Valido())
	assert.False(t, domain.TipoCambio("").Valido())
}

// TestComparar_TodasLasFilasLlevanLaMismaVentana verifies that every history
// row of one pass carries identical bounds. A pass is one instant; rows
// disagreeing about when they were detected would make the ledger unusable.
func TestComparar_TodasLasFilasLlevanLaMismaVentana(t *testing.T) {
	t.Parallel()

	previas := []*domain.Asignacion{
		espejo("uid-1", "a@msp.com", "A", intPtr(11341)),
		espejo("uid-2", "b@msp.com", "B", intPtr(11342)),
		espejo("uid-3", "c@msp.com", "C", intPtr(11343)),
	}
	observadas := []domain.Observacion{
		obs(t, "uid-1", "a@msp.com", "A", intPtr(11399)), // reasignación
		obs(t, "uid-2", "b@msp.com", "B", nil),           // retiro
		obs(t, "uid-9", "n@msp.com", "N", intPtr(51058)), // asignación
		// uid-3 desaparece → baja_usuario
	}

	plan, err := domain.Comparar(previas, observadas, paramsNormales())
	require.NoError(t, err)

	tipos := make(map[domain.TipoCambio]int, len(plan.Cambios))
	for _, c := range plan.Cambios {
		tipos[c.Tipo()]++
		assert.Equal(t, fotoAnterior, c.VentanaDesde())
		assert.Equal(t, fotoActual, c.DetectadoEn())
		assert.Equal(t, fotoActual, c.CreatedAt())
	}
	assert.Equal(t, map[domain.TipoCambio]int{
		domain.TipoReasignacion: 1,
		domain.TipoRetiro:       1,
		domain.TipoAsignacion:   1,
		domain.TipoBajaUsuario:  1,
	}, tipos, "las cuatro transiciones en una sola pasada")
}
