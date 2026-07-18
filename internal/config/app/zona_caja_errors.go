package app

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// errZonaNoExiste is returned when AsignarZonaCaja targets a ZONA_CLIENTE_ID
// that does not exist in Microsip's ZONAS_CLIENTES catalog.
var errZonaNoExiste = apperror.NewNotFound("zona_no_existe", "la zona no existe")

// errCajaNoExiste is returned when a non-sentinel CajaID does not exist in
// Microsip's CAJAS catalog.
var errCajaNoExiste = apperror.NewValidation("caja_no_existe", "la caja no existe")

// errCajeroNoExiste is returned when a non-sentinel CajeroID does not exist
// in Microsip's CAJEROS catalog.
var errCajeroNoExiste = apperror.NewValidation("cajero_no_existe", "el cajero no existe")

// errVendedorNoExisteCatalogo is returned when a non-sentinel VendedorID does
// not exist in Microsip's VENDEDORES catalog. Named distinctly from
// errVendedorListaIDNoPertenece (vendedores slice) to avoid confusion between
// the two unrelated "vendedor" concepts.
var errVendedorNoExisteCatalogo = apperror.NewValidation("vendedor_no_existe", "el vendedor no existe")

// errCobradorNoExiste is returned when a non-sentinel CobradorID does not
// exist in Microsip's COBRADORES catalog.
var errCobradorNoExiste = apperror.NewValidation("cobrador_no_existe", "el cobrador no existe")
