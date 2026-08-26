package failedintent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ─── dobles ──────────────────────────────────────────────────────────────────

// extractorFn adapta una función al puerto ResumenExtractor.
type extractorFn func(path string, body []byte, contentType string) *failedintent.Resumen

func (f extractorFn) Extraer(path string, body []byte, contentType string) *failedintent.Resumen {
	return f(path, body, contentType)
}

// blobEnMemoria es un BlobStorage mínimo respaldado por un mapa. Sólo Open y
// Save se usan aquí; el resto satisface la interfaz.
type blobEnMemoria struct {
	datos     map[string][]byte
	errAbrir  error
	abrituras int
}

func nuevoBlobEnMemoria() *blobEnMemoria {
	return &blobEnMemoria{datos: map[string][]byte{}}
}

func (b *blobEnMemoria) Save(_ context.Context, id uuid.UUID, r io.Reader, _ int64) (string, error) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	path := id.String() + ".bin"
	b.datos[path] = buf
	return path, nil
}

func (b *blobEnMemoria) Open(_ context.Context, path string) (io.ReadCloser, error) {
	b.abrituras++
	if b.errAbrir != nil {
		return nil, b.errAbrir
	}
	raw, ok := b.datos[path]
	if !ok {
		return nil, errors.New("blobEnMemoria: no existe " + path)
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func (b *blobEnMemoria) Delete(_ context.Context, path string) error {
	delete(b.datos, path)
	return nil
}

// construirMultipart arma un cuerpo multipart con los campos y archivos dados
// y devuelve los bytes junto con el Content-Type completo (incluye boundary).
func construirMultipart(
	t *testing.T, campos, archivos map[string]string,
) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for nombre, valor := range campos {
		if err := w.WriteField(nombre, valor); err != nil {
			t.Fatalf("WriteField(%q): %v", nombre, err)
		}
	}
	for nombre, contenido := range archivos {
		fw, err := w.CreateFormFile(nombre, nombre+".jpg")
		if err != nil {
			t.Fatalf("CreateFormFile(%q): %v", nombre, err)
		}
		if _, err := io.WriteString(fw, contenido); err != nil {
			t.Fatalf("write file %q: %v", nombre, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// ─── Resumen.Vacio ───────────────────────────────────────────────────────────

func TestResumenVacio(t *testing.T) {
	t.Parallel()

	monto := decimal.NewFromInt(1)
	casos := []struct {
		nombre  string
		resumen *failedintent.Resumen
		quiero  bool
	}{
		{"nil está vacío", nil, true},
		{"todo en cero está vacío", &failedintent.Resumen{}, true},
		{"sólo el módulo sigue vacío", &failedintent.Resumen{Modulo: "ventas"}, true},
		{"con título no está vacío", &failedintent.Resumen{Titulo: "Ana"}, false},
		{"con monto no está vacío", &failedintent.Resumen{Monto: &monto}, false},
		{"con referencia no está vacío", &failedintent.Resumen{Referencia: "A-1"}, false},
		{"título de puros espacios cuenta como vacío", &failedintent.Resumen{Titulo: "   "}, true},
		{"referencia de puros espacios cuenta como vacía", &failedintent.Resumen{Referencia: "\t"}, true},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			if got := c.resumen.Vacio(); got != c.quiero {
				t.Fatalf("Vacio() = %v, quiero %v", got, c.quiero)
			}
		})
	}
}

// ─── Registro ────────────────────────────────────────────────────────────────

func TestRegistroDespachaPorPrefijo(t *testing.T) {
	t.Parallel()

	ventas := extractorFn(func(_ string, _ []byte, _ string) *failedintent.Resumen {
		return &failedintent.Resumen{Titulo: "Ana Pérez"}
	})
	pagos := extractorFn(func(_ string, _ []byte, _ string) *failedintent.Resumen {
		return &failedintent.Resumen{Titulo: "Cobrador"}
	})

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", ventas).
		Registrar("/v2/cobranza/pagos", "pagos", pagos)

	got := reg.Extraer("/v2/ventas", []byte(`{}`), "application/json")
	if got == nil || got.Titulo != "Ana Pérez" || got.Modulo != "ventas" {
		t.Fatalf("ventas: got %+v", got)
	}
	got = reg.Extraer("/v2/cobranza/pagos", []byte(`{}`), "application/json")
	if got == nil || got.Titulo != "Cobrador" || got.Modulo != "pagos" {
		t.Fatalf("pagos: got %+v", got)
	}
}

func TestRegistroRutaNoRegistradaDevuelveNil(t *testing.T) {
	t.Parallel()

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				t.Fatal("no debió llamarse: la ruta no coincide")
				return nil
			},
		))

	if got := reg.Extraer("/v2/visitas", []byte(`{}`), "application/json"); got != nil {
		t.Fatalf("quiero nil para una ruta sin extractor, got %+v", got)
	}
}

