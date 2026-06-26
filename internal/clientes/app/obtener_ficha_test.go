//nolint:misspell // Spanish vocabulary (clientes, ficha, pulso, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func newFichaService(repo outbound.ClientesRepo, anl outbound.AnalyticsClient) *app.Service {
	return app.NewService(repo, anl, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})
}

func TestObtenerFicha_ClienteYPulsoPresentes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cliente := newCliente(1, "María López")
	pulso := analytics.ClientePulsoContract{ClienteID: 1, Score: 80, Segmento: "ACTIVO"}
	resumen := outbound.ResumenFicha{NumVentas: 5}

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: cliente},
		resumen:     resumen,
	}
	anl := &fakeAnalyticsClient{
		pulsos: map[int]analytics.ClientePulsoContract{1: pulso},
	}

	svc := newFichaService(repo, anl)
	ficha, err := svc.ObtenerFicha(ctx, 1, outbound.RangoFechas{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ficha.Cliente.ClienteID() != 1 {
		t.Errorf("expected ClienteID 1, got %d", ficha.Cliente.ClienteID())
	}
	if !ficha.TienePulso {
		t.Error("expected TienePulso=true")
	}
	if ficha.Pulso.Score != 80 {
		t.Errorf("expected Score 80, got %d", ficha.Pulso.Score)
	}
	if ficha.Resumen.NumVentas != 5 {
		t.Errorf("expected NumVentas 5, got %d", ficha.Resumen.NumVentas)
	}
}

func TestObtenerFicha_PulsoAusenteDegradasinError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cliente := newCliente(2, "Juan García")
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{2: cliente},
	}
	// Analytics client returns found=false (no materialized row).
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	svc := newFichaService(repo, anl)
	ficha, err := svc.ObtenerFicha(ctx, 2, outbound.RangoFechas{})
	if err != nil {
		t.Fatalf("expected no error on pulse degradation, got %v", err)
	}
	if ficha.TienePulso {
		t.Error("expected TienePulso=false when no analytics row")
	}
	// Zero value pulso — ClienteID should be zero.
	if ficha.Pulso.ClienteID != 0 {
		t.Errorf("expected zero-value Pulso, got ClienteID=%d", ficha.Pulso.ClienteID)
	}
}

func TestObtenerFicha_ClienteNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &fakeClientesRepo{clienteByID: map[int]*domain.Cliente{}}
	anl := &fakeAnalyticsClient{}

	svc := newFichaService(repo, anl)
	_, err := svc.ObtenerFicha(ctx, 999, outbound.RangoFechas{})

	if err == nil {
		t.Fatal("expected error on missing cliente")
	}
	if !errors.Is(err, domain.ErrClienteNotFound) {
		t.Errorf("expected ErrClienteNotFound, got %v", err)
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Source != "clientes.ObtenerFicha" {
		t.Errorf("expected source clientes.ObtenerFicha, got %q", appErr.Source)
	}
}

func TestObtenerFicha_ResumenInfraError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	infraErr := errors.New("db connection reset")
	cliente := newCliente(1, "Test Cliente")
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: cliente},
		resumenErr:  infraErr,
	}
	anl := &fakeAnalyticsClient{}

	svc := newFichaService(repo, anl)
	_, err := svc.ObtenerFicha(ctx, 1, outbound.RangoFechas{})

	if err == nil {
		t.Fatal("expected error when resumen fetch fails")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Kind != apperror.KindInternal {
		t.Errorf("expected KindInternal, got %v", appErr.Kind)
	}
	if appErr.Source != "clientes.ObtenerFicha" {
		t.Errorf("expected source clientes.ObtenerFicha, got %q", appErr.Source)
	}
}

func TestObtenerFicha_PulsoTransportError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	transportErr := errors.New("analytics service timeout")
	cliente := newCliente(1, "Test Cliente")
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: cliente},
	}
	anl := &fakeAnalyticsClient{pulsoErr: transportErr}

	svc := newFichaService(repo, anl)
	_, err := svc.ObtenerFicha(ctx, 1, outbound.RangoFechas{})

	if err == nil {
		t.Fatal("expected error on analytics transport failure")
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatal("expected *apperror.Error")
	}
	if appErr.Kind != apperror.KindInternal {
		t.Errorf("expected KindInternal for transport error, got %v", appErr.Kind)
	}
}

// TestObtenerFicha_TendenciaComputada verifies that ObtenerFicha computes the
// Tendencia from AbonosPorMes and attaches it to FichaCliente.
func TestObtenerFicha_TendenciaComputada(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cliente := newCliente(1, "Test Tendencia")
	// Increasing monthly series → slope > 0 → mejorando.
	resumen := outbound.ResumenFicha{
		AbonosPorMes: []outbound.PuntoMensual{
			{Anio: 2025, Mes: 1, Monto: decimal.NewFromInt(100)},
			{Anio: 2025, Mes: 2, Monto: decimal.NewFromInt(200)},
			{Anio: 2025, Mes: 3, Monto: decimal.NewFromInt(300)},
			{Anio: 2025, Mes: 4, Monto: decimal.NewFromInt(400)},
			{Anio: 2025, Mes: 5, Monto: decimal.NewFromInt(500)},
		},
	}
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: cliente},
		resumen:     resumen,
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	svc := newFichaService(repo, anl)
	ficha, err := svc.ObtenerFicha(ctx, 1, outbound.RangoFechas{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ficha.Tendencia.Slope <= 0 {
		t.Errorf("expected positive slope for increasing series, got %f", ficha.Tendencia.Slope)
	}
	if ficha.Tendencia.Direccion != domain.DireccionMejorando {
		t.Errorf("expected DireccionMejorando, got %q", ficha.Tendencia.Direccion)
	}
}

// TestObtenerFicha_RangoPasadoAlRepo verifies that the RangoFechas is forwarded
// to the repository unchanged, so the DB query uses the caller-supplied window.
func TestObtenerFicha_RangoPasadoAlRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cliente := newCliente(1, "Rango Test")
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: cliente},
	}
	anl := &fakeAnalyticsClient{pulsos: map[int]analytics.ClientePulsoContract{}}

	desde := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	hasta := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	rango := outbound.RangoFechas{Desde: &desde, Hasta: &hasta}

	svc := newFichaService(repo, anl)
	_, err := svc.ObtenerFicha(ctx, 1, rango)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastRango.Desde == nil {
		t.Error("expected non-nil Desde forwarded to repo")
	} else if !repo.lastRango.Desde.Equal(desde) {
		t.Errorf("Desde: got %v, want %v", repo.lastRango.Desde, desde)
	}
	if repo.lastRango.Hasta == nil {
		t.Error("expected non-nil Hasta forwarded to repo")
	} else if !repo.lastRango.Hasta.Equal(hasta) {
		t.Errorf("Hasta: got %v, want %v", repo.lastRango.Hasta, hasta)
	}
}
