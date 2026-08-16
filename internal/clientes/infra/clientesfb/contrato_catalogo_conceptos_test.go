// Pruebas de CONTRATO contra la realidad de Microsip para el módulo clientes.
//
// Hermanas de internal/cobranza/infra/ventfb/contrato_catalogo_microsip_test.go.
// Verifican que el catálogo CONCEPTOS_CC sigue diciendo lo que
// internal/clientes/domain/categoria.go cree que dice.
//
// Por qué importa aquí: categoria.go decide, por id, si un movimiento es dinero
// que entró. Categoria.EsIngreso() define el ingreso POR EXCLUSIÓN — todo lo
// que no sea condonación ni pérdida cuenta como ingreso, incluida la categoría
// de descarte CategoriaOtro. O sea que un concepto nuevo en Microsip entra a
// los totales del cliente como dinero real sin que nadie escriba una línea.
// Eso no lo puede atrapar una prueba unitaria: sólo se ve preguntándole al
// catálogo.
//
// Reglas: se saltan sin FB_DATABASE, sólo leen el catálogo (que sobrevive a
// `gbak -skip_data`, ver scripts/db-test-skip-tables.txt) y no dependen de
// ninguna fila de producción por id escrito a mano.
//
//nolint:paralleltest // serie por diseño: comparten la tx de rollback.
//nolint:misspell    // vocabulario español por convención.
package clientesfb_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// leerConceptosCC carga CONCEPTOS_CC en un mapa id → nombre.
//
// El CAST a WIN1252 es obligatorio: CONCEPTOS_CC.NOMBRE es CHARACTER SET NONE
// (bytes crudos Win1252) y leerlo verbatim sobre una conexión FB_CHARSET=UTF8
// hace que el driver truene en las filas con acentos.
func leerConceptosCC(ctx context.Context, t *testing.T, q firebird.Querier) map[int]string {
	t.Helper()
	rows, err := q.QueryContext(ctx, `
SELECT CONCEPTO_CC_ID, CAST(NOMBRE AS VARCHAR(100) CHARACTER SET WIN1252)
FROM CONCEPTOS_CC`)
	require.NoError(t, err, "leyendo CONCEPTOS_CC")
	defer func() { _ = rows.Close() }()

	out := make(map[int]string)
	for rows.Next() {
		var (
			id     int
			nombre sql.NullString
		)
		require.NoError(t, rows.Scan(&id, &nombre))
		out[id] = strings.TrimSpace(nombre.String)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, out, "CONCEPTOS_CC vino vacío — la base no es la que la prueba espera")
	return out
}

