package failedintent_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// moduloPermitido es el ÚNICO módulo que la plataforma de captura puede
// importar.
//
// `auth` está permitido porque la captura necesita el CurrentUser que planta
// el middleware de autenticación para saber DE QUIÉN es el intento, y ese
// contrato es transversal —lo usa todo el API— no de un módulo de negocio.
const moduloPermitido = "github.com/abdimuy/msp-api/internal/auth"

// TestPlataformaNoImportaModulos es la prueba de aislamiento del puerto.
//
// Todo el diseño del resumen descansa en una inversión: la plataforma declara
// `ResumenExtractor`, ventas y cobranza lo implementan, y el único sitio donde
// ambos se conocen es `cmd/api`. Si algún día alguien "arregla" esto
// importando `internal/ventas` aquí para leer un `cliente.nombre`, la captura
// deja de servir para pagos y para visitas — y el cambio se vería inocente en
// la revisión, porque compilaría y las pruebas de resumen seguirían verdes.
//
// Existe además de la regla `failedintent-no-modules` de depguard, no en su
// lugar. La regla vive en `.golangci.yml` y sólo protege mientras el linter
// corra con esa configuración; esta prueba corre con `go test` y protege
// también a quien empuje con `--no-verify`. Que sean dos no es redundancia: es
// que una de las dos se puede desactivar editando un YAML.
func TestPlataformaNoImportaModulos(t *testing.T) {
	t.Parallel()

	const raiz = "."
	fset := token.NewFileSet()
	violaciones := map[string][]string{}

	err := filepath.WalkDir(raiz, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(ruta, ".go") {
			return nil
		}
		archivo, perr := parser.ParseFile(fset, ruta, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range archivo.Imports {
			ruta2, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			if esModuloDeNegocio(ruta2) {
				violaciones[ruta] = append(violaciones[ruta], ruta2)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorriendo el paquete: %v", err)
	}

	// Control positivo: la prueba sólo prueba algo si de verdad leyó imports.
	// Sin esto, un WalkDir que no encontrara nada pasaría en verde y la
	// ausencia de violaciones no significaría nada.
	if !leyoAlgunImport(t, fset) {
		t.Fatal("la prueba no leyó ningún import; no está midiendo nada")
	}

	for archivo, imports := range violaciones {
		t.Errorf(
			"%s importa %v: la plataforma de captura no puede saber de un módulo "+
				"de negocio. Declare un puerto aquí y regístrelo en cmd/api "+
				"(ver failedintent.ResumenExtractor).",
			archivo, imports,
		)
	}
}

// esModuloDeNegocio reporta si el import apunta a un módulo del API que no sea
// el permitido ni la propia plataforma.
func esModuloDeNegocio(ruta string) bool {
	const prefijo = "github.com/abdimuy/msp-api/internal/"
	if !strings.HasPrefix(ruta, prefijo) {
		return false
	}
	if strings.HasPrefix(ruta, prefijo+"platform/") || ruta == prefijo+"platform" {
		return false
	}
	if ruta == moduloPermitido || strings.HasPrefix(ruta, moduloPermitido+"/") {
		return false
	}
	return true
}

// leyoAlgunImport comprueba que el recorrido encontró archivos con imports.
// Es el control positivo de la prueba de aislamiento.
func leyoAlgunImport(t *testing.T, fset *token.FileSet) bool {
	t.Helper()
	archivos := 0
	fset.Iterate(func(*token.File) bool {
		archivos++
		return true
	})
	return archivos > 0
}
