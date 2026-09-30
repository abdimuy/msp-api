package domain_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestNewImagen(t *testing.T) {
	t.Parallel()
	eventoID := uuid.New()
	img, err := domain.NewImagen(domain.NewImagenParams{
		EventoID:    eventoID,
		Ruta:        "garantias/2026/08/abc-123.jpg",
		Descripcion: "  mueble recién recogido  ",
		SubidaPor:   "  juan  ",
		CreatedAt:   fixed,
	})
	if err != nil {
		t.Fatalf("NewImagen: %v", err)
	}
	if img.ID() == uuid.Nil {
		t.Error("ID() = uuid.Nil, want generated")
	}
	if img.EventoID() != eventoID {
		t.Errorf("EventoID() = %v, want %v", img.EventoID(), eventoID)
	}
	if img.Ruta() != "garantias/2026/08/abc-123.jpg" {
		t.Errorf("Ruta() = %q, want trimmed relative path", img.Ruta())
	}
	if img.Descripcion() != "mueble recién recogido" {
		t.Errorf("Descripcion() = %q, want trimmed", img.Descripcion())
	}
	if img.SubidaPor() != "juan" {
		t.Errorf("SubidaPor() = %q, want %q", img.SubidaPor(), "juan")
	}
	if !img.CreatedAt().Equal(fixed) {
		t.Errorf("CreatedAt() = %v, want %v", img.CreatedAt(), fixed)
	}
}

func TestNewImagen_DescripcionVacia(t *testing.T) {
	t.Parallel()
	img, err := domain.NewImagen(domain.NewImagenParams{
		EventoID:  uuid.New(),
		Ruta:      "garantias/x.jpg",
		SubidaPor: "juan",
		CreatedAt: fixed,
	})
	if err != nil {
		t.Fatalf("NewImagen: %v", err)
	}
	if img.Descripcion() != "" {
		t.Errorf("Descripcion() = %q, want empty", img.Descripcion())
	}
}

// TestNewImagen_RutaInvalida covers the three shapes the domain refuses: an
// empty path, an absolute one (POSIX and Windows drive), and a ".." that
// escapes STORAGE_DIR. The check is textual on purpose, so the Windows forms
// are tested on every platform.
func TestNewImagen_RutaInvalida(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		ruta   string
	}{
		{"vacia", ""},
		{"solo_espacios", "   "},
		{"absoluta_unix", "/etc/passwd"},
		{"absoluta_windows", `C:\Users\Public\foto.jpg`},
		{"absoluta_windows_barra", `C:/fotos/foto.jpg`},
		{"raiz_barra", "/foto.jpg"},
		{"raiz_barra_inversa", `\foto.jpg`},
		{"sube_un_nivel", "../fuera.jpg"},
		{"sube_al_ultimo", "garantias/../../fuera.jpg"},
		{"sube_con_barra_inversa", `garantias\..\..\fuera.jpg`},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			img, err := domain.NewImagen(domain.NewImagenParams{
				EventoID:  uuid.New(),
				Ruta:      c.ruta,
				SubidaPor: "juan",
				CreatedAt: fixed,
			})
			if !errors.Is(err, domain.ErrImagenRutaInvalida) {
				t.Errorf("NewImagen(%q) err = %v, want ErrImagenRutaInvalida", c.ruta, err)
			}
			if img != nil {
				t.Errorf("NewImagen(%q) = %v, want nil", c.ruta, img)
			}
		})
	}
}

func TestNewImagen_SubidaPorObligatorio(t *testing.T) {
	t.Parallel()
	img, err := domain.NewImagen(domain.NewImagenParams{
		EventoID:  uuid.New(),
		Ruta:      "garantias/x.jpg",
		SubidaPor: "   ",
		CreatedAt: fixed,
	})
	if !errors.Is(err, domain.ErrImagenSubidaPorObligatorio) {
		t.Errorf("err = %v, want ErrImagenSubidaPorObligatorio", err)
	}
	if img != nil {
		t.Error("want nil imagen")
	}
}

// TestNewImagen_RutasValidas pins what the domain ACCEPTS, so tightening
// rutaRelativa later is caught here rather than in the field with a phone.
func TestNewImagen_RutasValidas(t *testing.T) {
	t.Parallel()
	for _, ruta := range []string{
		"x.jpg",
		"garantias/x.jpg",
		"garantias/2026/08/abc.jpg",
		`garantias\2026\abc.jpg`,
		"garantias/..jpg",
		"garantias/a..b/c.jpg",
		"garantias/./x.jpg",
		filepath.Join("garantias", "sub", "x.jpg"),
	} {
		if _, err := domain.NewImagen(domain.NewImagenParams{
			EventoID:  uuid.New(),
			Ruta:      ruta,
			SubidaPor: "juan",
			CreatedAt: fixed,
		}); err != nil {
			t.Errorf("NewImagen(%q): %v, want nil", ruta, err)
		}
	}
}

// TestNewImagen_RutaSeValidaAntesQueSubidaPor documents the order: the path is
// checked first, so a request with both problems reports the path.
func TestNewImagen_RutaSeValidaAntesQueSubidaPor(t *testing.T) {
	t.Parallel()
	_, err := domain.NewImagen(domain.NewImagenParams{
		EventoID:  uuid.New(),
		Ruta:      "/absoluta.jpg",
		SubidaPor: "",
		CreatedAt: fixed,
	})
	if !errors.Is(err, domain.ErrImagenRutaInvalida) {
		t.Errorf("err = %v, want ErrImagenRutaInvalida", err)
	}
}

// TestHydrateImagen_Garbage drills a broken row through the constructor:
// hydration must not validate, the repository only calls it with rows it
// already wrote.
func TestHydrateImagen_Garbage(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	eventoID := uuid.New()
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		EventoID:    eventoID,
		Ruta:        "/ruta/absoluta.jpg",
		Descripcion: "sin validar",
		SubidaPor:   "",
		CreatedAt:   fixed,
	})
	if img.ID() != id {
		t.Errorf("ID() = %v, want %v", img.ID(), id)
	}
	if img.EventoID() != eventoID {
		t.Errorf("EventoID() = %v, want %v", img.EventoID(), eventoID)
	}
	if img.Ruta() != "/ruta/absoluta.jpg" {
		t.Errorf("Ruta() = %q, want verbatim", img.Ruta())
	}
	if img.SubidaPor() != "" {
		t.Errorf("SubidaPor() = %q, want empty", img.SubidaPor())
	}
	if !img.CreatedAt().Equal(fixed) {
		t.Errorf("CreatedAt() = %v, want %v", img.CreatedAt(), fixed)
	}
}

func TestImagen_CreatedAtUTC(t *testing.T) {
	t.Parallel()
	otro := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        uuid.New(),
		EventoID:  uuid.New(),
		CreatedAt: otro,
	})
	if !img.CreatedAt().Equal(otro) {
		t.Errorf("CreatedAt() = %v, want %v", img.CreatedAt(), otro)
	}
}
