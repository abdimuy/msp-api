// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestEntregar_CaminoFeliz delivers a client folio: every ready article leaves
// with its own event and the folio records the delivery, GPS included.
//
// Two articles on purpose. CLAVE_IDEMPOTENCIA is UNIQUE, so a call that gave
// the same key to the command's event and to each article event would violate
// the index on the second INSERT: this is the case the fake repository alone
// cannot see, because it does not enforce the UNIQUE.
func TestEntregar_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(),
		domain.EtapaListoEntrega, domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()

	res, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}

	if res.Estado() != domain.EstadoFolioEntregado {
		t.Errorf("estado = %s, want entregado", res.Estado())
	}
	for _, id := range s.arts {
		art := articuloDe(t, res, id)
		if art.Etapa() != domain.EtapaEntregado {
			t.Errorf("etapa del articulo %s = %s, want entregado", id, art.Etapa())
		}
		if art.Ubicacion() != domain.UbicacionEntregado {
			t.Errorf("ubicacion del articulo %s = %s, want entregado", id, art.Ubicacion())
		}
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}

	// One article event per article, then the folio fact.
	eventos := eventosEscritos(t, grepo)
	if len(eventos) != 3 {
		t.Fatalf("eventos = %d, want 3 (2 articulos + folio)", len(eventos))
	}
	if eventos[0].Tipo() != domain.TipoEventoEtapaAvanzada ||
		eventos[1].Tipo() != domain.TipoEventoEtapaAvanzada {
		t.Errorf("los dos primeros eventos deben ser etapa_avanzada, got %s y %s",
			eventos[0].Tipo(), eventos[1].Tipo())
	}
	if eventos[2].Tipo() != domain.TipoEventoFolioEntregado {
		t.Errorf("ultimo evento = %s, want folio_entregado", eventos[2].Tipo())
	}
	if eventos[2].GPSLat() == nil || *eventos[2].GPSLat() != *lat {
		t.Errorf("el evento del folio debe guardar el GPS de la entrega")
	}
	// Each event needs its own key: two articles delivered by the same call
	// are two rows in a column with a UNIQUE index.
	vistas := map[string]bool{}
	for _, ev := range eventos {
		if vistas[ev.ClaveIdempotencia()] {
			t.Errorf("dos eventos comparten clave de idempotencia: %q", ev.ClaveIdempotencia())
		}
		vistas[ev.ClaveIdempotencia()] = true
	}
}

// TestEntregar_SinGps refuses before the transaction: a delivery without the
// agent's coordinates is the audit hole §4.1 exists to close, so the folio is
// not even locked for it.
func TestEntregar_SinGps(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrEntregaGPSRequerido) {
		t.Fatalf("want ErrEntregaGPSRequerido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestEntregar_EstadoNoCorresponde only delivers a folio that is listo_entrega.
func TestEntregar_EstadoNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, cliente(), domain.EtapaEnRevision)
	lat, lon := gpsPrueba()

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestEntregar_FolioDePiso pins R1: entregado is a customer stage, so a floor
// folio closes without touching its articles.
func TestEntregar_FolioDePiso(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, piso(), domain.EtapaReingresadoInventario)
	lat, lon := gpsPrueba()

	res, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if err != nil {
		t.Fatalf("Entregar: %v", err)
	}
	if res.Estado() != domain.EstadoFolioEntregado {
		t.Errorf("estado = %s, want entregado", res.Estado())
	}
	if art := articuloDe(t, res, s.primero()); art.Etapa() != domain.EtapaReingresadoInventario {
		t.Errorf("etapa del articulo = %s, want reingresado_inventario: un folio de piso no entrega articulos", art.Etapa())
	}
}