// El módulo se sabe por la RUTA, el resumen por el CUERPO. Son dos hechos
// distintos y el registro los separa: un cuerpo que el extractor no reconoce
// deja MODULO puesto y el resumen vacío, en vez de perder también el módulo y
// sacar el renglón del filtro de chips.
func TestRegistroConservaElModuloAunqueElCuerpoNoSeReconozca(t *testing.T) {
	t.Parallel()

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen { return nil },
		))

	got := reg.Extraer("/v2/ventas/123", []byte(`{"otra":"forma"}`), "application/json")
	if got == nil {
		t.Fatal("quiero un resumen con el módulo, got nil")
	}
	if got.Modulo != "ventas" {
		t.Fatalf("Modulo = %q, quiero \"ventas\"", got.Modulo)
	}
	if !got.Vacio() {
		t.Fatalf("quiero el resumen vacío, got %+v", got)
	}
}

// El registro es la autoridad sobre el módulo. Un extractor que declare otro
// no puede meter sus filas en el chip de un módulo ajeno.
func TestRegistroPisaElModuloQueDeclareElExtractor(t *testing.T) {
	t.Parallel()

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				return &failedintent.Resumen{Modulo: "pagos", Titulo: "Ana"}
			},
		))

	got := reg.Extraer("/v2/ventas", []byte(`{}`), "application/json")
	if got.Modulo != "ventas" {
		t.Fatalf("Modulo = %q, quiero \"ventas\"", got.Modulo)
	}
}

// Un extractor que revienta no puede tumbar la captura: perder la evidencia es
// peor que perder el nombre del cliente.
func TestRegistroAtrapaElPanicoDelExtractor(t *testing.T) {
	t.Parallel()

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				panic("el extractor se rompió")
			},
		))

	got := reg.Extraer("/v2/ventas", []byte(`{}`), "application/json")
	if got == nil {
		t.Fatal("quiero el resumen con el módulo aunque el extractor entre en pánico")
	}
	if got.Modulo != "ventas" || !got.Vacio() {
		t.Fatalf("got %+v, quiero sólo el módulo", got)
	}
}

func TestRegistroGanaElPrefijoMasLargo(t *testing.T) {
	t.Parallel()

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/cobranza", "cobranza", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen { return nil },
		)).
		Registrar("/v2/cobranza/pagos", "pagos", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen { return nil },
		))

	if got := reg.Extraer("/v2/cobranza/pagos", nil, ""); got.Modulo != "pagos" {
		t.Fatalf("Modulo = %q, quiero \"pagos\" (el prefijo más específico)", got.Modulo)
	}
	if got := reg.Extraer("/v2/cobranza/saldos", nil, ""); got.Modulo != "cobranza" {
		t.Fatalf("Modulo = %q, quiero \"cobranza\"", got.Modulo)
	}
}

func TestRegistroNiloEsUsable(t *testing.T) {
	t.Parallel()

	var reg *failedintent.RegistroExtractores
	if got := reg.Extraer("/v2/ventas", nil, ""); got != nil {
		t.Fatalf("un registro nil debe devolver nil, got %+v", got)
	}
}

// ─── ResumenDeIntento — cuerpo JSON en la fila ───────────────────────────────

func TestResumenDeIntentoLeeElCuerpoJSON(t *testing.T) {
	t.Parallel()

	var vistos struct {
		path        string
		body        string
		contentType string
	}
	ex := extractorFn(func(path string, body []byte, ct string) *failedintent.Resumen {
		vistos.path, vistos.body, vistos.contentType = path, string(body), ct
		return &failedintent.Resumen{Titulo: "Ana"}
	})

	intento := failedintent.Intent{
		Path: "/v2/ventas",
		Body: []byte(`{"cliente":{"nombre":"Ana"}}`),
	}
	got := failedintent.ResumenDeIntento(context.Background(), ex, nil, intento)
	if got == nil || got.Titulo != "Ana" {
		t.Fatalf("got %+v", got)
	}
	if vistos.path != "/v2/ventas" {
		t.Fatalf("path = %q", vistos.path)
	}
	if vistos.body != `{"cliente":{"nombre":"Ana"}}` {
		t.Fatalf("body = %q", vistos.body)
	}
	if vistos.contentType != "application/json" {
		t.Fatalf("contentType = %q, quiero application/json", vistos.contentType)
	}
}

