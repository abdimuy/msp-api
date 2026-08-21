//nolint:misspell // Spanish vocabulary by project convention.
package failedintents_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	ventasfailedintents "github.com/abdimuy/msp-api/internal/ventas/infra/failedintents"
)

func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// unaVentaReal devuelve el ID de una venta que existe. Salta la prueba si la
// base no tiene ninguna: sin una venta real no hay control positivo, y sin
// control positivo la prueba no distingue "la consulta funciona y no encontró
// nada" de "la consulta está rota".
func unaVentaReal(t *testing.T, pool *firebird.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRowContext(context.Background(),
		`SELECT FIRST 1 ID FROM MSP_VENTAS ORDER BY ID`).Scan(&id)
	if err != nil {
		t.Skipf("no hay ventas en la base de pruebas: %v", err)
	}
	return strings.TrimSpace(id)
}

// TestResolutionChecker_DistingueLaQueAterrizoDeLaQueNo es el control positivo
// del puerto invertido.
//
// Lo que fija: la clave de idempotencia que la app manda en POST /v2/ventas ES
// el id de la venta. No hay columna nueva ni contrato escrito en ningún lado —
// es una propiedad del cliente— así que si algún día la app deja de generar la
// clave así, esta prueba es lo que lo delata. Sin ella, el conciliador
// respondería "ninguna aterrizó" para siempre, en silencio, y el rezago de
// intentos pendientes no bajaría nunca.
//
//nolint:paralleltest // serial: comparte el pool de la BD compartida.
func TestResolutionChecker_DistingueLaQueAterrizoDeLaQueNo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	ventaViva := unaVentaReal(t, pool)
	inventada := uuid.NewString()

	checker := ventasfailedintents.NewResolutionChecker(pool)
	resueltas, err := checker.ResueltasEntre(
		context.Background(), "/v2/ventas", []string{ventaViva, inventada})
	require.NoError(t, err)

	assert.Contains(t, resueltas, ventaViva,
		"control positivo: la venta que SÍ existe tiene que volver; si no vuelve, "+
			"la consulta está rota y el conciliador no cerraría nada nunca")
	assert.NotContains(t, resueltas, inventada,
		"la clave que no tiene venta no puede volver: cerrar un intento cuyo trabajo "+
			"NO aterrizó pierde la venta de un cliente")
}

// TestResolutionChecker_NoContestaPorRutasAjenas: una clave sólo es única
// dentro de su recurso. Contestar por la ruta de otro módulo sería afirmar
// algo que este checker no sabe.
//
//nolint:paralleltest // serial: comparte el pool de la BD compartida.
func TestResolutionChecker_NoContestaPorRutasAjenas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	ventaViva := unaVentaReal(t, pool)
	checker := ventasfailedintents.NewResolutionChecker(pool)

	resueltas, err := checker.ResueltasEntre(
		context.Background(), "/v2/cobranza/pagos", []string{ventaViva})
	require.NoError(t, err)
	assert.Empty(t, resueltas)
}

// TestResolutionChecker_TroceaLotesGrandes ejercita el troceo con más claves
// que el tamaño de lote. Sin trocear, el lote grande falla ENTERO y ninguna
// venta que ya aterrizó se cierra.
//
//nolint:paralleltest // serial: comparte el pool de la BD compartida.
func TestResolutionChecker_TroceaLotesGrandes(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	ventaViva := unaVentaReal(t, pool)

	// 450 claves inventadas más la real al final: la real cae en el último
	// lote, así que sólo vuelve si los tres lotes corrieron.
	claves := make([]string, 0, 451)
	for range 450 {
		claves = append(claves, uuid.NewString())
	}
	claves = append(claves, ventaViva)

	checker := ventasfailedintents.NewResolutionChecker(pool)
	resueltas, err := checker.ResueltasEntre(context.Background(), "/v2/ventas", claves)
	require.NoError(t, err, "un lote de %d claves no puede reventar la sentencia", len(claves))
	assert.Equal(t, []string{ventaViva}, resueltas)
}

// TestResolutionChecker_SinClaves_NoVaALaBase.
//
//nolint:paralleltest // serial: comparte el pool de la BD compartida.
func TestResolutionChecker_SinClaves_NoVaALaBase(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	checker := ventasfailedintents.NewResolutionChecker(pool)
	resueltas, err := checker.ResueltasEntre(context.Background(), "/v2/ventas", nil)
	require.NoError(t, err)
	assert.Empty(t, resueltas)
}
