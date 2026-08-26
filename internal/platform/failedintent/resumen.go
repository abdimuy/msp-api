package failedintent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/shopspring/decimal"
)

// Resumen es el dato de negocio que hace legible un renglón de la pantalla de
// intentos fallidos: **quién** y **cuánto**.
//
// Existe porque ese dato no llegaba. El escritorio lo deducía del cuerpo
// capturado, y una venta lleva fotos —o sea multipart—, y en esa ruta el
// cuerpo NO está en la fila: vive en disco y la columna BODY guarda `null` a
// propósito. Las diecinueve filas de ventas decían "Sin nombre capturado" por
// construcción, no por un defecto de la UI.
//
// La respuesta no puede ser que el listado abra los blobs: existe
// GET /{id}/blob-parts, pero una petición por renglón convierte la ruta más
// caliente de la pantalla en un N+1 sobre el disco. El resumen se extrae UNA
// vez —en la captura, o después en el janitor para las filas viejas— y viaja
// en la fila.
//
// Los campos son deliberadamente pocos y genéricos. `Titulo` es "el nombre que
// una persona reconocería" (el cliente, en ventas y en pagos), no el nombre de
// una entidad concreta; `Referencia` es el ancla para buscarlo en otro lado
// (un folio, un id de cargo). Un módulo nuevo llena los mismos tres campos y
// no pide migración: en la tabla esto es un JSON.
type Resumen struct {
	// Modulo es el dueño de la ruta ('ventas', 'pagos'). Lo estampa el
	// registro, NO el extractor — ver RegistroExtractores.Extraer.
	Modulo string
	// Titulo es el nombre del cliente, o lo más parecido que tenga el módulo.
	Titulo string
	// Monto es el importe que le importa a quien mira la pantalla. Nil cuando
	// el cuerpo no traía ninguno reconocible; cero NO es lo mismo que ausente.
	Monto *decimal.Decimal
	// Referencia es el ancla para encontrar el trabajo en otro sistema.
	Referencia string
}

// Vacio reporta si el resumen no trae ningún dato de negocio.
//
// El módulo NO cuenta: un resumen que sólo dice "esto es de ventas" ya lo dice
// la columna MODULO, y guardarlo en RESUMEN haría que una fila sin nombre ni
// monto se viera "resuelta" ante cualquier consulta que pregunte
// `RESUMEN IS NULL` — entre ellas la del janitor, que dejaría de intentar
// rellenarla.
func (r *Resumen) Vacio() bool {
	if r == nil {
		return true
	}
	return strings.TrimSpace(r.Titulo) == "" &&
		r.Monto == nil &&
		strings.TrimSpace(r.Referencia) == ""
}

// ResumenExtractor traduce un cuerpo capturado al Resumen de su módulo.
//
// El puerto está aquí y no en los módulos porque la dirección de la
// dependencia no puede ser la otra: `internal/platform/failedintent` no puede
// importar `ventas` ni `cobranza` —lo prohíbe depguard, y con razón: la
// plataforma no sabe de dominios—. Se invierte: la plataforma declara el
// puerto, cada módulo lo implementa, y el único sitio donde ambos se conocen
// es la raíz de composición.
type ResumenExtractor interface {
	// Extraer lee un cuerpo YA aislado (un documento JSON, no un envoltorio
	// multipart) y devuelve el resumen de su módulo.
	//
	// Devuelve nil cuando no reconoce la forma. Eso es una respuesta, no un
	// fallo: el intento se guarda sin resumen en vez de inventar un nombre, y
	// un nombre inventado en esta pantalla es peor que un hueco — la pantalla
	// existe para decidir si una venta entró o no.
	//
	// Debe ser puro y no entrar en pánico. Si entra, el registro lo atrapa:
	// perder la evidencia porque un extractor se rompió sería exactamente el
	// fallo que este módulo existe para evitar.
	Extraer(path string, body []byte, contentType string) *Resumen
}

