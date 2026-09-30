package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

var fixed = time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

func actor(usuario string) domain.ActorParams {
	return domain.ActorParams{Usuario: usuario, ClaveIdempotencia: "clave-1", DeviceCreatedAt: fixed.Add(time.Hour)}
}

// actorDecisor is actor plus the role the three decision events require
// (RegistrarDiagnostico, AutorizarCambioFisico, RegistrarDesenlace).
func actorDecisor(usuario string) domain.ActorParams {
	a := actor(usuario)
	rol := domain.RolDecisorOficina
	a.RolDecisor = &rol
	return a
}

func countPending(g *domain.Garantia) int {
	n := 0
	for range g.EventosPendientes() {
		n++
	}
	return n
}

func lastEvent(t *testing.T, g *domain.Garantia) *domain.Evento {
	t.Helper()
	var last *domain.Evento
	for e := range g.EventosPendientes() {
		last = e
	}
	if last == nil {
		t.Fatalf("no pending events")
	}
	return last
}

func articuloByID(t *testing.T, g *domain.Garantia, id uuid.UUID) *domain.Articulo {
	t.Helper()
	for a := range g.Articulos() {
		if a.ID() == id {
			return a
		}
	}
	t.Fatalf("articulo %s not found", id)
	return nil
}

func firstArticulo(t *testing.T, g *domain.Garantia) *domain.Articulo {
	t.Helper()
	for a := range g.Articulos() {
		return a
	}
	t.Fatalf("no articulos")
	return nil
}

func addArticle(t *testing.T, g *domain.Garantia, desc string) *domain.Articulo {
	t.Helper()
	if err := g.AgregarArticulo(domain.AgregarArticuloParams{Description: desc}, actor("juan"), fixed); err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}
	arts := g.ArticulosForRepo()
	if len(arts) == 0 {
		t.Fatalf("no articulos after AgregarArticulo")
	}
	return arts[len(arts)-1]
}

func advance(t *testing.T, g *domain.Garantia, a *domain.Articulo, hasta domain.Etapa) *domain.Articulo {
	t.Helper()
	if err := g.AvanzarArticulo(a.ID(), hasta, actor("juan"), fixed); err != nil {
		t.Fatalf("AvanzarArticulo(%s): %v", hasta, err)
	}
	return articuloByID(t, g, a.ID())
}

func diagnostico(t *testing.T, g *domain.Garantia, a *domain.Articulo, ruta domain.RutaReparacion) *domain.Articulo {
	t.Helper()
	if err := g.RegistrarDiagnostico(a.ID(), ruta, actorDecisor("juan"), fixed); err != nil {
		t.Fatalf("RegistrarDiagnostico(%s): %v", ruta, err)
	}
	return articuloByID(t, g, a.ID())
}

func swap(t *testing.T, g *domain.Garantia, a *domain.Articulo) {
	t.Helper()
	if err := g.AutorizarCambioFisico(a.ID(), actorDecisor("juan"), fixed); err != nil {
		t.Fatalf("AutorizarCambioFisico: %v", err)
	}
}

func openPiso(t *testing.T) *domain.Garantia {
	t.Helper()
	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       domain.Folio("GA-000042"),
		Origen:      domain.OrigenFolioPiso,
		Description: "silla rota",
		AbiertoPor:  "Juan",
		Now:         fixed,
		Actor:       actor("juan"),
	})
	if err != nil {
		t.Fatalf("AbrirGarantia(piso): %v", err)
	}
	return g
}

func openCliente(t *testing.T) *domain.Garantia {
	t.Helper()
	g, err := domain.AbrirGarantia(clienteParams())
	if err != nil {
		t.Fatalf("AbrirGarantia(cliente): %v", err)
	}
	return g
}

func clienteParams() domain.AbrirGarantiaParams {
	clienteID := 7
	ventaID := 99
	estado := domain.EstadoCuentaLiquidada
	lat, lon := 19.427, -99.17
	vigencia := fixed.AddDate(0, 1, 0)
	return domain.AbrirGarantiaParams{
		Folio:          domain.Folio("GA-000042"),
		Origen:         domain.OrigenFolioCliente,
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estado,
		Description:    "silla rota",
		VigenciaHasta:  &vigencia,
		Calle:          "Av. JuÃ¡rez",
		NumeroExterior: "12",
		Colonia:        "Centro",
		Localidad:      "Zapopan",
		Ciudad:         "Guadalajara",
		CodigoPostal:   "45100",
		GPSLat:         &lat,
		GPSLon:         &lon,
		AbiertoPor:     "Juan",
		Now:            fixed,
		Actor:          actor("juan"),
	}
}

// --- AbrirGarantia ---

func TestAbrirGarantia_ClienteHappy(t *testing.T) {
	t.Parallel()
	g := openCliente(t)

	if g.Estado() != domain.EstadoFolioAbierto {
		t.Errorf("Estado() = %q, want abierto", g.Estado())
	}
	if g.Folio() != domain.Folio("GA-000042") {
		t.Errorf("Folio() = %q, want GA-000042", g.Folio())
	}
	if g.Origen() != domain.OrigenFolioCliente {
		t.Errorf("Origen() = %q, want cliente", g.Origen())
	}
	clienteID := 7
	ventaID := 99
	if g.ClienteID() == nil || *g.ClienteID() != clienteID {
		t.Errorf("ClienteID() = %v, want %d", g.ClienteID(), clienteID)
	}
	if g.VentaID() == nil || *g.VentaID() != ventaID {
		t.Errorf("VentaID() = %v, want %d", g.VentaID(), ventaID)
	}
	if g.EstadoCuenta() == nil || *g.EstadoCuenta() != domain.EstadoCuentaLiquidada {
		t.Errorf("EstadoCuenta() = %v, want liquidada", g.EstadoCuenta())
	}
	if g.Description() != "silla rota" || g.AbiertoPor() != "Juan" {
		t.Errorf("Descripcion/AbiertoPor = %q/%q", g.Description(), g.AbiertoPor())
	}
	if g.Calle() != "Av. JuÃ¡rez" || g.NumeroExterior() != "12" ||
		g.Colonia() != "Centro" || g.Localidad() != "Zapopan" ||
		g.Ciudad() != "Guadalajara" || g.CodigoPostal() != "45100" {
		t.Errorf("domicilio fields not set")
	}
	if g.VigenciaHasta() == nil {
		t.Error("VigenciaHasta() = nil, want non-nil")
	}
	if g.GPSLat() == nil || g.GPSLon() == nil {
		t.Errorf("GPS = %v/%v, want set", g.GPSLat(), g.GPSLon())
	}
	if g.CerradoEn() != nil {
		t.Errorf("CerradoEn() = %v, want nil", g.CerradoEn())
	}
	if g.ArticulosCount() != 0 || len(g.ArticulosForRepo()) != 0 {
		t.Errorf("ArticulosCount = %d, want 0", g.ArticulosCount())
	}
	if countPending(g) != 1 || g.EventosPendientesCount() != 1 || len(g.EventosPendientesForRepo()) != 1 {
		t.Fatalf("expected 1 pending event, got %d", countPending(g))
	}
	e := lastEvent(t, g)
	if e.Tipo() != domain.TipoEventoFolioAbierto {
		t.Errorf("opening event tipo = %q, want folio_abierto", e.Tipo())
	}
	if e.Usuario() != "juan" || e.ClaveIdempotencia() != "clave-1" {
		t.Errorf("actor fields = %q/%q", e.Usuario(), e.ClaveIdempotencia())
	}
	if !e.CreatedAt().Equal(fixed) || !e.DeviceCreatedAt().Equal(fixed.Add(time.Hour)) {
		t.Errorf("CreatedAt/DeviceCreatedAt = %v/%v", e.CreatedAt(), e.DeviceCreatedAt())
	}
	if e.ArticuloRef() != nil || e.EtapaDesde() != nil || e.EtapaHasta() != nil {
		t.Errorf("folio event carried stage/article data")
	}
}

