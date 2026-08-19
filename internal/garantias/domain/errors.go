package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the garantías domain value objects. English
// snake_case codes, Spanish lowercase messages without a trailing period
// (CLAUDE.md §3).
var (
	// ErrOrigenFolioInvalido is returned by NewOrigenFolio when the input is
	// not "piso" or "cliente".
	ErrOrigenFolioInvalido = apperror.NewValidation(
		"warranty_origin_invalid",
		"origen de folio inválido",
	)

	// ErrEstadoCuentaInvalido is returned by NewEstadoCuenta when the input
	// is not "liquidada" or "saldo_pendiente".
	ErrEstadoCuentaInvalido = apperror.NewValidation(
		"warranty_account_state_invalid",
		"estado de cuenta inválido",
	)

	// ErrEstadoFolioInvalido is returned by NewEstadoFolio when the input is
	// not one of the six recognized folio states.
	ErrEstadoFolioInvalido = apperror.NewValidation(
		"warranty_folio_state_invalid",
		"estado de folio inválido",
	)

	// ErrRutaReparacionInvalida is returned by NewRutaReparacion when the
	// input is not "proveedor" or "taller".
	ErrRutaReparacionInvalida = apperror.NewValidation(
		"warranty_repair_route_invalid",
		"ruta de reparación inválida",
	)

	// ErrDictamenInvalido is returned by NewDictamen when the input is not
	// "aceptada", "rechazada", or "sin_falla".
	ErrDictamenInvalido = apperror.NewValidation(
		"warranty_verdict_invalid",
		"dictamen inválido",
	)

	// ErrRolArticuloInvalido is returned by NewRolArticulo when the input is
	// not "original" or "reemplazo".
	ErrRolArticuloInvalido = apperror.NewValidation(
		"warranty_item_role_invalid",
		"rol de artículo inválido",
	)

	// ErrRolDecisorInvalido is returned by NewRolDecisor when the input is
	// not "carpinteria", "oficina", or "tecnica".
	ErrRolDecisorInvalido = apperror.NewValidation(
		"warranty_decider_role_invalid",
		"rol de quien decide inválido",
	)

	// ErrEtapaInvalida is returned by ParseEtapa when the input is not one
	// of the 19 recognized stages.
	ErrEtapaInvalida = apperror.NewValidation(
		"warranty_stage_invalid",
		"etapa inválida",
	)

	// ErrUbicacionInvalida is returned by ParseUbicacion when the input is
	// not one of the 8 recognized locations.
	ErrUbicacionInvalida = apperror.NewValidation(
		"warranty_location_invalid",
		"ubicación inválida",
	)

	// ErrDesenlaceInvalido is returned by ParseDesenlace when the input is
	// not one of the 6 recognized outcomes.
	ErrDesenlaceInvalido = apperror.NewValidation(
		"warranty_outcome_invalid",
		"desenlace inválido",
	)
)