func TestResumenDeIntentoSinExtractorDevuelveNil(t *testing.T) {
	t.Parallel()

	intento := failedintent.Intent{Path: "/v2/ventas", Body: []byte(`{}`)}
	if got := failedintent.ResumenDeIntento(context.Background(), nil, nil, intento); got != nil {
		t.Fatalf("quiero nil sin extractor, got %+v", got)
	}
}

// ─── ResumenDeIntento — cuerpo multipart en disco ────────────────────────────

func TestResumenDeIntentoAbreElBlobUnaSolaVez(t *testing.T) {
	t.Parallel()

	cuerpo, contentType := construirMultipart(
		t,
		map[string]string{"datos": `{"cliente":{"nombre":"Ana Pérez"}}`},
		map[string]string{"imagen": strings.Repeat("x", 1024)},
	)
	blob := nuevoBlobEnMemoria()
	blob.datos["venta.bin"] = cuerpo

	llamadas := 0
	ex := extractorFn(func(_ string, body []byte, _ string) *failedintent.Resumen {
		llamadas++
		if !bytes.Contains(body, []byte("Ana Pérez")) {
			return nil
		}
		return &failedintent.Resumen{Titulo: "Ana Pérez"}
	})

	intento := failedintent.Intent{
		Path:            "/v2/ventas",
		Body:            []byte(`null`),
		BodyBlobPath:    "venta.bin",
		BodyContentType: contentType,
	}
	got := failedintent.ResumenDeIntento(context.Background(), ex, blob, intento)
	if got == nil || got.Titulo != "Ana Pérez" {
		t.Fatalf("got %+v", got)
	}
	if blob.abrituras != 1 {
		t.Fatalf("el blob se abrió %d veces, quiero exactamente 1", blob.abrituras)
	}
	if llamadas != 1 {
		t.Fatalf("el extractor se llamó %d veces; los archivos no son campos", llamadas)
	}
}

func TestResumenDeIntentoIgnoraLasPartesDeArchivo(t *testing.T) {
	t.Parallel()

	cuerpo, contentType := construirMultipart(
		t,
		map[string]string{"datos": `{"cliente":{"nombre":"Ana"}}`},
		map[string]string{"imagen": "bytes-de-foto"},
	)
	blob := nuevoBlobEnMemoria()
	blob.datos["v.bin"] = cuerpo

	ex := extractorFn(func(_ string, body []byte, _ string) *failedintent.Resumen {
		if bytes.Contains(body, []byte("bytes-de-foto")) {
			t.Fatal("el extractor recibió una parte de archivo")
		}
		return &failedintent.Resumen{Titulo: "Ana"}
	})

	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: contentType,
	}
	if got := failedintent.ResumenDeIntento(context.Background(), ex, blob, intento); got == nil {
		t.Fatal("quiero resumen del campo datos")
	}
}

// Cuando el primer campo no se reconoce pero un campo posterior sí, gana el
// que trae el dato. El orden de los campos lo decide el teléfono, no nosotros.
func TestResumenDeIntentoSigueBuscandoTrasUnCampoAjeno(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("ruido", "no soy una venta")
	_ = w.WriteField("datos", `{"cliente":{"nombre":"Beto"}}`)
	_ = w.Close()

	blob := nuevoBlobEnMemoria()
	blob.datos["v.bin"] = buf.Bytes()

	ex := extractorFn(func(_ string, body []byte, _ string) *failedintent.Resumen {
		if !bytes.Contains(body, []byte("Beto")) {
			return nil
		}
		return &failedintent.Resumen{Titulo: "Beto"}
	})

	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: w.FormDataContentType(),
	}
	got := failedintent.ResumenDeIntento(context.Background(), ex, blob, intento)
	if got == nil || got.Titulo != "Beto" {
		t.Fatalf("got %+v", got)
	}
}