func TestAbrirGarantia_ClienteRejections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*domain.AbrirGarantiaParams)
		want   error
	}{
		{"folio_invalido", func(p *domain.AbrirGarantiaParams) { p.Folio = domain.Folio("XYZ") }, domain.ErrFolioInvalido},
		{"origen_invalido", func(p *domain.AbrirGarantiaParams) { p.Origen = domain.OrigenFolio("x") }, domain.ErrOrigenFolioInvalido},
		{"abierto_por_vacio", func(p *domain.AbrirGarantiaParams) { p.AbiertoPor = "  " }, domain.ErrAbiertoPorObligatorio},
		{"descripcion_vacia", func(p *domain.AbrirGarantiaParams) { p.Description = "  " }, domain.ErrDescriptionObligatoria},
		{"usuario_vacio", func(p *domain.AbrirGarantiaParams) { p.Actor.Usuario = "" }, domain.ErrEventoUsuarioObligatorio},
		{"clave_vacia", func(p *domain.AbrirGarantiaParams) { p.Actor.ClaveIdempotencia = "" }, domain.ErrEventoClaveIdempotenciaObligatoria},
		{"device_zero", func(p *domain.AbrirGarantiaParams) { p.Actor.DeviceCreatedAt = time.Time{} }, domain.ErrEventoDeviceCreatedAtObligatorio},
		{"cliente_nil", func(p *domain.AbrirGarantiaParams) { p.ClienteID = nil }, domain.ErrClienteIDObligatorio},
		{"venta_nil", func(p *domain.AbrirGarantiaParams) { p.VentaID = nil }, domain.ErrVentaIDObligatorio},
		{"estado_cuenta_nil", func(p *domain.AbrirGarantiaParams) { p.EstadoCuenta = nil }, domain.ErrEstadoCuentaObligatorio},
		{"domicilio_calle_vacia", func(p *domain.AbrirGarantiaParams) { p.Calle = "  " }, domain.ErrDomicilioObligatorio},
		{"domicilio_numero_vacio", func(p *domain.AbrirGarantiaParams) { p.NumeroExterior = "  " }, domain.ErrDomicilioObligatorio},
		{"domicilio_colonia_vacia", func(p *domain.AbrirGarantiaParams) { p.Colonia = "  " }, domain.ErrDomicilioObligatorio},
		{"domicilio_localidad_vacia", func(p *domain.AbrirGarantiaParams) { p.Localidad = "  " }, domain.ErrDomicilioObligatorio},
		{"domicilio_ciudad_vacia", func(p *domain.AbrirGarantiaParams) { p.Ciudad = "  " }, domain.ErrDomicilioObligatorio},
		{"domicilio_cp_vacio", func(p *domain.AbrirGarantiaParams) { p.CodigoPostal = "  " }, domain.ErrDomicilioObligatorio},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := clienteParams()
			tc.mutate(&p)
			_, err := domain.AbrirGarantia(p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("AbrirGarantia: want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestAbrirGarantia_PisoHappy(t *testing.T) {
	t.Parallel()
	g := openPiso(t)

	if g.Estado() != domain.EstadoFolioAbierto || g.Origen() != domain.OrigenFolioPiso {
		t.Errorf("estado/origen = %q/%q", g.Estado(), g.Origen())
	}
	if g.ClienteID() != nil || g.VentaID() != nil || g.EstadoCuenta() != nil {
		t.Errorf("cliente data must be nil on piso")
	}
	if g.VigenciaHasta() != nil || g.CerradoEn() != nil {
		t.Errorf("vigencia/cerrado = %v/%v, want nil/nil", g.VigenciaHasta(), g.CerradoEn())
	}
	if g.Calle() != "" || g.GPSLat() != nil || g.GPSLon() != nil {
		t.Errorf("domicilio must be nil on piso")
	}
	if g.ArticulosCount() != 0 {
		t.Errorf("ArticulosCount = %d, want 0", g.ArticulosCount())
	}
	if countPending(g) != 1 || lastEvent(t, g).Tipo() != domain.TipoEventoFolioAbierto {
		t.Fatalf("expected folio_abierto pending event")
	}
}

func TestAbrirGarantia_PisoRejections(t *testing.T) {
	t.Parallel()
	lat := 1.0
	cases := []struct {
		name   string
		mutate func(*domain.AbrirGarantiaParams)
		want   error
	}{
		{"clienteID", func(p *domain.AbrirGarantiaParams) { i := 1; p.ClienteID = &i }, domain.ErrClienteIDNoPermitido},
		{"ventaID", func(p *domain.AbrirGarantiaParams) { i := 1; p.VentaID = &i }, domain.ErrVentaIDNoPermitido},
		{"estadoCuenta", func(p *domain.AbrirGarantiaParams) { e := domain.EstadoCuentaLiquidada; p.EstadoCuenta = &e }, domain.ErrEstadoCuentaNoPermitido},
		{"calle", func(p *domain.AbrirGarantiaParams) { p.Calle = "Av" }, domain.ErrDomicilioNoPermitido},
		{"codigoPostal", func(p *domain.AbrirGarantiaParams) { p.CodigoPostal = "45100" }, domain.ErrDomicilioNoPermitido},
		{"gpsLat", func(p *domain.AbrirGarantiaParams) { p.GPSLat = &lat }, domain.ErrDomicilioNoPermitido},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := domain.AbrirGarantiaParams{
				Folio:       domain.Folio("GA-000043"),
				Origen:      domain.OrigenFolioPiso,
				Description: "mesa",
				AbiertoPor:  "Ana",
				Now:         fixed,
				Actor:       actor("ana"),
			}
			tc.mutate(&p)
			_, err := domain.AbrirGarantia(p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("AbrirGarantia: want %v, got %v", tc.want, err)
			}
		})
	}
}

// --- AgregarArticulo ---

func TestAgregarArticulo_UbicacionSegunOrigen(t *testing.T) {
	t.Parallel()
	t.Run("piso", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if a.Etapa() != domain.EtapaRegistrado {
			t.Errorf("Etapa() = %q, want registrado", a.Etapa())
		}
		if a.Ubicacion() != domain.UbicacionAlmacenRevision {
			t.Errorf("Ubicacion() = %q, want almacen_revision", a.Ubicacion())
		}
		if a.Rol() != domain.RolArticuloOriginal {
			t.Errorf("Rol() = %q, want original", a.Rol())
		}
		if g.ArticulosCount() != 1 {
			t.Errorf("ArticulosCount = %d, want 1", g.ArticulosCount())
		}
		e := lastEvent(t, g)
		if e.Tipo() != domain.TipoEventoArticuloAgregado || e.ArticuloRef() == nil || *e.ArticuloRef() != a.ID() {
			t.Fatalf("event must be articulo_agregado with articuloRef")
		}
	})
	t.Run("cliente", func(t *testing.T) {
		t.Parallel()
		g := openCliente(t)
		a := addArticle(t, g, "silla")
		if a.Ubicacion() != domain.UbicacionDomicilioCliente {
			t.Errorf("Ubicacion() = %q, want domicilio_cliente", a.Ubicacion())
		}
	})
}

func TestAgregarArticulo_DescripcionVacia(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	err := g.AgregarArticulo(domain.AgregarArticuloParams{Description: "  "}, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrArticuloDescriptionObligatoria) {
		t.Fatalf("want ErrArticuloDescriptionObligatoria, got %v", err)
	}
	if g.ArticulosCount() != 0 || countPending(g) != 1 {
		t.Fatalf("invariante Â§4.4 rota: article added or event queued on failure")
	}
}

func TestAgregarArticulo_FolioNoAdmite(t *testing.T) {
	t.Parallel()
	for _, estado := range []domain.EstadoFolio{
		domain.EstadoFolioEntregado, domain.EstadoFolioCerrado, domain.EstadoFolioCancelado,
	} {
		t.Run(estado.String(), func(t *testing.T) {
			t.Parallel()
			g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
				ID: uuid.New(), Folio: "GA-000001", Origen: domain.OrigenFolioPiso,
				Estado: estado, Description: "x", AbiertoPor: "juan",
				CreatedAt: fixed, UpdatedAt: fixed,
			})
			err := g.AgregarArticulo(domain.AgregarArticuloParams{Description: "silla"}, actor("juan"), fixed)
			if !errors.Is(err, domain.ErrFolioNoAdmiteArticulos) {
				t.Fatalf("want ErrFolioNoAdmiteArticulos, got %v", err)
			}
			if g.ArticulosCount() != 0 || countPending(g) != 0 {
				t.Fatalf("no mutation and no event expected on failure")
			}
		})
	}
}

