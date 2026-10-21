// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// The five permission codes of spec §7, as pointers so a fake can deny exactly
// one without spelling out the other four.
var (
	permCrear      = domain.PermisoCrear
	permActualizar = domain.PermisoActualizar
)

func setupService() (*app.Service, *fakeGarantiaRepo, *fakeEventoRepo, *fakeFolioGen, *fakeTxRunner, *fakeIdentity, *fakeClock, *registrador) {
	grepo := newFakeGarantiaRepo()
	erepo := newFakeEventoRepo()
	ord := &registrador{}
	grepo.orden = ord
	erepo.orden = ord
	fgen := &fakeFolioGen{nums: []int{1001}}
	tx := &fakeTxRunner{}
	tx.onCommit = grepo.commit
	tx.onRollback = grepo.rollback
	id := &fakeIdentity{usuario: outbound.Usuario{ID: "u1", Nombre: "Juan"}}
	clk := &fakeClock{now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	svc := app.NewService(app.Deps{
		GarantiaRepo:   grepo,
		EventoRepo:     erepo,
		FolioGenerator: fgen,
		TxRunner:       tx,
		Identity:       id,
		Clock:          clk,
	})
	return svc, grepo, erepo, fgen, tx, id, clk, ord
}

func TestAbrirGarantia_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, fgen, tx, _, clk, _ := setupService()
	cmd := app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	}
	g, err := svc.AbrirGarantia(context.Background(), cmd)
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	if g == nil {
		t.Fatal("garantia nil")
	}
	if fgen.calls != 1 {
		t.Errorf("folio calls = %d", fgen.calls)
	}
	if tx.calls < 1 {
		t.Errorf("tx.calls = %d", tx.calls)
	}
	if len(grepo.created) != 1 {
		t.Errorf("created = %d", len(grepo.created))
	}
	if g.AbiertoPor() != "Juan" {
		t.Errorf("AbiertoPor = %q, want el nombre que trae Identity", g.AbiertoPor())
	}
}

func TestAbrirGarantia_NoAutenticado(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, id, clk, _ := setupService()
	id.errUser = domain.ErrUsuarioNoAutenticado
	cmd := app.AbrirGarantiaCmd{
		Origen:          domain.OrigenFolioPiso,
		Description:     "mueble",
		DeviceCreatedAt: clk.Now(),
	}
	_, err := svc.AbrirGarantia(context.Background(), cmd)
	if !errors.Is(err, domain.ErrUsuarioNoAutenticado) {
		t.Fatalf("want ErrUsuarioNoAutenticado, got %v", err)
	}
	if tx.calls != 0 || len(grepo.created) != 0 {
		t.Errorf("no debe escribir: tx=%d created=%d", tx.calls, len(grepo.created))
	}
}

func TestAbrirGarantia_SinPermisoCrear(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, id, clk, _ := setupService()
	id.denegar = &permCrear
	cmd := app.AbrirGarantiaCmd{
		Origen:          domain.OrigenFolioPiso,
		Description:     "mueble",
		DeviceCreatedAt: clk.Now(),
	}
	_, err := svc.AbrirGarantia(context.Background(), cmd)
	if !errors.Is(err, domain.ErrPermisoDenegado) {
		t.Fatalf("want ErrPermisoDenegado, got %v", err)
	}
	if tx.calls != 0 || len(grepo.created) != 0 {
		t.Errorf("no debe escribir: tx=%d created=%d", tx.calls, len(grepo.created))
	}
}

// TestAbrirGarantia_PisoConGPSDelAgente pins review round 2, punto 2: the two
// GPS of the command are different data. The agent's position belongs to the
// folio_abierto event and is legal on a piso folio; only the address GPS
// (DomicilioGPS*) is rejected there. Before the split, one field fed both and
// a phone that reported its position could not open a piso folio at all.
func TestAbrirGarantia_PisoConGPSDelAgente(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	lat, lon := 19.4326, -99.1332

	g, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		GPSLat:            &lat,
		GPSLon:            &lon,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("un folio de piso con la posicion del agente no debe fallar: %v", err)
	}
	if g.GPSLat() != nil || g.GPSLon() != nil {
		t.Errorf("el GPS del agente no es domicilio: folio GPS = %v/%v, want nil", g.GPSLat(), g.GPSLon())
	}
	ev := unicoEventoPendiente(t, grepo.lastCreated)
	if ev.GPSLat() == nil || *ev.GPSLat() != lat || ev.GPSLon() == nil || *ev.GPSLon() != lon {
		t.Errorf("evento GPS = %v/%v, want %v/%v", ev.GPSLat(), ev.GPSLon(), lat, lon)
	}
}

// TestAbrirGarantia_PisoConDomicilioGPS is the control of the test above: the
// address GPS of a piso folio is still rejected by the domain.
func TestAbrirGarantia_PisoConDomicilioGPS(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	lat, lon := 19.4326, -99.1332

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		DomicilioGPSLat:   &lat,
		DomicilioGPSLon:   &lon,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrDomicilioNoPermitido) {
		t.Fatalf("want ErrDomicilioNoPermitido, got %v", err)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0", len(grepo.created))
	}
}

// TestAbrirGarantia_ClienteGuardaLosDosGPS proves the split keeps both values
// apart on a cliente folio: the address GPS ends in the folio and the agent's
// in the event (spec §3.1 y §3.3).
func TestAbrirGarantia_ClienteGuardaLosDosGPS(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	cliente, venta := 77, 9001
	estado := domain.EstadoCuentaSaldoPendiente
	domLat, domLon := 20.6597, -103.3496
	ageLat, ageLon := 20.6601, -103.3500

	g, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioCliente,
		ClienteID:         &cliente,
		VentaID:           &venta,
		EstadoCuenta:      &estado,
		Description:       "comoda con puerta quebrada",
		Calle:             "Av. Chapultepec",
		NumeroExterior:    "412",
		Colonia:           "Americana",
		Localidad:         "Guadalajara",
		Ciudad:            "Guadalajara",
		CodigoPostal:      "44160",
		DomicilioGPSLat:   &domLat,
		DomicilioGPSLon:   &domLon,
		GPSLat:            &ageLat,
		GPSLon:            &ageLon,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	if g.GPSLat() == nil || *g.GPSLat() != domLat || g.GPSLon() == nil || *g.GPSLon() != domLon {
		t.Errorf("folio GPS = %v/%v, want el domicilio %v/%v", g.GPSLat(), g.GPSLon(), domLat, domLon)
	}
	ev := unicoEventoPendiente(t, grepo.lastCreated)
	if ev.GPSLat() == nil || *ev.GPSLat() != ageLat || ev.GPSLon() == nil || *ev.GPSLon() != ageLon {
		t.Errorf("evento GPS = %v/%v, want el agente %v/%v", ev.GPSLat(), ev.GPSLon(), ageLat, ageLon)
	}
}
