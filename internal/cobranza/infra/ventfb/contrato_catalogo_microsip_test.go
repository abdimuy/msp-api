// Pruebas de CONTRATO contra la realidad de Microsip.
//
// POR QUÉ EXISTE ESTE ARCHIVO
// ===========================
// Las pruebas normales verifican nuestra lógica. Éstas verifican que el MUNDO
// sigue siendo como el código cree que es. Son la única clase de prueba que
// puede atrapar el defecto en que el código y su prueba comparten la misma
// premisa falsa sobre un catálogo externo.
//
// El caso que las motivó: el concepto 27969 está documentado como "abono
// mostrador" en cuatro sitios del código —
//
//	internal/cobranza/infra/ventfb/pagos_repo.go:152 y :302 (comentarios)
//	internal/cobranza/domain/pago_recibido.go:33        (conceptoAbonoMostrador)
//	internal/cobranza/ports/outbound/microsip_pago_writer.go:33 (doc del puerto)
//
// — y el catálogo CONCEPTOS_CC de Microsip dice que 27969 es "Condonaciones".
// Cualquier prueba escrita a mano habría codificado el mismo nombre equivocado
// y habría pasado en verde para siempre. Sólo lo desmiente preguntarle a la
// base.
//
// REGLAS DE ESTE ARCHIVO
// ======================
//   - Se saltan solas sin FB_DATABASE, como el resto de la suite.
//   - Sólo LEEN catálogos (CONCEPTOS_CC, FORMAS_COBRO_CC, FORMAS_COBRO) y
//     metadatos (RDB$PROCEDURES). Cero escrituras, cero DDL.
//   - NO dependen de filas de producción por id escrito a mano. Los catálogos
//     sobreviven a `gbak -skip_data` (ver scripts/db-test-skip-tables.txt), así
//     que estas pruebas pasan contra la base reducida de 15 MB.
//   - Los ids que se contrastan NO se escriben a mano: salen de la constante
//     pagoConceptoFilter, del cuerpo del procedure en la base y del catálogo
//     mismo. Si alguien edita la constante, la prueba lee el valor nuevo.
//
// El archivo vive en `package ventfb` (no ventfb_test) precisamente para poder
// leer pagoConceptoFilter: la fuente de verdad del filtro de entrega.
//
//nolint:misspell,paralleltest // vocabulario español por convención; serie por diseño.
package ventfb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// requireCatalogoFBEnv salta la prueba cuando no hay base Firebird apuntada.
func requireCatalogoFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set — apúntalo a la base Microsip para correr las pruebas de contrato")
	}
}

// reNumero extrae los enteros de un fragmento SQL o del cuerpo de un procedure.
var reNumero = regexp.MustCompile(`\d+`)

// reConceptoIn localiza cada predicado `CONCEPTO_CC_ID [NOT] IN (...)` dentro
// del cuerpo de un procedure. El grupo 1 es la lista cruda.
var reConceptoIn = regexp.MustCompile(`(?i)CONCEPTO_CC_ID\s+(?:NOT\s+)?IN\s*\(([^)]*)\)`)

// idsDeFragmento devuelve, ordenados, los enteros que aparecen en un fragmento
// SQL. Se usa para leer la lista de conceptos DESDE la constante del código en
// vez de repetirla a mano en la prueba: si alguien edita pagoConceptoFilter, la
// prueba lee el valor nuevo sin que nadie la actualice.
func idsDeFragmento(fragmento string) []int {
	crudos := reNumero.FindAllString(fragmento, -1)
	ids := make([]int, 0, len(crudos))
	for _, c := range crudos {
		n, err := strconv.Atoi(c)
		if err != nil {
			continue
		}
		ids = append(ids, n)
	}
	sort.Ints(ids)
	return ids
}

