// Control positivo de codificación para el catálogo de ciudades de ventas.
//
// CIUDADES.NOMBRE es CHARACTER SET ISO8859_1 y se estaba leyendo con
// firebird.Win1252, o sea decodificándola dos veces. Aquí el daño no era
// cosmético: el nombre corrompido se usa como CLAVE del índice en memoria.
//
//	NormalizeCiudad("CAÑADA MORELOS")   → "CANADA MORELOS"   ← lo que teclea el vendedor
//	NormalizeCiudad("CAÃ‘ADA MORELOS")  → "CAA‘ADA MORELOS"  ← lo que quedaba en el índice
//
// Las dos claves no se cruzan nunca, así que Resolver devolvía "no encontrada"
// para toda ciudad acentuada — un fallo silencioso, sin error, en el camino de
// aplicar una venta.
//
//nolint:paralleltest // serie por diseño: comparte el pool.
//nolint:misspell    // vocabulario español por convención.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

func TestCiudadCatalogoRepo_ResuelveCiudadAcentuada(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	// La ciudad acentuada sale del propio catálogo, no de una constante: la
	// prueba no depende de que exista una fila concreta, sólo de que exista
	// alguna con acentos (la base de desarrollo trae "CAÑADA MORELOS" y
	// "SAN ANTONIO CAÑADA").
	q := firebird.GetQuerier(ctx, pool.DB)
	verdaderos := fbtestutil.NombresDeCatalogo(ctx, t, q, "CIUDADES", "CIUDAD_ID", "NOMBRE")

	var (
		ciudadID int
		nombre   string
	)
	for id, n := range verdaderos {
		if fbtestutil.ContieneNoASCII(n) && (ciudadID == 0 || id < ciudadID) {
			ciudadID, nombre = id, n
		}
	}
	require.NotZerof(t, ciudadID,
		"ninguna de las %d ciudades del catálogo trae acentos; sin eso esta prueba no "+
			"distingue la lectura correcta de la corrompida", len(verdaderos))

	repo := ventfb.NewCiudadCatalogoRepo(pool)
	res, err := repo.Resolver(ctx, nombre)
	require.NoError(t, err)
	require.NotNilf(t, res,
		"Resolver(%q) devolvió \"no encontrada\". El índice se construye con "+
			"NormalizeCiudad(CIUDADES.NOMBRE): si el nombre llega con mojibake, la clave "+
			"del índice no coincide con la que produce el nombre que teclea el vendedor, "+
			"y toda ciudad acentuada deja de resolverse SIN error.", nombre)
	assert.Equal(t, ciudadID, res.CiudadID, "CIUDAD_ID resuelto para %q", nombre)
	t.Logf("Resolver(%q) → CIUDAD_ID=%d ESTADO_ID=%d", nombre, res.CiudadID, res.EstadoID)
}
