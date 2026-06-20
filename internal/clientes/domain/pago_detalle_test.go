//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

func TestHydratePagoDetalle_Roundtrip(t *testing.T) {
	t.Parallel()

	saldo := decimal.NewFromFloat(100.50)
	lat := decimal.NewFromFloat(19.432608)
	lon := decimal.NewFromFloat(-99.133209)
	recibidoAt := time.Date(2020, 7, 2, 10, 0, 0, 0, time.UTC)
	aplicadoAt := time.Date(2020, 7, 2, 11, 0, 0, 0, time.UTC)

	params := domain.HydratePagoDetalleParams{
		DoctoCCID:      4070588,
		Fecha:          time.Date(2020, 7, 2, 0, 0, 0, 0, time.UTC),
		Folio:          "000013412",
		Cancelado:      false,
		Aplicado:       true,
		Importe:        decimal.NewFromFloat(200.00),
		IVA:            decimal.NewFromFloat(0),
		ConceptoCCID:   24533,
		Concepto:       "Enganche",
		Categoria:      domain.CategoriaIngresoEnganche,
		CobradorID:     77486,
		Cobrador:       "Juan Cobrador",
		FormaCobroID:   52569,
		FormaCobro:     "Efectivo",
		Referencia:     "REF-001",
		AplicaACargoID: 4070585,
		SaldoCargo:     &saldo,
		DoctoPVID:      4070523,
		Lat:            &lat,
		Lon:            &lon,
		RecibidoAt:     recibidoAt,
		AplicadoAt:     aplicadoAt,
		Origen:         "microsip",
	}

	pd := domain.HydratePagoDetalle(params)

	if pd.DoctoCCID() != 4070588 {
		t.Errorf("DoctoCCID: want 4070588, got %d", pd.DoctoCCID())
	}
	if !pd.Fecha().Equal(params.Fecha.UTC()) {
		t.Errorf("Fecha: want %v, got %v", params.Fecha.UTC(), pd.Fecha())
	}
	if pd.Folio() != "000013412" {
		t.Errorf("Folio: want 000013412, got %q", pd.Folio())
	}
	if pd.Cancelado() != false {
		t.Error("Cancelado: want false")
	}
	if pd.Aplicado() != true {
		t.Error("Aplicado: want true")
	}
	if !pd.Importe().Equal(decimal.NewFromFloat(200.00)) {
		t.Errorf("Importe: want 200.00, got %v", pd.Importe())
	}
	if !pd.IVA().Equal(decimal.Zero) {
		t.Errorf("IVA: want 0, got %v", pd.IVA())
	}
	if pd.ConceptoCCID() != 24533 {
		t.Errorf("ConceptoCCID: want 24533, got %d", pd.ConceptoCCID())
	}
	if pd.Concepto() != "Enganche" {
		t.Errorf("Concepto: want Enganche, got %q", pd.Concepto())
	}
	if pd.Categoria() != domain.CategoriaIngresoEnganche {
		t.Errorf("Categoria: want %v, got %v", domain.CategoriaIngresoEnganche, pd.Categoria())
	}
	if pd.CobradorID() != 77486 {
		t.Errorf("CobradorID: want 77486, got %d", pd.CobradorID())
	}
	if pd.Cobrador() != "Juan Cobrador" {
		t.Errorf("Cobrador: want Juan Cobrador, got %q", pd.Cobrador())
	}
	if pd.FormaCobroID() != 52569 {
		t.Errorf("FormaCobroID: want 52569, got %d", pd.FormaCobroID())
	}
	if pd.FormaCobro() != "Efectivo" {
		t.Errorf("FormaCobro: want Efectivo, got %q", pd.FormaCobro())
	}
	if pd.Referencia() != "REF-001" {
		t.Errorf("Referencia: want REF-001, got %q", pd.Referencia())
	}
	if pd.AplicaACargoID() != 4070585 {
		t.Errorf("AplicaACargoID: want 4070585, got %d", pd.AplicaACargoID())
	}
	if pd.SaldoCargo() == nil || !pd.SaldoCargo().Equal(saldo) {
		t.Errorf("SaldoCargo: want %v, got %v", saldo, pd.SaldoCargo())
	}
	if pd.DoctoPVID() != 4070523 {
		t.Errorf("DoctoPVID: want 4070523, got %d", pd.DoctoPVID())
	}
	if pd.Lat() == nil || !pd.Lat().Equal(lat) {
		t.Errorf("Lat: want %v, got %v", lat, pd.Lat())
	}
	if pd.Lon() == nil || !pd.Lon().Equal(lon) {
		t.Errorf("Lon: want %v, got %v", lon, pd.Lon())
	}
	if !pd.RecibidoAt().Equal(recibidoAt) {
		t.Errorf("RecibidoAt: want %v, got %v", recibidoAt, pd.RecibidoAt())
	}
	if !pd.AplicadoAt().Equal(aplicadoAt) {
		t.Errorf("AplicadoAt: want %v, got %v", aplicadoAt, pd.AplicadoAt())
	}
	if pd.Origen() != "microsip" {
		t.Errorf("Origen: want microsip, got %q", pd.Origen())
	}
}

func TestHydratePagoDetalle_ZeroValues(t *testing.T) {
	t.Parallel()

	pd := domain.HydratePagoDetalle(domain.HydratePagoDetalleParams{})

	if pd.DoctoCCID() != 0 {
		t.Errorf("zero DoctoCCID: want 0, got %d", pd.DoctoCCID())
	}
	if !pd.Fecha().IsZero() {
		t.Errorf("zero Fecha: want zero time, got %v", pd.Fecha())
	}
	if pd.SaldoCargo() != nil {
		t.Errorf("zero SaldoCargo: want nil, got %v", pd.SaldoCargo())
	}
	if pd.Lat() != nil {
		t.Errorf("zero Lat: want nil, got %v", pd.Lat())
	}
	if pd.Lon() != nil {
		t.Errorf("zero Lon: want nil, got %v", pd.Lon())
	}
}
