package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// dptr is a tiny helper for the pointer-valued Desenlace fixtures.
func dptr(d domain.Desenlace) *domain.Desenlace { return &d }

// desenlaceStr renders an outcome for a failure message without dereferencing
// a nil pointer.
func desenlaceStr(d *domain.Desenlace) string {
	if d == nil {
		return "nil"
	}
	return string(*d)
}

// quiereDesenlace asserts the article's outcome, accepting a nil want.
func quiereDesenlace(t *testing.T, a *domain.Articulo, quiero *domain.Desenlace) {
	t.Helper()
	got := a.Desenlace()
	if quiero == nil {
		if got != nil {
			t.Fatalf("desenlace = %s, want nil", desenlaceStr(got))
		}
		return
	}
	if got == nil || *got != *quiero {
		t.Fatalf("desenlace = %s, want %s", desenlaceStr(got), desenlaceStr(quiero))
	}
}

// TestDesenlaceAutomatico covers every R6 row on the client path (the folio
// delivers the article and the outcome lands with it) plus the priority order.
func TestDesenlaceAutomatico(t *testing.T) {
	t.Parallel()
	rutaTaller := domain.RutaReparacionTaller
	rutaProveedor := domain.RutaReparacionProveedor
	aceptada := domain.DictamenAceptada
	rechazada := domain.DictamenRechazada
	sinFalla := domain.DictamenSinFalla

	casos := []struct {
		nombre   string
		rol      domain.RolArticulo
		ruta     *domain.RutaReparacion
		dictamen *domain.Dictamen
		quiero   *domain.Desenlace
	}{
		{"reemplazo_es_reemplazado", domain.RolArticuloReemplazo, &rutaTaller, nil, dptr(domain.DesenlaceReemplazado)},
		{"rechazada_es_devuelto", domain.RolArticuloOriginal, &rutaProveedor, &rechazada, dptr(domain.DesenlaceDevuelto)},
		{"sin_falla_es_devuelto", domain.RolArticuloOriginal, &rutaProveedor, &sinFalla, dptr(domain.DesenlaceDevuelto)},
		{"taller_es_reparado", domain.RolArticuloOriginal, &rutaTaller, nil, dptr(domain.DesenlaceReparado)},
		{"proveedor_aceptada_es_reparado", domain.RolArticuloOriginal, &rutaProveedor, &aceptada, dptr(domain.DesenlaceReparado)},
		{"sin_reparacion_no_tiene_desenlace", domain.RolArticuloOriginal, nil, nil, nil},
		// Priority: reemplazo wins over a rejected verdict.
		{"reemplazo_gana_a_rechazada", domain.RolArticuloReemplazo, &rutaProveedor, &rechazada, dptr(domain.DesenlaceReemplazado)},
	}

	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()
			g, a := hidratarFolio(t, hidratarParams{
				origen:    domain.OrigenFolioCliente,
				estado:    domain.EstadoFolioListoEntrega,
				etapa:     domain.EtapaListoEntrega,
				ubicacion: domain.UbicacionTaller,
				rol:       tc.rol,
				ruta:      tc.ruta,
				dictamen:  tc.dictamen,
			})
			if err := g.Entregar(actorEntrega("juan"), fixed); err != nil {
				t.Fatalf("Entregar: %v", err)
			}
			quiereDesenlace(t, a, tc.quiero)
		})
	}
}

// TestDesenlaceAutomatico_Piso covers the floor path: a floor article that lands
// on reingresado_inventario gets its outcome from the same rule, including nil
// for a purely reviewed item.
func TestDesenlaceAutomatico_Piso(t *testing.T) {
	t.Parallel()
	rutaTaller := domain.RutaReparacionTaller

	t.Run("reparado", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen:    domain.OrigenFolioPiso,
			estado:    domain.EstadoFolioEnProceso,
			etapa:     domain.EtapaListoEntrega,
			ubicacion: domain.UbicacionTaller,
			ruta:      &rutaTaller,
		})
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaReingresadoInventario, actor("juan"), fixed); err != nil {
			t.Fatalf("AvanzarArticulo: %v", err)
		}
		quiereDesenlace(t, a, dptr(domain.DesenlaceReparado))
	})

	t.Run("sin_desenlace", func(t *testing.T) {
		t.Parallel()
		g, a := hidratarFolio(t, hidratarParams{
			origen: domain.OrigenFolioPiso, estado: domain.EstadoFolioEnProceso, etapa: domain.EtapaEnRevision,
		})
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaReingresadoInventario, actor("juan"), fixed); err != nil {
			t.Fatalf("AvanzarArticulo: %v", err)
		}
		quiereDesenlace(t, a, nil)
	})
}