// leerCatalogo carga un catálogo de Microsip en un mapa id → nombre.
//
// El CAST a WIN1252 no es opcional: CONCEPTOS_CC.NOMBRE, FORMAS_COBRO_CC.NOMBRE
// y FORMAS_COBRO.NOMBRE son CHARACTER SET NONE (bytes crudos Win1252). Leerlas
// verbatim sobre una conexión FB_CHARSET=UTF8 hace que el driver truene en las
// filas con acentos ("Interés moratorio", "Condonación...", "Devolución...").
// Es el mismo cast que pagos_repo.go aplica a DOCTOS_CC.DESCRIPCION.
func leerCatalogo(ctx context.Context, t *testing.T, q firebird.Querier, tabla, pk string) map[int]string {
	t.Helper()
	query := fmt.Sprintf(
		`SELECT %s, CAST(NOMBRE AS VARCHAR(100) CHARACTER SET WIN1252) FROM %s`, pk, tabla)
	rows, err := q.QueryContext(ctx, query)
	require.NoError(t, err, "leyendo el catálogo %s", tabla)
	defer func() { _ = rows.Close() }()

	out := make(map[int]string)
	for rows.Next() {
		var (
			id     int
			nombre sql.NullString
		)
		require.NoError(t, rows.Scan(&id, &nombre), "scan de %s", tabla)
		out[id] = strings.TrimSpace(nombre.String)
	}
	require.NoError(t, rows.Err(), "iterando %s", tabla)
	require.NotEmpty(t, out, "el catálogo %s vino vacío — la base no es la que la prueba espera", tabla)
	return out
}

// nombresDe formatea "id=nombre" para los mensajes de error, que es lo que un
// humano necesita ver cuando el contrato se rompe.
func nombresDe(catalogo map[int]string, ids []int) string {
	partes := make([]string, 0, len(ids))
	for _, id := range ids {
		nombre, ok := catalogo[id]
		if !ok {
			nombre = "<NO EXISTE EN EL CATÁLOGO>"
		}
		partes = append(partes, fmt.Sprintf("%d=%q", id, nombre))
	}
	return strings.Join(partes, ", ")
}

func diferencia(a, b []int) []int {
	enB := make(map[int]struct{}, len(b))
	for _, x := range b {
		enB[x] = struct{}{}
	}
	var out []int
	for _, x := range a {
		if _, ok := enB[x]; !ok {
			out = append(out, x)
		}
	}
	sort.Ints(out)
	return out
}

func interseccion(a, b []int) []int {
	enB := make(map[int]struct{}, len(b))
	for _, x := range b {
		enB[x] = struct{}{}
	}
	var out []int
	for _, x := range a {
		if _, ok := enB[x]; ok {
			out = append(out, x)
		}
	}
	sort.Ints(out)
	return out
}

// ─── Supuesto 1: los conceptos que /sync/pagos entrega ────────────────────────