// --- IniciarProceso ---

func TestIniciarProceso(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("IniciarProceso: %v", err)
		}
		if g.Estado() != domain.EstadoFolioEnProceso {
			t.Errorf("Estado() = %q, want en_proceso", g.Estado())
		}
		e := lastEvent(t, g)
		if e.Tipo() != domain.TipoEventoEtapaAvanzada {
			t.Errorf("evento = %q, want etapa_avanzada", e.Tipo())
		}
		if e.EtapaDesde() != nil || e.EtapaHasta() != nil {
			t.Errorf("folio events must not carry stage data")
		}
	})
	t.Run("sin_articulos", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		err := g.IniciarProceso(actor("juan"), fixed)
		if !errors.Is(err, domain.ErrGarantiaSinArticulos) {
			t.Fatalf("want ErrGarantiaSinArticulos, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioAbierto || countPending(g) != 1 {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("ya_en_proceso", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		pendientes := countPending(g)
		err := g.IniciarProceso(actor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
			t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioEnProceso || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

// --- AvanzarArticulo ---

func TestAvanzarArticulo_TrunkCliente(t *testing.T) {
	t.Parallel()
	g := openCliente(t)
	a := addArticle(t, g, "silla")

	a = advance(t, g, a, domain.EtapaPendienteRecoleccion)
	if a.Etapa() != domain.EtapaPendienteRecoleccion {
		t.Fatalf("etapa = %q", a.Etapa())
	}
	if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoEtapaAvanzada ||
		e.EtapaDesde() == nil || *e.EtapaDesde() != domain.EtapaRegistrado ||
		e.EtapaHasta() == nil || *e.EtapaHasta() != domain.EtapaPendienteRecoleccion {
		t.Fatalf("event must carry desde/hasta")
	}

	a = advance(t, g, a, domain.EtapaRecolectado)
	a = advance(t, g, a, domain.EtapaEnRevision)
	if a.Etapa() != domain.EtapaEnRevision {
		t.Fatalf("etapa = %q, want en_revision", a.Etapa())
	}
}

func TestAvanzarArticulo_TrunkPiso(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	a = advance(t, g, a, domain.EtapaEnRevision)
	if a.Etapa() != domain.EtapaEnRevision {
		t.Fatalf("etapa = %q, want en_revision", a.Etapa())
	}
}

func TestAvanzarArticulo_Invalida(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	pendientes := countPending(g)

	err := g.AvanzarArticulo(a.ID(), domain.EtapaEntregado, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
		t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
	}
	a = articuloByID(t, g, a.ID())
	if a.Etapa() != domain.EtapaRegistrado {
		t.Errorf("state mutated on failure: %q", a.Etapa())
	}
	if countPending(g) != pendientes {
		t.Errorf("event queued on failure â€” Â§4.4 broken")
	}
}

func TestAvanzarArticulo_RequiereRutaDesdeEnRevision(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	a = advance(t, g, a, domain.EtapaEnRevision)
	pendientes := countPending(g)

	err := g.AvanzarArticulo(a.ID(), domain.EtapaOrdenGenerada, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrArticuloRutaRequerida) {
		t.Fatalf("want ErrArticuloRutaRequerida, got %v", err)
	}
	if a.Etapa() != domain.EtapaEnRevision || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected")
	}

	a = advance(t, g, a, domain.EtapaReingresadoInventario) // terminal sin ruta
	if !a.Etapa().EsTerminal() {
		t.Fatalf("etapa = %q, want terminal", a.Etapa())
	}
}

func TestAvanzarArticulo_EtapasExclusivasSwap(t *testing.T) {
	t.Parallel()
	t.Run("desde_en_taller", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller) // en_taller
		pendientes := countPending(g)
		for _, hasta := range []domain.Etapa{domain.EtapaCambioAutorizado, domain.EtapaStandby} {
			err := g.AvanzarArticulo(a.ID(), hasta, actor("juan"), fixed)
			if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
				t.Fatalf("AvanzarArticulo(%s): want ErrTransicionEtapaNoPermitida, got %v", hasta, err)
			}
		}
		if a.Etapa() != domain.EtapaEnTaller || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("desde_espera_respuesta", func(t *testing.T) {
		t.Parallel()
		g, a := supplierToVerdict(t)
		a = dictamen(t, g, a, domain.DictamenSinFalla) // espera_respuesta_cliente
		pendientes := countPending(g)
		err := g.AvanzarArticulo(a.ID(), domain.EtapaStandby, actor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
			t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
		}
		if a.Etapa() != domain.EtapaEsperaRespuestaCliente || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

func TestArticulo_MutacionesActualizanUpdatedAt(t *testing.T) {
	t.Parallel()
	cambio := fixed.Add(time.Hour)
	mut := domain.ActorParams{Usuario: "juan", ClaveIdempotencia: "clave-1", DeviceCreatedAt: cambio.Add(time.Hour)}
	mutDecisor := mut
	rol := domain.RolDecisorOficina
	mutDecisor.RolDecisor = &rol

	// ruta taller: avanzar, registrarDiagnostico, autorizarCambioFisico, registrarDesenlace.
	// El artÃ­culo nace en fixed; cada mutador corre en cambio, asÃ­ que UpdatedAt
	// debe moverse en cada uno (fallarÃ­a si un mutador perdiera MarkUpdatedAt).
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, mut, cambio); err != nil {
		t.Fatalf("AvanzarArticulo: %v", err)
	}
	if !a.UpdatedAt().Equal(cambio) {
		t.Fatalf("UpdatedAt after avanzar = %v, want %v", a.UpdatedAt(), cambio)
	}
	if err := g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, mutDecisor, cambio); err != nil {
		t.Fatalf("RegistrarDiagnostico: %v", err)
	}
	if !a.UpdatedAt().Equal(cambio) {
		t.Fatalf("UpdatedAt after diagnostico = %v, want %v", a.UpdatedAt(), cambio)
	}
	if err := g.AutorizarCambioFisico(a.ID(), mutDecisor, cambio); err != nil {
		t.Fatalf("AutorizarCambioFisico: %v", err)
	}
	if !a.UpdatedAt().Equal(cambio) {
		t.Fatalf("UpdatedAt after swap = %v, want %v", a.UpdatedAt(), cambio)
	}
	if err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, mutDecisor, cambio); err != nil {
		t.Fatalf("RegistrarDesenlace: %v", err)
	}
	if !a.UpdatedAt().Equal(cambio) {
		t.Fatalf("UpdatedAt after desenlace = %v, want %v", a.UpdatedAt(), cambio)
	}

	// ruta proveedor: registrarDictamen
	g2, b := supplierToVerdict(t)
	if err := g2.RegistrarDictamen(b.ID(), domain.DictamenAceptada, mut, cambio); err != nil {
		t.Fatalf("RegistrarDictamen: %v", err)
	}
	b = articuloByID(t, g2, b.ID())
	if !b.UpdatedAt().Equal(cambio) {
		t.Fatalf("UpdatedAt after dictamen = %v, want %v", b.UpdatedAt(), cambio)
	}
}

func TestAvanzarArticulo_NoEncontrado(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	addArticle(t, g, "silla")
	pendientes := countPending(g)
	err := g.AvanzarArticulo(uuid.New(), domain.EtapaEnRevision, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrArticuloNoEncontrado) {
		t.Fatalf("want ErrArticuloNoEncontrado, got %v", err)
	}
	if countPending(g) != pendientes {
		t.Fatalf("event queued on failure")
	}
}

// --- RegistrarDiagnostico ---

func TestRegistrarDiagnostico(t *testing.T) {
	t.Parallel()
	t.Run("taller", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		if a.Etapa() != domain.EtapaEnTaller {
			t.Fatalf("etapa = %q, want en_taller", a.Etapa())
		}
		if a.Ruta() == nil || !a.Ruta().EsTaller() {
			t.Fatalf("Ruta() = %v, want taller", a.Ruta())
		}
		e := lastEvent(t, g)
		if e.Tipo() != domain.TipoEventoDiagnosticoRegistrado ||
			e.EtapaDesde() == nil || *e.EtapaDesde() != domain.EtapaEnRevision ||
			e.EtapaHasta() == nil || *e.EtapaHasta() != domain.EtapaEnTaller {
			t.Fatalf("bad diagnostico event")
		}
	})
	t.Run("proveedor", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "pantalla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionProveedor)
		if a.Etapa() != domain.EtapaOrdenGenerada {
			t.Fatalf("etapa = %q, want orden_generada", a.Etapa())
		}
		if a.Dictamen() != nil {
			t.Fatalf("Dictamen() = %v, want nil en diagnostico", a.Dictamen())
		}
	})
	t.Run("ruta_invalida", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		pendientes := countPending(g)
		err := g.RegistrarDiagnostico(a.ID(), domain.RutaReparacion("x"), actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrRutaReparacionInvalida) {
			t.Fatalf("want ErrRutaReparacionInvalida, got %v", err)
		}
		if a.Etapa() != domain.EtapaEnRevision || a.Ruta() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("no_en_revision", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla") // registrado
		pendientes := countPending(g)
		err := g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
			t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
		}
		if a.Ruta() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

// --- RegistrarDictamen ---

func supplierToVerdict(t *testing.T) (*domain.Garantia, *domain.Articulo) {
	t.Helper()
	g := openPiso(t)
	a := addArticle(t, g, "pantalla")
	a = advance(t, g, a, domain.EtapaEnRevision)
	a = diagnostico(t, g, a, domain.RutaReparacionProveedor)
	a = advance(t, g, a, domain.EtapaEnviadoProveedor)
	a = advance(t, g, a, domain.EtapaDictamenRecibido)
	return g, a
}

func dictamen(t *testing.T, g *domain.Garantia, a *domain.Articulo, d domain.Dictamen) *domain.Articulo {
	t.Helper()
	if err := g.RegistrarDictamen(a.ID(), d, actor("juan"), fixed); err != nil {
		t.Fatalf("RegistrarDictamen(%s): %v", d, err)
	}
	return articuloByID(t, g, a.ID())
}

func TestRegistrarDictamen_Veredictos(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dictamen domain.Dictamen
		want     domain.Etapa
	}{
		{domain.DictamenAceptada, domain.EtapaReparadoProveedor},
		{domain.DictamenRechazada, domain.EtapaListoEntrega},
		{domain.DictamenSinFalla, domain.EtapaEsperaRespuestaCliente},
	}
	for _, tc := range cases {
		t.Run(tc.dictamen.String(), func(t *testing.T) {
			t.Parallel()
			g, a := supplierToVerdict(t)
			a = dictamen(t, g, a, tc.dictamen)
			if a.Etapa() != tc.want {
				t.Fatalf("etapa = %q, want %q", a.Etapa(), tc.want)
			}
			if a.Dictamen() == nil || *a.Dictamen() != tc.dictamen {
				t.Fatalf("Dictamen() = %v, want %v", a.Dictamen(), tc.dictamen)
			}
			if a.Desenlace() != nil {
				t.Fatalf("Desenlace() = %v, want nil (devuelto is not set here)", a.Desenlace())
			}
			e := lastEvent(t, g)
			if e.Tipo() != domain.TipoEventoDictamenRegistrado {
				t.Fatalf("evento = %q, want dictamen_registrado", e.Tipo())
			}
		})
	}
}

func TestRegistrarDictamen_SoloProveedor(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	a = advance(t, g, a, domain.EtapaEnRevision)
	a = diagnostico(t, g, a, domain.RutaReparacionTaller) // en_taller
	pendientes := countPending(g)

	err := g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, actor("juan"), fixed)
	if !errors.Is(err, domain.ErrArticuloDictamenSoloProveedor) {
		t.Fatalf("want ErrArticuloDictamenSoloProveedor, got %v", err)
	}
	if a.Etapa() != domain.EtapaEnTaller || a.Dictamen() != nil || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected")
	}
}

