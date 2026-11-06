//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp

// EncabezadoIdempotencia es el encabezado que toda ruta POST de garantías
// exige. Se embebe en la entrada de cada operación:
//
//	type CerrarRequest struct {
//		EncabezadoIdempotencia
//		ID   string `path:"id" format:"uuid"`
//		Body CerrarBody
//	}
//
// La idempotencia es la del dominio (spec §6): la clave se guarda en el
// evento junto con la huella del contenido, y el comando decide. Misma clave y
// mismo contenido → 200 con el estado actual, sin escribir; misma clave y
// contenido distinto → 409 warranty_idempotency_key_mismatch. Por eso estas
// rutas NO pasan por el middleware platform/idempotency.
//
// Huma rechaza con 422 la petición sin el encabezado o con un valor que no es
// UUID, antes de llegar al handler.
type EncabezadoIdempotencia struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" format:"uuid" doc:"UUID que genera el dispositivo; el mismo en cada reintento de la misma operación"`
}