// TestContrato_PagoConceptoFilter_ContraCatalogoConceptosCC contrasta la lista
// de conceptos que el servidor ENTREGA por /sync/pagos contra lo que el
// catálogo CONCEPTOS_CC de Microsip dice que esos conceptos son.
//
// De dónde sale el supuesto: pagos_repo.go:327
//
//	const pagoConceptoFilter = `p.CONCEPTO_CC_ID IN (87327, 27969)`
//
// y el comentario de :299-302 que los llama "cobranza en ruta y abono
// mostrador".
//
// Qué encontró la base: 87327 sí es "Cobranza en ruta". 27969 NO es abono
// mostrador — es "Condonaciones". El abono mostrador real es el concepto 155,
// "Cobro en mostrador", que el filtro EXCLUYE por considerarlo interno.
func TestContrato_PagoConceptoFilter_ContraCatalogoConceptosCC(t *testing.T) {
	requireCatalogoFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		conceptos := leerCatalogo(ctx, t, q, "CONCEPTOS_CC", "CONCEPTO_CC_ID")
		entregados := idsDeFragmento(pagoConceptoFilter)

		require.NotEmpty(t, entregados,
			"pagoConceptoFilter (pagos_repo.go) dejó de contener ids — el filtro de entrega ya no se puede leer")
		t.Logf("conceptos entregados por /sync/pagos: %s", nombresDe(conceptos, entregados))

		t.Run("existen_en_el_catalogo", func(t *testing.T) {
			for _, id := range entregados {
				assert.Containsf(t, conceptos, id,
					"SUPUESTO ROTO: pagoConceptoFilter (internal/cobranza/infra/ventfb/pagos_repo.go:327) "+
						"entrega el concepto %d, que ya NO existe en CONCEPTOS_CC. "+
						"Un concepto retirado del catálogo no vuelve a aparecer: el filtro está entregando "+
						"un conjunto vacío por ese lado y el cobrador dejó de ver esos abonos.", id)
			}
		})

		t.Run("87327_sigue_siendo_cobranza_en_ruta", func(t *testing.T) {
			// El único concepto del filtro cuyo nombre en el catálogo SÍ
			// coincide con lo que el código dice de él. Si Microsip lo
			// renombra o lo reutiliza, todo el canal de cobranza cambia de
			// significado sin que nada más se entere.
			const idRuta = 87327
			nombre, ok := conceptos[idRuta]
			require.True(t, ok, "el concepto %d desapareció de CONCEPTOS_CC", idRuta)
			bajo := strings.ToLower(nombre)
			assert.Truef(t, strings.Contains(bajo, "cobranza") && strings.Contains(bajo, "ruta"),
				"SUPUESTO ROTO: el código llama al concepto %d \"cobranza en ruta\" "+
					"(pagos_repo.go:300, pago_recibido.go:34 conceptoCobranzaRuta, "+
					"microsip_pago_writer.go:33), pero CONCEPTOS_CC ahora lo llama %q. "+
					"Es el concepto con el que se escriben TODOS los pagos de ruta a Microsip.",
				idRuta, nombre)
		})

		t.Run("27969_sigue_siendo_condonaciones", func(t *testing.T) {
			// DIVERGENCIA CONGELADA — no es un bug de esta prueba.
			//
			// El código llama a 27969 "abono mostrador" en cuatro sitios. El
			// catálogo dice "Condonaciones". Verificado con los documentos:
			// folios CO00249xx, descripciones "CONDONACION POR PRONTO PAGO A
			// UN MES...".
			//
			// La prueba NO afirma que el filtro esté bien ni mal: afirma que
			// 27969 SIGUE siendo el concepto de condonaciones. Ésa es la
			// premisa sobre la que descansa la decisión de negocio pendiente
			// (¿debe /sync/pagos entregar condonaciones al cobrador?). Si
			// Microsip renombra o reutiliza el id, la decisión cambia de
			// premisa y hay que volver a mirarla.
			const idCondonacion = 27969
			nombre, ok := conceptos[idCondonacion]
			require.True(t, ok, "el concepto %d desapareció de CONCEPTOS_CC", idCondonacion)
			assert.Containsf(t, strings.ToLower(nombre), "condonac",
				"SUPUESTO ROTO: el concepto %d dejó de ser condonaciones en CONCEPTOS_CC (ahora %q). "+
					"El código lo llama \"abono mostrador\" en pagos_repo.go:152 y :302, "+
					"domain/pago_recibido.go:33 (conceptoAbonoMostrador) y "+
					"ports/outbound/microsip_pago_writer.go:33 — ese nombre ya era falso, y ahora "+
					"además cambió el hecho subyacente. Revisar la decisión pendiente antes de tocar nada.",
				idCondonacion, nombre)
		})

		t.Run("155_es_el_abono_mostrador_de_verdad", func(t *testing.T) {
			// El nombre que el código le atribuye a 27969 pertenece en
			// realidad a 155, que pagoConceptoFilter EXCLUYE tratándolo de
			// "concepto interno del cache" (pagos_repo.go:326). El cobrador
			// no ve los cobros hechos en mostrador.
			const idMostrador = 155
			nombre, ok := conceptos[idMostrador]
			require.True(t, ok, "el concepto %d desapareció de CONCEPTOS_CC", idMostrador)
			assert.Containsf(t, strings.ToLower(nombre), "mostrador",
				"SUPUESTO ROTO: el concepto %d dejó de ser el cobro en mostrador (ahora %q). "+
					"pagos_repo.go:326 lo excluye del sync llamándolo \"concepto interno\"; "+
					"esa exclusión sólo tiene sentido mientras 155 sea el mostrador.",
				idMostrador, nombre)
			assert.NotContainsf(t, idsDeFragmento(pagoConceptoFilter), idMostrador,
				"el filtro empezó a entregar el concepto %d (%q). Es un cambio de comportamiento "+
					"deliberado o un accidente; en cualquier caso la asimetría documentada en "+
					"ventas_repo_test.go:140-144 y saldos_repo_test.go:180-183 ya no aplica.",
				idMostrador, nombre)
		})

		t.Run("todo_concepto_entregado_es_cobranza", func(t *testing.T) {
			// SALTADA A PROPÓSITO — divergencia real, decisión de negocio pendiente.
			//
			// Éste es el aserto que uno querría: que TODO concepto que
			// /sync/pagos entrega sea, según el catálogo, un concepto de
			// cobranza. Hoy FALLA, y falla por un hecho, no por un error de
			// la prueba:
			//
			//	87327 "Cobranza en ruta"  → sí es cobranza
			//	27969 "Condonaciones"     → NO es cobranza; es perdón de deuda
			//
			// Una condonación no es dinero que entró. El teléfono la recibe
			// y la pinta junto a los abonos reales. Al mismo tiempo el módulo
			// clientes clasifica ese mismo 27969 como condonación y lo
			// EXCLUYE de los ingresos (internal/clientes/domain/categoria.go:46,
			// Categoria.EsIngreso() == false). Los dos módulos leen el mismo
			// id y no coinciden en si es dinero.
			//
			// Corregirlo es una decisión de negocio (¿el cobrador debe seguir
			// viendo las condonaciones de sus clientes?), no un arreglo
			// técnico, y arrastra la lista del writer y la del reconciliador.
			// Se deja aquí escrito, saltado y visible, en vez de ajustar la
			// prueba para que pase.
			t.Skip("DIVERGENCIA CONOCIDA: /sync/pagos entrega 27969 = \"Condonaciones\", " +
				"que no es cobranza. pagoConceptoFilter (pagos_repo.go:327) lo incluye porque el " +
				"código lo cree \"abono mostrador\". Decisión de negocio pendiente — no ajustar " +
				"la prueba, resolver la lista.")
		})
	})
}

