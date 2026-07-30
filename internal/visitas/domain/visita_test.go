package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// fixedNow is the reference "now" used across tests — never time.Now(), per
// docs/module-standards/DATETIME_HANDLING.md.
var fixedNow = time.Date(2026, 7, 29, 16, 0, 0, 0, time.UTC)

func validParams() domain.CrearVisitaParams {
	impte := 918273
	return domain.CrearVisitaParams{
		ID:             uuid.MustParse("6f2a5c2e-2c8b-4c1a-9b7e-1a2b3c4d5e6f"),
		Cobrador:       "María de los Ángeles Hernández",
		CobradorID:     501,
		Fecha:          time.Date(2026, 7, 29, 15, 30, 0, 0, time.UTC),
		FormaCobroID:   1,
		Lat:            19.0414,
		Lng:            -98.2063,
		Nota:           "salió, vuelvo mañana",
		TipoVisita:     "cobro",
		ZonaClienteID:  12,
		ClienteID:      24037,
		ImpteDoctoCCID: &impte,
		CreatedBy:      uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Now:            fixedNow,
	}
}

func TestNewVisita_ValidConstructionRoundTripsThroughGetters(t *testing.T) {
	t.Parallel()

	p := validParams()
	v, err := domain.NewVisita(p)
	if err != nil {
		t.Fatalf("NewVisita() unexpected error: %v", err)
	}

	if got := v.ID(); got != p.ID {
		t.Errorf("ID() = %v, want %v", got, p.ID)
	}
	if got := v.Cobrador(); got != p.Cobrador {
		t.Errorf("Cobrador() = %q, want %q", got, p.Cobrador)
	}
	if got := v.CobradorID(); got != p.CobradorID {
		t.Errorf("CobradorID() = %d, want %d", got, p.CobradorID)
	}
	if got := v.Fecha(); !got.Equal(p.Fecha) {
		t.Errorf("Fecha() = %v, want %v", got, p.Fecha)
	}
	if got := v.FormaCobroID(); got != p.FormaCobroID {
		t.Errorf("FormaCobroID() = %d, want %d", got, p.FormaCobroID)
	}
	if got := v.Lat(); got != p.Lat {
		t.Errorf("Lat() = %v, want %v", got, p.Lat)
	}
	if got := v.Lng(); got != p.Lng {
		t.Errorf("Lng() = %v, want %v", got, p.Lng)
	}
	if got := v.Nota(); got != p.Nota {
		t.Errorf("Nota() = %q, want %q", got, p.Nota)
	}
	if got := v.TipoVisita(); got != p.TipoVisita {
		t.Errorf("TipoVisita() = %q, want %q", got, p.TipoVisita)
	}
	if got := v.ZonaClienteID(); got != p.ZonaClienteID {
		t.Errorf("ZonaClienteID() = %d, want %d", got, p.ZonaClienteID)
	}
	if got := v.ClienteID(); got != p.ClienteID {
		t.Errorf("ClienteID() = %d, want %d", got, p.ClienteID)
	}
	if got := v.ImpteDoctoCCID(); got == nil || *got != *p.ImpteDoctoCCID {
		t.Errorf("ImpteDoctoCCID() = %v, want %v", got, p.ImpteDoctoCCID)
	}
	a := v.Audit()
	if got := a.CreatedAt(); !got.Equal(p.Now) {
		t.Errorf("Audit().CreatedAt() = %v, want %v", got, p.Now)
	}
	if got := a.CreatedBy(); got != p.CreatedBy {
		t.Errorf("Audit().CreatedBy() = %v, want %v", got, p.CreatedBy)
	}
}

