package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func envioParamsValido() domain.NewEnvioParams {
	return domain.NewEnvioParams{
		Tipo:           domain.TipoVenta,
		Referencia:     uuid.New().String(),
		ClienteID:      42,
		Telefono:       strPtr("5551234567"),
		Estado:         domain.EstadoEnvioEnEspera,
		ProgramadoPara: time.Now().UTC().Add(time.Hour),
		Canal:          domain.CanalLocal,
	}
}

func strPtr(s string) *string { return &s }

func TestNewEnvio_HappyPath(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	p := envioParamsValido()
	e, err := domain.NewEnvio(p)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.ID() == (uuid.UUID{}) {
		t.Fatal("ID() returned zero UUID")
	}
	if e.Tipo() != domain.TipoVenta {
		t.Fatalf("Tipo() = %v, want %v", e.Tipo(), domain.TipoVenta)
	}
	if e.ClienteID() != 42 {
		t.Fatalf("ClienteID() = %d, want 42", e.ClienteID())
	}
	if e.Telefono() == nil || *e.Telefono() != "5551234567" {
		t.Fatalf("Telefono() = %v", e.Telefono())
	}
	if e.Estado() != domain.EstadoEnvioEnEspera {
		t.Fatalf("Estado() = %v, want %v", e.Estado(), domain.EstadoEnvioEnEspera)
	}
	if e.ProgramadoPara().Before(now) {
		t.Fatal("ProgramadoPara() is in the past")
	}
	if e.Canal() != domain.CanalLocal {
		t.Fatalf("Canal() = %v, want %v", e.Canal(), domain.CanalLocal)
	}
	if e.Intentos() != 0 {
		t.Fatalf("Intentos() = %d, want 0", e.Intentos())
	}
	if e.UltimoError() != nil {
		t.Fatal("UltimoError() should be nil")
	}
	if e.DetenidoPor() != nil {
		t.Fatal("DetenidoPor() should be nil")
	}
	if e.EnviadoEn() != nil {
		t.Fatal("EnviadoEn() should be nil")
	}
	if e.DocumentoRuta() != nil {
		t.Fatal("DocumentoRuta() should be nil")
	}
	if e.MensajeExternoID() != nil {
		t.Fatal("MensajeExternoID() should be nil")
	}
	if e.String() != "en_espera" {
		t.Fatalf("String() = %q, want en_espera", e.String())
	}
}

func TestNewEnvio_TipoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Tipo = "invalido"
	_, err := domain.NewEnvio(p)
	if !errors.Is(err, domain.ErrTipoComprobanteInvalido) {
		t.Fatalf("expected ErrTipoComprobanteInvalido, got %v", err)
	}
}

func TestNewEnvio_ReferenciaVacia(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Referencia = "  "
	_, err := domain.NewEnvio(p)
	if !errors.Is(err, domain.ErrEnvioReferenciaRequerido) {
		t.Fatalf("expected ErrEnvioReferenciaRequerido, got %v", err)
	}
}

func TestNewEnvio_EstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Estado = "invalido"
	_, err := domain.NewEnvio(p)
	if !errors.Is(err, domain.ErrEstadoEnvioInvalido) {
		t.Fatalf("expected ErrEstadoEnvioInvalido, got %v", err)
	}
}

func TestNewEnvio_CanalInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Canal = "invalido"
	_, err := domain.NewEnvio(p)
	if !errors.Is(err, domain.ErrCanalInvalido) {
		t.Fatalf("expected ErrCanalInvalido, got %v", err)
	}
}

func TestNewEnvio_TelefonoNil(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Telefono = nil
	e, err := domain.NewEnvio(p)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Telefono() != nil {
		t.Fatal("Telefono() should be nil")
	}
}

func TestNewEnvio_ReferenciaTrimeada(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Referencia = "  VENTA-123  "
	e, err := domain.NewEnvio(p)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Referencia() != "VENTA-123" {
		t.Fatalf("Referencia() = %q, want %q", e.Referencia(), "VENTA-123")
	}
}

