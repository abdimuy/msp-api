//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import "testing"

// DOCTOS_CC.LAT / .LON son VARCHAR(100), no numéricos: el driver los entrega
// como string o []byte. Y el 0 es el centinela de "sin señal GPS" que graba la
// app cuando captura sin fix — 68,526 pagos en producción lo traen.
//
// Estos casos son el contrato de scanCoordinate. Si alguien lo "simplifica" a
// scanNullableDecimal, el 0 vuelve a viajar como coordenada real y el cliente
// calcula centroides contra el punto (0,0) — distancias absurdas, sin ningún
// síntoma que delate la causa.
func TestScanCoordinate(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre     string
		raw        any
		quiereNil  bool
		quiereText string
	}{
		{
			nombre:     "latitud real en texto",
			raw:        "18.2708885",
			quiereText: "18.2708885",
		},
		{
			nombre:     "longitud negativa en texto",
			raw:        "-97.1446266",
			quiereText: "-97.1446266",
		},
		{
			nombre:     "el driver la entrega como bytes",
			raw:        []byte("18.9448351"),
			quiereText: "18.9448351",
		},
		{
			nombre:    "SQL NULL: el pago no trae coordenada",
			raw:       nil,
			quiereNil: true,
		},
		{
			nombre:    "cero entero: centinela de sin senal GPS",
			raw:       "0",
			quiereNil: true,
		},
		{
			nombre:    "cero con decimales: mismo centinela",
			raw:       "0.0",
			quiereNil: true,
		},
		{
			nombre:    "cero con mas decimales",
			raw:       "0.00000000",
			quiereNil: true,
		},
		{
			nombre:    "texto sucio: se descarta, no revienta la pagina del sync",
			raw:       "no-es-una-coordenada",
			quiereNil: true,
		},
		{
			nombre:    "cadena vacia",
			raw:       "",
			quiereNil: true,
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()

			got := scanCoordinate(c.raw)

			if c.quiereNil {
				if got != nil {
					t.Fatalf("esperaba nil (sin ubicacion), obtuve %s", got.String())
				}
				return
			}

			if got == nil {
				t.Fatalf("esperaba %s, obtuve nil", c.quiereText)
			}
			if got.String() != c.quiereText {
				t.Fatalf("esperaba %s, obtuve %s", c.quiereText, got.String())
			}
		})
	}
}

// Una coordenada válida jamás debe descartarse por parecerse al centinela:
// 0.5 no es 0, y una longitud negativa cercana a cero tampoco.
func TestScanCoordinateNoDescartaValoresCercanosACero(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"0.5", "-0.5", "0.00000001", "-0.00000001"} {
		if got := scanCoordinate(raw); got == nil {
			t.Fatalf("%s es una coordenada valida y se descarto", raw)
		}
	}
}
