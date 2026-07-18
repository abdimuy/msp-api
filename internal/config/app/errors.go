package app

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// errVendedorListaIDNoPertenece is returned when AsignarVendedor is given a
// lista id that exists in LISTAS_ATRIBUTOS but under a different attribute
// than the slot it was assigned to (e.g. a VENDEDOR_2 id passed as slot 1).
var errVendedorListaIDNoPertenece = apperror.NewValidation(
	"vendedor_lista_id_no_pertenece",
	"el vendedor seleccionado no corresponde al campo",
)

// errUsuarioNoExiste is returned when AsignarVendedor targets a usuarioID
// that does not exist in MSP_USUARIOS — surfaced as a Firebird FK violation
// on the upsert and translated here into a NotFound apperror.
var errUsuarioNoExiste = apperror.NewNotFound(
	"usuario_no_existe",
	"el usuario no existe",
)
