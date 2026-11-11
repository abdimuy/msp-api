package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain_ReglaDeOro_NoContieneMutaciones(t *testing.T) {
	t.Parallel()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no se pudo determinar la ruta de main_test.go")
	}

	dir := filepath.Dir(filename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no se pudo leer el directorio del paquete: %v", err)
	}

	cadenasProhibidas := []string{
		"delete",
		"update",
		"insert",
		"os.remove",
		"exec",
	}

	metodosProhibidos := []string{
		"Exec",
		"ExecContext",
		"Remove",
		"RemoveAll",
	}

	archivosRevisados := 0
	fset := token.NewFileSet()

	for _, entry := range entries {
		nombre := entry.Name()
		if entry.IsDir() || filepath.Ext(nombre) != ".go" || strings.HasSuffix(nombre, "_test.go") {
			continue
		}
		archivosRevisados++

		rutaArchivo := filepath.Join(dir, nombre)
		data, err := os.ReadFile(rutaArchivo)
		if err != nil {
			t.Fatalf("no se pudo leer %s: %v", nombre, err)
		}

		contenidoLower := strings.ToLower(string(data))
		for _, prohibida := range cadenasProhibidas {
			if strings.Contains(contenidoLower, strings.ToLower(prohibida)) {
				t.Fatalf("REGLA DE ORO VIOLADA: %s contiene la cadena prohibida %q (case-insensitive)", nombre, prohibida)
			}
		}

		fileAST, err := parser.ParseFile(fset, rutaArchivo, data, 0)
		if err != nil {
			t.Fatalf("no se pudo parsear el AST de %s: %v", nombre, err)
		}

		ast.Inspect(fileAST, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			for _, metodo := range metodosProhibidos {
				if strings.EqualFold(sel.Sel.Name, metodo) {
					t.Fatalf("REGLA DE ORO VIOLADA: %s invoca o referencia el metodo prohibido %q", nombre, sel.Sel.Name)
				}
			}
			return true
		})
	}

	if archivosRevisados == 0 {
		t.Fatal("no se encontro ningun archivo .go de produccion para revisar")
	}
}

func TestStorageDir_FallaSiNoExisteOVacio(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if _, err := ValidarStorageDir("", &buf); err == nil {
		t.Fatal("esperaba error cuando STORAGE_DIR esta vacio, pero fue nil")
	}

	rutaInexistente := filepath.Join(t.TempDir(), "directorio-que-no-existe")
	if _, err := ValidarStorageDir(rutaInexistente, &buf); err == nil {
		t.Fatalf("esperaba error cuando STORAGE_DIR no existe (%s), pero fue nil", rutaInexistente)
	}
}

func TestStorageDir_FallaSiNoEsDirectorio(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	archivoPath := filepath.Join(tempDir, "archivo.txt")
	if err := os.WriteFile(archivoPath, []byte("contenido"), 0o600); err != nil {
		t.Fatalf("no se pudo crear archivo de prueba: %v", err)
	}

	var buf bytes.Buffer
	if _, err := ValidarStorageDir(archivoPath, &buf); err == nil {
		t.Fatalf("esperaba error cuando STORAGE_DIR apunta a un archivo (%s), pero fue nil", archivoPath)
	}
}

func TestStorageDir_ImprimeRutaAbsolutaResuelta(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rutaConRelativo := filepath.Join(tempDir, ".")
	esperadaAbs, err := filepath.Abs(rutaConRelativo)
	if err != nil {
		t.Fatalf("error calculando filepath.Abs: %v", err)
	}

	var buf bytes.Buffer
	resuelta, err := ValidarStorageDir(rutaConRelativo, &buf)
	if err != nil {
		t.Fatalf("no esperaba error con directorio valido, obtenido: %v", err)
	}
	if resuelta != esperadaAbs {
		t.Errorf("esperaba ruta resuelta %q, obtenido %q", esperadaAbs, resuelta)
	}

	primeraLinea := strings.Split(strings.TrimSpace(buf.String()), "\n")[0]
	if primeraLinea != esperadaAbs {
		t.Errorf("esperaba en la primera linea %q, obtenido %q", esperadaAbs, primeraLinea)
	}
}

func TestContarArchivos_ControlPositivo_ReportaFaltante(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	storageKey := filepath.Join("pagos", "control-pago", "comprobante.jpg")
	fullPath := filepath.Join(tempDir, storageKey)

	// Crear el directorio y el archivo falso
	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err != nil {
		t.Fatalf("no se pudo crear directorio temporal: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("fake-image-bytes"), 0o600); err != nil {
		t.Fatalf("no se pudo crear archivo temporal: %v", err)
	}

	reg := RegistroImagen{
		ID:         "img-uuid-001",
		PagoID:     "pago-uuid-001",
		StorageKey: filepath.ToSlash(storageKey),
		CreatedAt:  time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC),
	}

	// 1. Control previo: el archivo existe físicamente
	resAntes := ContarArchivos(tempDir, []RegistroImagen{reg})
	if resAntes.Total != 1 || resAntes.ConArchivo != 1 || resAntes.SinArchivo != 0 {
		t.Fatalf("estado inicial incorrecto: %+v", resAntes)
	}

	// 2. Borrar manualmente con os.Remove dentro del test
	if err := os.Remove(fullPath); err != nil {
		t.Fatalf("no se pudo borrar el archivo en el test: %v", err)
	}

	// 3. Invocar la lógica de conteo y asegurar que reporta el archivo como faltante
	resDespues := ContarArchivos(tempDir, []RegistroImagen{reg})
	if resDespues.Total != 1 {
		t.Errorf("esperaba Total=1, obtenido=%d", resDespues.Total)
	}
	if resDespues.ConArchivo != 0 {
		t.Errorf("esperaba ConArchivo=0, obtenido=%d", resDespues.ConArchivo)
	}
	if resDespues.SinArchivo != 1 {
		t.Errorf("esperaba SinArchivo=1, obtenido=%d", resDespues.SinArchivo)
	}
	if len(resDespues.Faltantes) != 1 {
		t.Fatalf("esperaba 1 archivo faltante, obtenido=%d", len(resDespues.Faltantes))
	}
	if resDespues.Faltantes[0].ID != reg.ID {
		t.Errorf("esperaba ID %s, obtenido %s", reg.ID, resDespues.Faltantes[0].ID)
	}
	if resDespues.Faltantes[0].PagoID != reg.PagoID {
		t.Errorf("esperaba PagoID %s, obtenido %s", reg.PagoID, resDespues.Faltantes[0].PagoID)
	}
	if resDespues.Faltantes[0].StorageKey != reg.StorageKey {
		t.Errorf("esperaba StorageKey %s, obtenido %s", reg.StorageKey, resDespues.Faltantes[0].StorageKey)
	}
}