// ─── Supuesto 2: FECHA_ULT_PAGO cubre lo que el sync entrega ──────────────────

// conceptosDeFechaUltPago lee el cuerpo VIVO del procedure
// MSP_RECOMPUTE_SALDO_VENTA desde RDB$PROCEDURES y extrae la lista de conceptos
// con la que calcula FECHA_ULT_PAGO.
//
// Se lee de la base, no del archivo de migración, porque lo que gobierna el
// cache es lo que está instalado: una migración aplicada a medias, un ALTER
// hecho a mano en producción o un rollback dejan el archivo y la base
// diciendo cosas distintas. La base manda.
func conceptosDeFechaUltPago(ctx context.Context, t *testing.T, q firebird.Querier) []int {
	t.Helper()

	var fuente sql.NullString
	err := q.QueryRowContext(ctx, `
SELECT CAST(RDB$PROCEDURE_SOURCE AS VARCHAR(30000) CHARACTER SET WIN1252)
FROM RDB$PROCEDURES
WHERE RDB$PROCEDURE_NAME = 'MSP_RECOMPUTE_SALDO_VENTA'`).Scan(&fuente)
	if err != nil {
		t.Skipf("no se pudo leer el cuerpo de MSP_RECOMPUTE_SALDO_VENTA (¿migración 000010+ sin aplicar?): %v", err)
	}
	require.True(t, fuente.Valid && fuente.String != "",
		"MSP_RECOMPUTE_SALDO_VENTA existe pero su cuerpo vino vacío")

	coincidencias := reConceptoIn.FindAllStringSubmatch(fuente.String, -1)
	require.NotEmpty(t, coincidencias,
		"SUPUESTO ROTO: MSP_RECOMPUTE_SALDO_VENTA ya no filtra por CONCEPTO_CC_ID. "+
			"El procedure decide qué abonos cuentan para FECHA_ULT_PAGO, que es el eje de la "+
			"ventana del cobrador (pagoSaldoFilterConVentana, pagos_repo.go:347). "+
			"Si el filtro desapareció, la ventana cambió de significado.")

	// El procedure de la migración 000023 usa la MISMA lista en sus tres
	// predicados (TOTAL_IMPORTE IN, IMPTE_REST NOT IN, FECHA_ULT_PAGO IN). Si
	// alguien introduce una lista distinta, esta prueba deja de poder decir
	// "la lista de FECHA_ULT_PAGO" sin ambigüedad y hay que actualizarla a
	// propósito en vez de dejarla adivinando.
	primera := idsDeFragmento(coincidencias[0][1])
	for i, m := range coincidencias[1:] {
		require.Equalf(t, primera, idsDeFragmento(m[1]),
			"MSP_RECOMPUTE_SALDO_VENTA ahora usa MÁS DE UNA lista de conceptos "+
				"(predicado 1 = %v, predicado %d = %v). Esta prueba asume una sola lista para poder "+
				"identificar la de FECHA_ULT_PAGO; actualízala junto con la migración que introdujo "+
				"la segunda lista.", primera, i+2, idsDeFragmento(m[1]))
	}
	return primera
}

