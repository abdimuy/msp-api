// Controles positivos de codificación para rutasfb.
//
// Dos columnas de este paquete se leían con un doble-decode:
//
//	CLIENTES.NOMBRE    ISO8859_1 → string           (cobranza_repo.go)
//	COBRADORES.NOMBRE  ISO8859_1 → sql.NullString   (repo.go)
//
// Y una que NO: ZONAS_CLIENTES.NOMBRE y DOCTOS_PV.FOLIO son CHARACTER SET NONE
// y siguen pasando por firebird.Win1252.
//
// Ambas pruebas siembran los acentos que la base de desarrollo no tiene: los 52
// cobradores del catálogo son 100% ASCII, y sobre ASCII el destino de escaneo
// equivocado da el mismo resultado que el correcto.
//
//nolint:paralleltest // serie por diseño: comparten la tx de rollback.
//nolint:misspell    // vocabulario español por convención.
package rutasfb_test

import (
	"context"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

func TestCobranzaRepo_Acentos_NombreDelCliente(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		repo := rutasfb.NewCobranzaRepo(pool)

		const nombre = "SILVIA CARRANZA MUÑOZ PEÑA"
		zona := microsipseed.PrimeraZona(t, q)
		clienteID := microsipseed.ClienteEnZona(t, q, nombre, zona)
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			Fecha: time.Now().UTC(),
			Total: decimal.NewFromInt(12000),
		})

		ventas, err := repo.VentasPorZona(ctx, zona,
			time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, 1))
		require.NoError(t, err)

		encontrada := false
		for _, v := range ventas {
			assert.Truef(t, utf8.ValidString(v.ClienteNombre),
				"CLIENTES.NOMBRE de la venta %d no es UTF-8 válido: %q", v.VentaID, v.ClienteNombre)
			if v.DoctoPVID != venta.DoctoPVID {
				continue
			}
			encontrada = true
			assert.Equal(t, nombre, v.ClienteNombre,
				"CLIENTES.NOMBRE es ISO8859_1: Firebird ya lo transliteró, se escanea como string")
			assert.NotContains(t, v.ClienteNombre, "Ã", "firma del doble-decode")
			// DOCTOS_PV.FOLIO es CHARACTER SET NONE y se sigue leyendo con
			// firebird.Win1252: el folio sembrado tiene que llegar intacto.
			assert.Equal(t, venta.Folio, v.Folio, "DOCTOS_PV.FOLIO (columna NONE)")
		}
		require.True(t, encontrada, "la venta sembrada debe aparecer en la zona")
	})
}

func TestRutasRepo_Acentos_NombreDelCobrador(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		repo := rutasfb.NewRutasRepo(pool)

		const nombreCobrador = "RUTA 98 - ROSAURA IBÁÑEZ MUÑOZ"
		cobradorID := microsipseed.CobradorConNombre(t, q, nombreCobrador)
		zona := microsipseed.PrimeraZona(t, q)

		// MSP_CFG_ZONA_CAJA es la tabla nuestra que amarra zona → cobrador; es
		// de donde ListarRutas saca el COBRADOR_ID que resuelve contra
		// COBRADORES. Se escribe aquí con todos los valores explícitos, como
		// exige CLAUDE.md §1.
		_, err := q.ExecContext(ctx,
			`UPDATE OR INSERT INTO MSP_CFG_ZONA_CAJA
			   (ZONA_CLIENTE_ID, CAJA_ID, CAJERO_ID, VENDEDOR_ID, COBRADOR_ID)
			 VALUES (?, -1, -1, -1, ?)
			 MATCHING (ZONA_CLIENTE_ID)`,
			zona, cobradorID)
		require.NoError(t, err, "amarrando la zona al cobrador sembrado")

		rutas, err := repo.ListarRutas(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, rutas)

		encontrada := false
		for _, r := range rutas {
			assert.Truef(t, utf8.ValidString(r.CobradorNombre),
				"COBRADORES.NOMBRE de la zona %d no es UTF-8 válido: %q", r.ZonaID, r.CobradorNombre)
			if r.ZonaID != zona {
				continue
			}
			encontrada = true
			require.NotNil(t, r.CobradorID)
			assert.Equal(t, cobradorID, *r.CobradorID)
			assert.Equal(t, nombreCobrador, r.CobradorNombre,
				"COBRADORES.NOMBRE es ISO8859_1: se escanea como sql.NullString, no con firebird.Win1252")
			assert.NotContains(t, r.CobradorNombre, "Ã", "firma del doble-decode")
		}
		require.True(t, encontrada, "la zona configurada debe aparecer en el listado")
	})
}
