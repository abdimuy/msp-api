//nolint:misspell // Spanish vocabulary by project convention.
package failedintent

import "context"

// ResolutionChecker answers which of the given idempotency keys correspond to
// work that already landed. The platform never learns what a venta is; the
// owning module answers for its own keys.
//
// Por qué el puerto va INVERTIDO —la plataforma lo declara y el módulo dueño
// lo implementa— y no al revés:
//
//   - `internal/platform/failedintent` no puede importar `internal/ventas`:
//     lo prohíbe el depguard y, más al fondo, la razón por la que existe esa
//     regla. La captura es un mecanismo genérico; el día que sepa qué es una
//     venta deja de servir para los pagos.
//   - El que SÍ puede contestar "esta clave ya aterrizó" es el módulo dueño
//     del recurso, porque es el único que sabe dónde mirar.
//
// Por qué preguntar en vez de escuchar el evento: el marcado por 2xx sólo
// atrapa un camino. Se le escapan que la app se rinda, que la venta entre por
// reenvío o captura manual, que el éxito viaje con otra clave, y **todo el
// rezago actual**. Como esto es dinero, la red tiene que preguntarle a la
// fuente.
type ResolutionChecker interface {
	// ResueltasEntre devuelve el subconjunto de `claves` cuyo trabajo ya
	// existe en la fuente del módulo. El orden no importa y las claves que no
	// reconozca simplemente no vuelven.
	//
	// `path` acompaña a las claves porque una clave sólo es única dentro de
	// su recurso: el implementador puede —y debe— ignorar las rutas que no
	// son suyas devolviendo nil.
	//
	// Un error se propaga tal cual: el janitor lo registra y sigue. Nunca
	// puede tumbar la purga.
	ResueltasEntre(ctx context.Context, path string, claves []string) ([]string, error)
}
