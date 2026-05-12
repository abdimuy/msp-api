package authhttp

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/response"
	"github.com/abdimuy/msp-api/internal/platform/validator"
)

// Handlers groups every HTTP handler function for the auth module. It depends
// on the application service for command/query work and on the UsuarioRepo
// port to pull a freshly-loaded permission list when building the
// CurrentUser response (Service does not expose that lookup directly).
type Handlers struct {
	svc      *app.Service
	usuarios outbound.UsuarioRepo
}

// NewHandlers builds a Handlers wired with its dependencies.
func NewHandlers(svc *app.Service, usuarios outbound.UsuarioRepo) *Handlers {
	return &Handlers{svc: svc, usuarios: usuarios}
}

// Login handles POST /v2/auth/login. It is anonymous — the request bypasses
// the authn middleware. The service verifies the Firebase ID token, syncs
// the usuario row, and the handler then loads the usuario's effective
// permisos to assemble the CurrentUser response.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if fe := validator.Default().Struct(req); fe != nil {
		response.ValidationError(w, r, fe)
		return
	}

	u, err := h.svc.SyncFromFirebase(r.Context(), req.IDToken)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	perms, err := h.usuarios.PermisosFor(r.Context(), u.ID())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, toCurrentUserResponse(auth.ToContract(u, perms)))
}

// Me handles GET /v2/me. It returns the CurrentUser planted on the context
// by the authn middleware, which guarantees ok==true here.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	cu, ok := auth.CurrentUserFromContext(r.Context())
	if !ok {
		response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
		return
	}
	response.JSON(w, r, http.StatusOK, toCurrentUserResponse(cu))
}

// decodeJSON decodes the request body into dst and translates malformed input
// into a stable validation apperror so clients see a 422 with a known code.
// The body is bounded by the platform middleware (BodyLimit) so we do not need
// to apply MaxBytesReader here.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			return apperror.NewValidation("invalid_json", "cuerpo de solicitud vacío").WithError(err)
		}
		return apperror.NewValidation("invalid_json", "cuerpo de solicitud inválido").WithError(err)
	}
	return nil
}