// El caso de producción del recorrido de partes, y el que una prueba con un
// extractor suelto NO alcanza: con un REGISTRO, el primer campo que no se
// reconoce devuelve un resumen NO NILO —trae el módulo y nada más—, y quedarse
// con él dejaría fuera el nombre que sí venía en el campo siguiente.
//
// La app manda el JSON en `datos`, pero el orden de las partes lo decide el
// teléfono: cualquier campo anterior apagaría el renglón.
func TestResumenDeIntentoNoSeQuedaConElResumenVacioDelPrimerCampo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("origen", "app-android")
	_ = w.WriteField("datos", `{"cliente":{"nombre":"Carmen López"}}`)
	_ = w.Close()

	blob := nuevoBlobEnMemoria()
	blob.datos["v.bin"] = buf.Bytes()

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, body []byte, _ string) *failedintent.Resumen {
				if !bytes.Contains(body, []byte(`"cliente"`)) {
					return nil // el registro lo convierte en {Modulo: "ventas"}
				}
				return &failedintent.Resumen{Titulo: "Carmen López"}
			},
		))

	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: w.FormDataContentType(),
	}
	got := failedintent.ResumenDeIntento(context.Background(), reg, blob, intento)
	if got == nil || got.Titulo != "Carmen López" {
		t.Fatalf("got %+v, quiero el nombre del campo `datos`", got)
	}
	if got.Modulo != "ventas" {
		t.Fatalf("Modulo = %q", got.Modulo)
	}
}

// Las partes de ARCHIVO no se le ofrecen al extractor, y la prueba lo mide con
// una parte de archivo que SÍ trae bytes inline.
//
// La distinción importa: hoy `readFilePart` deja `Value` en nil, así que el
// filtro por `Kind` parecería redundante. Es una coincidencia del lector de
// partes, no una garantía del contrato — el día que un archivo chico se
// inline, sin el filtro las fotos entrarían al extractor y una foto JPEG que
// por azar contenga la palabra `cliente` produciría un nombre inventado.
func TestResumenDePartesNoOfreceLosArchivosAunqueTraiganBytes(t *testing.T) {
	t.Parallel()

	partes := []failedintent.BlobPart{
		{
			Index: 0, Name: "imagen", Kind: failedintent.BlobPartKindFile,
			ContentType: "image/jpeg", Filename: "ine.jpg",
			Value: []byte(`{"cliente":{"nombre":"NO SOY UN NOMBRE"}}`),
		},
		{
			Index: 1, Name: "datos", Kind: failedintent.BlobPartKindField,
			ContentType: "application/json",
			Value:       []byte(`{"cliente":{"nombre":"Carmen López"}}`),
		},
	}

	vistos := []string{}
	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, body []byte, _ string) *failedintent.Resumen {
				if len(body) > 0 {
					vistos = append(vistos, string(body))
				}
				if !bytes.Contains(body, []byte("Carmen")) {
					return nil
				}
				return &failedintent.Resumen{Titulo: "Carmen López"}
			},
		))

	got := failedintent.ResumenDePartes(reg, "/v2/ventas", partes)
	if got == nil || got.Titulo != "Carmen López" {
		t.Fatalf("got %+v", got)
	}
	for _, v := range vistos {
		if strings.Contains(v, "NO SOY UN NOMBRE") {
			t.Fatal("el extractor recibió los bytes de una parte de ARCHIVO")
		}
	}
}

// El módulo se conoce por la ruta aunque ninguna parte se reconozca. Ese
// resumen vacío-con-módulo es lo que mantiene la fila dentro del chip.
func TestResumenDeIntentoConservaElModuloCuandoNingunCampoSirve(t *testing.T) {
	t.Parallel()

	cuerpo, contentType := construirMultipart(t,
		map[string]string{"datos": `{"nada":"que ver"}`}, nil)
	blob := nuevoBlobEnMemoria()
	blob.datos["v.bin"] = cuerpo

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen { return nil },
		))

	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: contentType,
	}
	got := failedintent.ResumenDeIntento(context.Background(), reg, blob, intento)
	if got == nil || got.Modulo != "ventas" || !got.Vacio() {
		t.Fatalf("got %+v, quiero sólo el módulo", got)
	}
}

