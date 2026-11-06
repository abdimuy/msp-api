package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Pruebas del contrato de las tareas 9 y 10: los tipos, campos y centinelas
// que las dos tareas comparten. Las reglas (firma obligatoria, auditoría GPS,
// cálculo de la huella) NO viven aquí: son de la tarea 9.

// huellaEjemplo is the lowercase hex of a SHA-256, the only shape HUELLA takes.
const huellaEjemplo = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func TestTipoImagen_ValoresYParse(t *testing.T) {
	t.Parallel()
	casos := map[domain.TipoImagen]string{
		domain.TipoImagenEvidencia: "evidencia",
		domain.TipoImagenFirma:     "firma",
	}
	for tipo, wire := range casos {
		if tipo.String() != wire {
			t.Errorf("String() = %q, want %q", tipo.String(), wire)
		}
		got, err := domain.ParseTipoImagen(wire)
		if err != nil || got != tipo {
			t.Errorf("ParseTipoImagen(%q) = %q, %v", wire, got, err)
		}
	}
	for _, malo := range []string{"", "Firma", "foto", " firma"} {
		got, err := domain.ParseTipoImagen(malo)
		if !errors.Is(err, domain.ErrTipoImagenInvalido) || got != "" {
			t.Errorf("ParseTipoImagen(%q) = %q, %v; want ErrTipoImagenInvalido", malo, got, err)
		}
	}
}

func TestAuditoriaGPS_ValoresYParse(t *testing.T) {
	t.Parallel()
	casos := map[domain.AuditoriaGPS]string{
		domain.AuditoriaGPSSinDesvio: "sin_desvio",
		domain.AuditoriaGPSConDesvio: "con_desvio",
		domain.AuditoriaGPSNoAplica:  "no_aplica",
	}
	for a, wire := range casos {
		if a.String() != wire {
			t.Errorf("String() = %q, want %q", a.String(), wire)
		}
		if len(wire) > 20 {
			t.Errorf("%q no cabe en VARCHAR(20)", wire)
		}
		got, err := domain.ParseAuditoriaGPS(wire)
		if err != nil || got != a {
			t.Errorf("ParseAuditoriaGPS(%q) = %q, %v", wire, got, err)
		}
	}
	for _, malo := range []string{"", "desvio", "NO_APLICA"} {
		got, err := domain.ParseAuditoriaGPS(malo)
		if !errors.Is(err, domain.ErrAuditoriaGPSInvalida) || got != "" {
			t.Errorf("ParseAuditoriaGPS(%q) = %q, %v; want ErrAuditoriaGPSInvalida", malo, got, err)
		}
	}
}

func imagenParams() domain.NewImagenParams {
	return domain.NewImagenParams{
		EventoID:  uuid.New(),
		Tipo:      domain.TipoImagenEvidencia,
		Ruta:      "garantias/ga-000001/foto.jpg",
		SubidaPor: "juan",
		CreatedAt: fixed,
	}
}

func TestNewImagen_TipoObligatorio(t *testing.T) {
	t.Parallel()
	p := imagenParams()
	p.Tipo = ""
	img, err := domain.NewImagen(p)
	if !errors.Is(err, domain.ErrTipoImagenInvalido) || img != nil {
		t.Fatalf("NewImagen sin tipo = %v, %v; want ErrTipoImagenInvalido", img, err)
	}
	p.Tipo = domain.TipoImagenFirma
	img, err = domain.NewImagen(p)
	if err != nil {
		t.Fatalf("NewImagen firma: %v", err)
	}
	if img.Tipo() != domain.TipoImagenFirma {
		t.Errorf("Tipo() = %q, want firma", img.Tipo())
	}
}

