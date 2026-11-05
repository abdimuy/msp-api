package garhttp

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// These tests exercise the defensive helpers of the command handlers directly.
// Huma validates the UUIDs and the enums before the handler runs, so the HTTP
// suite never reaches these branches; without a unit test they would sit
// uncovered and hide a change that breaks the handler's own contract.

func TestMapAppErrorErrorNil_DevuelveNil(t *testing.T) {
	t.Parallel()

	if err := mapAppError(context.Background(), nil); err != nil {
		t.Errorf("mapAppError(nil) = %v, want nil", err)
	}
}

func TestParseUUIDDeRuta(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	got, err := parseUUIDDeRuta(id.String(), "la garantía")
	if err != nil {
		t.Fatalf("parseUUIDDeRuta válido: %v", err)
	}
	if got != id {
		t.Errorf("id = %s, want %s", got, id)
	}

	if _, err := parseUUIDDeRuta("no-es-uuid", "la garantía"); err == nil {
		t.Error("un UUID inválido debe devolver error de validación")
	}
}

func TestParseUUIDsDeRuta_Errores(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.NewString()
	articuloID := uuid.NewString()

	if _, _, err := parseUUIDsDeRuta(context.Background(), "malo", articuloID); err == nil {
		t.Error("un UUID de garantía inválido debe devolver error")
	}
	if _, _, err := parseUUIDsDeRuta(context.Background(), garantiaID, "malo"); err == nil {
		t.Error("un UUID de artículo inválido debe devolver error")
	}

	gid, aid, err := parseUUIDsDeRuta(context.Background(), garantiaID, articuloID)
	if err != nil {
		t.Fatalf("parseUUIDsDeRuta válido: %v", err)
	}
	if gid.String() != garantiaID || aid.String() != articuloID {
		t.Errorf("ids = %s/%s, want %s/%s", gid, aid, garantiaID, articuloID)
	}
}

func TestValidarRolDecisor(t *testing.T) {
	t.Parallel()

	if err := validarRolDecisor(""); err != nil {
		t.Errorf("un rol vacío lo decide el dominio, got %v", err)
	}
	if err := validarRolDecisor("  tecnica  "); err != nil {
		t.Errorf("un rol del catálogo debe ser válido, got %v", err)
	}
	if err := validarRolDecisor("direccion"); err == nil {
		t.Error("un rol fuera del catálogo debe devolver error")
	}
}

func TestParseEstadoCuentaOpcional(t *testing.T) {
	t.Parallel()

	estado, err := parseEstadoCuentaOpcional(nil)
	if err != nil || estado != nil {
		t.Errorf("sin campo debe ser nil, nil; got %v, %v", estado, err)
	}

	malo := "no-existe"
	if _, err := parseEstadoCuentaOpcional(&malo); err == nil {
		t.Error("un estado de cuenta inválido debe devolver error")
	}
}