// entradaExtractor es un prefijo de ruta con su módulo y su implementación.
type entradaExtractor struct {
	prefijo string
	modulo  string
	ex      ResumenExtractor
}

// RegistroExtractores despacha por prefijo de ruta al extractor del módulo
// dueño, y es la ÚNICA autoridad sobre el valor de MODULO.
//
// Esa separación importa: el módulo se sabe por la **ruta** y el resumen por
// el **cuerpo**, y son dos hechos independientes. Un cuerpo que el extractor
// no reconoce deja el módulo puesto y el resumen vacío; si el módulo también
// se perdiera, la fila se caería del chip "Ventas" —que filtra por SQL— y el
// operador vería una lista incompleta sin que nada se lo advierta.
//
// El cero valor no sirve: use NewRegistroExtractores. Un puntero nil sí es
// usable y se comporta como "no hay extractores", para que un despliegue sin
// registro no cambie el comportamiento de la captura.
type RegistroExtractores struct {
	entradas []entradaExtractor
}

// NewRegistroExtractores construye un registro vacío.
func NewRegistroExtractores() *RegistroExtractores {
	return &RegistroExtractores{}
}

// Registrar asocia un prefijo de ruta con el módulo y su extractor. Devuelve
// el mismo registro para poder encadenar.
//
// Añadir un módulo a la pantalla es exactamente esto: una implementación del
// puerto y una línea aquí. Ni el esquema ni el escritorio se tocan.
func (r *RegistroExtractores) Registrar(prefijo, modulo string, ex ResumenExtractor) *RegistroExtractores {
	if r == nil || ex == nil || prefijo == "" || modulo == "" {
		return r
	}
	r.entradas = append(r.entradas, entradaExtractor{prefijo: prefijo, modulo: modulo, ex: ex})
	return r
}

// Extraer implementa ResumenExtractor. Busca el prefijo MÁS LARGO que case con
// path —para que "/v2/cobranza/pagos" gane sobre "/v2/cobranza"— y delega.
//
// Devuelve nil sólo cuando ninguna ruta registrada casa. Cuando casa pero el
// extractor no reconoce el cuerpo (o revienta), devuelve un resumen con el
// módulo y nada más.
func (r *RegistroExtractores) Extraer(path string, body []byte, contentType string) *Resumen {
	if r == nil {
		return nil
	}
	entrada, ok := r.buscar(path)
	if !ok {
		return nil
	}
	res := invocarExtractor(entrada, path, body, contentType)
	if res == nil {
		return &Resumen{Modulo: entrada.modulo}
	}
	// El módulo lo decide la ruta, siempre. Un extractor que declare otro no
	// puede colar sus filas en el chip de un módulo ajeno.
	res.Modulo = entrada.modulo
	return res
}

// buscar devuelve la entrada con el prefijo más largo que case con path.
func (r *RegistroExtractores) buscar(path string) (entradaExtractor, bool) {
	var mejor entradaExtractor
	encontrada := false
	for _, e := range r.entradas {
		if !strings.HasPrefix(path, e.prefijo) {
			continue
		}
		if !encontrada || len(e.prefijo) > len(mejor.prefijo) {
			mejor, encontrada = e, true
		}
	}
	return mejor, encontrada
}

// invocarExtractor llama al extractor con red: un pánico se convierte en nil y
// un log, nunca en una petición caída.
func invocarExtractor(e entradaExtractor, path string, body []byte, contentType string) *Resumen {
	var res *Resumen
	func() {
		defer func() {
			if p := recover(); p != nil {
				slog.Error(
					"failedintent: el extractor de resumen entró en pánico",
					"modulo", e.modulo, "path", path, "panic", p,
				)
				res = nil
			}
		}()
		res = e.ex.Extraer(path, body, contentType)
	}()
	return res
}