func TestNewImagen_IDOpcional(t *testing.T) {
	t.Parallel()
	p := imagenParams()
	generada, err := domain.NewImagen(p)
	if err != nil {
		t.Fatalf("NewImagen: %v", err)
	}
	if generada.ID() == uuid.Nil {
		t.Error("sin ID explícito, NewImagen debe generar uno")
	}

	id := uuid.New()
	p.ID = &id
	explicita, err := domain.NewImagen(p)
	if err != nil {
		t.Fatalf("NewImagen con ID: %v", err)
	}
	if explicita.ID() != id {
		t.Errorf("ID() = %v, want %v (el ID explícito se respeta)", explicita.ID(), id)
	}

	nulo := uuid.Nil
	p.ID = &nulo
	img, err := domain.NewImagen(p)
	if !errors.Is(err, domain.ErrImagenIDInvalido) || img != nil {
		t.Errorf("NewImagen con uuid.Nil = %v, %v; want ErrImagenIDInvalido", img, err)
	}
}

func TestNewImagen_Huella(t *testing.T) {
	t.Parallel()
	p := imagenParams()
	p.Huella = huellaEjemplo
	img, err := domain.NewImagen(p)
	if err != nil {
		t.Fatalf("NewImagen: %v", err)
	}
	if img.Huella() != huellaEjemplo {
		t.Errorf("Huella() = %q, want %q", img.Huella(), huellaEjemplo)
	}
	for _, mala := range huellasInvalidas() {
		p.Huella = mala
		img, err := domain.NewImagen(p)
		if !errors.Is(err, domain.ErrHuellaInvalida) || img != nil {
			t.Errorf("NewImagen(huella %q) = %v, %v; want ErrHuellaInvalida", mala, img, err)
		}
	}
}

func huellasInvalidas() []string {
	return []string{
		huellaEjemplo[:63],
		huellaEjemplo + "0",
		strings.ToUpper(huellaEjemplo),
		"g" + huellaEjemplo[1:],
		"/" + huellaEjemplo[1:],
		":" + huellaEjemplo[1:],
		"`" + huellaEjemplo[1:],
	}
}

func TestHydrateImagen_TipoYHuella(t *testing.T) {
	t.Parallel()
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:       uuid.New(),
		EventoID: uuid.New(),
		Tipo:     domain.TipoImagenFirma,
		Ruta:     "garantias/firma.png",
		Huella:   huellaEjemplo,
	})
	if img.Tipo() != domain.TipoImagenFirma || img.Huella() != huellaEjemplo {
		t.Errorf("HydrateImagen perdió tipo o huella: %q %q", img.Tipo(), img.Huella())
	}
}

func TestActorHuella_LlegaAlEvento(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	a := actor("juan")
	a.ClaveIdempotencia = uuid.NewString()
	a.Huella = huellaEjemplo
	if err := g.AgregarArticulo(domain.AgregarArticuloParams{Clave: "SOF-1", Description: "sofá"}, a, fixed); err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}
	if got := lastEvent(t, g).Huella(); got != huellaEjemplo {
		t.Errorf("Huella() = %q, want %q", got, huellaEjemplo)
	}
}

func TestActorHuellaInvalida_NoEncolaNada(t *testing.T) {
	t.Parallel()
	for _, mala := range huellasInvalidas() {
		g := openPiso(t)
		antes := countPending(g)
		a := actor("juan")
		a.ClaveIdempotencia = uuid.NewString()
		a.Huella = mala
		err := g.AgregarArticulo(domain.AgregarArticuloParams{Clave: "SOF-1", Description: "sofá"}, a, fixed)
		if !errors.Is(err, domain.ErrHuellaInvalida) {
			t.Errorf("huella %q: err = %v, want ErrHuellaInvalida", mala, err)
		}
		if countPending(g) != antes || g.ArticulosCount() != 0 {
			t.Errorf("huella %q: el rechazo dejó estado o eventos", mala)
		}
	}
}

