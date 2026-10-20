package sender_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
	"github.com/abdimuy/msp-api/internal/comprobantes/infra/sender"
	"github.com/abdimuy/msp-api/internal/comprobantes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// closingReader wraps a reader and records whether Close was called, so a
// test can prove Enviar honours the port contract of not closing doc.Body.
type closingReader struct {
	r      io.Reader
	closed bool
}

func newClosingReader(b []byte) *closingReader {
	return &closingReader{r: bytes.NewReader(b)}
}

func (c *closingReader) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *closingReader) Close() error {
	c.closed = true
	return nil
}

func newSender(t *testing.T) (*sender.LocalSender, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := sender.NewLocalSender(dir)
	require.NoError(t, err)
	require.NotNil(t, s)
	return s, dir
}

func makeDoc(nombre string, body []byte) outbound.Documento {
	return outbound.Documento{
		Nombre:      nombre,
		ContentType: "application/pdf",
		SizeBytes:   int64(len(body)),
		Body:        bytes.NewReader(body),
	}
}

// envioRecord mirrors the sidecar JSON shape.
type envioRecord struct {
	ClienteID int       `json:"cliente_id"`
	Telefono  string    `json:"telefono"`
	Plantilla string    `json:"plantilla"`
	Variables []string  `json:"variables"`
	EnviadoEn time.Time `json:"enviado_en"`
}

func readSidecar(t *testing.T, dir, docName string) envioRecord {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, docName+".envio.json"))
	require.NoError(t, err)
	var rec envioRecord
	require.NoError(t, json.Unmarshal(raw, &rec))
	return rec
}

func TestLocalSender_Enviar_WritesDocAndSidecar(t *testing.T) {
	t.Parallel()
	s, dir := newSender(t)
	ctx := context.Background()
	payload := []byte("%PDF-1.7 los bytes exactos")
	doc := makeDoc("comprobante-A123.pdf", payload)

	id, err := s.Enviar(ctx, outbound.Destino{ClienteID: 42, Telefono: "5512345678"},
		doc, "comprobante_venta", []string{"A123", "María"})
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "a document and its sidecar must be written")

	var docName, sidecarName string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".envio.json") {
			sidecarName = e.Name()
		} else {
			docName = e.Name()
		}
	}
	require.NotEmpty(t, docName)
	require.NotEmpty(t, sidecarName)
	assert.Equal(t, docName+".envio.json", sidecarName)

	// The document holds the exact bytes passed in; the file name is
	// uuid-prefixed so two sends with the same Nombre never collide, and the
	// returned id is local:<uuid>.
	got, err := os.ReadFile(filepath.Join(dir, docName))
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.True(t, strings.HasPrefix(id, "local:"))
	assert.True(t, strings.HasPrefix(docName, id[len("local:"):]))
}

func TestLocalSender_Enviar_SidecarHasDestinoTemplateVariables(t *testing.T) {
	t.Parallel()
	s, dir := newSender(t)
	ctx := context.Background()
	doc := makeDoc("comprobante-B98.pdf", []byte("pago"))

	_, err := s.Enviar(ctx, outbound.Destino{ClienteID: 7, Telefono: "5511223344"},
		doc, "comprobante_pago", []string{"B-98", "Pedro", "El resto"})
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var docName string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".envio.json") {
			docName = e.Name()
		}
	}
	require.NotEmpty(t, docName)

	rec := readSidecar(t, dir, docName)
	assert.Equal(t, 7, rec.ClienteID)
	assert.Equal(t, "5511223344", rec.Telefono)
	assert.Equal(t, "comprobante_pago", rec.Plantilla)
	// Variables in the order received — the template placeholders substitute
	// positionally.
	assert.Equal(t, []string{"B-98", "Pedro", "El resto"}, rec.Variables)
	// The instant is recorded so the sidecar is a usable audit trail of "who
	// was sent what, and when".
	assert.False(t, rec.EnviadoEn.IsZero(), "the sidecar must record the instant")
	assert.WithinDuration(t, time.Now().UTC(), rec.EnviadoEn, 5*time.Minute)
}

func TestLocalSender_Enviar_SameNombre_TwoSendsDoNotCollide(t *testing.T) {
	t.Parallel()
	s, dir := newSender(t)
	ctx := context.Background()
	doc := makeDoc("comprobante-pago.pdf", []byte("%PDF same name"))
	dest := outbound.Destino{ClienteID: 1, Telefono: "5511223344"}

	id1, err := s.Enviar(ctx, dest, doc, "comprobante_pago", []string{"x"})
	require.NoError(t, err)
	id2, err := s.Enviar(ctx, dest, doc, "comprobante_pago", []string{"x"})
	require.NoError(t, err)

	require.NotEqual(t, id1, id2)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 4, "two docs + two sidecars, no overwrite")
}