func TestRegistrarDictamen_Invalidos(t *testing.T) {
	t.Parallel()
	t.Run("dictamen_invalido", func(t *testing.T) {
		t.Parallel()
		g, a := supplierToVerdict(t)
		pendientes := countPending(g)
		err := g.RegistrarDictamen(a.ID(), domain.Dictamen("x"), actor("juan"), fixed)
		if !errors.Is(err, domain.ErrDictamenInvalido) {
			t.Fatalf("want ErrDictamenInvalido, got %v", err)
		}
		if a.Dictamen() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("etapa_incorrecta", func(t *testing.T) {
		t.Parallel()
		// proveedor, pero sin haber llegado a dictamen_recibido todavÃ­a
		g := openPiso(t)
		a := addArticle(t, g, "pantalla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionProveedor) // orden_generada
		a = advance(t, g, a, domain.EtapaEnviadoProveedor)
		pendientes := countPending(g)
		err := g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, actor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
			t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
		}
		if a.Dictamen() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

// --- AutorizarCambioFisico ---

func TestAutorizarCambioFisico(t *testing.T) {
	t.Parallel()
	t.Run("desde_en_taller", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		swap(t, g, a)

		original := articuloByID(t, g, a.ID())
		if original.Etapa() != domain.EtapaStandby {
			t.Fatalf("original etapa = %q, want standby", original.Etapa())
		}
		if g.ArticulosCount() != 2 {
			t.Fatalf("ArticulosCount = %d, want 2", g.ArticulosCount())
		}
		var reemplazo *domain.Articulo
		for art := range g.Articulos() {
			if art.ID() != a.ID() {
				reemplazo = art
			}
		}
		if reemplazo == nil {
			t.Fatal("reemplazo not found")
		}
		if reemplazo.Rol() != domain.RolArticuloReemplazo ||
			reemplazo.Etapa() != domain.EtapaListoEntrega ||
			reemplazo.Ubicacion() != domain.UbicacionAlmacenRevision ||
			reemplazo.ReemplazaA() == nil || *reemplazo.ReemplazaA() != original.ID() {
			t.Fatalf("reemplazo mal configurado")
		}
		e := lastEvent(t, g)
		if e.Tipo() != domain.TipoEventoCambioAutorizado ||
			e.EtapaDesde() == nil || *e.EtapaDesde() != domain.EtapaEnTaller ||
			e.EtapaHasta() == nil || *e.EtapaHasta() != domain.EtapaStandby {
			t.Fatalf("bad cambio_autorizado event")
		}
	})
	t.Run("desde_espera_respuesta", func(t *testing.T) {
		t.Parallel()
		g, a := supplierToVerdict(t)
		a = dictamen(t, g, a, domain.DictamenSinFalla) // espera_respuesta_cliente
		swap(t, g, a)
		if articuloByID(t, g, a.ID()).Etapa() != domain.EtapaStandby {
			t.Fatalf("etapa != standby after swap")
		}
	})
	t.Run("reemplazo_falla_sin_mutacion", func(t *testing.T) {
		t.Parallel()
		gID := uuid.New()
		artID := uuid.New()
		g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
			ID: gID, Folio: "GA-000001", Origen: domain.OrigenFolioPiso,
			Estado: domain.EstadoFolioAbierto, Description: "x", AbiertoPor: "juan",
			CreatedAt: fixed, UpdatedAt: fixed,
			Articulos: []*domain.Articulo{
				domain.HydrateArticulo(domain.HydrateArticuloParams{
					ID: artID, GarantiaID: gID, Rol: domain.RolArticuloOriginal,
					Description: "", // fila que HydrateArticulo acepta y newArticulo rechaza
					Etapa:       domain.EtapaEnTaller,
					Ubicacion:   domain.UbicacionTaller,
					CreatedAt:   fixed, UpdatedAt: fixed,
				}),
			},
		})
		err := g.AutorizarCambioFisico(artID, actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrArticuloDescriptionObligatoria) {
			t.Fatalf("want ErrArticuloDescriptionObligatoria, got %v", err)
		}
		if articuloByID(t, g, artID).Etapa() != domain.EtapaEnTaller ||
			g.ArticulosCount() != 1 || countPending(g) != 0 {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("no_autorizable", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla") // registrado
		pendientes := countPending(g)
		err := g.AutorizarCambioFisico(a.ID(), actorDecisor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
			t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
		}
		if a.Etapa() != domain.EtapaRegistrado || g.ArticulosCount() != 1 || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

func TestAutorizarCambioFisico_Encadenado(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	a = advance(t, g, a, domain.EtapaEnRevision)
	a = diagnostico(t, g, a, domain.RutaReparacionTaller)
	swap(t, g, a) // original -> standby, reemplazo(listo_entrega)

	// el reemplazo sale malo: se cambia de nuevo desde listo_entrega
	var segundo *domain.Articulo
	for art := range g.Articulos() {
		if art.ID() != a.ID() {
			segundo = art
		}
	}
	swap(t, g, segundo)

	if articuloByID(t, g, segundo.ID()).Etapa() != domain.EtapaStandby {
		t.Fatalf("reemplazo debe quedar en standby")
	}
	if g.ArticulosCount() != 3 {
		t.Fatalf("ArticulosCount = %d, want 3 (tercera fila)", g.ArticulosCount())
	}
	var tercero *domain.Articulo
	for art := range g.Articulos() {
		if art.ID() != a.ID() && art.ID() != segundo.ID() {
			tercero = art
		}
	}
	if tercero == nil {
		t.Fatal("tercera fila no creada")
	}
	if tercero.Rol() != domain.RolArticuloReemplazo ||
		tercero.Etapa() != domain.EtapaListoEntrega ||
		tercero.ReemplazaA() == nil || *tercero.ReemplazaA() != segundo.ID() {
		t.Fatalf("tercera fila mal configurada")
	}
}

// --- RegistrarDesenlace ---

func TestRegistrarDesenlace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desenlace domain.Desenlace
		want      domain.Etapa
	}{
		{domain.DesenlaceSegundaMano, domain.EtapaSegundaMano},
		{domain.DesenlaceDesarmado, domain.EtapaDesarmado},
		{domain.DesenlaceMerma, domain.EtapaMerma},
	}
	for _, tc := range cases {
		t.Run(tc.desenlace.String(), func(t *testing.T) {
			t.Parallel()
			g := openPiso(t)
			a := addArticle(t, g, "silla")
			a = advance(t, g, a, domain.EtapaEnRevision)
			a = diagnostico(t, g, a, domain.RutaReparacionTaller)
			swap(t, g, a) // standby

			if err := g.RegistrarDesenlace(a.ID(), tc.desenlace, actorDecisor("juan"), fixed); err != nil {
				t.Fatalf("RegistrarDesenlace: %v", err)
			}
			a = articuloByID(t, g, a.ID())
			if a.Etapa() != tc.want || !a.Etapa().EsTerminal() {
				t.Fatalf("etapa = %q, want %q terminal", a.Etapa(), tc.want)
			}
			if a.Desenlace() == nil || *a.Desenlace() != tc.desenlace {
				t.Fatalf("Desenlace() = %v, want %v", a.Desenlace(), tc.desenlace)
			}
			e := lastEvent(t, g)
			if e.Tipo() != domain.TipoEventoDesenlaceRegistrado ||
				e.EtapaHasta() == nil || *e.EtapaHasta() != tc.want {
				t.Fatalf("bad desenlace event")
			}
		})
	}
}

func TestRegistrarDesenlace_NoParalelo(t *testing.T) {
	t.Parallel()
	for _, d := range []domain.Desenlace{
		domain.DesenlaceReparado, domain.DesenlaceReemplazado, domain.DesenlaceDevuelto,
	} {
		t.Run(d.String(), func(t *testing.T) {
			t.Parallel()
			g := openPiso(t)
			a := addArticle(t, g, "silla")
			a = advance(t, g, a, domain.EtapaEnRevision)
			a = diagnostico(t, g, a, domain.RutaReparacionTaller)
			swap(t, g, a) // standby
			pendientes := countPending(g)

			err := g.RegistrarDesenlace(a.ID(), d, actorDecisor("juan"), fixed)
			if !errors.Is(err, domain.ErrArticuloDesenlaceSoloParalelo) {
				t.Fatalf("want ErrArticuloDesenlaceSoloParalelo, got %v", err)
			}
			if a.Etapa() != domain.EtapaStandby || a.Desenlace() != nil || countPending(g) != pendientes {
				t.Fatalf("no mutation/event expected")
			}
		})
	}
}

func TestRegistrarDesenlace_NoDesdeStandby(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla") // registrado: standby no alcanzado
	pendientes := countPending(g)
	err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceMerma, actorDecisor("juan"), fixed)
	if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
		t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
	}
	if a.Desenlace() != nil || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected")
	}
}

func TestRegistrarDesenlace_Invalido(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	a = advance(t, g, a, domain.EtapaEnRevision)
	a = diagnostico(t, g, a, domain.RutaReparacionTaller)
	swap(t, g, a) // standby
	pendientes := countPending(g)

	err := g.RegistrarDesenlace(a.ID(), domain.Desenlace("x"), actorDecisor("juan"), fixed)
	if !errors.Is(err, domain.ErrDesenlaceInvalido) {
		t.Fatalf("want ErrDesenlaceInvalido, got %v", err)
	}
	if a.Etapa() != domain.EtapaStandby || a.Desenlace() != nil || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected")
	}
}

func TestMutaciones_RechazanActorInvalido(t *testing.T) {
	t.Parallel()
	malo := domain.ActorParams{Usuario: "", ClaveIdempotencia: "k", DeviceCreatedAt: fixed}
	t.Run("agregar_articulo", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		err := g.AgregarArticulo(domain.AgregarArticuloParams{Description: "silla"}, malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.ArticulosCount() != 0 {
			t.Fatalf("artículo creado pese al actor inválido")
		}
	})
	t.Run("avanzar", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		pendientes := countPending(g)
		err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Etapa() != domain.EtapaRegistrado || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("diagnostico", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		pendientes := countPending(g)
		err := g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Ruta() != nil || a.Etapa() != domain.EtapaEnRevision || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("dictamen", func(t *testing.T) {
		t.Parallel()
		g, a := supplierToVerdict(t)
		pendientes := countPending(g)
		err := g.RegistrarDictamen(a.ID(), domain.DictamenAceptada, malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Dictamen() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("autorizar_cambio", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		pendientes := countPending(g)
		err := g.AutorizarCambioFisico(a.ID(), malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Etapa() != domain.EtapaEnTaller || g.ArticulosCount() != 1 || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("desenlace", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		swap(t, g, a) // standby
		pendientes := countPending(g)
		err := g.RegistrarDesenlace(a.ID(), domain.DesenlaceMerma, malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if a.Etapa() != domain.EtapaStandby || a.Desenlace() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("iniciar_proceso", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		addArticle(t, g, "silla")
		pendientes := countPending(g)
		err := g.IniciarProceso(malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioAbierto || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("marcar_listo_entrega", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		a = advance(t, g, a, domain.EtapaReparadoTaller)
		advance(t, g, a, domain.EtapaListoEntrega)
		pendientes := countPending(g)
		err := g.MarcarListoEntrega(malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioEnProceso || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("entregar", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		a = advance(t, g, a, domain.EtapaReparadoTaller)
		advance(t, g, a, domain.EtapaListoEntrega)
		if err := g.MarcarListoEntrega(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		pendientes := countPending(g)
		err := g.Entregar(malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioListoEntrega || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("cerrar", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		a = advance(t, g, a, domain.EtapaReparadoTaller)
		advance(t, g, a, domain.EtapaListoEntrega)
		if err := g.MarcarListoEntrega(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := g.Entregar(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		pendientes := countPending(g)
		err := g.Cerrar(malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioEntregado || g.CerradoEn() != nil || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("cancelar", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		addArticle(t, g, "silla")
		pendientes := countPending(g)
		err := g.Cancelar("ya no lo necesita", malo, fixed)
		if !errors.Is(err, domain.ErrEventoUsuarioObligatorio) {
			t.Fatalf("want ErrEventoUsuarioObligatorio, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioAbierto || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

func TestEventosPendientes_CancelaTemprano(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	n := 0
	for range g.EventosPendientes() {
		n++
		break // yield returns false: la iteraciÃ³n se corta limpia
	}
	if n != 1 {
		t.Fatalf("ranges %d, want 1 tras el break", n)
	}
	if countPending(g) != 1 {
		t.Fatalf("el agregado no debe mutar por un break")
	}
}

func TestMutaciones_ArticuloInexistente(t *testing.T) {
	t.Parallel()
	bogus := uuid.New()
	cases := []struct {
		name string
		call func(g *domain.Garantia) error
	}{
		{
			name: "avanzar",
			call: func(g *domain.Garantia) error {
				return g.AvanzarArticulo(bogus, domain.EtapaEnRevision, actor("juan"), fixed)
			},
		},
		{
			name: "diagnostico",
			call: func(g *domain.Garantia) error {
				return g.RegistrarDiagnostico(bogus, domain.RutaReparacionTaller, actorDecisor("juan"), fixed)
			},
		},
		{
			name: "dictamen",
			call: func(g *domain.Garantia) error {
				return g.RegistrarDictamen(bogus, domain.DictamenAceptada, actor("juan"), fixed)
			},
		},
		{
			name: "autorizar_cambio",
			call: func(g *domain.Garantia) error {
				return g.AutorizarCambioFisico(bogus, actorDecisor("juan"), fixed)
			},
		},
		{
			name: "desenlace",
			call: func(g *domain.Garantia) error {
				return g.RegistrarDesenlace(bogus, domain.DesenlaceMerma, actorDecisor("juan"), fixed)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := openPiso(t)
			pendientes := countPending(g)
			err := tc.call(g)
			if !errors.Is(err, domain.ErrArticuloNoEncontrado) {
				t.Fatalf("want ErrArticuloNoEncontrado, got %v", err)
			}
			if g.ArticulosCount() != 0 || countPending(g) != pendientes {
				t.Fatalf("no mutation/event expected")
			}
		})
	}
}

// --- MarcarListoEntrega / Entregar / Cerrar / Cancelar ---

func TestMarcarListoEntrega_Guardia(t *testing.T) {
	t.Parallel()
	t.Run("pasa_con_standby_y_reemplazo", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		swap(t, g, a) // original standby + reemplazo listo_entrega

		if err := g.MarcarListoEntrega(actor("juan"), fixed); err != nil {
			t.Fatalf("MarcarListoEntrega: %v", err)
		}
		if g.Estado() != domain.EstadoFolioListoEntrega {
			t.Fatalf("Estado() = %q, want listo_entrega", g.Estado())
		}
	})
	t.Run("rechaza_articulo_en_camino", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		a = diagnostico(t, g, a, domain.RutaReparacionTaller)
		swap(t, g, a)

		// tercer artÃ­culo atascado en en_taller
		tercero := addArticle(t, g, "mesa")
		tercero = advance(t, g, tercero, domain.EtapaEnRevision)
		diagnostico(t, g, tercero, domain.RutaReparacionTaller)

		pendientes := countPending(g)
		err := g.MarcarListoEntrega(actor("juan"), fixed)
		if !errors.Is(err, domain.ErrArticulosNoListosParaEntrega) {
			t.Fatalf("want ErrArticulosNoListosParaEntrega, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioEnProceso {
			t.Fatalf("estado mutado: %q", g.Estado())
		}
		if countPending(g) != pendientes {
			t.Fatalf("evento encolado en fallo â€” Â§4.4 rota")
		}
	})
	t.Run("pasa_con_terminal", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		a = advance(t, g, a, domain.EtapaEnRevision)
		advance(t, g, a, domain.EtapaReingresadoInventario) // terminal
		if err := g.MarcarListoEntrega(actor("juan"), fixed); err != nil {
			t.Fatalf("MarcarListoEntrega: %v", err)
		}
	})
	t.Run("estado_incorrecto", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t) // abierto
		pendientes := countPending(g)
		err := g.MarcarListoEntrega(actor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
			t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioAbierto || countPending(g) != pendientes {
			t.Fatalf("no mutation/event expected")
		}
	})
}

func TestFolio_EntregaYCierra(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := addArticle(t, g, "silla")
	if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
		t.Fatalf("setup: %v", err)
	}
	a = advance(t, g, a, domain.EtapaEnRevision)
	a = diagnostico(t, g, a, domain.RutaReparacionTaller)
	a = advance(t, g, a, domain.EtapaReparadoTaller)
	advance(t, g, a, domain.EtapaListoEntrega)
	if err := g.MarcarListoEntrega(actor("juan"), fixed); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// entregar
	err := g.Entregar(actor("juan"), fixed)
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if g.Estado() != domain.EstadoFolioEntregado {
		t.Fatalf("Estado() = %q, want entregado", g.Estado())
	}
	if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioEntregado {
		t.Fatalf("evento = %q, want folio_entregado", e.Tipo())
	}

	// entregar de nuevo no procede
	pendientes := countPending(g)
	if err := g.Entregar(actor("juan"), fixed); !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("double Entregar: want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if g.Estado() != domain.EstadoFolioEntregado || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected")
	}

	// cerrar
	if err := g.Cerrar(actor("juan"), fixed); err != nil {
		t.Fatalf("Cerrar: %v", err)
	}
	if g.Estado() != domain.EstadoFolioCerrado {
		t.Fatalf("Estado() = %q, want cerrado", g.Estado())
	}
	if g.CerradoEn() == nil || !g.CerradoEn().Equal(fixed) {
		t.Fatalf("CerradoEn() = %v, want %v", g.CerradoEn(), fixed)
	}
	if e := lastEvent(t, g); e.Tipo() != domain.TipoEventoFolioCerrado {
		t.Fatalf("evento = %q, want folio_cerrado", e.Tipo())
	}
}

func TestCerrar_DesdeAbierto(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	pendientes := countPending(g)
	err := g.Cerrar(actor("juan"), fixed)
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if g.Estado() != domain.EstadoFolioAbierto || g.CerradoEn() != nil || countPending(g) != pendientes {
		t.Fatalf("no mutation/event expected")
	}
}

func TestCancelar(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		addArticle(t, g, "silla")
		if err := g.IniciarProceso(actor("juan"), fixed); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := g.Cancelar("cliente desistiÃ³", actor("juan"), fixed); err != nil {
			t.Fatalf("Cancelar: %v", err)
		}
		if g.Estado() != domain.EstadoFolioCancelado {
			t.Fatalf("Estado() = %q, want cancelado", g.Estado())
		}
		e := lastEvent(t, g)
		if e.Tipo() != domain.TipoEventoFolioCancelado || e.Description() != "cliente desistiÃ³" {
			t.Fatalf("bad folio_cancelado event")
		}
	})
	t.Run("motivo_vacio", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		err := g.Cancelar("  ", actor("juan"), fixed)
		if !errors.Is(err, domain.ErrMotivoCancelacionObligatorio) {
			t.Fatalf("want ErrMotivoCancelacionObligatorio, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioAbierto || countPending(g) != 1 {
			t.Fatalf("no mutation/event expected")
		}
	})
	t.Run("desde_cerrado", func(t *testing.T) {
		t.Parallel()
		g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
			ID: uuid.New(), Folio: "GA-000001", Origen: domain.OrigenFolioPiso,
			Estado: domain.EstadoFolioCerrado, Description: "x", AbiertoPor: "juan",
			CreatedAt: fixed, UpdatedAt: fixed,
		})
		err := g.Cancelar("motivo", actor("juan"), fixed)
		if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
			t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
		}
		if g.Estado() != domain.EstadoFolioCerrado || countPending(g) != 0 {
			t.Fatalf("no mutation/event expected")
		}
	})
}

// --- HydrateGarantia ---

func TestHydrateGarantia_Basura(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	clienteID, ventaID := 7, 99
	estadoCuenta := domain.EstadoCuentaSaldoPendiente
	vigencia := fixed.AddDate(0, 1, 0)
	cerrado := fixed.Add(2 * time.Hour)
	lat, lon := 1.0, -2.0
	artID := uuid.New()

	g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:             id,
		Folio:          domain.Folio("XYZ"),
		Origen:         domain.OrigenFolio("basura"),
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estadoCuenta,
		Estado:         domain.EstadoFolio("basura"),
		Description:    "x",
		VigenciaHasta:  &vigencia,
		Calle:          "c",
		NumeroExterior: "n",
		Colonia:        "col",
		Localidad:      "loc",
		Ciudad:         "cd",
		CodigoPostal:   "cp",
		GPSLat:         &lat,
		GPSLon:         &lon,
		AbiertoPor:     "juan",
		CerradoEn:      &cerrado,
		CreatedAt:      fixed,
		UpdatedAt:      fixed.Add(time.Hour),
		Articulos: []*domain.Articulo{domain.HydrateArticulo(domain.HydrateArticuloParams{
			ID: artID, GarantiaID: id, Rol: domain.RolArticulo("basura"),
			Etapa: domain.Etapa("basura"), Ubicacion: domain.Ubicacion("basura"),
		})},
	})

	if g.ID() != id || g.Folio() != domain.Folio("XYZ") || g.Origen() != domain.OrigenFolio("basura") {
		t.Errorf("identity fields not preserved")
	}
	if g.ClienteID() == nil || *g.ClienteID() != clienteID || g.VentaID() == nil || *g.VentaID() != ventaID {
		t.Errorf("cliente/venta not preserved")
	}
	if g.EstadoCuenta() == nil || *g.EstadoCuenta() != estadoCuenta {
		t.Errorf("EstadoCuenta not preserved")
	}
	if g.Estado() != domain.EstadoFolio("basura") {
		t.Errorf("Estado() = %q, want garbage preserved", g.Estado())
	}
	if g.VigenciaHasta() == nil || !g.VigenciaHasta().Equal(vigencia) {
		t.Errorf("VigenciaHasta not preserved")
	}
	if g.CerradoEn() == nil || !g.CerradoEn().Equal(cerrado) {
		t.Errorf("CerradoEn not preserved")
	}
	if g.GPSLat() == nil || *g.GPSLat() != lat || g.GPSLon() == nil || *g.GPSLon() != lon {
		t.Errorf("GPS not preserved")
	}
	if g.Calle() != "c" || g.NumeroExterior() != "n" || g.Colonia() != "col" ||
		g.Localidad() != "loc" || g.Ciudad() != "cd" || g.CodigoPostal() != "cp" {
		t.Errorf("domicilio not preserved")
	}
	if g.AbiertoPor() != "juan" {
		t.Errorf("AbiertoPor not preserved")
	}
	if !g.CreatedAt().Equal(fixed) || !g.UpdatedAt().Equal(fixed.Add(time.Hour)) {
		t.Errorf("timestamps not preserved")
	}
	if g.ArticulosCount() != 1 {
		t.Fatalf("ArticulosCount = %d, want 1", g.ArticulosCount())
	}
	art := firstArticulo(t, g)
	if art.ID() != artID || art.Etapa() != domain.Etapa("basura") {
		t.Fatalf("garbage article not preserved")
	}
	if countPending(g) != 0 {
		t.Fatalf("hydrated folio must not carry pending events")
	}
}

// --- RolDecisor obligatorio en los tres eventos de decisión ---

// actorSinRol is actor() with no RolDecisor: what a phone that forgot to say
// who decided sends. The three decision methods must refuse it.
func actorSinRol(usuario string) domain.ActorParams {
	a := actor(usuario)
	a.RolDecisor = nil
	return a
}

// TestEventosDecision_SinRolRechazadoSinMutarNiEncolar is the two-halves
// invariant of brief decision 7: a decision without its role fails with
// ErrRolDecisorObligatorio, the article is exactly where it was, and NOTHING
// was appended to the pending queue. A rejection that enqueued an event would
// leave the timeline saying a decision happened that never did.
func TestEventosDecision_SinRolRechazadoSinMutarNiEncolar(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		// preparar leaves the article in the stage the method needs.
		preparar func(t *testing.T, g *domain.Garantia) *domain.Articulo
		llamar   func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error
		etapa    domain.Etapa
	}{
		{
			nombre: "diagnostico",
			preparar: func(t *testing.T, g *domain.Garantia) *domain.Articulo {
				t.Helper()
				a := addArticle(t, g, "silla")
				return advance(t, g, a, domain.EtapaEnRevision)
			},
			llamar: func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error {
				return g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, act, fixed)
			},
			etapa: domain.EtapaEnRevision,
		},
		{
			nombre: "cambio_fisico",
			preparar: func(t *testing.T, g *domain.Garantia) *domain.Articulo {
				t.Helper()
				a := addArticle(t, g, "silla")
				a = advance(t, g, a, domain.EtapaEnRevision)
				return diagnostico(t, g, a, domain.RutaReparacionTaller)
			},
			llamar: func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error {
				return g.AutorizarCambioFisico(a.ID(), act, fixed)
			},
			etapa: domain.EtapaEnTaller,
		},
		{
			nombre: "desenlace",
			preparar: func(t *testing.T, g *domain.Garantia) *domain.Articulo {
				t.Helper()
				a := addArticle(t, g, "silla")
				a = advance(t, g, a, domain.EtapaEnRevision)
				a = diagnostico(t, g, a, domain.RutaReparacionTaller)
				swap(t, g, a)
				return articuloByID(t, g, a.ID())
			},
			llamar: func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error {
				return g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, act, fixed)
			},
			etapa: domain.EtapaStandby,
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			g := openPiso(t)
			a := c.preparar(t, g)
			pendientes := countPending(g)
			articulos := g.ArticulosCount()
			estado := g.Estado()

			err := c.llamar(g, a, actorSinRol("juan"))
			if !errors.Is(err, domain.ErrRolDecisorObligatorio) {
				t.Fatalf("want ErrRolDecisorObligatorio, got %v", err)
			}
			// Mitad 1: el estado no se movió.
			if got := articuloByID(t, g, a.ID()).Etapa(); got != c.etapa {
				t.Errorf("Etapa() = %q, want %q (mutó pese al rechazo)", got, c.etapa)
			}
			if g.ArticulosCount() != articulos {
				t.Errorf("ArticulosCount = %d, want %d", g.ArticulosCount(), articulos)
			}
			if g.Estado() != estado {
				t.Errorf("Estado = %q, want %q", g.Estado(), estado)
			}
			// Mitad 2: no se encoló nada.
			if countPending(g) != pendientes {
				t.Errorf("EventosPendientes = %d, want %d (encoló pese al rechazo)",
					countPending(g), pendientes)
			}
		})
	}
}

// TestEventosDecision_ConRolSeGuarda covers the other half of decision 7: the
// role travels into the event, so six months later the expediente can say who
// authorized the swap.
func TestEventosDecision_ConRolSeGuarda(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre   string
		rol      domain.RolDecisor
		preparar func(t *testing.T, g *domain.Garantia) *domain.Articulo
		llamar   func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error
	}{
		{
			nombre: "diagnostico",
			rol:    domain.RolDecisorTecnica,
			preparar: func(t *testing.T, g *domain.Garantia) *domain.Articulo {
				t.Helper()
				a := addArticle(t, g, "silla")
				return advance(t, g, a, domain.EtapaEnRevision)
			},
			llamar: func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error {
				return g.RegistrarDiagnostico(a.ID(), domain.RutaReparacionTaller, act, fixed)
			},
		},
		{
			nombre: "cambio_fisico",
			rol:    domain.RolDecisorOficina,
			preparar: func(t *testing.T, g *domain.Garantia) *domain.Articulo {
				t.Helper()
				a := addArticle(t, g, "silla")
				a = advance(t, g, a, domain.EtapaEnRevision)
				return diagnostico(t, g, a, domain.RutaReparacionTaller)
			},
			llamar: func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error {
				return g.AutorizarCambioFisico(a.ID(), act, fixed)
			},
		},
		{
			nombre: "desenlace",
			rol:    domain.RolDecisorCarpinteria,
			preparar: func(t *testing.T, g *domain.Garantia) *domain.Articulo {
				t.Helper()
				a := addArticle(t, g, "silla")
				a = advance(t, g, a, domain.EtapaEnRevision)
				a = diagnostico(t, g, a, domain.RutaReparacionTaller)
				swap(t, g, a)
				return articuloByID(t, g, a.ID())
			},
			llamar: func(g *domain.Garantia, a *domain.Articulo, act domain.ActorParams) error {
				return g.RegistrarDesenlace(a.ID(), domain.DesenlaceSegundaMano, act, fixed)
			},
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			g := openPiso(t)
			a := c.preparar(t, g)
			act := actorDecisor("juan")
			rol := c.rol
			act.RolDecisor = &rol
			if err := c.llamar(g, a, act); err != nil {
				t.Fatalf("con rol: %v", err)
			}
			e := lastEvent(t, g)
			if e.RolDecisor() == nil || *e.RolDecisor() != c.rol {
				t.Errorf("RolDecisor = %v, want %v", e.RolDecisor(), c.rol)
			}
		})
	}
}

// TestEventosNoDecision_RolOpcionalSeGuarda documents the other side of
// decision 7: everywhere else the role is optional, and when it arrives it is
// stored rather than dropped.
func TestEventosNoDecision_RolOpcionalSeGuarda(t *testing.T) {
	t.Parallel()
	t.Run("sin_rol_no_falla", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actor("juan"), fixed); err != nil {
			t.Fatalf("AvanzarArticulo sin rol: %v", err)
		}
		if e := lastEvent(t, g); e.RolDecisor() != nil {
			t.Errorf("RolDecisor = %v, want nil", e.RolDecisor())
		}
	})
	t.Run("con_rol_se_guarda", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		act := actorDecisor("juan")
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, act, fixed); err != nil {
			t.Fatalf("AvanzarArticulo con rol: %v", err)
		}
		if e := lastEvent(t, g); e.RolDecisor() == nil || *e.RolDecisor() != domain.RolDecisorOficina {
			t.Errorf("RolDecisor = %v, want oficina", e.RolDecisor())
		}
	})
}

// --- GPS del evento ---

// actorGPS is actor() with the given coordinates, which may be half a pair.
func actorGPS(lat, lon *float64) domain.ActorParams {
	a := actor("juan")
	a.GPSLat, a.GPSLon = lat, lon
	return a
}

// TestEventoGPS_InvalidoRechazado covers newEvento's GPS rule (decision 7):
// both or neither, each in range, neither NaN nor infinite. AdvanzarArticulo
// is only used as a door into the event constructor — the rule lives there.
func TestEventoGPS_InvalidoRechazado(t *testing.T) {
	t.Parallel()
	lat, lon := 19.427, -99.17
	arriba90, abajoMin90 := 90.0, -90.0
	arriba180, abajoMin180 := 180.0, -180.0
	fueraLat, fueraLon := 91.0, 181.0
	nan, inf := math.NaN(), math.Inf(1)

	casos := []struct {
		nombre string
		lat    *float64
		lon    *float64
	}{
		{"solo_lat", &lat, nil},
		{"solo_lon", nil, &lon},
		{"lat_fuera_de_rango", &fueraLat, &lon},
		{"lon_fuera_de_rango", &lat, &fueraLon},
		{"lat_nan", &nan, &lon},
		{"lon_inf", &lat, &inf},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			g := openPiso(t)
			a := addArticle(t, g, "silla")
			pendientes := countPending(g)
			err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actorGPS(c.lat, c.lon), fixed)
			if !errors.Is(err, domain.ErrEventoGPSInvalido) {
				t.Fatalf("want ErrEventoGPSInvalido, got %v", err)
			}
			if a.Etapa() != domain.EtapaRegistrado {
				t.Errorf("Etapa = %q, want registrado (mutó pese al rechazo)", a.Etapa())
			}
			if countPending(g) != pendientes {
				t.Errorf("encoló pese al rechazo: %d != %d", countPending(g), pendientes)
			}
		})
	}

	t.Run("bordes_validos", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actorGPS(&arriba90, &arriba180), fixed); err != nil {
			t.Fatalf("lat 90 / lon 180 rechazado: %v", err)
		}
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaReingresadoInventario, actorGPS(&abajoMin90, &abajoMin180), fixed); err != nil {
			t.Fatalf("lat -90 / lon -180 rechazado: %v", err)
		}
		if a.Etapa() != domain.EtapaReingresadoInventario {
			t.Errorf("Etapa = %q, want reingresado_inventario", a.Etapa())
		}
	})
}

