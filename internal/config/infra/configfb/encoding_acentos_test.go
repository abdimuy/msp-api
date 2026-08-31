// Control positivo de codificación para los catálogos que lee configfb.
//
// POR QUÉ ESTE ARCHIVO
// ====================
// listCatalogoRef sirve CINCO catálogos de Microsip con UN solo destino de
// escaneo, y los cinco no comparten charset:
//
//	ZONAS_CLIENTES.NOMBRE  NONE       → firebird.Win1252
//	CAJAS.NOMBRE           NONE       → firebird.Win1252
//	CAJEROS.NOMBRE         ISO8859_1  → sql.NullString
//	VENDEDORES.NOMBRE      ISO8859_1  → sql.NullString
//	COBRADORES.NOMBRE      ISO8859_1  → sql.NullString
//
// Leerlos todos con firebird.Win1252 —lo que hacía— corrompe los tres de abajo.
//
// EL PROBLEMA DEL DATO: en la base de desarrollo los 52 cobradores, los 66
// cajeros y los 46 vendedores tienen nombres 100% ASCII, y sobre ASCII el
// doble-decode es la identidad. Una comparación contra el catálogo pasaría
// igual con el defecto puesto. Por eso la prueba SIEMBRA un cobrador con
// acentos dentro de la transacción que siempre se revierte.
//
//nolint:paralleltest // serie por diseño: comparten la tx de rollback.
//nolint:misspell    // vocabulario español por convención.
package configfb_test

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/config/infra/configfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// TestConfigRepo_Acentos_CobradorSembrado cubre la familia ISO8859_1 del helper.
func TestConfigRepo_Acentos_CobradorSembrado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := configfb.NewConfigRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		const nombre = "RUTA 99 - IGNACIO MUÑOZ PEÑA"
		cobradorID := microsipseed.CobradorConNombre(t, q, nombre)

		cobradores, err := repo.ListarCobradores(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cobradores)

		encontrado := false
		for _, c := range cobradores {
			assert.Truef(t, utf8.ValidString(c.Nombre),
				"COBRADORES.NOMBRE del id %d no es UTF-8 válido: %q", c.ID, c.Nombre)
			if c.ID != cobradorID {
				continue
			}
			encontrado = true
			assert.Equalf(t, nombre, c.Nombre,
				"COBRADORES.NOMBRE es CHARACTER SET ISO8859_1: Firebird ya lo transliteró "+
					"a UTF-8 y hay que escanearlo como string. Leerlo con firebird.Win1252 "+
					"lo decodifica una segunda vez.")
			assert.NotContains(t, c.Nombre, "Ã", "firma del doble-decode")
		}
		require.True(t, encontrado, "el cobrador sembrado debe aparecer en el catálogo")
	})
}

// TestConfigRepo_Acentos_CatalogosContraElCatalogo compara los cinco catálogos
// contra su valor verdadero, leído con un CAST que no depende del destino de
// escaneo del repositorio. Cubre las dos familias en una sola pasada.
//
// Para ZONAS_CLIENTES y CAJAS (CHARACTER SET NONE) la comparación es hoy
// ESTRUCTURAL, no un control positivo: ninguna de sus filas trae no-ASCII, así
// que pasaría igual con el destino equivocado. La red para esas dos es
// TestCatalogoCharsets_ClasificacionDeColumnas, que le pregunta el charset a
// RDB$FIELDS en vez de mirar datos. El caso ISO8859_1 sí queda cubierto por
// TestConfigRepo_Acentos_CobradorSembrado, que siembra los acentos que faltan.
func TestConfigRepo_Acentos_CatalogosContraElCatalogo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := configfb.NewConfigRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		casos := []struct {
			tabla  string
			colID  string
			listar func(context.Context) ([]configdomain.CatalogoRef, error)
		}{
			{"ZONAS_CLIENTES", "ZONA_CLIENTE_ID", repo.ListarZonas},
			{"CAJAS", "CAJA_ID", repo.ListarCajas},
			{"CAJEROS", "CAJERO_ID", repo.ListarCajeros},
			{"VENDEDORES", "VENDEDOR_ID", repo.ListarVendedoresCatalogo},
			{"COBRADORES", "COBRADOR_ID", repo.ListarCobradores},
		}

		for _, c := range casos {
			verdaderos := fbtestutil.NombresDeCatalogo(ctx, t, q, c.tabla, c.colID, "NOMBRE")
			filas, err := c.listar(ctx)
			require.NoErrorf(t, err, "listando %s", c.tabla)
			require.NotEmptyf(t, filas, "%s vino vacío", c.tabla)

			acentuados := 0
			for _, f := range filas {
				assert.Equalf(t, verdaderos[f.ID], f.Nombre, "%s.NOMBRE del id %d", c.tabla, f.ID)
				assert.Truef(t, utf8.ValidString(f.Nombre),
					"%s.NOMBRE del id %d no es UTF-8 válido: %q", c.tabla, f.ID, f.Nombre)
				if fbtestutil.ContieneNoASCII(verdaderos[f.ID]) {
					acentuados++
				}
			}
			t.Logf("%s: %d filas, %d con acentos", c.tabla, len(filas), acentuados)
		}
	})
}

// TestConfigRepo_Acentos_ZonaSembrada es el control positivo que le faltaba a
// ZONAS_CLIENTES.NOMBRE, la columna donde el cambio fue en dirección CONTRARIA
// (de string plano a firebird.Win1252).
//
// Es la única de las dos familias que no tenía prueba de comportamiento: las 46
// zonas de la base son 100% ASCII y sobre ASCII el doble-decode es la
// identidad, así que TestConfigRepo_Acentos_CatalogosContraElCatalogo pasa con
// el destino de escaneo equivocado. Comprobado: cambiando el destino a `string`
// pelado, aquella prueba sigue en verde y ésta falla.
//
// La zona se renombra dentro de la transacción que siempre revierte y el nombre
// se escribe en bytes Windows-1252, que es como Microsip guarda una columna
// CHARACTER SET NONE.
func TestConfigRepo_Acentos_ZonaSembrada(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := configfb.NewConfigRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		const nombre = "ZONA CAÑADA MORELOS ORIÉNTE"
		zonaID := microsipseed.ZonaConNombre(t, q, nombre)

		zonas, err := repo.ListarZonas(ctx)
		require.NoError(t, err)

		var vista bool
		for _, z := range zonas {
			if z.ID != zonaID {
				continue
			}
			vista = true
			assert.Equal(t, nombre, z.Nombre,
				"ZONAS_CLIENTES.NOMBRE es CHARACTER SET NONE: Firebird entrega los bytes "+
					"Windows-1252 crudos y hay que decodificarlos con firebird.Win1252. "+
					"Un string pelado deja UTF-8 inválido; un doble-decode parte la Ñ en Ã‘.")
			assert.Truef(t, utf8.ValidString(z.Nombre),
				"la zona %d no volvió como UTF-8 válido: %q", zonaID, z.Nombre)
			assert.NotContains(t, z.Nombre, "Ã",
				"la \"Ã\" es la firma del doble-decode Windows-1252")
		}
		require.True(t, vista, "la zona %d sembrada debe aparecer en el listado", zonaID)
	})
}