// TestContrato_ListasDeConceptos_SyncVsFechaUltPago fija la relación entre las
// DOS listas de conceptos que gobiernan la cobranza, que no son la misma y
// nunca lo fueron:
//
//	entrega  — pagoConceptoFilter, pagos_repo.go:327 → qué pagos viajan al teléfono.
//	ventana  — MSP_RECOMPUTE_SALDO_VENTA (migraciones 000011 → 000023) → qué
//	           abonos mueven FECHA_ULT_PAGO, que es lo que decide si una venta
//	           saldada sigue dentro de la ventana del cobrador
//	           (pagoSaldoFilterConVentana, pagos_repo.go:347).
//
// La prueba NO exige que coincidan: exige que la DIFERENCIA siga siendo la
// conocida. Cambiar una lista sin la otra es exactamente el movimiento que
// nadie nota hasta que un cobrador reclama, porque las dos listas viven en
// archivos distintos (Go y SQL) y ninguna prueba las miraba juntas.
//
// Consecuencia real de cada mitad de la diferencia:
//
//   - sólo en entrega (27969): el abono viaja al teléfono, pero si es el que
//     saldó la venta, FECHA_ULT_PAGO no se mueve y la venta sale de la
//     ventana. Medido contra la base de desarrollo: 57,392 abonos de concepto
//     27969 sobre ventas ya saldadas cuya FECHA_ULT_PAGO no los cubre.
//   - sólo en ventana (155): la venta saldada SÍ se queda visible, pero su
//     abono no viaja — el cobrador ve la venta saldada sin ver con qué se
//     saldó. Es la asimetría ya documentada en ventas_repo_test.go:140-144.
func TestContrato_ListasDeConceptos_SyncVsFechaUltPago(t *testing.T) {
	requireCatalogoFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		conceptos := leerCatalogo(ctx, t, q, "CONCEPTOS_CC", "CONCEPTO_CC_ID")

		entrega := idsDeFragmento(pagoConceptoFilter)
		ventana := conceptosDeFechaUltPago(ctx, t, q)

		t.Logf("entrega (/sync/pagos): %s", nombresDe(conceptos, entrega))
		t.Logf("ventana (FECHA_ULT_PAGO): %s", nombresDe(conceptos, ventana))

		const donde = "pagoConceptoFilter está en internal/cobranza/infra/ventfb/pagos_repo.go:327; " +
			"la lista de FECHA_ULT_PAGO está en el procedure MSP_RECOMPUTE_SALDO_VENTA, " +
			"instalado por migrations-firebird/000011_fix_fecha_ult_pago_y_conceptos.up.sql y " +
			"vigente en 000023_recompute_changelog.up.sql"

		// Línea base congelada. No es "lo correcto" — es lo que hay hoy,
		// medido. Su valor está en que cualquier edición de cualquiera de las
		// dos listas la mueve y hace fallar la prueba con el nombre real del
		// concepto que entró o salió.
		soloEntregaEsperado := []int{27969} // "Condonaciones"
		soloVentanaEsperado := []int{155}   // "Cobro en mostrador"
		comunesEsperado := []int{87327}     // "Cobranza en ruta"

		assert.Equalf(t, soloEntregaEsperado, diferencia(entrega, ventana),
			"SUPUESTO ROTO — conceptos que /sync/pagos ENTREGA pero FECHA_ULT_PAGO NO cuenta.\n"+
				"esperado: %s\nobtenido: %s\n%s\n"+
				"Un concepto en este grupo hace que la venta que ese abono salda se caiga de la "+
				"ventana del cobrador: el abono llega al teléfono, la venta desaparece de la ruta.",
			nombresDe(conceptos, soloEntregaEsperado),
			nombresDe(conceptos, diferencia(entrega, ventana)), donde)

		assert.Equalf(t, soloVentanaEsperado, diferencia(ventana, entrega),
			"SUPUESTO ROTO — conceptos que FECHA_ULT_PAGO cuenta pero /sync/pagos NO entrega.\n"+
				"esperado: %s\nobtenido: %s\n%s\n"+
				"Un concepto en este grupo deja al cobrador viendo una venta saldada sin el abono "+
				"que la saldó.",
			nombresDe(conceptos, soloVentanaEsperado),
			nombresDe(conceptos, diferencia(ventana, entrega)), donde)

		assert.Equalf(t, comunesEsperado, interseccion(entrega, ventana),
			"SUPUESTO ROTO — conceptos donde entrega y ventana SÍ coinciden.\n"+
				"esperado: %s\nobtenido: %s\n%s",
			nombresDe(conceptos, comunesEsperado),
			nombresDe(conceptos, interseccion(entrega, ventana)), donde)
	})
}

