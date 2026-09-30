// Package garantias is the cross-module surface of the garantías bounded
// context. Other modules import only this package — never
// internal/garantias/domain, internal/garantias/app, or
// internal/garantias/infra.
//
// Garantías is a sealed vertical slice (spec §2.1, ADR-0009): the whole
// module imports nothing outside itself and internal/platform, not even
// another module's contracts. What it does expose is this one file, and only
// because the composition root has no other way to learn the permission codes
// the module defines: auth owns the catalog and a sealed module cannot import
// it (spec §2.2).
//
// The contract exports:
//   - PermisoInfo and Permisos: the five permission codes of spec §7, for the
//     composition root to register in the auth catalog.
//
//nolint:misspell // Spanish vocabulary (Descripcion) per project convention.
package garantias

import "github.com/abdimuy/msp-api/internal/garantias/domain"

// PermisoInfo describes one permission this module defines, for the
// composition root to register in the auth catalog.
type PermisoInfo struct {
	Codigo      string
	Descripcion string
}

// Permisos returns the five permission codes of spec §7, in a stable order.
// The descriptions are the ones office sees in the roles screen, so they are
// in Spanish and say what the permission COVERS, not which endpoint it guards.
//
// The codes come from the domain enum rather than from string literals here:
// that way the wire value is written once, and the test that round-trips
// every code through domain.ParsePermiso catches a sixth permission added on
// only one of the two sides.
func Permisos() []PermisoInfo {
	return []PermisoInfo{
		{Codigo: domain.PermisoLeer.String(), Descripcion: "ver la bandeja y consultar garantías"},
		{Codigo: domain.PermisoCrear.String(), Descripcion: "abrir folios y agregar artículos"},
		{Codigo: domain.PermisoActualizar.String(), Descripcion: "avanzar artículos, registrar diagnóstico, dictamen y evidencia"},
		{Codigo: domain.PermisoAutorizar.String(), Descripcion: "autorizar cambio físico y desenlaces del standby"},
		{Codigo: domain.PermisoCerrar.String(), Descripcion: "entregar, cerrar y cancelar garantías"},
	}
}
