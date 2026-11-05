package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
)

// This file pins the fingerprint of tarea 9: it must hash the whole command
// except the idempotency key. Two things are checked here that no behavioural
// test can guarantee on its own — that every command is covered and that every
// field of each command counts. A field added later without being hashed would
// let a key be reused with different content and answer "already done", which
// is exactly the defect this task removes.

// comandosDePrueba is every exported command of the app package, by its type
// name. The parser test compares this map against the source, so a new command
// cannot be forgotten.
func comandosDePrueba() map[string]any {
	return map[string]any{
		"AbrirGarantiaCmd":         app.AbrirGarantiaCmd{},
		"AdjuntarImagenCmd":        app.AdjuntarImagenCmd{},
		"AgregarArticuloCmd":       app.AgregarArticuloCmd{},
		"AutorizarCambioFisicoCmd": app.AutorizarCambioFisicoCmd{},
		"AvanzarArticuloCmd":       app.AvanzarArticuloCmd{},
		"CancelarCmd":              app.CancelarCmd{},
		"CerrarCmd":                app.CerrarCmd{},
		"EntregarCmd":              app.EntregarCmd{},
		"IniciarProcesoCmd":        app.IniciarProcesoCmd{},
		"MarcarListoEntregaCmd":    app.MarcarListoEntregaCmd{},
		"MarcarNoDevueltoCmd":      app.MarcarNoDevueltoCmd{},
		"MoverArticuloCmd":         app.MoverArticuloCmd{},
		"RegistrarDesenlaceCmd":    app.RegistrarDesenlaceCmd{},
		"RegistrarDiagnosticoCmd":  app.RegistrarDiagnosticoCmd{},
		"RegistrarDictamenCmd":     app.RegistrarDictamenCmd{},
	}
}

// TestHuellaDe_CubreTodosLosComandos parses the app package source and asserts
// that comandosDePrueba lists exactly the exported *Cmd structs it finds. The
// check is on names and fields, so a new command or a new field shows up here
// instead of slipping past the fingerprint tests.
func TestHuellaDe_CubreTodosLosComandos(t *testing.T) {
	t.Parallel()
	delFuente := camposDeComandos(t)
	delMapa := comandosDePrueba()

	for nombre := range delFuente {
		if _, ok := delMapa[nombre]; !ok {
			t.Errorf("el comando %s existe en app y no esta cubierto por comandosDePrueba", nombre)
		}
	}
	for nombre := range delMapa {
		if _, ok := delFuente[nombre]; !ok {
			t.Errorf("comandosDePrueba declara %s, que ya no existe en app", nombre)
		}
	}
}

// TestHuellaDe_TodosLosCamposCuentan builds every command with non-zero values
// and mutates one field at a time: every field except ClaveIdempotencia must
// change the fingerprint. The key is the one exception because it is blanked
// before hashing — the same request under two key spellings is the same
// request.
func TestHuellaDe_TodosLosCamposCuentan(t *testing.T) {
	t.Parallel()
	for nombre, cero := range comandosDePrueba() {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			base := reflect.New(reflect.TypeOf(cero)).Elem()
			for i := range base.NumField() {
				llenarComando(base.Field(i))
			}
			huellaBase := app.HuellaDeParaPruebas(base.Interface())
			if huellaBase == "" {
				t.Fatalf("la huella base no debe ser vacia")
			}

			for i := range base.NumField() {
				campo := base.Field(i)
				original := reflect.New(campo.Type()).Elem()
				original.Set(campo)
				campo.Set(mutarCampo(campo))

				huella := app.HuellaDeParaPruebas(base.Interface())
				if campoName := base.Type().Field(i).Name; campoName == "ClaveIdempotencia" {
					if huella != huellaBase {
						t.Errorf("cambiar %s no debe cambiar la huella: %s != %s", campoName, huella, huellaBase)
					}
				} else if huella == huellaBase {
					t.Errorf("el campo %s no cuenta en la huella", campoName)
				}

				campo.Set(original)
			}
		})
	}
}

