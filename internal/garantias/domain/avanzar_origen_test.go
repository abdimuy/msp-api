package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// todasLasEtapas lists every stage once, in the order of etapa.go.
func todasLasEtapas() []domain.Etapa {
	return []domain.Etapa{
		domain.EtapaRegistrado,
		domain.EtapaPendienteRecoleccion,
		domain.EtapaRecolectado,
		domain.EtapaEnRevision,
		domain.EtapaOrdenGenerada,
		domain.EtapaEnviadoProveedor,
		domain.EtapaDictamenRecibido,
		domain.EtapaReparadoProveedor,
		domain.EtapaEsperaRespuestaCliente,
		domain.EtapaEnTaller,
		domain.EtapaReparadoTaller,
		domain.EtapaCambioAutorizado,
		domain.EtapaListoEntrega,
		domain.EtapaEntregado,
		domain.EtapaReingresadoInventario,
		domain.EtapaStandby,
		domain.EtapaSegundaMano,
		domain.EtapaDesarmado,
		domain.EtapaMerma,
	}
}

// folioConArticuloEn rebuilds a folio with a single article already sitting in
// desde, so the test can drive AvanzarArticulo from any state without walking
// the machine first. The route is always set so the cross-validation #3 guard
// (leaving en_revision through a repair route requires one) never masks the
// R1/R2 rule under test.
func folioConArticuloEn(t *testing.T, origen domain.OrigenFolio, desde domain.Etapa) (*domain.Garantia, *domain.Articulo) {
	t.Helper()
	gID := uuid.New()
	ruta := domain.RutaReparacionProveedor
	a := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          uuid.New(),
		GarantiaID:  gID,
		Rol:         domain.RolArticuloOriginal,
		Clave:       "CLAVE",
		Description: "silla",
		Ruta:        &ruta,
		Etapa:       desde,
		Ubicacion:   domain.UbicacionAlmacenRevision,
		CreatedAt:   fixed,
		UpdatedAt:   fixed,
	})
	g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:          gID,
		Folio:       domain.Folio("GA-000042"),
		Origen:      origen,
		Estado:      domain.EstadoFolioEnProceso,
		Description: "silla rota",
		AbiertoPor:  "juan",
		CreatedAt:   fixed,
		UpdatedAt:   fixed,
		Articulos:   []*domain.Articulo{a},
	})
	return g, a
}

// avanzarEsperado is the hand-written R1+R2 oracle: for each origin and stage,
// the exact set of destinations AvanzarArticulo accepts. It is deliberately
// independent of transiciones.go, so mutant D1 (dropping the origin filter) and
// mutant D2 (allowing an exit from dictamen_recibido) make it fail.
var avanzarEsperado = map[domain.OrigenFolio]map[domain.Etapa][]domain.Etapa{
	domain.OrigenFolioCliente: {
		domain.EtapaRegistrado:             {domain.EtapaPendienteRecoleccion},
		domain.EtapaPendienteRecoleccion:   {domain.EtapaRecolectado},
		domain.EtapaRecolectado:            {domain.EtapaEnRevision},
		domain.EtapaEnRevision:             {domain.EtapaOrdenGenerada, domain.EtapaEnTaller},
		domain.EtapaOrdenGenerada:          {domain.EtapaEnviadoProveedor},
		domain.EtapaEnviadoProveedor:       {domain.EtapaDictamenRecibido},
		domain.EtapaDictamenRecibido:       {},
		domain.EtapaReparadoProveedor:      {domain.EtapaListoEntrega},
		domain.EtapaEsperaRespuestaCliente: {domain.EtapaListoEntrega},
		domain.EtapaEnTaller:               {domain.EtapaReparadoTaller},
		domain.EtapaReparadoTaller:         {domain.EtapaListoEntrega},
		domain.EtapaCambioAutorizado:       {},
		domain.EtapaListoEntrega:           {},
		domain.EtapaEntregado:              {},
		domain.EtapaReingresadoInventario:  {},
		domain.EtapaStandby:                {},
		domain.EtapaSegundaMano:            {},
		domain.EtapaDesarmado:              {},
		domain.EtapaMerma:                  {},
	},
	domain.OrigenFolioPiso: {
		domain.EtapaRegistrado:             {domain.EtapaEnRevision},
		domain.EtapaPendienteRecoleccion:   {},
		domain.EtapaRecolectado:            {domain.EtapaEnRevision},
		domain.EtapaEnRevision:             {domain.EtapaOrdenGenerada, domain.EtapaEnTaller, domain.EtapaReingresadoInventario},
		domain.EtapaOrdenGenerada:          {domain.EtapaEnviadoProveedor},
		domain.EtapaEnviadoProveedor:       {domain.EtapaDictamenRecibido},
		domain.EtapaDictamenRecibido:       {},
		domain.EtapaReparadoProveedor:      {domain.EtapaListoEntrega},
		domain.EtapaEsperaRespuestaCliente: {domain.EtapaListoEntrega},
		domain.EtapaEnTaller:               {domain.EtapaReparadoTaller},
		domain.EtapaReparadoTaller:         {domain.EtapaListoEntrega},
		domain.EtapaCambioAutorizado:       {},
		domain.EtapaListoEntrega:           {domain.EtapaReingresadoInventario},
		domain.EtapaEntregado:              {},
		domain.EtapaReingresadoInventario:  {},
		domain.EtapaStandby:                {},
		domain.EtapaSegundaMano:            {},
		domain.EtapaDesarmado:              {},
		domain.EtapaMerma:                  {},
	},
}

