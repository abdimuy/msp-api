// Package domain holds the inventario module's aggregate root, child entities,
// value objects, domain events, and sentinel errors. It depends only on the
// standard library, uuid, decimal, and the platform/{domain,apperror,audit}
// packages — never on app, infra, ports, or other modules.
//
//nolint:misspell // domain vocabulary is Spanish (almacén, artículo, etc.) per project convention.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the inventario domain. All are produced via
// apperror.New* constructors so they participate in the typed error model
// (Kind → HTTPStatus) and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per the project conventions.
var (
	// ErrArticuloSinExistencia is returned when a traspaso requests a
	// movement with insufficient stock in the source warehouse.
	ErrArticuloSinExistencia = apperror.NewValidation(
		"articulo_sin_existencia",
		"el artículo no tiene existencia suficiente en el almacén origen",
	)
	// ErrArticuloIDInvalido is returned when articuloID is not > 0.
	ErrArticuloIDInvalido = apperror.NewValidation(
		"articulo_id_invalido",
		"el articulo_id es inválido",
	)
	// ErrAlmacenOrigenInvalido is returned when almacen_origen is not > 0.
	ErrAlmacenOrigenInvalido = apperror.NewValidation(
		"almacen_origen_invalido",
		"el almacén origen es inválido",
	)
	// ErrAlmacenDestinoInvalido is returned when almacen_destino is not > 0.
	ErrAlmacenDestinoInvalido = apperror.NewValidation(
		"almacen_destino_invalido",
		"el almacén destino es inválido",
	)
	// ErrAlmacenesIguales is returned when almacen_origen == almacen_destino.
	ErrAlmacenesIguales = apperror.NewValidation(
		"almacenes_iguales",
		"los almacenes de origen y destino no pueden ser iguales",
	)
	// ErrCantidadInvalida is returned when Cantidad is ≤ 0.
	ErrCantidadInvalida = apperror.NewValidation(
		"cantidad_invalida",
		"la cantidad debe ser mayor a cero",
	)
	// ErrCantidadEscalaInvalida is returned when Cantidad has more than 4
	// decimal places.
	ErrCantidadEscalaInvalida = apperror.NewValidation(
		"cantidad_escala_invalida",
		"la cantidad excede la precisión permitida",
	)
	// ErrFolioInvalido is returned when a folio string does not match the
	// expected format ^MS[A-Z]\d{6}$.
	ErrFolioInvalido = apperror.NewValidation(
		"folio_invalido",
		"el folio del traspaso no es válido",
	)
	// ErrTipoMovimientoInvalido is returned for any value other than "S" or
	// "E".
	ErrTipoMovimientoInvalido = apperror.NewValidation(
		"tipo_movimiento_invalido",
		"el tipo de movimiento debe ser 'S' o 'E'",
	)
	// ErrTraspasoSinDetalles is returned when a traspaso is created without
	// at least one detalle.
	ErrTraspasoSinDetalles = apperror.NewValidation(
		"traspaso_sin_detalles",
		"el traspaso debe tener al menos un detalle",
	)
	// ErrTraspasoDescripcionDemasiadoLarga is returned when descripcion
	// exceeds 200 characters.
	ErrTraspasoDescripcionDemasiadoLarga = apperror.NewValidation(
		"traspaso_descripcion_demasiado_larga",
		"la descripción del traspaso excede 200 caracteres",
	)
	// ErrTraspasoYaReversado is returned when Reversar is called on a
	// traspaso that is already a reverso.
	ErrTraspasoYaReversado = apperror.NewConflict(
		"traspaso_ya_reversado",
		"el traspaso ya es un reverso",
	)
	// ErrTraspasoYaAplicado is returned when MarcarAplicado is called on a
	// traspaso that already has a doctoInID.
	ErrTraspasoYaAplicado = apperror.NewConflict(
		"traspaso_ya_aplicado",
		"el traspaso ya fue aplicado en Microsip",
	)
	// ErrTraspasoNoEncontrado is returned when a traspaso lookup misses.
	ErrTraspasoNoEncontrado = apperror.NewNotFound(
		"traspaso_no_encontrado",
		"traspaso no encontrado",
	)
	// ErrAlmacenNoEncontrado is returned when an almacén lookup misses.
	ErrAlmacenNoEncontrado = apperror.NewNotFound(
		"almacen_no_encontrado",
		"almacén no encontrado",
	)
)