// ─── Supuesto 3: dos ejes distintos, no la misma lista dos veces ──────────────

// TestContrato_DosEjes_ConceptoCC_vs_FormaCobroCC fija que el filtrado de
// cobranza corre sobre DOS columnas distintas de DOS catálogos distintos, y que
// sus universos de ids no se tocan:
//
//	servidor — DOCTOS_CC.CONCEPTO_CC_ID → catálogo CONCEPTOS_CC
//	           (pagoConceptoFilter, pagos_repo.go:327)
//	app      — FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID → catálogo FORMAS_COBRO_CC
//	           (157 efectivo, 158 cheque, 52569 transferencia, 137026 condonación)
//
// Confundirlas es fácil y ya pasó: varios fixtures de la suite pasan 87327 —un
// CONCEPTO_CC_ID— como FormaCobroID (pagos_recibidos_repo_test.go:36,60,852;
// pagos_recibidos_concurrency_test.go:91; los e2e de cobranzahttp). No revientan
// porque DerivarConceptoCC devuelve el concepto de ruta para cualquier valor que
// no sea el de condonación, así que un forma_cobro inventado produce un pago de
// ruta silenciosamente.
//
// Además fija dos hechos que el código enuncia mal:
//
//   - ports/outbound/microsip_pago_writer.go:33 dice que FormaCobroID sale de
//     "formas_cobro.forma_cobro_id". La tabla FORMAS_COBRO existe y NO contiene
//     ninguno de esos ids (sólo 67 Efectivo, 68 Cheque, 71 Crédito, 27773). El
//     catálogo bueno es FORMAS_COBRO_CC.
//   - domain/pago_recibido.go llama al mapeo "abono mostrador". El catálogo dice
//     que 137026 es "Condonacion" y que el concepto al que mapea, 27969, es
//     "Condonaciones". El MAPEO es coherente; el NOMBRE en nuestro código no.
func TestContrato_DosEjes_ConceptoCC_vs_FormaCobroCC(t *testing.T) {
	requireCatalogoFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		conceptos := leerCatalogo(ctx, t, q, "CONCEPTOS_CC", "CONCEPTO_CC_ID")
		formasCC := leerCatalogo(ctx, t, q, "FORMAS_COBRO_CC", "FORMA_COBRO_CC_ID")
		formasPV := leerCatalogo(ctx, t, q, "FORMAS_COBRO", "FORMA_COBRO_ID")

		t.Logf("FORMAS_COBRO_CC (eje de la app): %v", formasCC)
		t.Logf("FORMAS_COBRO (eje de ventas/PV): %v", formasPV)

		t.Run("los_dos_universos_de_ids_no_se_tocan", func(t *testing.T) {
			for id, nombre := range formasCC {
				assert.NotContainsf(t, conceptos, id,
					"SUPUESTO ROTO: el id %d (%q) existe en LOS DOS catálogos, FORMAS_COBRO_CC y "+
						"CONCEPTOS_CC. Mientras los universos eran disjuntos, confundir "+
						"forma_cobro_id con concepto_cc_id producía un id inexistente y algo "+
						"fallaba ruidosamente. Ahora produce una fila válida del catálogo "+
						"equivocado. Revisar DerivarConceptoCC (domain/pago_recibido.go:40) y los "+
						"fixtures que pasan conceptos como FormaCobroID.", id, nombre)
			}
		})

		t.Run("los_conceptos_del_sync_son_del_catalogo_de_conceptos", func(t *testing.T) {
			for _, id := range idsDeFragmento(pagoConceptoFilter) {
				assert.Containsf(t, conceptos, id,
					"pagoConceptoFilter (pagos_repo.go:327) filtra por el id %d, que no está en "+
						"CONCEPTOS_CC", id)
				assert.NotContainsf(t, formasCC, id,
					"pagoConceptoFilter (pagos_repo.go:327) filtra por el id %d, que resulta ser una "+
						"FORMA DE COBRO, no un concepto. El filtro corre sobre "+
						"DOCTOS_CC.CONCEPTO_CC_ID: un forma_cobro_id ahí no casa con nada y el "+
						"cobrador deja de ver esos pagos.", id)
			}
		})

		t.Run("el_puerto_nombra_el_catalogo_equivocado", func(t *testing.T) {
			// microsip_pago_writer.go:33 dice "formas_cobro.forma_cobro_id".
			// La tabla FORMAS_COBRO es la de punto de venta (DOCTOS_PV_COBROS)
			// y no comparte ni un id con la de cuentas por cobrar. Se fija el
			// hecho para que el día que alguien vaya a "validar contra
			// FORMAS_COBRO" tenga la prueba diciéndole que ésa no es.
			for id, nombre := range formasCC {
				assert.NotContainsf(t, formasPV, id,
					"el id %d (%q) de FORMAS_COBRO_CC apareció también en FORMAS_COBRO. "+
						"ports/outbound/microsip_pago_writer.go:33 documenta FormaCobroID como "+
						"\"formas_cobro.forma_cobro_id\"; hasta hoy eso era falso y los dos "+
						"catálogos eran disjuntos.", id, nombre)
			}
		})

		t.Run("derivar_concepto_devuelve_conceptos_reales", func(t *testing.T) {
			// Contrato del write-path: para CUALQUIER forma de cobro que la
			// app pueda mandar, el concepto que escribimos a Microsip tiene
			// que existir en CONCEPTOS_CC. Las formas se toman del catálogo,
			// no de una lista escrita a mano en la prueba.
			for id, nombre := range formasCC {
				derivado := domain.DerivarConceptoCC(id)
				assert.Containsf(t, conceptos, derivado,
					"SUPUESTO ROTO: DerivarConceptoCC(%d /* %q */) = %d, que no existe en "+
						"CONCEPTOS_CC. domain/pago_recibido.go:40 decide con ese valor el "+
						"CONCEPTO_CC_ID de cada pago que escribimos a Microsip.",
					id, nombre, derivado)
			}
		})

		t.Run("la_forma_de_cobro_especial_es_la_de_condonacion", func(t *testing.T) {
			// DerivarConceptoCC trata a UNA forma de cobro distinto del resto.
			// La descubrimos preguntándole a la función, no escribiendo 137026
			// a mano: el fallback es lo que devuelve para un id que no existe.
			fallback := domain.DerivarConceptoCC(-1)
			var especiales []int
			for id := range formasCC {
				if domain.DerivarConceptoCC(id) != fallback {
					especiales = append(especiales, id)
				}
			}
			sort.Ints(especiales)

			require.Lenf(t, especiales, 1,
				"SUPUESTO ROTO: DerivarConceptoCC (domain/pago_recibido.go:40) trata de forma "+
					"especial a %d formas de cobro (%v), no a una. El mapeo del write-path cambió.",
				len(especiales), especiales)

			idEspecial := especiales[0]
			nombreForma := strings.ToLower(formasCC[idEspecial])
			nombreConcepto := strings.ToLower(conceptos[domain.DerivarConceptoCC(idEspecial)])

			// El hecho central: el mapeo es coherente con el catálogo —
			// condonación → Condonaciones. Lo que está mal es cómo lo llama
			// nuestro código: formaCobroIDAbonoMostrador / conceptoAbonoMostrador.
			assert.Containsf(t, nombreForma, "condonac",
				"SUPUESTO ROTO: la forma de cobro que DerivarConceptoCC trata distinto (%d) ya no "+
					"es la de condonación — FORMAS_COBRO_CC ahora la llama %q. En el código se "+
					"llama formaCobroIDAbonoMostrador (domain/pago_recibido.go:32), nombre que ya "+
					"era falso; si además cambió el hecho, el write-path está mapeando pagos al "+
					"concepto equivocado.", idEspecial, formasCC[idEspecial])

			assert.Containsf(t, nombreConcepto, "condonac",
				"SUPUESTO ROTO: la forma de cobro %d (%q) mapea al concepto %d, que CONCEPTOS_CC "+
					"ahora llama %q. En el código ese destino se llama conceptoAbonoMostrador "+
					"(domain/pago_recibido.go:33).",
				idEspecial, formasCC[idEspecial], domain.DerivarConceptoCC(idEspecial),
				conceptos[domain.DerivarConceptoCC(idEspecial)])

			t.Logf("write-path: forma_cobro %d (%q) → concepto %d (%q) — "+
				"el mapeo es correcto; los nombres formaCobroIDAbonoMostrador/conceptoAbonoMostrador no lo son",
				idEspecial, formasCC[idEspecial],
				domain.DerivarConceptoCC(idEspecial), conceptos[domain.DerivarConceptoCC(idEspecial)])
		})
	})
}