// ResumenDeIntento extrae el resumen de un intento YA armado, sea cual sea la
// ruta por la que se capturó su cuerpo.
//
// Es el único punto de extracción del sistema: lo usan las dos ramas de la
// captura y el relleno del janitor. Que sea uno solo es lo que garantiza que
// una fila vieja rellenada después diga exactamente lo mismo que habría dicho
// al capturarse.
//
// Sobre el multipart: el cuerpo NO se parsea mientras se transmite. Se lee del
// blob **una vez**, y sólo en la rama que ya decidió persistir el intento —o
// sea, en un 4xx/5xx—. La ruta feliz, que es la común, no paga nada. Los
// campos se recorren en orden y gana el primero que produzca un resumen con
// datos; las partes de archivo (las fotos) ni siquiera se ofrecen al
// extractor.
//
// Nunca devuelve error. Un cuerpo ilegible, un blob perdido o un Content-Type
// que no es multipart degradan a "sin resumen" (con el módulo, si la ruta lo
// dice) y se registran. La captura no se rompe por no poder adornar un
// renglón.
func ResumenDeIntento(
	ctx context.Context, ex ResumenExtractor, blob BlobStorage, i Intent,
) *Resumen {
	if ex == nil {
		return nil
	}
	if i.BodyBlobPath == "" {
		return ex.Extraer(i.Path, i.Body, "application/json")
	}
	if blob == nil {
		// La fila apunta a un cuerpo en disco y no hay con qué abrirlo. Sin
		// leer nada no se puede afirmar ni el módulo con honestidad... salvo
		// que la ruta ya lo diga, que es justo lo que hace Extraer con un
		// cuerpo vacío.
		return conModuloSolo(ex, i.Path)
	}
	partes, err := NewBlobPartsInspector(blob).ListParts(ctx, i.BodyBlobPath, i.BodyContentType)
	if err != nil {
		slog.WarnContext(
			ctx, "failedintent: no se pudo leer el cuerpo en disco para el resumen",
			"error", err, "intent_id", i.ID.String(), "path", i.Path,
		)
		return conModuloSolo(ex, i.Path)
	}
	return ResumenDePartes(ex, i.Path, partes)
}

// ResumenDePartes recorre las partes de CAMPO y devuelve el primer resumen con
// datos. Si ninguna produce datos pero alguna produjo módulo, devuelve ese.
//
// Las partes de archivo —las fotos— no se le ofrecen al extractor. Hoy además
// llegan sin bytes inline, así que el filtro por `Kind` parece redundante;
// no lo es: eso es una coincidencia del lector de partes, y el día que un
// archivo chico se inline, una foto que por azar contenga la palabra `cliente`
// produciría un nombre inventado.
//
// Exportada para que la prueba pueda pasarle partes armadas a mano, incluida
// una de archivo CON bytes — que es la única forma de medir ese filtro.
func ResumenDePartes(ex ResumenExtractor, path string, partes []BlobPart) *Resumen {
	var respaldo *Resumen
	for _, p := range partes {
		if p.Kind != BlobPartKindField || len(p.Value) == 0 {
			continue
		}
		res := ex.Extraer(path, p.Value, p.ContentType)
		if res == nil {
			continue
		}
		if !res.Vacio() {
			return res
		}
		if respaldo == nil {
			respaldo = res
		}
	}
	if respaldo != nil {
		return respaldo
	}
	return conModuloSolo(ex, path)
}

// conModuloSolo le pregunta al extractor por una ruta con cuerpo vacío. Un
// registro contesta con el módulo y nada más; cualquier otra implementación
// que no reconozca un cuerpo vacío contesta nil, que también es correcto.
func conModuloSolo(ex ResumenExtractor, path string) *Resumen {
	res := ex.Extraer(path, nil, "")
	if res == nil || !res.Vacio() {
		// Un extractor que saca datos de la nada no es de fiar para esto.
		if res != nil {
			return &Resumen{Modulo: res.Modulo}
		}
		return nil
	}
	return res
}
