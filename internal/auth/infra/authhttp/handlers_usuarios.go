package authhttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/pagination"
	"github.com/abdimuy/msp-api/internal/platform/response"
	"github.com/abdimuy/msp-api/internal/platform/validator"
)

// ListarUsuarios handles GET /v2/usuarios.
func (h *Handlers) ListarUsuarios(w http.ResponseWriter, r *http.Request) {
	p, err := pagination.FromRequest(r)
	if err != nil {
		response.Error(w, r, apperror.NewValidation("invalid_pagination", "parámetros de paginación inválidos").WithError(err))
		return
	}
	page, err := h.svc.Listar(r.Context(), outbound.ListParams{Cursor: p.After, PageSize: p.Limit})
	if err != nil {
		response.Error(w, r, err)
		return
	}

	items := make([]UsuarioResponse, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, toUsuarioResponse(u))
	}
	response.JSON(w, r, http.StatusOK, ListResponse[UsuarioResponse]{Items: items, NextCursor: page.NextCursor})
}

// ObtenerUsuario handles GET /v2/usuarios/{id}.
func (h *Handlers) ObtenerUsuario(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	u, err := h.svc.Obtener(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, toUsuarioResponse(u))
}

// ActualizarUsuario handles PATCH /v2/usuarios/{id}.
func (h *Handlers) ActualizarUsuario(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req ActualizarUsuarioRequest
	if decErr := decodeJSON(r, &req); decErr != nil {
		response.Error(w, r, decErr)
		return
	}
	if fe := validator.Default().Struct(req); fe != nil {
		response.ValidationError(w, r, fe)
		return
	}
	by, ok := currentUserID(r)
	if !ok {
		response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
		return
	}

	u, err := h.svc.Actualizar(r.Context(), app.ActualizarParams{
		ID:        id,
		Email:     req.Email,
		Nombre:    req.Nombre,
		Telefono:  req.Telefono,
		AlmacenID: req.AlmacenID,
	}, by)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, toUsuarioResponse(u))
}

// DesactivarUsuario handles DELETE /v2/usuarios/{id}.
func (h *Handlers) DesactivarUsuario(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	by, ok := currentUserID(r)
	if !ok {
		response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
		return
	}
	if err := h.svc.Desactivar(r.Context(), id, by); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// AsignarRolAUsuario handles POST /v2/usuarios/{id}/roles.
func (h *Handlers) AsignarRolAUsuario(w http.ResponseWriter, r *http.Request) {
	usuarioID, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req AsignarRolRequest
	if decErr := decodeJSON(r, &req); decErr != nil {
		response.Error(w, r, decErr)
		return
	}
	if fe := validator.Default().Struct(req); fe != nil {
		response.ValidationError(w, r, fe)
		return
	}
	rolID, parseErr := uuid.Parse(req.RolID)
	if parseErr != nil {
		response.Error(w, r,
			apperror.NewValidation("invalid_uuid", "el rol_id no es un UUID válido").WithError(parseErr),
		)
		return
	}
	by, ok := currentUserID(r)
	if !ok {
		response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
		return
	}
	if err := h.svc.AsignarRolAUsuario(r.Context(), usuarioID, rolID, by); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// RevocarRolDeUsuario handles DELETE /v2/usuarios/{id}/roles/{rol_id}.
func (h *Handlers) RevocarRolDeUsuario(w http.ResponseWriter, r *http.Request) {
	usuarioID, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	rolID, err := parseUUIDParam(r, "rol_id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.svc.RevocarRolDeUsuario(r.Context(), usuarioID, rolID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// parseUUIDParam reads a URL parameter and parses it as a UUID, returning a
// 422 apperror with a stable "invalid_uuid" code on failure.
func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation("invalid_uuid", "el identificador en la URL no es un UUID válido").
			WithField("param", name).
			WithError(err)
	}
	return id, nil
}

// currentUserID returns the planted user's ID for use as the "by" actor on
// write commands. The ok==false path should never happen inside the
// authenticated subgroup but is checked defensively.
func currentUserID(r *http.Request) (uuid.UUID, bool) {
	cu, ok := auth.CurrentUserFromContext(r.Context())
	if !ok {
		return uuid.Nil, false
	}
	return cu.ID, true
}
