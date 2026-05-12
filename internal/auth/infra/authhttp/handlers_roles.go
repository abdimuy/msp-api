package authhttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/pagination"
	"github.com/abdimuy/msp-api/internal/platform/response"
	"github.com/abdimuy/msp-api/internal/platform/validator"
)

// ListarRoles handles GET /v2/roles.
func (h *Handlers) ListarRoles(w http.ResponseWriter, r *http.Request) {
	p, err := pagination.FromRequest(r)
	if err != nil {
		response.Error(w, r, apperror.NewValidation("invalid_pagination", "parámetros de paginación inválidos").WithError(err))
		return
	}
	page, err := h.svc.ListarRoles(r.Context(), outbound.ListParams{Cursor: p.After, PageSize: p.Limit})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	items := make([]RolResponse, 0, len(page.Items))
	for _, rol := range page.Items {
		items = append(items, toRolResponse(rol))
	}
	response.JSON(w, r, http.StatusOK, ListResponse[RolResponse]{Items: items, NextCursor: page.NextCursor})
}

// ObtenerRol handles GET /v2/roles/{id}.
func (h *Handlers) ObtenerRol(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	rol, err := h.svc.ObtenerRol(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, toRolResponse(rol))
}

// CrearRol handles POST /v2/roles.
func (h *Handlers) CrearRol(w http.ResponseWriter, r *http.Request) {
	var req CrearRolRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
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
	rol, err := h.svc.CrearRol(r.Context(), app.CrearRolParams{
		Nombre:      req.Nombre,
		Description: req.Description,
	}, by)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, toRolResponse(rol))
}

// ActualizarRol handles PATCH /v2/roles/{id}.
func (h *Handlers) ActualizarRol(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req ActualizarRolRequest
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
	rol, err := h.svc.ActualizarRol(r.Context(), app.ActualizarRolParams{
		ID:          id,
		Nombre:      req.Nombre,
		Description: req.Description,
	}, by)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, toRolResponse(rol))
}

// DesactivarRol handles DELETE /v2/roles/{id}.
func (h *Handlers) DesactivarRol(w http.ResponseWriter, r *http.Request) {
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
	if err := h.svc.DesactivarRol(r.Context(), id, by); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// AsignarPermisoARol handles POST /v2/roles/{id}/permisos.
func (h *Handlers) AsignarPermisoARol(w http.ResponseWriter, r *http.Request) {
	rolID, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req AsignarPermisoRequest
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
	if err := h.svc.AsignarPermisoARol(r.Context(), rolID, domain.Permission(req.Codigo), by); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// RevocarPermisoDeRol handles DELETE /v2/roles/{id}/permisos/{codigo}.
func (h *Handlers) RevocarPermisoDeRol(w http.ResponseWriter, r *http.Request) {
	rolID, err := parseUUIDParam(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	codigo := chi.URLParam(r, "codigo")
	if codigo == "" {
		response.Error(w, r, apperror.NewValidation("invalid_codigo", "el código del permiso es obligatorio"))
		return
	}
	if err := h.svc.RevocarPermisoDeRol(r.Context(), rolID, domain.Permission(codigo)); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
