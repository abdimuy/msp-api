package domain

import "sort"

// Permission is the typed form of a permission code (e.g. "usuarios:listar").
// Permission values are compared by string equality and round-trip via the
// Code() / String() methods.
type Permission string

// Code returns the underlying string representation, suitable for storing in
// Firebird's MSP_PERMISOS.CODIGO column or for serializing in JSON.
func (p Permission) Code() string { return string(p) }

// String implements fmt.Stringer.
func (p Permission) String() string { return string(p) }

// Equals reports whether two permissions are identical.
func (p Permission) Equals(other Permission) bool { return p == other }

// Permission code constants. The full list is the canonical authority for
// the permissions known to the auth module; the AllPermissions catalog
// below pairs each with its human-readable metadata.
//
// Codes are stable identifiers and must not be renamed once shipped — they
// are persisted in MSP_ROLES_PERMISOS rows. Adding new codes is fine; remove
// or rename only via a migration that scrubs the join table.
const (
	// PermUsuariosListar grants listing the usuarios collection.
	PermUsuariosListar Permission = "usuarios:listar"
	// PermUsuariosVer grants reading a single usuario.
	PermUsuariosVer Permission = "usuarios:ver"
	// PermUsuariosActualizar grants editing a usuario's mutable fields.
	PermUsuariosActualizar Permission = "usuarios:actualizar"
	// PermUsuariosDesactivar grants soft-deleting a usuario.
	PermUsuariosDesactivar Permission = "usuarios:desactivar"
	// PermUsuariosAsignarRol grants attaching/detaching roles on a usuario.
	PermUsuariosAsignarRol Permission = "usuarios:asignar_rol"

	// PermRolesListar grants listing the roles collection.
	PermRolesListar Permission = "roles:listar"
	// PermRolesCrear grants creating new roles.
	PermRolesCrear Permission = "roles:crear"
	// PermRolesActualizar grants editing a rol's mutable fields.
	PermRolesActualizar Permission = "roles:actualizar"
	// PermRolesAsignarPermiso grants attaching/detaching permisos on a rol.
	PermRolesAsignarPermiso Permission = "roles:asignar_permiso"

	// PermPermisosListar grants listing the permissions catalog.
	PermPermisosListar Permission = "permisos:listar"

	// PermVentasListar grants listing ventas.
	PermVentasListar Permission = "ventas:listar"
	// PermVentasVer grants reading a single venta.
	PermVentasVer Permission = "ventas:ver"
	// PermVentasCrear grants creating new ventas.
	PermVentasCrear Permission = "ventas:crear"
	// PermVentasCancelar grants soft-cancelling a venta.
	PermVentasCancelar Permission = "ventas:cancelar"
	// PermVentasEditar grants editing a venta while it is in 'borrador' status.
	PermVentasEditar Permission = "ventas:editar"
	// PermVentasSubirImagenes grants uploading evidence images to a venta.
	PermVentasSubirImagenes Permission = "ventas:subir_imagenes"
	// PermVentasEliminarImagenes grants deleting evidence images of a venta.
	PermVentasEliminarImagenes Permission = "ventas:eliminar_imagenes"
	// PermVentasRevisar grants sending a venta to revision.
	PermVentasRevisar Permission = "ventas:revisar"
	// PermVentasAprobar grants approving or returning a venta to borrador.
	PermVentasAprobar Permission = "ventas:aprobar"
	// PermVentasAplicar grants materializing (applying) a venta in Microsip.
	PermVentasAplicar Permission = "ventas:aplicar"

	// PermFailedIntentsVer grants reading captured failed intents (full
	// request payload included — see ADR-0005 for the PII trade-off).
	PermFailedIntentsVer Permission = "failed_intents:ver"
	// PermFailedIntentsResolver grants replaying, ignoring and resolving
	// captured failed intents.
	PermFailedIntentsResolver Permission = "failed_intents:resolver"

	// PermCobranzaVerSaldos grants reading materialized cobranza balances.
	PermCobranzaVerSaldos Permission = "cobranza:ver_saldos"
	// PermCobranzaVerPagos grants reading materialized pagos detail (per-importe rows).
	PermCobranzaVerPagos Permission = "cobranza:ver_pagos"
	// PermCobranzaReconciliar grants triggering a manual reconcile and viewing cache errors.
	PermCobranzaReconciliar Permission = "cobranza:reconciliar"
	// PermCobranzaBackfill grants triggering an admin backfill of the balances cache.
	PermCobranzaBackfill Permission = "cobranza:backfill"
)

