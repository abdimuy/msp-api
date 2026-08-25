package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

var now = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func crearParamsValido() domain.CrearEnvioParams {
	return domain.CrearEnvioParams{
		Tipo:           domain.TipoVenta,
		Referencia:     uuid.New().String(),
		ClienteID:      42,
		Telefono:       strPtr("5551234567"),
		ProgramadoPara: now.Add(time.Hour),
		Canal:          domain.CanalLocal,
	}
}

func strPtr(s string) *string { return &s }

func crearEnvioEnEspera(t *testing.T) *domain.Envio {
	t.Helper()
	e, err := domain.CrearEnvio(crearParamsValido(), now)
	if err != nil {
		t.Fatalf("CrearEnvio failed: %v", err)
	}
	return e
}

// --- CrearEnvio ---

func TestCrearEnvio_HappyPath(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	e, err := domain.CrearEnvio(p, now)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.ID() == (uuid.UUID{}) {
		t.Fatal("ID() returned zero UUID")
	}
	if e.Tipo() != domain.TipoVenta {
		t.Fatalf("Tipo() = %v, want TipoVenta", e.Tipo())
	}
	if e.ClienteID() != 42 {
		t.Fatalf("ClienteID() = %d, want 42", e.ClienteID())
	}
	if e.Telefono() == nil || *e.Telefono() != "5551234567" {
		t.Fatalf("Telefono() = %v", e.Telefono())
	}
	if e.Estado() != domain.EstadoEnvioEnEspera {
		t.Fatalf("Estado() = %v, want en_espera", e.Estado())
	}
	if e.ProgramadoPara() != p.ProgramadoPara {
		t.Fatalf("ProgramadoPara() = %v, want %v", e.ProgramadoPara(), p.ProgramadoPara)
	}
	if e.Canal() != domain.CanalLocal {
		t.Fatalf("Canal() = %v, want local", e.Canal())
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

func TestCrearEnvio_SinTelefono_NaceEnSinTelefono(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.Telefono = nil
	e, err := domain.CrearEnvio(p, now)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Estado() != domain.EstadoEnvioSinTelefono {
		t.Fatalf("Estado() = %v, want sin_telefono", e.Estado())
	}
	if e.Telefono() != nil {
		t.Fatal("Telefono() should be nil")
	}
}

func TestCrearEnvio_TipoInvalido(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.Tipo = "invalido"
	_, err := domain.CrearEnvio(p, now)
	if !errors.Is(err, domain.ErrTipoComprobanteInvalido) {
		t.Fatalf("expected ErrTipoComprobanteInvalido, got %v", err)
	}
}

func TestCrearEnvio_ReferenciaVacia(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.Referencia = "  "
	_, err := domain.CrearEnvio(p, now)
	if !errors.Is(err, domain.ErrEnvioReferenciaRequerido) {
		t.Fatalf("expected ErrEnvioReferenciaRequerido, got %v", err)
	}
}

func TestCrearEnvio_ClienteIDCero(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.ClienteID = 0
	_, err := domain.CrearEnvio(p, now)
	if !errors.Is(err, domain.ErrEnvioClienteIDInvalido) {
		t.Fatalf("expected ErrEnvioClienteIDInvalido, got %v", err)
	}
}

func TestCrearEnvio_ClienteIDNegativo(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.ClienteID = -5
	_, err := domain.CrearEnvio(p, now)
	if !errors.Is(err, domain.ErrEnvioClienteIDInvalido) {
		t.Fatalf("expected ErrEnvioClienteIDInvalido, got %v", err)
	}
}

func TestCrearEnvio_CanalInvalido(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.Canal = "invalido"
	_, err := domain.CrearEnvio(p, now)
	if !errors.Is(err, domain.ErrCanalInvalido) {
		t.Fatalf("expected ErrCanalInvalido, got %v", err)
	}
}

func TestCrearEnvio_ReferenciaTrimeada(t *testing.T) {
	t.Parallel()
	p := crearParamsValido()
	p.Referencia = "  VENTA-123  "
	e, err := domain.CrearEnvio(p, now)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if e.Referencia() != "VENTA-123" {
		t.Fatalf("Referencia() = %q, want %q", e.Referencia(), "VENTA-123")
	}
}

// --- HydrateEnvio ---

func TestHydrateEnvio_BypassesValidation(t *testing.T) {
	t.Parallel()
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
	if e.ClienteID() != 0 {
		t.Fatalf("ClienteID() = %d, want 0", e.ClienteID())
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

// buildFromEstado genera un envío en el estado dado para tests de transición.
func buildFromEstado(t *testing.T, estado domain.EstadoEnvio) *domain.Envio {
	t.Helper()
	switch estado {
	case domain.EstadoEnvioEnEspera:
		return crearEnvioEnEspera(t)
	case domain.EstadoEnvioSinTelefono:
		p := crearParamsValido()
		p.Telefono = nil
		e, err := domain.CrearEnvio(p, now)
		if err != nil {
			t.Fatalf("CrearEnvio (sin_telefono) failed: %v", err)
		}
		return e
	case domain.EstadoEnvioEnviando:
		e := crearEnvioEnEspera(t)
		if err := e.Reclamar(now); err != nil {
			t.Fatalf("Reclamar failed: %v", err)
		}
		return e
	case domain.EstadoEnvioEnviado:
		e := crearEnvioEnEspera(t)
		e.Reclamar(now)
		if err := e.MarcarEnviado("msg", now); err != nil {
			t.Fatalf("MarcarEnviado failed: %v", err)
		}
		return e
	case domain.EstadoEnvioFallido:
		e := crearEnvioEnEspera(t)
		e.Reclamar(now)
		if err := e.MarcarFallido("reason", now); err != nil {
			t.Fatalf("MarcarFallido failed: %v", err)
		}
		return e
	case domain.EstadoEnvioDetenido:
		e := crearEnvioEnEspera(t)
		if err := e.Detener("admin", now); err != nil {
			t.Fatalf("Detener failed: %v", err)
		}
		return e
	default:
		t.Fatalf("buildFromEstado: unknown estado %s", estado)
		return nil
	}
}

func TestReclamar(t *testing.T) {
	t.Parallel()

	// Happy path: en_espera → enviando
	t.Run("desde_en_espera", func(t *testing.T) {
		t.Parallel()
		e := crearEnvioEnEspera(t)
		if err := e.Reclamar(now); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if e.Estado() != domain.EstadoEnvioEnviando {
			t.Fatalf("Estado() = %v, want enviando", e.Estado())
		}
	})

	// Invalid: all other states
	for _, estado := range []domain.EstadoEnvio{
		domain.EstadoEnvioSinTelefono,
		domain.EstadoEnvioEnviando,
		domain.EstadoEnvioEnviado,
		domain.EstadoEnvioFallido,
		domain.EstadoEnvioDetenido,
	} {
		estado := estado
		t.Run("desde_"+string(estado), func(t *testing.T) {
			t.Parallel()
			e := buildFromEstado(t, estado)
			before := e.Estado()
			if err := e.Reclamar(now); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
				t.Fatalf("expected ErrEnvioTransicionInvalido from %s, got %v", estado, err)
			}
			if e.Estado() != before {
				t.Fatalf("estado mutated from %s to %v", before, e.Estado())
			}
		})
	}
}

func TestMarcarEnviado(t *testing.T) {
	t.Parallel()

	// Happy path: enviando → enviado
	t.Run("desde_enviando", func(t *testing.T) {
		t.Parallel()
		e := crearEnvioEnEspera(t)
		e.Reclamar(now)
		enviadoEn := now.Add(time.Minute)
		if err := e.MarcarEnviado("msg-abc", enviadoEn); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if e.Estado() != domain.EstadoEnvioEnviado {
			t.Fatalf("Estado() = %v, want enviado", e.Estado())
		}
		if e.MensajeExternoID() == nil || *e.MensajeExternoID() != "msg-abc" {
			t.Fatalf("MensajeExternoID() = %v, want msg-abc", e.MensajeExternoID())
		}
		if e.EnviadoEn() == nil || !e.EnviadoEn().Equal(enviadoEn) {
			t.Fatalf("EnviadoEn() = %v, want %v", e.EnviadoEn(), enviadoEn)
		}
	})

	// Invalid: all other states
	for _, estado := range []domain.EstadoEnvio{
		domain.EstadoEnvioEnEspera,
		domain.EstadoEnvioSinTelefono,
		domain.EstadoEnvioEnviado,
		domain.EstadoEnvioFallido,
		domain.EstadoEnvioDetenido,
	} {
		estado := estado
		t.Run("desde_"+string(estado), func(t *testing.T) {
			t.Parallel()
			e := buildFromEstado(t, estado)
			before := e.Estado()
			if err := e.MarcarEnviado("msg", now); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
				t.Fatalf("expected ErrEnvioTransicionInvalido from %s, got %v", estado, err)
			}
			if e.Estado() != before {
				t.Fatalf("estado mutated from %s to %v", before, e.Estado())
			}
		})
	}
}

func TestMarcarFallido(t *testing.T) {
	t.Parallel()

	// Happy path: enviando → fallido
	t.Run("desde_enviando", func(t *testing.T) {
		t.Parallel()
		e := crearEnvioEnEspera(t)
		e.Reclamar(now)
		if err := e.MarcarFallido("channel rejected", now); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if e.Estado() != domain.EstadoEnvioFallido {
			t.Fatalf("Estado() = %v, want fallido", e.Estado())
		}
		if e.UltimoError() == nil || *e.UltimoError() != "channel rejected" {
			t.Fatalf("UltimoError() = %v", e.UltimoError())
		}
	})

	// Invalid: all other states
	for _, estado := range []domain.EstadoEnvio{
		domain.EstadoEnvioEnEspera,
		domain.EstadoEnvioSinTelefono,
		domain.EstadoEnvioEnviado,
		domain.EstadoEnvioFallido,
		domain.EstadoEnvioDetenido,
	} {
		estado := estado
		t.Run("desde_"+string(estado), func(t *testing.T) {
			t.Parallel()
			e := buildFromEstado(t, estado)
			before := e.Estado()
			if err := e.MarcarFallido("reason", now); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
				t.Fatalf("expected ErrEnvioTransicionInvalido from %s, got %v", estado, err)
			}
			if e.Estado() != before {
				t.Fatalf("estado mutated from %s to %v", before, e.Estado())
			}
		})
	}
}

func TestReenviar(t *testing.T) {
	t.Parallel()

	// Happy path: fallido → en_espera
	t.Run("desde_fallido", func(t *testing.T) {
		t.Parallel()
		e := crearEnvioEnEspera(t)
		e.Reclamar(now)
		e.MarcarFallido("timeout", now)
		if err := e.Reenviar(now); err != nil {
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
	})

	// Reenviar increments intentos across cycles
	t.Run("incrementa_intentos", func(t *testing.T) {
		t.Parallel()
		e := crearEnvioEnEspera(t)
		for range 3 {
			e.Reclamar(now)
			e.MarcarFallido("reason", now)
			e.Reenviar(now)
		}
		if e.Intentos() != 3 {
			t.Fatalf("Intentos() = %d, want 3", e.Intentos())
		}
	})

	// Invalid: all other states
	for _, estado := range []domain.EstadoEnvio{
		domain.EstadoEnvioEnEspera,
		domain.EstadoEnvioSinTelefono,
		domain.EstadoEnvioEnviando,
		domain.EstadoEnvioEnviado,
		domain.EstadoEnvioDetenido,
	} {
		estado := estado
		t.Run("desde_"+string(estado), func(t *testing.T) {
			t.Parallel()
			e := buildFromEstado(t, estado)
			before := e.Estado()
			if err := e.Reenviar(now); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
				t.Fatalf("expected ErrEnvioTransicionInvalido from %s, got %v", estado, err)
			}
			if e.Estado() != before {
				t.Fatalf("estado mutated from %s to %v", before, e.Estado())
			}
		})
	}
}

func TestDetener(t *testing.T) {
	t.Parallel()

	// Happy path: en_espera → detenido
	t.Run("desde_en_espera", func(t *testing.T) {
		t.Parallel()
		e := crearEnvioEnEspera(t)
		if err := e.Detener("admin", now); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if e.Estado() != domain.EstadoEnvioDetenido {
			t.Fatalf("Estado() = %v, want detenido", e.Estado())
		}
		if e.DetenidoPor() == nil || *e.DetenidoPor() != "admin" {
			t.Fatalf("DetenidoPor() = %v", e.DetenidoPor())
		}
	})

	// Invalid: all other states
	for _, estado := range []domain.EstadoEnvio{
		domain.EstadoEnvioSinTelefono,
		domain.EstadoEnvioEnviando,
		domain.EstadoEnvioEnviado,
		domain.EstadoEnvioFallido,
		domain.EstadoEnvioDetenido,
	} {
		estado := estado
		t.Run("desde_"+string(estado), func(t *testing.T) {
			t.Parallel()
			e := buildFromEstado(t, estado)
			before := e.Estado()
			if err := e.Detener("admin", now); !errors.Is(err, domain.ErrEnvioTransicionInvalido) {
				t.Fatalf("expected ErrEnvioTransicionInvalido from %s, got %v", estado, err)
			}
			if e.Estado() != before {
				t.Fatalf("estado mutated from %s to %v", before, e.Estado())
			}
		})
	}
}