func conNombres(catalogo map[int]string, ids []int) string {
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

// idsClasificados descubre, sin escribir ni un id a mano, el conjunto de
// CONCEPTO_CC_ID que categoria.go clasifica (todo lo que no cae en
// CategoriaOtro). Se barre desde 0 hasta el id más alto del catálogo: si
// categoria.go clasifica un id que Microsip ya retiró, el barrido lo encuentra
// igual y la prueba lo puede reportar como regla muerta.
func idsClasificados(tope int) map[int]domain.Categoria {
	out := make(map[int]domain.Categoria)
	for id := range tope + 1 {
		if cat := domain.ClasificarConcepto(id); cat != domain.CategoriaOtro {
			out[id] = cat
		}
	}
	return out
}

// TestContrato_ClasificacionDeConceptos_ContraCatalogoConceptosCC fija que las
// cuatro listas de internal/clientes/domain/categoria.go siguen describiendo
// los conceptos que Microsip tiene hoy.
//
// De dónde sale el supuesto: categoria.go:32-56 declara cuatro conjuntos de
// CONCEPTO_CC_ID (pago, enganche, condonación, pérdida) con comentarios que les
// atribuyen un significado. El propio archivo advierte en :6 "Never parse the
// concepto name — IDs are stable, names drift". En producción eso es correcto;
// en una prueba es exactamente al revés: leer el nombre es la ÚNICA forma de
// comprobar que el id sigue significando lo que creemos.
func TestContrato_ClasificacionDeConceptos_ContraCatalogoConceptosCC(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		conceptos := leerConceptosCC(ctx, t, q)

		tope := 0
		for id := range conceptos {
			if id > tope {
				tope = id
			}
		}
		clasificados := idsClasificados(tope)
		require.NotEmpty(t, clasificados,
			"categoria.go dejó de clasificar cualquier concepto — ClasificarConcepto siempre devuelve 'otro'")

		t.Run("todo_id_clasificado_existe_en_el_catalogo", func(t *testing.T) {
			for id, cat := range clasificados {
				assert.Containsf(t, conceptos, id,
					"SUPUESTO ROTO: internal/clientes/domain/categoria.go clasifica el concepto %d "+
						"como %q, pero ese id ya NO existe en CONCEPTOS_CC. Es una regla muerta: "+
						"nunca va a casar con un movimiento real, y el concepto que Microsip puso "+
						"en su lugar está cayendo en CategoriaOtro (que EsIngreso() cuenta como "+
						"dinero que entró).", id, cat)
			}
		})

		t.Run("los_nombres_del_catalogo_confirman_la_categoria", func(t *testing.T) {
			// Sólo se exige raíz común donde el catálogo la tiene. Los
			// conceptos de pérdida no comparten palabra ("Cancelaciones",
			// "Fugas", "Mal Cliente"), así que ahí sólo se registra.
			raices := map[domain.Categoria]string{
				domain.CategoriaIngresoPago:     "cobr",     // Cobro, Cobro en mostrador, Cobranza en ruta
				domain.CategoriaIngresoEnganche: "enganche", // Enganche
				domain.CategoriaCondonacion:     "condonac", // Condonaciones
				domain.CategoriaPerdida:         "",         // Cancelaciones / Fugas / Mal Cliente: sin raíz común
				domain.CategoriaOtro:            "",         // no llega aquí: clasificados excluye 'otro'
			}
			for id, cat := range clasificados {
				nombre, existe := conceptos[id]
				if !existe {
					continue // ya lo reporta el subtest anterior
				}
				raiz := raices[cat]
				if raiz == "" {
					t.Logf("categoría %q: %d=%q (sin raíz común que exigir)", cat, id, nombre)
					continue
				}
				assert.Containsf(t, strings.ToLower(nombre), raiz,
					"SUPUESTO ROTO: categoria.go clasifica el concepto %d como %q, pero "+
						"CONCEPTOS_CC lo llama %q. La clasificación mueve dinero: decide si el "+
						"movimiento entra a TotalAbonado y si es_ingreso sale true "+
						"(internal/clientes/domain/categoria.go:78, ritmo_pago.go:497, "+
						"infra/clientesfb/clientes_repo.go:329).", id, cat, nombre)
			}
		})

		t.Run("27969_es_condonacion_en_clientes_y_pago_en_cobranza", func(t *testing.T) {
			// CONTRADICCIÓN REAL ENTRE MÓDULOS, congelada aquí a propósito.
			//
			// El mismo id, dos lecturas incompatibles:
			//
			//	clientes  — categoria.go:46 lo mete en condonacionConceptoIDs;
			//	            EsIngreso() == false. NO es dinero.
			//	cobranza  — domain/pago_recibido.go:33 lo llama
			//	            conceptoAbonoMostrador y es el concepto con el que
			//	            se escriben a Microsip los pagos cuya forma de cobro
			//	            es 137026; infra/ventfb/pagos_repo.go:327 lo entrega
			//	            por /sync/pagos como si fuera cobranza. SÍ es dinero.
			//
			// El catálogo le da la razón a clientes: 27969 = "Condonaciones".
			// La prueba no resuelve la contradicción (es decisión de negocio);
			// la deja escrita y detecta si alguno de los dos lados se mueve.
			const idCondonacion = 27969
			nombre, ok := conceptos[idCondonacion]
			require.Truef(t, ok, "el concepto %d desapareció de CONCEPTOS_CC", idCondonacion)

			assert.Containsf(t, strings.ToLower(nombre), "condonac",
				"SUPUESTO ROTO: CONCEPTOS_CC ya no llama condonación al concepto %d (ahora %q). "+
					"Sobre ese hecho descansa toda la clasificación de %d en categoria.go:46 "+
					"y la divergencia pendiente con cobranza.", idCondonacion, nombre, idCondonacion)

			cat := domain.ClasificarConcepto(idCondonacion)
			assert.Equalf(t, domain.CategoriaCondonacion, cat,
				"clientes dejó de clasificar el concepto %d (%q) como condonación (ahora %q). "+
					"Si el cambio es deliberado, revisar también cobranza: pagos_repo.go:327 lo "+
					"entrega por /sync/pagos y pago_recibido.go:33 lo escribe a Microsip.",
				idCondonacion, nombre, cat)
			assert.Falsef(t, cat.EsIngreso(),
				"el concepto %d (%q) empezó a contar como ingreso en clientes. "+
					"Una condonación es deuda perdonada, no dinero cobrado: entraría a "+
					"TotalAbonado y al gráfico comprado-vs-abonado.", idCondonacion, nombre)
		})

		t.Run("el_catalogo_no_creció_por_debajo_de_EsIngreso", func(t *testing.T) {
			// EL ASERTO QUE MUEVE DINERO.
			//
			// Categoria.EsIngreso() (categoria.go:78) define el ingreso por
			// exclusión: cualquier concepto que categoria.go NO conoce cae en
			// CategoriaOtro y se cuenta como dinero que entró. Así que cada
			// concepto nuevo que Microsip dé de alta —una fuga nueva, un
			// ajuste, una devolución— se suma solo a los totales del cliente,
			// sin código, sin revisión y sin fallar nada.
			//
			// La línea base de abajo no es "lo correcto": es la foto de lo que
			// hay. Su valor está en que crecer el catálogo la rompe y obliga a
			// clasificar el concepto nuevo A PROPÓSITO.
			//
			// Los que ya están y hoy cuentan como ingreso sin serlo del todo
			// (medido sobre la base de desarrollo, abonos TIPO_IMPTE='R' no
			// cancelados): 12 "Devolución" 1,159 movs / $6.7M; 55334
			// "FCOBRADOR" 1,040 / $875k; 247507 "Cobro Anticipo de Apartado"
			// 697 / $1.5M; 15 "Ajuste de saldo" 318 / $792k; 13 "Devolución en
			// mostrador" 169 / $884k; 27774 "Traspaso Efectivo Cta Ant" 6 /
			// $5k. Se dejan en la línea base porque cambiarlos es decisión de
			// negocio, no de esta prueba.
			sinClasificarEsperado := []int{
				4, 5, 6, 7, 8, 9, 10, 12, 13, 14, 15, 16,
				181, 182, 183, 201,
				27774, 27970, 52604, 52605, 55334, 84453, 247507, 3036289,
			}

			var sinClasificar []int
			for id := range conceptos {
				if domain.ClasificarConcepto(id) == domain.CategoriaOtro {
					sinClasificar = append(sinClasificar, id)
				}
			}
			sort.Ints(sinClasificar)

			nuevos := make([]int, 0)
			esperados := make(map[int]struct{}, len(sinClasificarEsperado))
			for _, id := range sinClasificarEsperado {
				esperados[id] = struct{}{}
			}
			for _, id := range sinClasificar {
				if _, ok := esperados[id]; !ok {
					nuevos = append(nuevos, id)
				}
			}

			assert.Emptyf(t, nuevos,
				"SUPUESTO ROTO: CONCEPTOS_CC tiene conceptos que categoria.go no conoce y que no "+
					"estaban en la línea base: %s.\n"+
					"Cada uno cae en CategoriaOtro y Categoria.EsIngreso() "+
					"(internal/clientes/domain/categoria.go:78) lo cuenta como DINERO QUE ENTRÓ: "+
					"suma a TotalAbonado, al gráfico comprado-vs-abonado y al ritmo de pago del "+
					"cliente. Clasifícalo en categoria.go (pago / enganche / condonación / pérdida) "+
					"y sólo entonces agrégalo a la línea base de esta prueba.",
				conNombres(conceptos, nuevos))

			assert.Equalf(t, sinClasificarEsperado, sinClasificar,
				"la lista de conceptos sin clasificar cambió.\nesperado: %s\nobtenido: %s\n"+
					"Si un id salió de la lista porque ahora está clasificado, actualiza la línea "+
					"base. Si salió porque Microsip lo retiró del catálogo, verifica que ningún "+
					"movimiento histórico lo siga usando antes de darlo por muerto.",
				conNombres(conceptos, sinClasificarEsperado), conNombres(conceptos, sinClasificar))
		})
	})
}

