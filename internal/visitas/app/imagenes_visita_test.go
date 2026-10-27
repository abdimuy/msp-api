package app_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

func imagenDe(t *testing.T, visitaID uuid.UUID) *domain.Imagen {
	t.Helper()
	id := uuid.New()
	st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "visitas/"+visitaID.String()+"/"+id.String()+".jpg")
	require.NoError(t, err)
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID: id, Storage: st, Mime: domain.MimeJPEG, SizeBytes: 4,
		CreatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC),
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
	})
}

func TestObtenerImagenVisita_Pertenencia(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	imgs := newFakeImagenesRepo()
	store := newFakeStorage()
	visitaA, visitaB := uuid.New(), uuid.New()
	img := imagenDe(t, visitaA)
	imgs.porVisita[visitaA] = []*domain.Imagen{img}
	require.NoError(t, store.Store(context.Background(), img.Storage().Key(), domain.MimeJPEG, 4, strings.NewReader("JPEG")))
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithImagenes(imgs, store, &fakeTxRunner{}))

	_, err := svc.ObtenerImagenVisita(context.Background(), visitaB, img.ID())
	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada, "la foto de otra visita es 404")

	res, err := svc.ObtenerImagenVisita(context.Background(), visitaA, img.ID())
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	assert.Equal(t, "JPEG", string(b))
	assert.Equal(t, domain.MimeJPEG, res.ContentType)
}

func TestListarImagenesVisita(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	imgs := newFakeImagenesRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithImagenes(imgs, newFakeStorage(), &fakeTxRunner{}))

	_, err := svc.ListarImagenesVisita(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVisitaNoEncontrada, "visita inexistente es 404, no lista vacía")

	visita, err := domain.NewVisita(domain.CrearVisitaParams{
		ID: uuid.New(), Cobrador: "Ramírez García, Jorge", CobradorID: 42, Fecha: fixedNow.Add(-time.Hour),
		TipoVisita: string(domain.TipoVisitaNoSeEncontraba), ClienteID: 11486, ZonaClienteID: 7,
		CreatedBy: uuid.New(), Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Insert(context.Background(), visita))
	v := visita.ID()
	got, err := svc.ListarImagenesVisita(context.Background(), v)
	require.NoError(t, err)
	assert.Empty(t, got)
	imgs.porVisita[v] = []*domain.Imagen{imagenDe(t, v), imagenDe(t, v)}
	got, err = svc.ListarImagenesVisita(context.Background(), v)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestImagenesVisita_SinConfigurarEsErrorInterno(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeVisitasRepo(), &fakeClock{t: fixedNow})
	_, err := svc.ListarImagenesVisita(context.Background(), uuid.New())
	require.Error(t, err)
	_, err = svc.ObtenerImagenVisita(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}
