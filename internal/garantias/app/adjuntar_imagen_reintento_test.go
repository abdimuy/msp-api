package app

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestImagenCoincideReintento_EvidenciaMismoEvento(t *testing.T) {
	t.Parallel()

	eventoID := uuid.New()
	huella := strings.Repeat("a", 64)

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        uuid.New(),
		EventoID:  eventoID,
		Tipo:      domain.TipoImagenEvidencia,
		Ruta:      "garantias/prueba.png",
		SubidaPor: "Ruben",
		Huella:    huella,
		CreatedAt: time.Now().UTC(),
	})

	if !imagenCoincideReintento(imagen, eventoID, huella) {
		t.Fatal("la misma evidencia, evento y huella debe aceptarse como reintento")
	}
}

func TestImagenCoincideReintento_RechazaFirma(t *testing.T) {
	t.Parallel()

	eventoID := uuid.New()
	huella := strings.Repeat("b", 64)

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        uuid.New(),
		EventoID:  eventoID,
		Tipo:      domain.TipoImagenFirma,
		Ruta:      "garantias/firma.png",
		SubidaPor: "Ruben",
		Huella:    huella,
		CreatedAt: time.Now().UTC(),
	})

	if imagenCoincideReintento(imagen, eventoID, huella) {
		t.Fatal("una firma nunca debe aceptarse como reintento de evidencia")
	}
}

func TestImagenCoincideReintento_RechazaOtroEvento(t *testing.T) {
	t.Parallel()

	eventoOriginal := uuid.New()
	eventoSolicitado := uuid.New()
	huella := strings.Repeat("c", 64)

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        uuid.New(),
		EventoID:  eventoOriginal,
		Tipo:      domain.TipoImagenEvidencia,
		Ruta:      "garantias/evidencia.png",
		SubidaPor: "Ruben",
		Huella:    huella,
		CreatedAt: time.Now().UTC(),
	})

	if imagenCoincideReintento(imagen, eventoSolicitado, huella) {
		t.Fatal("una evidencia de otro evento no debe aceptarse como reintento")
	}
}
