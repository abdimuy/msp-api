//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// TestAuthz_VentasEditar_IsGlobalNotPerOwnership pins down the deliberate
// authorization model for ventas edit endpoints: any caller that holds the
// PermVentasEditar permission may edit any venta regardless of whether they
// appear among the venta's vendedores.
//
// This is by design — the role model is admin / jefe-de-ventas, not
// per-row ownership. Tests below assert the current behavior so that any
// future change (e.g. introducing a "vendedor:editar_propias" finer
// permission) shows up as a deliberate diff in this file rather than a
// silent privilege escalation. See `internal/auth/domain/permission_codes.go`
// for the canonical permission catalog.
//
// If a future requirement calls for per-row ownership enforcement, the
// fix involves: (a) adding a new permission code, (b) decorating the
// edit handlers with a check that the caller's UsuarioID is among
// v.Vendedores(), and (c) updating the rol assignments in seeds.
func TestAuthz_VentasEditar_IsGlobalNotPerOwnership(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()

	// Create the venta as "creator" with a specific set of vendedores.
	creator := fullPerms(uuid.New())
	r := buildRouter(t, svc, creator)
	createBody := validCreateBody()

	// Snapshot the assigned vendedor UsuarioID so we can later assert the
	// editor is NOT this user.
	require.Len(t, createBody.Vendedores, 1)
	assignedVendedorUsuarioID := createBody.Vendedores[0].UsuarioID

	createReq := crearVentaMultipartRequest(t, createBody)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, "body=%s", createRec.Body.String())

	// Now build an editor with PermVentasEditar but a DIFFERENT user id and
	// no relationship to the assigned vendedor.
	editorUserID := uuid.New()
	require.NotEqual(t, assignedVendedorUsuarioID, editorUserID.String(),
		"editor must be a different user than the assigned vendedor")
	editor := fullPerms(editorUserID)
	rAsEditor := buildRouter(t, svc, editor)

	// Attempt PATCH /ventas/{id} as the unrelated editor.
	editBody := venthttp.ActualizarHeaderBody{
		Direccion:  createBody.Direccion,
		GPS:        createBody.GPS,
		FechaVenta: createBody.FechaVenta,
		Montos: venthttp.MontosDTO{
			Anual:      "1100",
			CortoPlazo: "1000",
			Contado:    "900",
		},
	}
	editReq := jsonRequest(t, http.MethodPatch, "/ventas/"+createBody.ID, editBody)
	editRec := httptest.NewRecorder()
	rAsEditor.ServeHTTP(editRec, editReq)

	// EXPECTED: the edit succeeds because the global PermVentasEditar grants
	// edit rights regardless of vendedor membership.
	require.Equal(t, http.StatusOK, editRec.Code,
		"PermVentasEditar is global by design; non-vendedor caller should edit successfully. body=%s",
		editRec.Body.String())

	var updated venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(editRec.Body.Bytes(), &updated))
	assert.Equal(t, "900.00", updated.Montos.Contado, "edit was applied")
	assert.NotEqual(t, editorUserID.String(), updated.Vendedores[0].UsuarioID,
		"editor is not retroactively added as a vendedor")
}

// TestAuthz_VentasEditar_WithoutPermission_Rejects pins down the inverse:
// a caller without PermVentasEditar is rejected with 403 even if they ARE
// the assigned vendedor on the venta. This proves the gate is purely the
// permission code, not row-level ownership.
func TestAuthz_VentasEditar_WithoutPermission_Rejects(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()

	// Create as full-perms.
	createBody := validCreateBody()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	createReq := crearVentaMultipartRequest(t, createBody)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// Caller that is the assigned vendedor BUT lacks PermVentasEditar.
	assignedID, err := uuid.Parse(createBody.Vendedores[0].UsuarioID)
	require.NoError(t, err)
	readerOnly := minimalReader(assignedID)
	rReader := buildRouter(t, svc, readerOnly)

	editBody := venthttp.ActualizarHeaderBody{
		Direccion:  createBody.Direccion,
		GPS:        createBody.GPS,
		FechaVenta: createBody.FechaVenta,
		Montos:     createBody.Montos,
	}
	editReq := jsonRequest(t, http.MethodPatch, "/ventas/"+createBody.ID, editBody)
	editRec := httptest.NewRecorder()
	rReader.ServeHTTP(editRec, editReq)

	assert.Equal(t, http.StatusForbidden, editRec.Code,
		"missing PermVentasEditar must reject even the assigned vendedor")
}

// minimalReader returns a CurrentUser with read-only ventas permissions.
// Used to assert that ownership alone does NOT confer edit rights.
func minimalReader(id uuid.UUID) auth.CurrentUser {
	return auth.CurrentUser{
		ID:          id,
		FirebaseUID: "fb-reader",
		Email:       "reader@example.com",
		Nombre:      "Reader",
		Permisos: []string{
			string(authdomain.PermVentasListar),
			string(authdomain.PermVentasVer),
		},
	}
}
