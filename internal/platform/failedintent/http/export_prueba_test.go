package failedintenthttp

import "context"

// NuevoResumenDTOParaPrueba construye un ResumenDTO con su clienteID sin
// exportar, que es el insumo del resolvedor.
func NuevoResumenDTOParaPrueba(titulo string, clienteID *int) *ResumenDTO {
	return &ResumenDTO{Titulo: titulo, clienteID: clienteID}
}

// ResolverNombresParaPrueba expone el resolvedor, que no se exporta porque no
// es contrato: es un paso interno del listado.
func ResolverNombresParaPrueba(ctx context.Context, s *Service, items []IntentDTO) {
	s.resolverNombresDeCliente(ctx, items)
}
