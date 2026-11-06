package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// huellaPrueba is a well-formed SHA-256 hex, the only shape MSP_GA_IMAGEN.HUELLA
// accepts when it is not empty.
var huellaPrueba = strings.Repeat("ab", 32)

// folioListoParaEntregar rebuilds a cliente folio with one article ready to be
// delivered, the state Entregar requires.
func folioListoParaEntregar(t *testing.T) (*domain.Garantia, *domain.Articulo) {
	t.Helper()
	return hidratarFolio(t, hidratarParams{
		origen:    domain.OrigenFolioCliente,
		estado:    domain.EstadoFolioListoEntrega,
		etapa:     domain.EtapaListoEntrega,
		ubicacion: domain.UbicacionTaller,
	})
}

// TestEntregar_SinFirma: the client signature is mandatory. A blank path (or
// one that is only spaces) is refused before any state changes and before an
// image row is created.
func TestEntregar_SinFirma(t *testing.T) {
	t.Parallel()
	for _, firma := range []string{"", "   "} {
		t.Run("firma="+firma, func(t *testing.T) {
			t.Parallel()
			g, a := folioListoParaEntregar(t)
			estado := g.Estado()
			eventos := g.EventosPendientesCount()
			imagenes := len(g.ImagenesPendientesForRepo())

			err := g.Entregar(firma, actorEntrega("juan"), fixed)
			if !errors.Is(err, domain.ErrFirmaRequerida) {
				t.Fatalf("want ErrFirmaRequerida, got %v", err)
			}
			if g.Estado() != estado || a.Etapa() != domain.EtapaListoEntrega {
				t.Errorf("estado/etapa mutados: %q/%q", g.Estado(), a.Etapa())
			}
			if g.EventosPendientesCount() != eventos {
				t.Errorf("eventos = %d, want %d", g.EventosPendientesCount(), eventos)
			}
			if len(g.ImagenesPendientesForRepo()) != imagenes {
				t.Errorf("imagenes = %d, want %d", len(g.ImagenesPendientesForRepo()), imagenes)
			}
		})
	}
}

// TestEntregar_RutaDeFirmaInvalida: the signature path must stay inside
// STORAGE_DIR. A path that escapes it fails as an image error, with the
// aggregate untouched, so the database never points outside the storage root.
func TestEntregar_RutaDeFirmaInvalida(t *testing.T) {
	t.Parallel()
	for _, ruta := range []string{"/etc/firma.png", `C:\firma.png`, "../firma.png", "garantias/../firma.png"} {
		t.Run(ruta, func(t *testing.T) {
			t.Parallel()
			g, _ := folioListoParaEntregar(t)
			estado := g.Estado()
			eventos := g.EventosPendientesCount()

			err := g.Entregar(ruta, actorEntrega("juan"), fixed)
			if !errors.Is(err, domain.ErrImagenRutaInvalida) {
				t.Fatalf("want ErrImagenRutaInvalida, got %v", err)
			}
			if g.Estado() != estado || g.EventosPendientesCount() != eventos {
				t.Errorf("mutacion pese al rechazo: estado=%q eventos=%d", g.Estado(), g.EventosPendientesCount())
			}
			if n := len(g.ImagenesPendientesForRepo()); n != 0 {
				t.Errorf("imagenes = %d, want 0", n)
			}
		})
	}
}

// TestEntregar_ImagenDeFirma walks the signature row Entregar now creates: one
// pending image, typed firma, hanging from the folio_entregado event of this
// call (not from an article event), with the id derived from the command key so
// a retry lands on the same primary key. The path is normalized and the upload
// is attributed to the actor and carries the request fingerprint.
func TestEntregar_ImagenDeFirma(t *testing.T) {
	t.Parallel()
	g, _ := folioListoParaEntregar(t)
	actor := actorEntrega("juan")
	actor.Huella = huellaPrueba

	if err := g.Entregar("  garantias/firmas/firma.png  ", actor, fixed); err != nil {
		t.Fatalf("Entregar: %v", err)
	}

	imagenes := g.ImagenesPendientesForRepo()
	if len(imagenes) != 1 {
		t.Fatalf("imagenes pendientes = %d, want 1", len(imagenes))
	}
	img := imagenes[0]
	if img.Tipo() != domain.TipoImagenFirma {
		t.Errorf("tipo = %q, want firma", img.Tipo())
	}
	if img.ID() != domain.FirmaImagenID(actor.ClaveIdempotencia) {
		t.Errorf("id = %s, want el derivado de la clave", img.ID())
	}
	if img.Ruta() != "garantias/firmas/firma.png" {
		t.Errorf("ruta = %q, want normalizada", img.Ruta())
	}
	if img.SubidaPor() != "juan" {
		t.Errorf("subidaPor = %q, want juan", img.SubidaPor())
	}
	if img.Huella() != huellaPrueba {
		t.Errorf("huella = %q, want la del actor", img.Huella())
	}
	if !img.CreatedAt().Equal(fixed) {
		t.Errorf("createdAt = %v, want %v", img.CreatedAt(), fixed)
	}

	folio := lastEvent(t, g)
	if folio.Tipo() != domain.TipoEventoFolioEntregado {
		t.Fatalf("ultimo evento = %q, want folio_entregado", folio.Tipo())
	}
	if img.EventoID() != folio.ID() {
		t.Errorf("la firma cuelga de %s, want el folio_entregado %s", img.EventoID(), folio.ID())
	}
	for _, ev := range eventosDeTipo(g, domain.TipoEventoEtapaAvanzada) {
		if img.EventoID() == ev.ID() {
			t.Errorf("la firma cuelga de un evento de articulo %s, no del folio_entregado", ev.ID())
		}
	}
}

// TestEntregar_ImagenEnFolioDePiso: a floor folio moves no article, so the only
// event of the call is folio_entregado; the signature hangs from it.
func TestEntregar_ImagenEnFolioDePiso(t *testing.T) {
	t.Parallel()
	g, _ := hidratarFolio(t, hidratarParams{
		origen:    domain.OrigenFolioPiso,
		estado:    domain.EstadoFolioListoEntrega,
		etapa:     domain.EtapaReingresadoInventario,
		ubicacion: domain.UbicacionAlmacenRevision,
	})
	actor := actorEntrega("juan")

	if err := g.Entregar(firmaPrueba, actor, fixed); err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	imagenes := g.ImagenesPendientesForRepo()
	if len(imagenes) != 1 {
		t.Fatalf("imagenes pendientes = %d, want 1", len(imagenes))
	}
	if got := imagenes[0].EventoID(); got != lastEvent(t, g).ID() {
		t.Errorf("la firma cuelga de %s, want el folio_entregado", got)
	}
}