func TestHydrateEnvio_BypassesValidation(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	telefono := "5550000000"
	docRuta := "/tmp/test.pdf"
	msgID := "msg-123"
	razon := "timeout"
	detenido := "admin"
	enviado := now.Add(time.Minute)

	e := domain.HydrateEnvio(domain.HydrateEnvioParams{
		ID:               uuid.New(),
		Tipo:             domain.TipoPago,
		Referencia:       "",
		ClienteID:        0,
		Telefono:         &telefono,
		Estado:           domain.EstadoEnvioFallido,
		ProgramadoPara:   now,
		DocumentoRuta:    &docRuta,
		Canal:            domain.CanalWhatsappBusiness,
		MensajeExternoID: &msgID,
		Intentos:         3,
		UltimoError:      &razon,
		DetenidoPor:      &detenido,
		EnviadoEn:        &enviado,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if e.Tipo() != domain.TipoPago {
		t.Fatalf("Tipo() = %v, want Pago", e.Tipo())
	}
	if e.Referencia() != "" {
		t.Fatalf("Referencia() = %q, want empty", e.Referencia())
	}
	if e.Estado() != domain.EstadoEnvioFallido {
		t.Fatalf("Estado() = %v, want fallido", e.Estado())
	}
	if e.Intentos() != 3 {
		t.Fatalf("Intentos() = %d, want 3", e.Intentos())
	}
	if e.UltimoError() == nil || *e.UltimoError() != "timeout" {
		t.Fatalf("UltimoError() = %v", e.UltimoError())
	}
	if e.DocumentoRuta() == nil || *e.DocumentoRuta() != "/tmp/test.pdf" {
		t.Fatalf("DocumentoRuta() = %v", e.DocumentoRuta())
	}
	if e.MensajeExternoID() == nil || *e.MensajeExternoID() != "msg-123" {
		t.Fatalf("MensajeExternoID() = %v", e.MensajeExternoID())
	}
	if e.DetenidoPor() == nil || *e.DetenidoPor() != "admin" {
		t.Fatalf("DetenidoPor() = %v", e.DetenidoPor())
	}
	if e.EnviadoEn() == nil {
		t.Fatal("EnviadoEn() should not be nil")
	}
}

// --- Transiciones ---

func TestEnvio_Reclamar_HappyPath(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	if err := e.Reclamar(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioEnviando {
		t.Fatalf("Estado() = %v, want enviando", e.Estado())
	}
}

func TestEnvio_Reclamar_DesdeEstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Estado = domain.EstadoEnvioEnviando
	e, _ := domain.NewEnvio(p)
	if err := e.Reclamar(); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
		t.Fatalf("expected ErrEnvioTransicionInvalido, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioEnviando {
		t.Fatalf("Estado() changed to %v, should stay enviando", e.Estado())
	}
}

func TestEnvio_MarcarEnviado_HappyPath(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	e.Reclamar()
	if err := e.MarcarEnviado(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioEnviado {
		t.Fatalf("Estado() = %v, want enviado", e.Estado())
	}
	if e.EnviadoEn() == nil {
		t.Fatal("EnviadoEn() should be set")
	}
}

func TestEnvio_MarcarEnviado_DesdeEstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	if err := e.MarcarEnviado(); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
		t.Fatalf("expected ErrEnvioTransicionInvalido, got %v", err)
	}
}

func TestEnvio_MarcarFallido_HappyPath(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	e.Reclamar()
	if err := e.MarcarFallido("channel rejected"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioFallido {
		t.Fatalf("Estado() = %v, want fallido", e.Estado())
	}
	if e.UltimoError() == nil || *e.UltimoError() != "channel rejected" {
		t.Fatalf("UltimoError() = %v", e.UltimoError())
	}
}

func TestEnvio_MarcarFallido_DesdeEstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	if err := e.MarcarFallido("reason"); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
		t.Fatalf("expected ErrEnvioTransicionInvalido, got %v", err)
	}
}

func TestEnvio_Reenviar_HappyPath(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	e.Reclamar()
	e.MarcarFallido("timeout")
	if err := e.Reenviar(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioEnEspera {
		t.Fatalf("Estado() = %v, want en_espera", e.Estado())
	}
	if e.Intentos() != 1 {
		t.Fatalf("Intentos() = %d, want 1", e.Intentos())
	}
	if e.UltimoError() != nil {
		t.Fatal("UltimoError() should be nil after Reenviar")
	}
}

func TestEnvio_Reenviar_IncrementsIntentos(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	for range 3 {
		e.Reclamar()
		e.MarcarFallido("reason")
		e.Reenviar()
	}
	if e.Intentos() != 3 {
		t.Fatalf("Intentos() = %d, want 3", e.Intentos())
	}
}

func TestEnvio_Reenviar_DesdeEstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	if err := e.Reenviar(); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
		t.Fatalf("expected ErrEnvioTransicionInvalido, got %v", err)
	}
}

func TestEnvio_Detener_HappyPath(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	if err := e.Detener("admin"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioDetenido {
		t.Fatalf("Estado() = %v, want detenido", e.Estado())
	}
	if e.DetenidoPor() == nil || *e.DetenidoPor() != "admin" {
		t.Fatalf("DetenidoPor() = %v", e.DetenidoPor())
	}
}

func TestEnvio_Detener_DesdeEstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	e.Reclamar()
	if err := e.Detener("admin"); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
		t.Fatalf("expected ErrEnvioTransicionInvalido, got %v", err)
	}
}

func TestEnvio_MarcarSinTelefono_HappyPath(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	e, _ := domain.NewEnvio(p)
	if err := e.MarcarSinTelefono(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioSinTelefono {
		t.Fatalf("Estado() = %v, want sin_telefono", e.Estado())
	}
}

func TestEnvio_MarcarSinTelefono_DesdeEstadoInvalido(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Estado = domain.EstadoEnvioEnviando
	e, _ := domain.NewEnvio(p)
	if err := e.MarcarSinTelefono(); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
		t.Fatalf("expected ErrEnvioTransicionInvalido, got %v", err)
	}
}

func TestEnvio_EstadoNoCambiaOnError(t *testing.T) {
	t.Parallel()
	p := envioParamsValido()
	p.Estado = domain.EstadoEnvioEnviando
	e, _ := domain.NewEnvio(p)
	_ = e.Reclamar()
	if e.Estado() != domain.EstadoEnvioEnviando {
		t.Fatalf("Estado() = %v, want enviando (unchanged)", e.Estado())
	}
}
