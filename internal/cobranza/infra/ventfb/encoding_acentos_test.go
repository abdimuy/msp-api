// Control positivo de codificación para productos_repo.go.
//
// ARTICULOS.NOMBRE es CHARACTER SET ISO8859_1: Firebird ya la transliterar a
// UTF-8 sobre la conexión charset=UTF8, y este repositorio la volvía a
// decodificar con firebird.Win1252. El nombre del artículo llegaba a la app del
// cobrador como "CAFÃ‰" en vez de "CAFÉ".
//
// El comentario que justificaba el Win1252 citaba a clientes/rowmappers.go como
// precedente y afirmaba que una conexión UTF8 "tira Malformed string al forzar
// acentos legacy". Las dos cosas eran falsas: el precedente tenía el mismo
// defecto, y la lectura plana devuelve "NIÑO" sin error — medido.
//
// La prueba siembra una venta con un artículo del catálogo ELEGIDO por traer
// acentos: los artículos acentuados son 121 de 6,113, así que tomar uno
// cualquiera deja la cobertura en ~2%.
//
//nolint:paralleltest // serie por diseño: comparte la tx de rollback.
//nolint:misspell    // vocabulario español por convención.
package ventfb_test

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

func TestVentasRepo_Acentos_NombreDelArticulo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		articuloID := microsipseed.ArticuloConAcento(t, q)
		require.NotZero(t, articuloID,
			"ningún artículo del catálogo trae acentos; sin eso la comparación pasa "+
				"igual con el doble-decode puesto")
		nombreEsperado := microsipseed.NombreDeCatalogo(t, q, "ARTICULOS", "ARTICULO_ID", articuloID)
		require.True(t, fbtestutil.ContieneNoASCII(nombreEsperado))

		clienteID := microsipseed.Cliente(t, q, "COBRANZA ACENTOS PEÑA MUÑOZ")
		venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
			ArticuloID: articuloID,
		})

		repo := cobranzaventfb.NewVentasRepo(pool)
		result, err := repo.ProductosByPVIDs(ctx, []int{venta.DoctoPVID})
		require.NoError(t, err)

		lineas, ok := result[venta.DoctoPVID]
		require.True(t, ok, "la venta sembrada debe tener líneas")
		require.Len(t, lineas, 1)

		assert.Equal(t, articuloID, lineas[0].ArticuloID())
		assert.Equal(t, nombreEsperado, lineas[0].Articulo(),
			"ARTICULOS.NOMBRE es ISO8859_1: se escanea como string, no con firebird.Win1252")
		assert.True(t, utf8.ValidString(lineas[0].Articulo()))
		assert.NotContains(t, lineas[0].Articulo(), "Ã", "firma del doble-decode")
	})
}