// PermissionMeta is a catalog entry pairing a permission code with the
// description and category that will be persisted in MSP_PERMISOS. Mirrors
// the MSP_PERMISOS row shape but lives in domain code to keep the catalog
// the single source of truth.
type PermissionMeta struct {
	Code        Permission
	Description string
	Categoria   string
}

// Categoria constants — short enough to fit MSP_PERMISOS.CATEGORIA (30
// chars). Used internally by AllPermissions to keep the catalog DRY.
const (
	categoriaUsuarios      = "usuarios"
	categoriaRoles         = "roles"
	categoriaPermisos      = "permisos"
	categoriaVentas        = "ventas"
	categoriaFailedIntents = "failed_intents"
	categoriaCobranza      = "cobranza"
)

// AllPermissions returns every permission known to the auth module, sorted
// by Code for deterministic output. The slice is freshly allocated on every
// call so callers can safely mutate it.
func AllPermissions() []PermissionMeta {
	perms := []PermissionMeta{
		{PermUsuariosListar, "listar usuarios", categoriaUsuarios},
		{PermUsuariosVer, "ver un usuario", categoriaUsuarios},
		{PermUsuariosActualizar, "actualizar un usuario", categoriaUsuarios},
		{PermUsuariosDesactivar, "desactivar un usuario", categoriaUsuarios},
		{PermUsuariosAsignarRol, "asignar o revocar roles a un usuario", categoriaUsuarios},

		{PermRolesListar, "listar roles", categoriaRoles},
		{PermRolesCrear, "crear roles", categoriaRoles},
		{PermRolesActualizar, "actualizar roles", categoriaRoles},
		{PermRolesAsignarPermiso, "asignar o revocar permisos a un rol", categoriaRoles},

		{PermPermisosListar, "listar el catálogo de permisos", categoriaPermisos},

		{PermVentasListar, "listar ventas", categoriaVentas},
		{PermVentasVer, "ver una venta", categoriaVentas},
		{PermVentasCrear, "crear una venta", categoriaVentas},
		{PermVentasCancelar, "cancelar una venta", categoriaVentas},
		{PermVentasEditar, "editar una venta en borrador", categoriaVentas},
		{PermVentasSubirImagenes, "subir imágenes de evidencia a una venta", categoriaVentas},
		{PermVentasEliminarImagenes, "eliminar imágenes de evidencia de una venta", categoriaVentas},
		{PermVentasRevisar, "enviar una venta a revisión", categoriaVentas},
		{PermVentasAprobar, "aprobar o regresar a borrador una venta", categoriaVentas},
		{PermVentasAplicar, "aplicar (materializar) una venta en Microsip", categoriaVentas},

		{PermFailedIntentsVer, "ver intents fallidos y sus payloads", categoriaFailedIntents},
		{PermFailedIntentsResolver, "reproducir, ignorar y resolver intents fallidos", categoriaFailedIntents},

		{PermCobranzaVerSaldos, "ver saldos materializados de cobranza", categoriaCobranza},
		{PermCobranzaVerPagos, "ver pagos materializados (detalle por importe)", categoriaCobranza},
		{PermCobranzaReconciliar, "disparar reconcile y ver errores del cache de saldos", categoriaCobranza},
		{PermCobranzaBackfill, "disparar backfill manual del cache de saldos", categoriaCobranza},
	}
	sort.Slice(perms, func(i, j int) bool { return perms[i].Code < perms[j].Code })
	return perms
}
