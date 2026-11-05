package sender

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
	"github.com/abdimuy/msp-api/internal/comprobantes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// senderDirMode is the permission bits used for the base directory created by
// NewLocalSender, matching the storage provider.
const senderDirMode os.FileMode = 0o700

// maxDocNameLength is the upper bound on document file names. Names are
// caller-supplied and short; 500 chars is far above the realistic shape.
const maxDocNameLength = 500

// sidecarSuffix is appended to the written document file name to derive the
// JSON record of what (and to whom) would have been sent.
const sidecarSuffix = ".envio.json"

// errSenderInvalidNombre is returned by validateDocName on any malformed name.
// The document name becomes a file name in the delivery directory, so it gets
// the same path-traversal guards as a storage key.
var errSenderInvalidNombre = apperror.NewValidation(
	"sender_nombre_invalido",
	"nombre de documento inválido",
)

// LocalSender delivers rendered receipts to the local filesystem instead of
// WhatsApp. It is the test channel that anyone can use to verify the flow end
// to end without spending production message credits, and it stays in the
// codebase forever as the testing mode.
//
// Each Enviar writes two files under baseDir: the document with the exact
// bytes received (uuid-prefixed so two sends with the same Nombre never
// collide), and a `<doc>.envio.json` sidecar recording the destination, the
// template, its variables and the instant it was written — the trace of "who
// was sent what, and when".
type LocalSender struct {
	baseDir string
}

// NewLocalSender constructs a LocalSender rooted at baseDir. It resolves the
// path to absolute, creates the directory tree if missing, and verifies it is
// a writable directory — the same base-dir contract as the storage provider.
func NewLocalSender(baseDir string) (*LocalSender, error) {
	return newLocalSender(baseDir, nil)
}

// newLocalSender is the internal constructor that accepts an optional stat
// override for testability. statFn=nil falls back to os.Stat.
func newLocalSender(baseDir string, statFn func(string) (os.FileInfo, error)) (*LocalSender, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, apperror.NewValidation(
			"sender_basedir_required",
			"directorio base de envíos requerido",
		)
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, apperror.NewInternal(
			"sender_basedir_invalid",
			"directorio base de envíos inválido",
		).WithError(err).WithSource("sender.local")
	}
	if mkErr := os.MkdirAll(abs, senderDirMode); mkErr != nil {
		return nil, apperror.NewInternal(
			"sender_basedir_unwritable",
			"directorio base de envíos no se puede crear",
		).WithError(mkErr).WithSource("sender.local").WithField("dir", abs)
	}
	if statFn == nil {
		statFn = os.Stat
	}
	info, err := statFn(abs)
	if err != nil {
		return nil, apperror.NewInternal(
			"sender_basedir_unreadable",
			"directorio base de envíos no accesible",
		).WithError(err).WithSource("sender.local").WithField("dir", abs)
	}
	if !info.IsDir() {
		return nil, apperror.NewValidation(
			"sender_basedir_not_directory",
			"la ruta base de envíos no es un directorio",
		).WithField("dir", abs)
	}
	return &LocalSender{baseDir: abs}, nil
}

var _ outbound.Sender = (*LocalSender)(nil)

// Enviar writes the document and its .envio.json sidecar under baseDir.
//
// The document file is named `<uuid>-<doc.Nombre>` so two deliveries sharing
// a Nombre never overwrite each other. The returned id is `local:<uuid>` —
// the prefix is what keeps a test send from ever being confused with a real
// WhatsApp message id when someone reads the mensaje externo column later.
//
// doc.Body is NOT closed here: the port contract says the caller closes it.
// The phone number is NOT re-validated: the port already guarantees a usable
// phone, and a client without one is decided in the domain (sin_telefono).
func (s *LocalSender) Enviar(
	_ context.Context, dest outbound.Destino, doc outbound.Documento,
	plantilla string, variables []string,
) (string, error) {
	if err := validateDocName(doc.Nombre); err != nil {
		return "", err
	}
	id := uuid.New().String()
	written := id + "-" + doc.Nombre

	if err := writeDocFile(filepath.Join(s.baseDir, written), doc.Body); err != nil {
		return "", err
	}
	record := envioRecord{
		ClienteID: dest.ClienteID,
		Telefono:  dest.Telefono,
		Plantilla: plantilla,
		Variables: variables,
		EnviadoEn: time.Now().UTC(),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return "", apperror.NewInternal(
			"sender_record_encode_failed",
			"no se pudo codificar el registro de envío",
		).WithError(err).WithSource("sender.local")
	}
	if err := os.WriteFile(filepath.Join(s.baseDir, written+sidecarSuffix), payload, 0o600); err != nil {
		_ = os.Remove(filepath.Join(s.baseDir, written))
		return "", apperror.NewInternal(
			"sender_record_write_failed",
			"no se pudo escribir el registro de envío",
		).WithError(err).WithSource("sender.local").WithField("nombre", written)
	}
	return "local:" + id, nil
}

// Canal identifies which implementation answered. It is persisted on the
// delivery record so a simulated send can never be counted as a real one.
func (s *LocalSender) Canal() string { return string(domain.CanalLocal) }

// envioRecord is the sidecar JSON: the trace of whom the document was sent to,
// with which template and at which instant — the audit trail of a simulated
// send. The instant is UTC wall clock; it is test-channel bookkeeping, not a
// user-facing date, so no business-zone conversion applies.
type envioRecord struct {
	ClienteID int       `json:"cliente_id"`
	Telefono  string    `json:"telefono"`
	Plantilla string    `json:"plantilla"`
	Variables []string  `json:"variables"`
	EnviadoEn time.Time `json:"enviado_en"`
}

// writeDocFile streams the body into path with restrictive permissions. The
// body is read through io.Reader, never closed — the caller owns it.
func writeDocFile(path string, body io.Reader) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return apperror.NewInternal(
			"sender_file_create_failed",
			"no se pudo crear el archivo del documento",
		).WithError(err).WithSource("sender.local").WithField("nombre", path)
	}
	if _, copyErr := io.Copy(out, body); copyErr != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return apperror.NewInternal(
			"sender_file_write_failed",
			"no se pudo escribir el archivo del documento",
		).WithError(copyErr).WithSource("sender.local").WithField("nombre", path)
	}
	if closeErr := out.Close(); closeErr != nil {
		_ = os.Remove(path)
		return apperror.NewInternal(
			"sender_file_close_failed",
			"no se pudo cerrar el archivo del documento",
		).WithError(closeErr).WithSource("sender.local").WithField("nombre", path)
	}
	return nil
}

// validateDocName rejects document names that would escape baseDir or confuse
// the host filesystem — the same guards as a storage key.
func validateDocName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errSenderInvalidNombre.WithField("reason", "empty")
	}
	if len(name) > maxDocNameLength {
		return errSenderInvalidNombre.WithField("reason", "too_long")
	}
	if strings.Contains(name, "..") {
		return errSenderInvalidNombre.WithField("reason", "path_traversal")
	}
	if strings.ContainsRune(name, 0x00) {
		return errSenderInvalidNombre.WithField("reason", "null_byte")
	}
	if strings.Contains(name, `\`) {
		return errSenderInvalidNombre.WithField("reason", "backslash")
	}
	if strings.HasPrefix(name, "/") {
		return errSenderInvalidNombre.WithField("reason", "absolute_path")
	}
	return nil
}
