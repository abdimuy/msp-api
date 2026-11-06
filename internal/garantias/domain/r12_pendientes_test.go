package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// fptr returns a pointer to a float, the shape the GPS fields take.
func fptr(f float64) *float64 { return &f }

// clienteConGPS is valid cliente params with the coordinates replaced, so a test
// can drive the folio GPS validation without repeating the whole address.
func clienteConGPS(lat, lon *float64) domain.AbrirGarantiaParams {
	p := clienteParams()
	p.GPSLat, p.GPSLon = lat, lon
	return p
}

// TestR12_DomicilioGPS_SeValidaComoElEvento pins R12: the folio coordinates obey
// the event rule (gpsValido) — the pair complete or absent, each in range and
// finite. Half a position and an impossible coordinate are both rejected.
func TestR12_DomicilioGPS_SeValidaComoElEvento(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		lat    *float64
		lon    *float64
	}{
		{"solo_lat", fptr(19.4), nil},
		{"solo_lon", nil, fptr(-99.1)},
		{"lat_fuera_de_rango", fptr(91), fptr(0)},
		{"lon_fuera_de_rango", fptr(0), fptr(-181)},
		{"nan", fptr(math.NaN()), fptr(0)},
		{"infinito", fptr(math.Inf(1)), fptr(0)},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			_, err := domain.AbrirGarantia(clienteConGPS(c.lat, c.lon))
			if !errors.Is(err, domain.ErrDomicilioGPSInvalido) {
				t.Fatalf("AbrirGarantia(%s): err = %v, want ErrDomicilioGPSInvalido", c.nombre, err)
			}
		})
	}
}

// TestR12_DomicilioGPS_AusenteOCompletoSeAcepta: nothing and a complete in-range
// pair both pass, so the rule rejects only what gpsValido rejects.
func TestR12_DomicilioGPS_AusenteOCompletoSeAcepta(t *testing.T) {
	t.Parallel()
	t.Run("ausente", func(t *testing.T) {
		t.Parallel()
		g, err := domain.AbrirGarantia(clienteConGPS(nil, nil))
		if err != nil {
			t.Fatalf("AbrirGarantia: %v", err)
		}
		if g.GPSLat() != nil || g.GPSLon() != nil {
			t.Errorf("GPS = (%v, %v), want both nil", g.GPSLat(), g.GPSLon())
		}
	})
	t.Run("completo", func(t *testing.T) {
		t.Parallel()
		g, err := domain.AbrirGarantia(clienteConGPS(fptr(19.427), fptr(-99.17)))
		if err != nil {
			t.Fatalf("AbrirGarantia: %v", err)
		}
		if g.GPSLat() == nil || *g.GPSLat() != 19.427 || g.GPSLon() == nil || *g.GPSLon() != -99.17 {
			t.Errorf("GPS = (%v, %v), want (19.427, -99.17)", g.GPSLat(), g.GPSLon())
		}
	})
}

// TestR12_DomicilioGPS_PisoSigueRechazandoElDomicilio: for a piso folio the GPS
// belongs to the forbidden domicile, so ErrDomicilioNoPermitido still wins over
// the coordinate rule (validarOrigenPiso runs first).
func TestR12_DomicilioGPS_PisoSigueRechazandoElDomicilio(t *testing.T) {
	t.Parallel()
	p := domain.AbrirGarantiaParams{
		Folio:       domain.Folio("GA-000042"),
		Origen:      domain.OrigenFolioPiso,
		Description: "silla rota",
		GPSLat:      fptr(19.427),
		GPSLon:      fptr(-99.17),
		AbiertoPor:  "juan",
		Now:         fixed,
		Actor:       actor("juan"),
	}
	_, err := domain.AbrirGarantia(p)
	if !errors.Is(err, domain.ErrDomicilioNoPermitido) {
		t.Fatalf("AbrirGarantia(piso+gps): err = %v, want ErrDomicilioNoPermitido", err)
	}
}

// compuesto and descompuesto spell "Juárez" two ways: the same text, two byte
// sequences. NFC collapses them (R12), and the test source is UTF-8, so the
// expectations are spelled with an explicit escape to stay unambiguous.
const (
	compuesto    = "Ju\u00e1rez"  // á as one code point
	descompuesto = "Jua\u0301rez" // a + combining acute
)

// TestR12_NFC_FolioNormalizaElTextoLibre: the fields that enter at AbrirGarantia
// are stored composed, so "Juárez" typed either way is one string afterwards.
func TestR12_NFC_FolioNormalizaElTextoLibre(t *testing.T) {
	t.Parallel()
	if compuesto == descompuesto {
		t.Fatal("sanity: the two spellings must differ before normalization")
	}
	p := clienteParams()
	p.Description = "reparar " + descompuesto
	p.Calle = "calle " + descompuesto
	p.Colonia = descompuesto
	p.AbiertoPor = descompuesto

	g, err := domain.AbrirGarantia(p)
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	if got := g.Description(); got != "reparar "+compuesto {
		t.Errorf("Description() = %q, want %q", got, "reparar "+compuesto)
	}
	if got := g.Calle(); got != "calle "+compuesto {
		t.Errorf("Calle() = %q, want %q", got, "calle "+compuesto)
	}
	if got := g.Colonia(); got != compuesto {
		t.Errorf("Colonia() = %q, want %q", got, compuesto)
	}
	if got := g.AbiertoPor(); got != compuesto {
		t.Errorf("AbiertoPor() = %q, want %q", got, compuesto)
	}
}

// TestR12_NFC_ArticuloNormalizaLaDescripcion: the article description enters at
// newArticulo and is stored composed too.
func TestR12_NFC_ArticuloNormalizaLaDescripcion(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	if err := g.AgregarArticulo(
		domain.AgregarArticuloParams{Description: "caja " + descompuesto},
		actor("juan"), fixed,
	); err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}
	a := firstArticulo(t, g)
	if got := a.Description(); got != "caja "+compuesto {
		t.Errorf("Description() = %q, want %q", got, "caja "+compuesto)
	}
}

// TestR12_NFC_EventoNormalizaLaDescripcion: the reason a mutator records travels
// in the event description, so the cancel reason comes out composed as well.
func TestR12_NFC_EventoNormalizaLaDescripcion(t *testing.T) {
	t.Parallel()
	g, _ := hidratarFolio(t, hidratarParams{
		origen: domain.OrigenFolioPiso, etapa: domain.EtapaEnRevision,
		ubicacion: domain.UbicacionAlmacenRevision,
	})
	if err := g.Cancelar("no aplica "+descompuesto, actor("juan"), fixed); err != nil {
		t.Fatalf("Cancelar: %v", err)
	}
	if got := lastEvent(t, g).Description(); got != "no aplica "+compuesto {
		t.Errorf("evento Description() = %q, want %q", got, "no aplica "+compuesto)
	}
}
