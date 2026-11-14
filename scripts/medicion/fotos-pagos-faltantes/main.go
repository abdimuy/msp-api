// Package main provee un script de solo lectura para medir si faltan comprobantes fisicos
// asociados a los registros en la base de datos de pagos (E-COB-060).
//
//nolint:misspell // El script utiliza vocabulario en español.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"

	_ "github.com/nakagami/firebirdsql"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var (
	errConfig             = errors.New("FB_DATABASE (o FIREBIRD_DSN) es obligatorio")
	errStorageDirVacio    = errors.New("la variable de entorno STORAGE_DIR es obligatoria y no puede estar vacia")
	errStorageDirNoExiste = errors.New("el directorio especificado en STORAGE_DIR no existe")
	errStorageDirNoEsDir  = errors.New("la ruta especificada en STORAGE_DIR no es un directorio")
)

// RegistroImagen representa una fila de la tabla MSP_PAGOS_IMAGENES.
type RegistroImagen struct {
	ID         string
	PagoID     string
	StorageKey string
	CreatedAt  time.Time
}

// ErrorRevision registra un fallo distinto a fs.ErrNotExist al inspeccionar un archivo.
type ErrorRevision struct {
	Registro RegistroImagen
	Err      error
}

// ResultadoConteo almacena los totales, el detalle de archivos faltantes y los errores de lectura.
type ResultadoConteo struct {
	Total          int
	ConArchivo     int
	SinArchivo     int
	ErrorAlRevisar int
	Faltantes      []RegistroImagen
	Errores        []ErrorRevision
}

// ValidarStorageDir verifica que STORAGE_DIR exista, no este vacia y apunte a un directorio
// existente, e imprime la ruta absoluta resuelta en la primera linea de la salida estandar.
func ValidarStorageDir(storageDir string, w io.Writer) (string, error) {
	if storageDir == "" {
		return "", errStorageDirVacio
	}

	absDir, err := filepath.Abs(storageDir)
	if err != nil {
		return "", fmt.Errorf("error resolviendo la ruta absoluta de STORAGE_DIR: %w", err)
	}

	//nolint:gosec // la ruta viene de STORAGE_DIR
	info, err := os.Stat(absDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", errStorageDirNoExiste, absDir)
		}
		return "", fmt.Errorf("error al verificar STORAGE_DIR (%s): %w", absDir, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s", errStorageDirNoEsDir, absDir)
	}

	_, _ = fmt.Fprintln(w, absDir)
	return absDir, nil
}

// ContarArchivos itera los registros comprobando con os.Stat si cada archivo
// existe fisicamente bajo storageDir.
func ContarArchivos(storageDir string, registros []RegistroImagen) ResultadoConteo {
	res := ResultadoConteo{
		Total: len(registros),
	}
	for _, reg := range registros {
		rutaLimpia := filepath.Clean(filepath.Join(storageDir, reg.StorageKey))
		_, err := os.Stat(rutaLimpia)
		switch {
		case err == nil:
			res.ConArchivo++
		case errors.Is(err, fs.ErrNotExist):
			res.SinArchivo++
			res.Faltantes = append(res.Faltantes, reg)
		default:
			res.ErrorAlRevisar++
			res.Errores = append(res.Errores, ErrorRevision{
				Registro: reg,
				Err:      err,
			})
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
		var (
			r            RegistroImagen
			createdAtRaw any
		)
		if err := rows.Scan(&r.ID, &r.PagoID, &r.StorageKey, &createdAtRaw); err != nil {
			return nil, fmt.Errorf("error al escanear fila: %w", err)
		}
		createdAt, err := firebird.ScanUTCTime(createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("error al convertir CREATED_AT: %w", err)
		}
		r.CreatedAt = createdAt
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
	_, _ = fmt.Fprintf(w, "Error al revisar: %d\n", res.ErrorAlRevisar)

	if len(res.Faltantes) > 0 {
		_, _ = fmt.Fprintf(w, "\nDetalle de faltantes:\n")
		for _, f := range res.Faltantes {
			_, _ = fmt.Fprintf(w, "ID: %s, Pago: %s, Fecha: %s, Ruta: %s\n",
				f.ID, f.PagoID, f.CreatedAt.Format(time.RFC3339), f.StorageKey)
		}
	}

	if len(res.Errores) > 0 {
		_, _ = fmt.Fprintf(w, "\nDetalle de errores al revisar:\n")
		for _, e := range res.Errores {
			_, _ = fmt.Fprintf(w, "ID: %s, Pago: %s, Fecha: %s, Ruta: %s, Error: %v\n",
				e.Registro.ID, e.Registro.PagoID, e.Registro.CreatedAt.Format(time.RFC3339), e.Registro.StorageKey, e.Err)
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
	storageDir, err := ValidarStorageDir(os.Getenv("STORAGE_DIR"), os.Stdout)
	if err != nil {
		return err
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
		os.Exit(1)
	}
}