// TestAvanzarArticulo_Exhaustivo drives every (origen × etapa × destino) triple
// and checks AvanzarArticulo accepts exactly the R1+R2 oracle. Every rejection
// also proves the two halves: stage unchanged and no event queued.
func TestAvanzarArticulo_Exhaustivo(t *testing.T) {
	t.Parallel()

	for origen, porEtapa := range avanzarEsperado {
		for _, desde := range todasLasEtapas() {
			permitido := make(map[domain.Etapa]bool)
			for _, e := range porEtapa[desde] {
				permitido[e] = true
			}
			for _, hasta := range todasLasEtapas() {
				t.Run(origen.String()+"_"+desde.String()+"_"+hasta.String(), func(t *testing.T) {
					t.Parallel()

					g, a := folioConArticuloEn(t, origen, desde)
					err := g.AvanzarArticulo(a.ID(), hasta, actor("juan"), fixed)

					if permitido[hasta] {
						if err != nil {
							t.Fatalf("AvanzarArticulo(%s -> %s, origen %s): want ok, got %v", desde, hasta, origen, err)
						}
						if a.Etapa() != hasta {
							t.Fatalf("etapa = %q, want %q", a.Etapa(), hasta)
						}
						if countPending(g) != 1 {
							t.Fatalf("pending events = %d, want 1", countPending(g))
						}
						return
					}

					if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
						t.Fatalf("AvanzarArticulo(%s -> %s, origen %s): want ErrTransicionEtapaNoPermitida, got %v",
							desde, hasta, origen, err)
					}
					if a.Etapa() != desde {
						t.Fatalf("stage changed on rejection: %q, want %q", a.Etapa(), desde)
					}
					if countPending(g) != 0 {
						t.Fatalf("events queued on rejection: %d, want 0", countPending(g))
					}
				})
			}
		}
	}
}

// TestAvanzarArticulo_ReglaDeOrigen names the R1 bullets, so a failure reads.
func TestAvanzarArticulo_ReglaDeOrigen(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		origen domain.OrigenFolio
		desde  domain.Etapa
		hasta  domain.Etapa
		ok     bool
	}{
		{"cliente no se salta la recoleccion", domain.OrigenFolioCliente, domain.EtapaRegistrado, domain.EtapaEnRevision, false},
		{"cliente recolecta", domain.OrigenFolioCliente, domain.EtapaRegistrado, domain.EtapaPendienteRecoleccion, true},
		{"piso nunca recolecta", domain.OrigenFolioPiso, domain.EtapaRegistrado, domain.EtapaPendienteRecoleccion, false},
		{"piso entra directo a revision", domain.OrigenFolioPiso, domain.EtapaRegistrado, domain.EtapaEnRevision, true},
		{"reingresado_inventario solo piso", domain.OrigenFolioCliente, domain.EtapaEnRevision, domain.EtapaReingresadoInventario, false},
		{"piso si reingresa a inventario", domain.OrigenFolioPiso, domain.EtapaEnRevision, domain.EtapaReingresadoInventario, true},
		{"entregado no lo mueve el articulo", domain.OrigenFolioCliente, domain.EtapaListoEntrega, domain.EtapaEntregado, false},
		{"listo_entrega de piso cierra en inventario", domain.OrigenFolioPiso, domain.EtapaListoEntrega, domain.EtapaReingresadoInventario, true},
		{"espera -> listo_entrega si pasa por AvanzarArticulo", domain.OrigenFolioCliente, domain.EtapaEsperaRespuestaCliente, domain.EtapaListoEntrega, true},
		{"espera -> standby es decision", domain.OrigenFolioCliente, domain.EtapaEsperaRespuestaCliente, domain.EtapaStandby, false},
		{"en_taller -> cambio_autorizado es decision", domain.OrigenFolioCliente, domain.EtapaEnTaller, domain.EtapaCambioAutorizado, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, a := folioConArticuloEn(t, tc.origen, tc.desde)
			err := g.AvanzarArticulo(a.ID(), tc.hasta, actor("juan"), fixed)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok && !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
				t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
			}
		})
	}
}

// TestAvanzarArticulo_SalidasReservadas pins R2: AvanzarArticulo can never
// leave dictamen_recibido or standby, nor reach cambio_autorizado, standby or
// entregado.
func TestAvanzarArticulo_SalidasReservadas(t *testing.T) {
	t.Parallel()

	t.Run("ninguna salida de dictamen_recibido", func(t *testing.T) {
		t.Parallel()
		for _, origen := range []domain.OrigenFolio{domain.OrigenFolioPiso, domain.OrigenFolioCliente} {
			for _, hasta := range todasLasEtapas() {
				g, a := folioConArticuloEn(t, origen, domain.EtapaDictamenRecibido)
				err := g.AvanzarArticulo(a.ID(), hasta, actor("juan"), fixed)
				if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
					t.Fatalf("dictamen_recibido -> %s (%s): want rejection, got %v", hasta, origen, err)
				}
			}
		}
	})

	t.Run("ninguna salida de standby", func(t *testing.T) {
		t.Parallel()
		for _, origen := range []domain.OrigenFolio{domain.OrigenFolioPiso, domain.OrigenFolioCliente} {
			for _, hasta := range todasLasEtapas() {
				g, a := folioConArticuloEn(t, origen, domain.EtapaStandby)
				err := g.AvanzarArticulo(a.ID(), hasta, actor("juan"), fixed)
				if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
					t.Fatalf("standby -> %s (%s): want rejection, got %v", hasta, origen, err)
				}
			}
		}
	})
}
