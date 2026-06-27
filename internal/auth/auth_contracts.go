// Package auth is the cross-module surface of the auth bounded context.
// Other modules import only this package — never internal/auth/domain,
// internal/auth/app, or internal/auth/infra. The depguard linter enforces
// the rule.
//
// The contract exports:
//   - CurrentUser: the projected view of the authenticated principal that
//     the HTTP middleware plants on the request context.
//   - Permission: a type alias for domain.Permission so other modules can
//     reference permission codes without crossing the domain boundary.
//   - Permission code re-exports: one constant per permission known to the
//     auth domain.
//   - PlantCurrentUser / CurrentUserFromContext (defined in ctxuser.go): the
//     context-key plumbing other modules use to read who is making a call.
package auth

import (
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// CurrentUser is the projected, cross-module view of the authenticated
// principal. It is intentionally a flat struct of primitive values so
// other modules can consume it without importing the auth domain types.
type CurrentUser struct {
	// ID is the usuario's primary key.
	ID uuid.UUID
	// FirebaseUID is the linked Firebase Authentication uid.
	FirebaseUID string
	// Email is the usuario's email address, lowercased.
	Email string
	// Nombre is the usuario's display name.
	Nombre string
	// AlmacenID is the warehouse the usuario is bound to, or nil.
	AlmacenID *int
	// Permisos is the flattened set of permission codes the usuario holds,
	// union'd across every active rol it owns.
	Permisos []string
}

// Permission is re-exported from the auth domain so other modules can
// reference permission codes by their typed form (e.g. for use in middleware
// helpers) without importing auth/domain directly.
type Permission = domain.Permission

// Permission code re-exports. Adding a new code requires adding a constant
// here as well as in internal/auth/domain/permission_codes.go.
const (
	// PermUsuariosListar — see domain.PermUsuariosListar.
	PermUsuariosListar = domain.PermUsuariosListar
	// PermUsuariosVer — see domain.PermUsuariosVer.
	PermUsuariosVer = domain.PermUsuariosVer
	// PermUsuariosActualizar — see domain.PermUsuariosActualizar.
	PermUsuariosActualizar = domain.PermUsuariosActualizar
	// PermUsuariosDesactivar — see domain.PermUsuariosDesactivar.
	PermUsuariosDesactivar = domain.PermUsuariosDesactivar
	// PermUsuariosAsignarRol — see domain.PermUsuariosAsignarRol.
	PermUsuariosAsignarRol = domain.PermUsuariosAsignarRol

	// PermRolesListar — see domain.PermRolesListar.
	PermRolesListar = domain.PermRolesListar
	// PermRolesCrear — see domain.PermRolesCrear.
	PermRolesCrear = domain.PermRolesCrear
	// PermRolesActualizar — see domain.PermRolesActualizar.
	PermRolesActualizar = domain.PermRolesActualizar
	// PermRolesAsignarPermiso — see domain.PermRolesAsignarPermiso.
	PermRolesAsignarPermiso = domain.PermRolesAsignarPermiso

	// PermPermisosListar — see domain.PermPermisosListar.
	PermPermisosListar = domain.PermPermisosListar

	// PermVentasListar — see domain.PermVentasListar.
	PermVentasListar = domain.PermVentasListar
	// PermVentasVer — see domain.PermVentasVer.
	PermVentasVer = domain.PermVentasVer
	// PermVentasCrear — see domain.PermVentasCrear.
	PermVentasCrear = domain.PermVentasCrear
	// PermVentasCancelar — see domain.PermVentasCancelar.
	PermVentasCancelar = domain.PermVentasCancelar
	// PermVentasEditar — see domain.PermVentasEditar.
	PermVentasEditar = domain.PermVentasEditar
	// PermVentasSubirImagenes — see domain.PermVentasSubirImagenes.
	PermVentasSubirImagenes = domain.PermVentasSubirImagenes
	// PermVentasEliminarImagenes — see domain.PermVentasEliminarImagenes.
	PermVentasEliminarImagenes = domain.PermVentasEliminarImagenes
	// PermVentasRevisar — see domain.PermVentasRevisar.
	PermVentasRevisar = domain.PermVentasRevisar
	// PermVentasAprobar — see domain.PermVentasAprobar.
	PermVentasAprobar = domain.PermVentasAprobar
	// PermVentasAplicar — see domain.PermVentasAplicar.
	PermVentasAplicar = domain.PermVentasAplicar

	// PermFailedIntentsVer — see domain.PermFailedIntentsVer.
	PermFailedIntentsVer = domain.PermFailedIntentsVer
	// PermFailedIntentsResolver — see domain.PermFailedIntentsResolver.
	PermFailedIntentsResolver = domain.PermFailedIntentsResolver

	// PermCobranzaVerSaldos — see domain.PermCobranzaVerSaldos.
	PermCobranzaVerSaldos = domain.PermCobranzaVerSaldos
	// PermCobranzaVerPagos — see domain.PermCobranzaVerPagos.
	PermCobranzaVerPagos = domain.PermCobranzaVerPagos
	// PermCobranzaReconciliar — see domain.PermCobranzaReconciliar.
	PermCobranzaReconciliar = domain.PermCobranzaReconciliar
	// PermCobranzaBackfill — see domain.PermCobranzaBackfill.
	PermCobranzaBackfill = domain.PermCobranzaBackfill

	// PermInventarioVer — see domain.PermInventarioVer.
	PermInventarioVer = domain.PermInventarioVer
	// PermTraspasosVer — see domain.PermTraspasosVer.
	PermTraspasosVer = domain.PermTraspasosVer
	// PermStockConsultar — see domain.PermStockConsultar.
	PermStockConsultar = domain.PermStockConsultar

	// PermAnalyticsWinbackRead — see domain.PermAnalyticsWinbackRead.
	PermAnalyticsWinbackRead = domain.PermAnalyticsWinbackRead
	// PermAnalyticsRefresh — see domain.PermAnalyticsRefresh.
	PermAnalyticsRefresh = domain.PermAnalyticsRefresh
	// PermAnalyticsCarteraRead — see domain.PermAnalyticsCarteraRead.
	PermAnalyticsCarteraRead = domain.PermAnalyticsCarteraRead

	// PermClientesLeer — see domain.PermClientesLeer.
	PermClientesLeer = domain.PermClientesLeer
	// PermClientesReindexar — see domain.PermClientesReindexar.
	PermClientesReindexar = domain.PermClientesReindexar

	// PermRutasLeer — see domain.PermRutasLeer.
	PermRutasLeer = domain.PermRutasLeer
)