func TestNewVisita_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p domain.CrearVisitaParams) domain.CrearVisitaParams
		wantErr error
	}{
		{
			name: "id zero value",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.ID = uuid.Nil
				return p
			},
			wantErr: domain.ErrVisitaIDRequerido,
		},
		{
			name: "cliente id zero",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.ClienteID = 0
				return p
			},
			wantErr: domain.ErrVisitaClienteRequerido,
		},
		{
			name: "cliente id negative",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.ClienteID = -5
				return p
			},
			wantErr: domain.ErrVisitaClienteRequerido,
		},
		{
			name: "cobrador blank",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Cobrador = "   "
				return p
			},
			wantErr: domain.ErrVisitaCobradorRequerido,
		},
		{
			name: "cobrador too long",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Cobrador = strings.Repeat("a", 151)
				return p
			},
			wantErr: domain.ErrVisitaCobradorDemasiadoLargo,
		},
		{
			name: "tipo visita blank",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.TipoVisita = ""
				return p
			},
			wantErr: domain.ErrVisitaTipoRequerido,
		},
		{
			name: "tipo visita too long",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.TipoVisita = strings.Repeat("b", 101)
				return p
			},
			wantErr: domain.ErrVisitaTipoDemasiadoLargo,
		},
		{
			name: "nota too long",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Nota = strings.Repeat("c", 10001)
				return p
			},
			wantErr: domain.ErrVisitaNotaDemasiadoLarga,
		},
		{
			name: "fecha zero value",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Fecha = time.Time{}
				return p
			},
			wantErr: domain.ErrVisitaFechaRequerida,
		},
		{
			name: "fecha futura mas alla de la tolerancia",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Fecha = p.Now.Add(72 * time.Hour)
				return p
			},
			wantErr: domain.ErrVisitaFechaFutura,
		},
		{
			name: "cobrador con NUL invalido",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Cobrador = "Juan\x00Pérez"
				return p
			},
			wantErr: domain.ErrVisitaStringCaracteresInvalidos,
		},
		{
			name: "cobrador con caracter de control ASCII invalido",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Cobrador = "Juan\x01Pérez"
				return p
			},
			wantErr: domain.ErrVisitaStringCaracteresInvalidos,
		},
		{
			name: "cobrador con DEL invalido",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Cobrador = "Juan\x7fPérez"
				return p
			},
			wantErr: domain.ErrVisitaStringCaracteresInvalidos,
		},
		{
			name: "cobrador con bytes UTF-8 invalidos",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Cobrador = "\xff\xfe"
				return p
			},
			wantErr: domain.ErrVisitaStringCaracteresInvalidos,
		},
		{
			name: "nota con caracter de control invalido",
			mutate: func(p domain.CrearVisitaParams) domain.CrearVisitaParams {
				p.Nota = "salió\x01, vuelvo mañana"
				return p
			},
			wantErr: domain.ErrVisitaStringCaracteresInvalidos,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := tc.mutate(validParams())
			v, err := domain.NewVisita(p)
			if v != nil {
				t.Errorf("NewVisita() returned non-nil Visita alongside error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewVisita() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewVisita_FechaDentroDeTolerancia_Aceptada(t *testing.T) {
	t.Parallel()

	p := validParams()
	// Justo en el límite de la tolerancia (48h) — no debe rechazarse.
	p.Fecha = p.Now.Add(48 * time.Hour)

	if _, err := domain.NewVisita(p); err != nil {
		t.Fatalf("NewVisita() unexpected error at tolerance boundary: %v", err)
	}
}

func TestNewVisita_FechaAntigua_Aceptada(t *testing.T) {
	t.Parallel()

	// Una visita capturada offline y subida meses después no debe rechazarse
	// por antigüedad — solo se rechazan fechas futuras (ver doc comment de
	// fechaFuturaTolerancia).
	p := validParams()
	p.Fecha = time.Date(2020, 1, 15, 9, 0, 0, 0, time.UTC)

	v, err := domain.NewVisita(p)
	if err != nil {
		t.Fatalf("NewVisita() unexpected error for old fecha: %v", err)
	}
	if !v.Fecha().Equal(p.Fecha) {
		t.Errorf("Fecha() = %v, want %v", v.Fecha(), p.Fecha)
	}
}

func TestNewVisita_NotaOpcional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nota     string
		wantNota string
	}{
		{name: "vacia permanece vacia", nota: "", wantNota: ""},
		{name: "solo espacios se normaliza a vacia", nota: "   ", wantNota: ""},
		{name: "con contenido se conserva trimeada", nota: "  no había nadie, regreso mañana  ", wantNota: "no había nadie, regreso mañana"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := validParams()
			p.Nota = tc.nota
			v, err := domain.NewVisita(p)
			if err != nil {
				t.Fatalf("NewVisita() unexpected error: %v", err)
			}
			if got := v.Nota(); got != tc.wantNota {
				t.Errorf("Nota() = %q, want %q", got, tc.wantNota)
			}
		})
	}
}