// TestContrato_ConceptoDeCargo_QueLasKPIsDelClienteAsumen fija el significado
// del literal `5` que las consultas de la ficha del cliente usan sin nombre ni
// comentario.
//
// De dónde sale el supuesto: internal/clientes/infra/clientesfb/queries.go
// filtra `CONCEPTO_CC_ID = 5` en :144 (TotalComprado / NumVentas), :166
// (TotalAbonado / NumPagos), :195, :232 y :247 (gráfico comprado-vs-abonado) y
// :334 (serie de ritmo de pagos). Ningún comentario dice qué es 5. Si ese id
// deja de ser el cargo de venta, las seis consultas devuelven cero en silencio
// y la ficha del cliente sale vacía sin un solo error.
func TestContrato_ConceptoDeCargo_QueLasKPIsDelClienteAsumen(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		conceptos := leerConceptosCC(ctx, t, q)

		const idCargoVenta = 5
		nombre, ok := conceptos[idCargoVenta]
		require.Truef(t, ok,
			"SUPUESTO ROTO: el concepto %d desapareció de CONCEPTOS_CC. Es el único cargo que "+
				"cuentan las KPIs de la ficha del cliente (internal/clientes/infra/clientesfb/"+
				"queries.go:144, :166, :195, :232, :247, :334): sin él, TotalComprado, "+
				"TotalAbonado, el gráfico y el ritmo salen todos en cero, sin error.",
			idCargoVenta)

		assert.Containsf(t, strings.ToLower(nombre), "venta",
			"SUPUESTO ROTO: el concepto %d ya no es un cargo de venta — CONCEPTOS_CC lo llama %q. "+
				"queries.go lo usa como el ÚNICO concepto de cargo en seis consultas de la ficha "+
				"del cliente, sin constante ni comentario que explique el literal.",
			idCargoVenta, nombre)

		// El filtro es `= 5`, no `IN (4, 5)`. El concepto 4 "Venta" existe y
		// queda fuera. Es poco material (58 cargos contra 102,491 de concepto
		// 5 en la base de desarrollo) pero está fuera a propósito de nadie: es
		// un literal sin comentario. Se deja registrado para que quien toque
		// esas consultas lo sepa.
		if nombre4, hay := conceptos[4]; hay {
			t.Logf("nota: queries.go filtra CONCEPTO_CC_ID = %d (%q) y deja fuera el concepto 4 (%q); "+
				"el literal no tiene comentario que diga si la exclusión es deliberada",
				idCargoVenta, nombre, nombre4)
		}
	})
}