func TestEventoGPS_SeGuardaEnElEvento(t *testing.T) {
	t.Parallel()
	t.Run("ambos", func(t *testing.T) {
		t.Parallel()
		lat, lon := 19.427, -99.17
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actorGPS(&lat, &lon), fixed); err != nil {
			t.Fatalf("AvanzarArticulo: %v", err)
		}
		e := lastEvent(t, g)
		if e.GPSLat() == nil || *e.GPSLat() != lat {
			t.Errorf("GPSLat = %v, want %v", e.GPSLat(), lat)
		}
		if e.GPSLon() == nil || *e.GPSLon() != lon {
			t.Errorf("GPSLon = %v, want %v", e.GPSLon(), lon)
		}
	})
	t.Run("ninguno", func(t *testing.T) {
		t.Parallel()
		g := openPiso(t)
		a := addArticle(t, g, "silla")
		if err := g.AvanzarArticulo(a.ID(), domain.EtapaEnRevision, actorGPS(nil, nil), fixed); err != nil {
			t.Fatalf("AvanzarArticulo sin GPS: %v", err)
		}
		e := lastEvent(t, g)
		if e.GPSLat() != nil || e.GPSLon() != nil {
			t.Errorf("GPS = %v/%v, want nil/nil", e.GPSLat(), e.GPSLon())
		}
	})
}