// TestHuellaDe_ClaveNoCuenta is the behavioural counterpart of the table: the
// same request under two different keys has one fingerprint.
func TestHuellaDe_ClaveNoCuenta(t *testing.T) {
	t.Parallel()
	a := app.CancelarCmd{
		GarantiaID:        uuid.New(),
		Motivo:            "cliente no aparece",
		ClaveIdempotencia: uuid.NewString(),
		DeviceCreatedAt:   time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	b := a
	b.ClaveIdempotencia = strings.ToUpper(uuid.NewString())

	if app.HuellaDeParaPruebas(a) != app.HuellaDeParaPruebas(b) {
		t.Error("la clave de idempotencia no debe entrar en la huella")
	}
}

// TestHuellaDe_ContenidoDistinto: any other change is a different fingerprint.
func TestHuellaDe_ContenidoDistinto(t *testing.T) {
	t.Parallel()
	lat, lon := 19.4326, -99.1332
	a := app.EntregarCmd{
		GarantiaID:        uuid.New(),
		Firma:             []byte("firma-a"),
		FirmaContentType:  "image/png",
		ClaveIdempotencia: uuid.NewString(),
		DeviceCreatedAt:   time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		GPSLat:            &lat,
		GPSLon:            &lon,
	}
	b := a
	b.Firma = []byte("firma-b")
	if app.HuellaDeParaPruebas(a) == app.HuellaDeParaPruebas(b) {
		t.Error("dos firmas distintas deben dar huellas distintas")
	}

	c := a
	otroLat := lat + 0.001
	c.GPSLat = &otroLat
	if app.HuellaDeParaPruebas(a) == app.HuellaDeParaPruebas(c) {
		t.Error("dos posiciones distintas deben dar huellas distintas")
	}
}

// TestHuellaDe_MismaFormaDistintoTipo: IniciarProceso and MarcarListoEntrega
// have the same fields; only the type name tells them apart, so the type must
// be part of the hash.
func TestHuellaDe_MismaFormaDistintoTipo(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	iniciar := app.IniciarProcesoCmd{GarantiaID: id, ClaveIdempotencia: clave(), DeviceCreatedAt: now}
	listo := app.MarcarListoEntregaCmd{GarantiaID: id, ClaveIdempotencia: clave(), DeviceCreatedAt: now}

	if app.HuellaDeParaPruebas(iniciar) == app.HuellaDeParaPruebas(listo) {
		t.Error("dos comandos de la misma forma deben distinguirse por su tipo")
	}
}

// TestHuellaDe_Puntero: a pointer to a command hashes like the command.
func TestHuellaDe_Puntero(t *testing.T) {
	t.Parallel()
	cmd := app.CerrarCmd{GarantiaID: uuid.New(), ClaveIdempotencia: clave()}
	if app.HuellaDeParaPruebas(&cmd) != app.HuellaDeParaPruebas(cmd) {
		t.Error("el puntero y el valor deben dar la misma huella")
	}
}

// TestHuellaDe_FallaCerrada: a nil pointer, a non-struct and a value JSON cannot
// marshal all yield the empty string, which never matches a stored fingerprint.
func TestHuellaDe_FallaCerrada(t *testing.T) {
	t.Parallel()
	var puntero *app.CerrarCmd
	casos := map[string]any{
		"nil":        nil,
		"puntero":    puntero,
		"entero":     42,
		"rebanada":   []string{"x"},
		"no-marshal": struct{ F func() }{},
	}
	for nombre, cmd := range casos {
		if got := app.HuellaDeParaPruebas(cmd); got != "" {
			t.Errorf("%s: huella = %q, want vacia", nombre, got)
		}
	}
}

// TestHuellaDe_SinClaveIdempotencia: a struct without the key field hashes
// normally instead of panicking.
func TestHuellaDe_SinClaveIdempotencia(t *testing.T) {
	t.Parallel()
	tipo := struct{ A string }{A: "x"}
	if got := app.HuellaDeParaPruebas(tipo); got == "" {
		t.Error("un struct sin ClaveIdempotencia debe poder hashearse")
	}
}

// camposDeComandos parses every non-test .go file of the app package and
// returns the exported *Cmd structs with their field names, in declaration
// order.
func camposDeComandos(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	entradas, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	out := make(map[string][]string)
	for _, entrada := range entradas {
		nombre := entrada.Name()
		if entrada.IsDir() || !strings.HasSuffix(nombre, ".go") ||
			strings.HasSuffix(nombre, "_test.go") {
			continue
		}
		archivo, err := parser.ParseFile(fset, nombre, nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", nombre, err)
		}
		for _, decl := range archivo.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if !strings.HasSuffix(ts.Name.Name, "Cmd") || !ast.IsExported(ts.Name.Name) {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				var campos []string
				for _, f := range st.Fields.List {
					for _, n := range f.Names {
						campos = append(campos, n.Name)
					}
				}
				out[ts.Name.Name] = campos
			}
		}
	}
	return out
}

// llenarComando fills a zero struct value with a non-zero value per field type,
// so every field is a candidate for the mutation comparison.
func llenarComando(v reflect.Value) {
	t := v.Type()
	switch {
	case t == reflect.TypeOf(uuid.UUID{}):
		v.Set(reflect.ValueOf(uuid.New()))
	case t == reflect.TypeOf(time.Time{}):
		v.Set(reflect.ValueOf(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)))
	case t.Kind() == reflect.Pointer:
		p := reflect.New(t.Elem())
		llenarComando(p.Elem())
		v.Set(p)
	case t.Kind() == reflect.Slice:
		s := reflect.MakeSlice(t, 1, 1)
		llenarComando(s.Index(0))
		v.Set(s)
	case t.Kind() == reflect.String:
		v.SetString("base")
	case t.Kind() == reflect.Int:
		v.SetInt(7)
	case t.Kind() == reflect.Float64:
		v.SetFloat(19.5)
	case t.Kind() == reflect.Uint8:
		v.SetUint(1)
	default:
		panic("tipo de campo sin valor de prueba: " + t.String())
	}
}

// mutarCampo returns a value of the same type that differs in JSON from v.
func mutarCampo(v reflect.Value) reflect.Value {
	t := v.Type()
	switch {
	case t == reflect.TypeOf(uuid.UUID{}):
		return reflect.ValueOf(uuid.New())
	case t == reflect.TypeOf(time.Time{}):
		return reflect.ValueOf(v.Interface().(time.Time).Add(time.Minute))
	case t.Kind() == reflect.Pointer:
		p := reflect.New(t.Elem())
		p.Elem().Set(mutarCampo(v.Elem()))
		return p
	case t.Kind() == reflect.Slice:
		s := reflect.MakeSlice(t, v.Len()+1, v.Len()+1)
		reflect.Copy(s, v)
		llenarComando(s.Index(v.Len()))
		return s
	case t.Kind() == reflect.String:
		return reflect.ValueOf(v.String() + "-x").Convert(t)
	case t.Kind() == reflect.Int:
		return reflect.ValueOf(v.Int() + 1).Convert(t)
	case t.Kind() == reflect.Float64:
		return reflect.ValueOf(v.Float() + 0.5).Convert(t)
	case t.Kind() == reflect.Uint8:
		return reflect.ValueOf(v.Uint() + 1).Convert(t)
	default:
		panic("tipo de campo sin mutacion: " + t.String())
	}
}
