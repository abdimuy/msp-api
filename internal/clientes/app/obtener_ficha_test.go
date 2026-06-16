//nolint:misspell // Spanish vocabulary (clientes, ficha, pulso, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

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
	ficha, err := svc.ObtenerFicha(ctx, 1)
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
	ficha, err := svc.ObtenerFicha(ctx, 2)
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
	_, err := svc.ObtenerFicha(ctx, 999)

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
	_, err := svc.ObtenerFicha(ctx, 1)

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
	_, err := svc.ObtenerFicha(ctx, 1)

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