func TestHydrateEvento_CamposDelContrato(t *testing.T) {
	t.Parallel()
	dist := 412.5
	aud := domain.AuditoriaGPSConDesvio
	e := domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                uuid.New(),
		GarantiaID:        uuid.New(),
		Tipo:              domain.TipoEventoFolioCerrado,
		Usuario:           "juan",
		ClaveIdempotencia: claveValida,
		Huella:            huellaEjemplo,
		DistanciaEntregaM: &dist,
		AuditoriaGPS:      &aud,
	})
	if e.Huella() != huellaEjemplo {
		t.Errorf("Huella() = %q", e.Huella())
	}
	if e.DistanciaEntregaM() == nil || *e.DistanciaEntregaM() != dist {
		t.Errorf("DistanciaEntregaM() = %v, want %v", e.DistanciaEntregaM(), dist)
	}
	if e.AuditoriaGPS() == nil || *e.AuditoriaGPS() != aud {
		t.Errorf("AuditoriaGPS() = %v, want %v", e.AuditoriaGPS(), aud)
	}
}

func TestEventoNuevo_SinAuditoriaPorOmision(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	e := lastEvent(t, g)
	if e.DistanciaEntregaM() != nil || e.AuditoriaGPS() != nil || e.Huella() != "" {
		t.Errorf("un evento sin auditoría ni huella debe dejarlas vacías: %v %v %q",
			e.DistanciaEntregaM(), e.AuditoriaGPS(), e.Huella())
	}
}

func TestImagenesPendientesForRepo_VaciaYCopia(t *testing.T) {
	t.Parallel()
	g := openPiso(t)
	if n := len(g.ImagenesPendientesForRepo()); n != 0 {
		t.Fatalf("un folio recién abierto no tiene imágenes pendientes, tiene %d", n)
	}
	copia := g.ImagenesPendientesForRepo()
	copia = append(copia, &domain.Imagen{})
	if len(copia) != 1 || len(g.ImagenesPendientesForRepo()) != 0 {
		t.Error("ImagenesPendientesForRepo debe devolver una copia")
	}
}

func TestCentinelasDelContrato(t *testing.T) {
	t.Parallel()
	casos := []struct {
		err  error
		kind apperror.Kind
		code string
	}{
		{domain.ErrTipoImagenInvalido, apperror.KindValidation, "warranty_image_type_invalid"},
		{domain.ErrAuditoriaGPSInvalida, apperror.KindValidation, "warranty_gps_audit_invalid"},
		{domain.ErrHuellaInvalida, apperror.KindValidation, "warranty_fingerprint_invalid"},
		{domain.ErrImagenIDInvalido, apperror.KindValidation, "warranty_image_id_invalid"},
		{domain.ErrImagenDuplicada, apperror.KindConflict, "warranty_image_duplicate"},
		{domain.ErrImagenEventoAjeno, apperror.KindNotFound, "warranty_image_event_not_in_folio"},
		{domain.ErrImagenNoEncontrada, apperror.KindNotFound, "warranty_image_not_found"},
		{domain.ErrImagenVacia, apperror.KindValidation, "warranty_image_empty"},
		{domain.ErrImagenFormatoInvalido, apperror.KindValidation, "warranty_image_format_invalid"},
		{domain.ErrImagenMuyGrande, apperror.KindValidation, "warranty_image_too_large"},
		{domain.ErrFirmaRequerida, apperror.KindValidation, "warranty_signature_required"},
		{domain.ErrJustificacionDesvioRequerida, apperror.KindValidation, "warranty_deviation_justification_required"},
	}
	for _, c := range casos {
		var ae *apperror.Error
		if !errors.As(c.err, &ae) {
			t.Fatalf("%v no es *apperror.Error", c.err)
		}
		if ae.Kind != c.kind || ae.Code != c.code {
			t.Errorf("%s: kind=%v code=%q, want kind=%v code=%q", c.code, ae.Kind, ae.Code, c.kind, c.code)
		}
		if ae.Message == "" || strings.HasSuffix(ae.Message, ".") || ae.Message != strings.ToLower(ae.Message) {
			t.Errorf("%s: el mensaje debe ir en minúsculas y sin punto final: %q", c.code, ae.Message)
		}
	}
}
