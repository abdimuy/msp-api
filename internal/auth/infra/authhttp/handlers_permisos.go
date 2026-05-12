package authhttp

import (
	"net/http"

	"github.com/abdimuy/msp-api/internal/platform/response"
)

// ListarPermisos handles GET /v2/permisos. The permission catalog is small
// (one entry per code declared in the domain) so pagination is omitted —
// callers always receive the full list.
func (h *Handlers) ListarPermisos(w http.ResponseWriter, r *http.Request) {
	perms, err := h.svc.ListarPermisos(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	out := make([]PermisoResponse, 0, len(perms))
	for _, p := range perms {
		out = append(out, toPermisoResponse(p))
	}
	response.JSON(w, r, http.StatusOK, ListResponse[PermisoResponse]{Items: out})
}