// TestEntregar_FolioDePisoConListoEntrega pins the defensive check of regla 1:
// a piso folio stored in listo_entrega with an article still in listo_entrega
// is refused before any write, so R4 cannot strand the article.
func TestEntregar_FolioDePisoConListoEntrega(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, piso(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrArticuloPisoSinReingresar) {
		t.Fatalf("want ErrArticuloPisoSinReingresar, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestEntregar_SinFirma refuses before the transaction: the client's signature
// is the proof of conformity §4.1 requires, so a delivery without it never
// locks the folio and never writes a blob.
func TestEntregar_SinFirma(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrFirmaRequerida) {
		t.Fatalf("want ErrFirmaRequerida, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
	if n := grepo.storage.total(); n != 0 {
		t.Errorf("no debe guardar blob: %d", n)
	}
}

// TestEntregar_FirmaFormatoInvalido rejects a payload that is not PNG or JPEG
// before the transaction. The blob is never stored, so a bad upload leaves no
// orphan behind.
func TestEntregar_FirmaFormatoInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             []byte("no soy una imagen"),
		FirmaContentType:  "text/plain",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrImagenFormatoInvalido) {
		t.Fatalf("want ErrImagenFormatoInvalido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
	if n := grepo.storage.total(); n != 0 {
		t.Errorf("no debe guardar blob: %d", n)
	}
}

// TestEntregar_FirmaMuyGrande rejects a signature over the 1 MiB cap.
func TestEntregar_FirmaMuyGrande(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()
	grande := append(firmaPNGPrueba(), make([]byte, 1<<20)...)

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             grande,
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrImagenMuyGrande) {
		t.Fatalf("want ErrImagenMuyGrande, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestEntregar_GuardaLaFirma walks the signature all the way to its row: the
// delivery writes one firma image, keyed by the canonical idempotency key — so
// a retry lands on the same primary key — hanging from the folio_entregado
// event, uploaded by the Identity user and backed by a real blob.
func TestEntregar_GuardaLaFirma(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()
	k := clave()

	if _, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	}); err != nil {
		t.Fatalf("Entregar: %v", err)
	}

	firmas := grepo.imagenes.porTipo(domain.TipoImagenFirma)
	if len(firmas) != 1 {
		t.Fatalf("imagenes de firma = %d, want 1", len(firmas))
	}
	img := firmas[0]
	if img.ID() != domain.FirmaImagenID(k) {
		t.Errorf("id de la firma = %s, want el derivado de la clave %s", img.ID(), domain.FirmaImagenID(k))
	}
	if img.SubidaPor() != "Juan" {
		t.Errorf("subidaPor = %q, want el usuario de Identity", img.SubidaPor())
	}
	if !strings.HasPrefix(img.Ruta(), "garantias/firmas/") {
		t.Errorf("ruta = %q, want bajo garantias/firmas/", img.Ruta())
	}
	var entrega *domain.Evento
	for _, ev := range eventosEscritos(t, grepo) {
		if ev.Tipo() == domain.TipoEventoFolioEntregado {
			entrega = ev
		}
	}
	if entrega == nil {
		t.Fatal("falta el evento folio_entregado")
	}
	if img.EventoID() != entrega.ID() {
		t.Errorf("evento de la firma = %s, want el folio_entregado %s", img.EventoID(), entrega.ID())
	}
	if img.Huella() != entrega.Huella() || img.Huella() == "" {
		t.Errorf("la firma debe llevar la huella de la entrega, got %q y %q", img.Huella(), entrega.Huella())
	}
	if _, ok := grepo.storage.blobs[img.Ruta()]; !ok {
		t.Errorf("el blob de la firma no quedo en storage: %v", grepo.storage.claves())
	}
}

// TestEntregar_ErrorDescartaElBlob: the blob is written before the transaction,
// so a command that fails after it must delete its file. Otherwise every
// refused delivery would leave an orphan on the disk.
func TestEntregar_ErrorDescartaElBlob(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, cliente(), domain.EtapaEnRevision)
	lat, lon := gpsPrueba()

	_, err := svc.Entregar(context.Background(), app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	})
	if !errors.Is(err, domain.ErrTransicionEstadoNoPermitida) {
		t.Fatalf("want ErrTransicionEstadoNoPermitida, got %v", err)
	}
	if n := grepo.storage.total(); n != 0 {
		t.Errorf("el blob rechazado debe borrarse: quedan %d (%v)", n, grepo.storage.claves())
	}
	if grepo.storage.storeCalls != 1 || grepo.storage.deleteCalls != 1 {
		t.Errorf("store/delete = %d/%d, want 1/1", grepo.storage.storeCalls, grepo.storage.deleteCalls)
	}
}

// TestEntregar_ReintentoDescartaSuBlob: a replay answers with the original
// folio and writes no image row; the blob it just stored is deleted, so only
// the first attempt's file survives.
func TestEntregar_ReintentoDescartaSuBlob(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioListoEntrega, cliente(), domain.EtapaListoEntrega)
	lat, lon := gpsPrueba()
	k := clave()
	cmd := app.EntregarCmd{
		GarantiaID:        s.g.ID(),
		Firma:             firmaPNGPrueba(),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
		GPSLat:            lat,
		GPSLon:            lon,
	}
	if _, err := svc.Entregar(context.Background(), cmd); err != nil {
		t.Fatalf("primera entrega: %v", err)
	}
	blobsTrasLaPrimera := grepo.storage.total()
	// Register the event as the store would have it and replay the same body.
	erepo.addEvento(hydrateEventoConHuella(
		s.g.ID(), k, domain.TipoEventoFolioEntregado,
		app.HuellaDeParaPruebas(cmd), clk.Now()))

	if _, err := svc.Entregar(context.Background(), cmd); err != nil {
		t.Fatalf("la repeticion debe tener exito: %v", err)
	}
	if got := grepo.storage.total(); got != blobsTrasLaPrimera {
		t.Errorf("blobs = %d, want %d: el reintento no deja su archivo", got, blobsTrasLaPrimera)
	}
	if got := len(grepo.imagenes.porTipo(domain.TipoImagenFirma)); got != 1 {
		t.Errorf("imagenes de firma = %d, want 1: el reintento no inserta otra", got)
	}
}