// TestAbrirGarantia_EventoLlevaGPSYRol covers the opening event: buildEvent
// passes the actor's coordinates and role through, which is what closes
// pendiente 10 of the task-4 review.
func TestAbrirGarantia_EventoLlevaGPSYRol(t *testing.T) {
	t.Parallel()
	lat, lon := 19.427, -99.17
	rol := domain.RolDecisorOficina
	act := actor("juan")
	act.RolDecisor, act.GPSLat, act.GPSLon = &rol, &lat, &lon
	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       "GA-000001",
		Origen:      domain.OrigenFolioPiso,
		Description: "silla rota",
		AbiertoPor:  "juan",
		Now:         fixed,
		Actor:       act,
	})
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	e := lastEvent(t, g)
	if e.RolDecisor() == nil || *e.RolDecisor() != rol {
		t.Errorf("RolDecisor = %v, want %v", e.RolDecisor(), rol)
	}
	if e.GPSLat() == nil || *e.GPSLat() != lat || e.GPSLon() == nil || *e.GPSLon() != lon {
		t.Errorf("GPS = %v/%v, want %v/%v", e.GPSLat(), e.GPSLon(), lat, lon)
	}
}

// TestAbrirGarantia_GPSInvalidoRechazado checks the opening event goes through
// the same newEvento validation as every other one.
func TestAbrirGarantia_GPSInvalidoRechazado(t *testing.T) {
	t.Parallel()
	lat := 19.427
	act := actor("juan")
	act.GPSLat, act.GPSLon = &lat, nil
	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       "GA-000001",
		Origen:      domain.OrigenFolioPiso,
		Description: "silla rota",
		AbiertoPor:  "juan",
		Now:         fixed,
		Actor:       act,
	})
	if !errors.Is(err, domain.ErrEventoGPSInvalido) {
		t.Fatalf("want ErrEventoGPSInvalido, got %v", err)
	}
	if g != nil {
		t.Errorf("want nil folio, got %v", g)
	}
}