func TestNewVisita_ImpteDoctoCCID_NilOSet(t *testing.T) {
	t.Parallel()

	t.Run("nil cuando la visita no generó cobro", func(t *testing.T) {
		t.Parallel()

		p := validParams()
		p.ImpteDoctoCCID = nil
		v, err := domain.NewVisita(p)
		if err != nil {
			t.Fatalf("NewVisita() unexpected error: %v", err)
		}
		if got := v.ImpteDoctoCCID(); got != nil {
			t.Errorf("ImpteDoctoCCID() = %v, want nil", got)
		}
	})

	t.Run("set cuando la visita generó cobro aplicado", func(t *testing.T) {
		t.Parallel()

		p := validParams()
		impte := 555111
		p.ImpteDoctoCCID = &impte
		v, err := domain.NewVisita(p)
		if err != nil {
			t.Fatalf("NewVisita() unexpected error: %v", err)
		}
		if got := v.ImpteDoctoCCID(); got == nil || *got != impte {
			t.Errorf("ImpteDoctoCCID() = %v, want %d", got, impte)
		}
	})
}

func TestNewVisita_NormalizaNFC(t *testing.T) {
	t.Parallel()

	// "México" con "e" descompuesta (e U+0065 + acento combinante
	// U+0301) debe normalizarse a la forma compuesta (NFC, U+00E9) para que
	// las comparaciones byte-a-byte funcionen tras persistir/leer.
	decomposed := "no encontramos al cliente en México"
	composed := "no encontramos al cliente en México"

	p := validParams()
	p.Nota = decomposed
	v, err := domain.NewVisita(p)
	if err != nil {
		t.Fatalf("NewVisita() unexpected error: %v", err)
	}
	if got := v.Nota(); got != composed {
		t.Errorf("Nota() = %q (not NFC-normalized), want %q", got, composed)
	}
}

func TestNewVisita_PermiteTabSaltoDeLineaYRetornoDeCarro(t *testing.T) {
	t.Parallel()

	// Tab, LF y CR son los únicos caracteres de control que un cobrador
	// puede legítimamente escribir en una nota de texto libre.
	p := validParams()
	p.Nota = "salió\tno estaba\nregreso\rmañana"

	v, err := domain.NewVisita(p)
	if err != nil {
		t.Fatalf("NewVisita() unexpected error: %v", err)
	}
	if got, want := v.Nota(), "salió\tno estaba\nregreso\rmañana"; got != want {
		t.Errorf("Nota() = %q, want %q", got, want)
	}
}

func TestNewVisita_CamposOpcionalesEnCero(t *testing.T) {
	t.Parallel()

	// CobradorID, FormaCobroID y ZonaClienteID no son obligatorios: una
	// visita nueva puede no tener aún el cobrador_id resuelto en Microsip, o
	// no haber producido cobro (forma_cobro_id), o no tener zona conocida.
	p := validParams()
	p.CobradorID = 0
	p.FormaCobroID = 0
	p.ZonaClienteID = 0

	v, err := domain.NewVisita(p)
	if err != nil {
		t.Fatalf("NewVisita() unexpected error: %v", err)
	}
	if got := v.CobradorID(); got != 0 {
		t.Errorf("CobradorID() = %d, want 0", got)
	}
	if got := v.FormaCobroID(); got != 0 {
		t.Errorf("FormaCobroID() = %d, want 0", got)
	}
	if got := v.ZonaClienteID(); got != 0 {
		t.Errorf("ZonaClienteID() = %d, want 0", got)
	}
}

