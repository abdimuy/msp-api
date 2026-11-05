// Package main provee un script de solo lectura para medir si faltan comprobantes fisicos
// asociados a los registros en la base de datos de pagos.
//
//nolint:misspell // El script utiliza vocabulario en español.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	_ "github.com/nakagami/firebirdsql"
)

var errConfig = errors.New("FB_DATABASE (o FIREBIRD_DSN) es obligatorio")

// RegistroImagen representa una fila de la tabla MSP_PAGOS_IMAGENES.
type RegistroImagen struct {
	ID         string
	PagoID     string
	StorageKey string
	CreatedAt  time.Time
}

// ResultadoConteo almacena los totales y el detalle de archivos faltantes.
type ResultadoConteo struct {
	Total      int
	ConArchivo int
	SinArchivo int
	Faltantes  []RegistroImagen
}

// ContarArchivos itera los registros comprobando con os.Stat si cada archivo
// existe fisicamente bajo storageDir.
func ContarArchivos(storageDir string, registros []RegistroImagen) ResultadoConteo {
	res := ResultadoConteo{
		Total: len(registros),
	}
	for _, reg := range registros {
		rutaLimpia := filepath.Clean(filepath.Join(storageDir, reg.StorageKey))
		if _, err := os.Stat(rutaLimpia); err == nil {
			res.ConArchivo++
		} else {
			res.SinArchivo++
			res.Faltantes = append(res.Faltantes, reg)
		}
	}
	return res
}

// LeerRegistros consulta la base de datos para obtener las fotos de pagos registradas.
func LeerRegistros(ctx context.Context, db *sql.DB) ([]RegistroImagen, error) {
	const consulta = "SELECT ID, PAGO_ID, STORAGE_KEY, CREATED_AT FROM MSP_PAGOS_IMAGENES"
	rows, err := db.QueryContext(ctx, consulta)
	if err != nil {
		return nil, fmt.Errorf("error al consultar imagenes de pagos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var registros []RegistroImagen
	for rows.Next() {
		var r RegistroImagen
		if err := rows.Scan(&r.ID, &r.PagoID, &r.StorageKey, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("error al escanear fila: %w", err)
		}
		registros = append(registros, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error en iteracion de filas: %w", err)
	}
	return registros, nil
}

// ImprimirReporte da formato a la salida de la medicion.
func ImprimirReporte(w io.Writer, res ResultadoConteo) {
	_, _ = fmt.Fprintf(w, "Total de filas: %d\n", res.Total)
	_, _ = fmt.Fprintf(w, "ConArchivo: %d\n", res.ConArchivo)
	_, _ = fmt.Fprintf(w, "Sin archivo: %d\n", res.SinArchivo)

	if len(res.Faltantes) > 0 {
		_, _ = fmt.Fprintf(w, "\nDetalle de faltantes:\n")
		for _, f := range res.Faltantes {
			_, _ = fmt.Fprintf(w, "ID: %s, Pago: %s, Fecha: %s, Ruta: %s\n",
				f.ID, f.PagoID, f.CreatedAt.Format(time.RFC3339), f.StorageKey)
		}
	}
}

func obtenerEnv(clave, valorPorDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return valorPorDefecto
}

func armarDSN() (string, error) {
	if dsn := os.Getenv("FIREBIRD_DSN"); dsn != "" {
		return dsn, nil
	}
	db := os.Getenv("FB_DATABASE")
	if db == "" {
		return "", errConfig
	}

	host := obtenerEnv("FB_HOST", "localhost")
	port := obtenerEnv("FB_PORT", "3050")
	user := obtenerEnv("FB_USER", "SYSDBA")
	pass := os.Getenv("FB_PASSWORD")
	charset := obtenerEnv("FB_CHARSET", "UTF8")
	wireCrypt := obtenerEnv("FB_WIRE_CRYPT", "true")
	wireCompress := obtenerEnv("FB_WIRE_COMPRESS", "false")

	hostPort := net.JoinHostPort(host, port)
	return fmt.Sprintf(
		"%s:%s@%s/%s?charset=%s&wire_crypt=%s&wire_compress=%s",
		user, pass, hostPort, db, charset, wireCrypt, wireCompress,
	), nil
}

func run() error {
	storageDir := os.Getenv("STORAGE_DIR")
	if storageDir == "" {
		storageDir = "./var/uploads"
	}

	dsn, err := armarDSN()
	if err != nil {
		return fmt.Errorf("error de configuracion: %w", err)
	}

	db, err := sql.Open("firebirdsql", dsn)
	if err != nil {
		return fmt.Errorf("error abriendo conexion: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("error conectando a la base de datos: %w", err)
	}

	registros, err := LeerRegistros(ctx, db)
	if err != nil {
		return fmt.Errorf("error leyendo registros: %w", err)
	}

	resultado := ContarArchivos(storageDir, registros)
	ImprimirReporte(os.Stdout, resultado)

	return nil
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Fallo critico: %v\n", err)
		os.Exit(2)
	}
}