// Sin BlobStorage no hay de dónde leer el cuerpo. El extractor puede seguir
// contestando por la RUTA —de ahí sale el módulo— pero jamás debe recibir
// bytes: no los hay, y pasárselos vacíos como si fueran el cuerpo lo haría
// concluir sobre algo que nunca leyó.
func TestResumenDeIntentoSinBlobStorageNoLeeCuerpo(t *testing.T) {
	t.Parallel()

	ex := extractorFn(func(_ string, body []byte, _ string) *failedintent.Resumen {
		if len(body) > 0 {
			t.Fatalf("el extractor recibió cuerpo (%q) y no hay de dónde leerlo", body)
		}
		return nil
	})
	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: "multipart/form-data; boundary=x",
	}
	if got := failedintent.ResumenDeIntento(context.Background(), ex, nil, intento); got != nil {
		t.Fatalf("got %+v, quiero nil", got)
	}

	// Con un registro sí queda el módulo, que es lo único que la ruta prueba.
	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", ex)
	got := failedintent.ResumenDeIntento(context.Background(), reg, nil, intento)
	if got == nil || got.Modulo != "ventas" || !got.Vacio() {
		t.Fatalf("got %+v, quiero sólo el módulo", got)
	}
}

func TestResumenDeIntentoBlobIlegibleNoRevienta(t *testing.T) {
	t.Parallel()

	blob := nuevoBlobEnMemoria()
	blob.errAbrir = errors.New("disco fuera")

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				return &failedintent.Resumen{Titulo: "no llega"}
			},
		))

	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin",
		BodyContentType: "multipart/form-data; boundary=x",
	}
	got := failedintent.ResumenDeIntento(context.Background(), reg, blob, intento)
	// El cuerpo no se pudo leer, pero la ruta sigue diciendo el módulo.
	if got == nil || got.Modulo != "ventas" || !got.Vacio() {
		t.Fatalf("got %+v, quiero sólo el módulo", got)
	}
}

// Un blob cuyo Content-Type no es multipart (no debería pasar, pero la fila lo
// permite) no puede tumbar nada.
func TestResumenDeIntentoBlobNoMultipart(t *testing.T) {
	t.Parallel()

	blob := nuevoBlobEnMemoria()
	blob.datos["v.bin"] = []byte(`{"cliente":{"nombre":"Ana"}}`)

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, _ []byte, _ string) *failedintent.Resumen {
				return &failedintent.Resumen{Titulo: "Ana"}
			},
		))

	intento := failedintent.Intent{
		Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: "application/json",
	}
	got := failedintent.ResumenDeIntento(context.Background(), reg, blob, intento)
	if got == nil || got.Modulo != "ventas" || !got.Vacio() {
		t.Fatalf("got %+v", got)
	}
}

// ─── Fuzz: ningún cuerpo arbitrario puede tumbar la extracción ──────────────

// FuzzResumenDeIntentoMultipart empuja bytes arbitrarios por la ruta de
// parseo del multipart. La captura corre dentro de la petición del vendedor:
// un pánico aquí es una venta perdida, no un log feo.
func FuzzResumenDeIntentoMultipart(f *testing.F) {
	semilla, ct := construirMultipart(&testing.T{},
		map[string]string{"datos": `{"cliente":{"nombre":"Ana"}}`}, nil)
	f.Add(semilla, ct)
	f.Add([]byte("--x\r\n\r\n--x--"), "multipart/form-data; boundary=x")
	f.Add([]byte(""), "multipart/form-data; boundary=")
	f.Add([]byte("basura"), "no-es-un-tipo")

	reg := failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", extractorFn(
			func(_ string, body []byte, _ string) *failedintent.Resumen {
				if len(body) == 0 {
					return nil
				}
				return &failedintent.Resumen{Titulo: string(body)}
			},
		))

	f.Fuzz(func(t *testing.T, cuerpo []byte, contentType string) {
		blob := nuevoBlobEnMemoria()
		blob.datos["v.bin"] = cuerpo
		intento := failedintent.Intent{
			Path: "/v2/ventas", BodyBlobPath: "v.bin", BodyContentType: contentType,
		}
		got := failedintent.ResumenDeIntento(context.Background(), reg, blob, intento)
		// El contrato mínimo: nunca entra en pánico y, si devuelve algo, el
		// módulo es el de la ruta y no el que traiga el cuerpo.
		if got != nil && got.Modulo != "ventas" {
			t.Fatalf("Modulo = %q", got.Modulo)
		}
	})
}