func TestRehydrateVisita_RoundTripsFieldsAndZeroesAudit(t *testing.T) {
	t.Parallel()

	impte := 42
	p := domain.RehydrateVisitaParams{
		ID:             uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
		Cobrador:       "Rosa Isela Martínez del Campo",
		CobradorID:     77,
		Fecha:          time.Date(2025, 3, 10, 8, 15, 0, 0, time.UTC),
		FormaCobroID:   2,
		Lat:            18.9186,
		Lng:            -97.3878,
		Nota:           "cliente no encontrado, casa cerrada",
		TipoVisita:     "no_encontrado",
		ZonaClienteID:  7,
		ClienteID:      9981,
		ImpteDoctoCCID: &impte,
	}

	v := domain.RehydrateVisita(p)

	if got := v.ID(); got != p.ID {
		t.Errorf("ID() = %v, want %v", got, p.ID)
	}
	if got := v.Cobrador(); got != p.Cobrador {
		t.Errorf("Cobrador() = %q, want %q", got, p.Cobrador)
	}
	if got := v.CobradorID(); got != p.CobradorID {
		t.Errorf("CobradorID() = %d, want %d", got, p.CobradorID)
	}
	if got := v.Fecha(); !got.Equal(p.Fecha) {
		t.Errorf("Fecha() = %v, want %v", got, p.Fecha)
	}
	if got := v.FormaCobroID(); got != p.FormaCobroID {
		t.Errorf("FormaCobroID() = %d, want %d", got, p.FormaCobroID)
	}
	if got := v.Lat(); got != p.Lat {
		t.Errorf("Lat() = %v, want %v", got, p.Lat)
	}
	if got := v.Lng(); got != p.Lng {
		t.Errorf("Lng() = %v, want %v", got, p.Lng)
	}
	if got := v.Nota(); got != p.Nota {
		t.Errorf("Nota() = %q, want %q", got, p.Nota)
	}
	if got := v.TipoVisita(); got != p.TipoVisita {
		t.Errorf("TipoVisita() = %q, want %q", got, p.TipoVisita)
	}
	if got := v.ZonaClienteID(); got != p.ZonaClienteID {
		t.Errorf("ZonaClienteID() = %d, want %d", got, p.ZonaClienteID)
	}
	if got := v.ClienteID(); got != p.ClienteID {
		t.Errorf("ClienteID() = %d, want %d", got, p.ClienteID)
	}
	if got := v.ImpteDoctoCCID(); got == nil || *got != *p.ImpteDoctoCCID {
		t.Errorf("ImpteDoctoCCID() = %v, want %v", got, p.ImpteDoctoCCID)
	}

	// MSP_VISITAS no tiene columnas de auditoría: RehydrateVisita siempre
	// deja el subrecord en su valor cero — no hay nada que rehidratar.
	a := v.Audit()
	if got := a.CreatedAt(); !got.IsZero() {
		t.Errorf("Audit().CreatedAt() = %v, want zero value", got)
	}
	if got := a.UpdatedAt(); !got.IsZero() {
		t.Errorf("Audit().UpdatedAt() = %v, want zero value", got)
	}
	if got := a.CreatedBy(); got != uuid.Nil {
		t.Errorf("Audit().CreatedBy() = %v, want uuid.Nil", got)
	}
	if got := a.UpdatedBy(); got != uuid.Nil {
		t.Errorf("Audit().UpdatedBy() = %v, want uuid.Nil", got)
	}
}