func TestLocalSender_Enviar_IdsDistinctAndPrefixed(t *testing.T) {
	t.Parallel()
	s, _ := newSender(t)
	ctx := context.Background()
	doc := makeDoc("a.pdf", []byte("a"))

	id1, err := s.Enviar(ctx, outbound.Destino{ClienteID: 1, Telefono: "1"}, doc, "t", []string{})
	require.NoError(t, err)
	id2, err := s.Enviar(ctx, outbound.Destino{ClienteID: 1, Telefono: "1"}, doc, "t", []string{})
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)
	assert.True(t, strings.HasPrefix(id1, "local:"))
	assert.True(t, strings.HasPrefix(id2, "local:"))
}

func TestLocalSender_Enviar_EmptyDocument_Works(t *testing.T) {
	t.Parallel()
	s, dir := newSender(t)
	ctx := context.Background()
	doc := makeDoc("vacío.pdf", nil)

	id, err := s.Enviar(ctx, outbound.Destino{ClienteID: 1, Telefono: "1"}, doc, "t", []string{})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(id, "local:"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

func TestLocalSender_Canal_MatchesDomainCanalLocal(t *testing.T) {
	t.Parallel()
	s, _ := newSender(t)
	assert.Equal(t, string(domain.CanalLocal), s.Canal())
	assert.Equal(t, "local", s.Canal())
}

// TestLocalSender_Enviar_DoesNotCloseBody is one line of the port contract no
// other test covers: the sender must NOT close doc.Body — the caller does.
func TestLocalSender_Enviar_DoesNotCloseBody(t *testing.T) {
	t.Parallel()
	s, _ := newSender(t)
	ctx := context.Background()
	rd := newClosingReader([]byte("%PDF un documento"))
	doc := outbound.Documento{
		Nombre:      "no-cierra.pdf",
		ContentType: "application/pdf",
		SizeBytes:   8,
		Body:        rd,
	}

	_, err := s.Enviar(ctx, outbound.Destino{ClienteID: 1, Telefono: "1"}, doc, "t", []string{})
	require.NoError(t, err)
	assert.False(t, rd.closed, "Enviar must not close doc.Body, the caller does")
}

func TestLocalSender_NewLocalSender_EmptyDir_Errors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "   "} {
		_, err := sender.NewLocalSender(in)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "sender_basedir_required", appErr.Code)
	}
}

func TestLocalSender_NewLocalSender_CreatesBaseDir(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	target := filepath.Join(parent, "nested", "outbox")
	s, err := sender.NewLocalSender(target)
	require.NoError(t, err)
	require.NotNil(t, s)

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLocalSender_NewLocalSender_PathIsFile_Errors(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	filePath := filepath.Join(parent, "iam-a-file")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	_, err := sender.NewLocalSender(filePath)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	// MkdirAll on an existing file returns ENOTDIR — surfaced as
	// sender_basedir_unwritable.
	assert.Equal(t, "sender_basedir_unwritable", appErr.Code)
}

func TestLocalSender_Enviar_InvalidNombre_RejectedAndNoFiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		nombre string
	}{
		{"double_dot", "../escape.pdf"},
		{"embedded_double_dot", "foo/../bar.pdf"},
		{"absolute_path", "/etc/passwd"},
		{"null_byte", "ok\x00bad.pdf"},
		{"backslash", `windows\path.pdf`},
		{"empty", ""},
		{"whitespace_only", "   "},
		{"too_long", strings.Repeat("a", 501)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			s, err := sender.NewLocalSender(dir)
			require.NoError(t, err)
			doc := makeDoc(tc.nombre, []byte("x"))

			_, err = s.Enviar(context.Background(), outbound.Destino{ClienteID: 1, Telefono: "1"},
				doc, "t", []string{})
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "sender_nombre_invalido", appErr.Code)
			assert.Equal(t, apperror.KindValidation, appErr.Kind)

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries, "no file may be created for invalid document name %q", tc.nombre)
		})
	}
}

// TestLocalSender_Enviar_BaseDirReplacedByFile_Errors surfaces
// sender_file_create_failed: a base directory that has been swapped for a
// file since construction makes the document file uncreatable.
func TestLocalSender_Enviar_BaseDirReplacedByFile_Errors(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	bdir := filepath.Join(parent, "outbox")
	s, err := sender.NewLocalSender(bdir)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(bdir))
	require.NoError(t, os.WriteFile(bdir, []byte("x"), 0o600))

	_, err = s.Enviar(context.Background(), outbound.Destino{ClienteID: 1, Telefono: "1"},
		makeDoc("x.pdf", []byte("x")), "t", []string{})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "sender_file_create_failed", appErr.Code)
}

// TestLocalSender_Enviar_BodyReadFailure_CleansDoc tests that a mid-read
// failure leaves no partial document and no sidecar behind.
func TestLocalSender_Enviar_BodyReadFailure_CleansDoc(t *testing.T) {
	t.Parallel()
	s, dir := newSender(t)
	ctx := context.Background()
	failing := &errReader{err: errors.New("boom")}
	doc := outbound.Documento{
		Nombre:      "fail.pdf",
		ContentType: "application/pdf",
		SizeBytes:   1,
		Body:        failing,
	}

	_, err := s.Enviar(ctx, outbound.Destino{ClienteID: 1, Telefono: "1"}, doc, "t", []string{})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "sender_file_write_failed", appErr.Code)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no doc or sidecar may survive a body read failure")
}

type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }
