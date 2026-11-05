package main

import (
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

	mainPath := filepath.Join(filepath.Dir(filename), "main.go")
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("no se pudo leer main.go: %v", err)
	}

	contenido := string(data)
	cadenasProhibidas := []string{
		"DELETE",
		"UPDATE",
		"INSERT",
		"os.Remove",
	}

	for _, prohibida := range cadenasProhibidas {
		if strings.Contains(contenido, prohibida) {
			t.Fatalf("REGLA DE ORO VIOLADA: main.go contiene la cadena prohibida %q", prohibida)
		}
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
