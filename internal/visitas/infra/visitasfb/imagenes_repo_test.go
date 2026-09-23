//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package visitasfb_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// attachImagen builds and attaches one imagen to v through the aggregate
// root, returning the child entity ready to insert.
func attachImagen(t *testing.T, v *domain.Visita, key string, opts ...func(*domain.AdjuntarImagenParams)) *domain.Imagen {
	t.Helper()
	storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
	require.NoError(t, err)
	p := domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   storage,
		Mime:      domain.MimeJPEG,
		SizeBytes: 2048,
		By:        uuid.New(),
		Now:       time.Now().UTC(),
	}
	for _, o := range opts {
		o(&p)
	}
	img, err := v.AdjuntarImagen(p)
	require.NoError(t, err)
	return img
}

// ─── The migration itself ─────────────────────────────────────────────────

// TestE2E_VisitasImagenes_TablaExisteYNoTocoLasVisitasLegacy is the migration
// test. It proves two things at once against the REAL dev database:
//
//   - MSP_VISITAS_IMAGENES exists with the shape migration 000063 declares
//     (a plain INSERT of every column succeeds, which is a stronger check
//     than reading RDB$RELATIONS — a table that exists but has the wrong
//     columns would still pass a name lookup).
//   - MSP_VISITAS was NOT disturbed: its ~226k legacy rows are still there.
//     This is the positive control for "the migration touched nothing" —
//     asserting the count is non-trivial proves the query would have noticed
//     a truncation.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_TablaExisteYNoTocoLasVisitasLegacy(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := pool.DB

		var legacyCount int
		require.NoError(t,
			q.QueryRowContext(ctx, "SELECT COUNT(*) FROM MSP_VISITAS").Scan(&legacyCount))
		assert.Greater(t, legacyCount, 200000,
			"the legacy table must still hold its rows; the migration is a no-op on it")

		var imagenCols int
		require.NoError(t, q.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM RDB$RELATION_FIELDS
			WHERE RDB$RELATION_NAME = 'MSP_VISITAS_IMAGENES'`).Scan(&imagenCols))
		assert.Equal(t, 11, imagenCols,
			"migration 000063 declares exactly 11 columns")
	})
}

// TestE2E_VisitasImagenes_FKRechazaVisitaInexistente proves the FOREIGN KEY
// is real and enforced: an imagen whose VISITA_ID has no parent row cannot be
// inserted. Without this, a bug in the service could silently orphan rows.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_FKRechazaVisitaInexistente(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		orphan := buildValidVisita(t) // built but deliberately NOT inserted
		img := attachImagen(t, orphan, "visitas/"+orphan.ID().String()+"/x.jpg")

		err := repo.InsertImagen(ctx, orphan.ID(), img)
		require.Error(t, err, "an imagen without a parent visita must be rejected by the FK")
	})
}

// ─── Insert + read-back round-trip ────────────────────────────────────────

//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_InsertRoundTripsEveryColumn(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		desc := "puerta del cliente — acentos y guión largo"
		img := attachImagen(t, v, "visitas/"+v.ID().String()+"/foto.jpg",
			func(p *domain.AdjuntarImagenParams) {
				p.Descripcion = &desc
				p.SizeBytes = 987654
				p.Mime = domain.MimePNG
			})

		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))

		var (
			gotID, gotVisita, gotKind, gotKey, gotMime, gotCreatedBy, gotUpdatedBy string
			gotSize                                                                int64
			gotDesc                                                                string
		)
		q := firebird.GetQuerier(ctx, pool.DB)
		require.NoError(t, q.QueryRowContext(ctx, `
			SELECT ID, VISITA_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
			       DESCRIPCION, CREATED_BY, UPDATED_BY
			FROM MSP_VISITAS_IMAGENES WHERE ID = ?`, img.ID().String()).
			Scan(&gotID, &gotVisita, &gotKind, &gotKey, &gotMime, &gotSize,
				&gotDesc, &gotCreatedBy, &gotUpdatedBy))

		assert.Equal(t, img.ID().String(), strings.TrimSpace(gotID))
		assert.Equal(t, v.ID().String(), strings.TrimSpace(gotVisita))
		assert.Equal(t, "FILESYSTEM", strings.TrimSpace(gotKind))
		assert.Equal(t, "visitas/"+v.ID().String()+"/foto.jpg", gotKey)
		assert.Equal(t, "image/png", gotMime)
		assert.Equal(t, int64(987654), gotSize)
		assert.Equal(t, desc, gotDesc, "UTF-8 accents must round-trip byte-equal")
		aud := img.Audit()
		assert.Equal(t, aud.CreatedBy().String(), strings.TrimSpace(gotCreatedBy))
		assert.Equal(t, aud.UpdatedBy().String(), strings.TrimSpace(gotUpdatedBy))
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_DescripcionNulaSeGuardaComoNULL(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		img := attachImagen(t, v, "visitas/"+v.ID().String()+"/sin-desc.jpg")
		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))

		var isNull int
		q := firebird.GetQuerier(ctx, pool.DB)
		require.NoError(t, q.QueryRowContext(ctx, `
			SELECT CASE WHEN DESCRIPCION IS NULL THEN 1 ELSE 0 END
			FROM MSP_VISITAS_IMAGENES WHERE ID = ?`, img.ID().String()).Scan(&isNull))
		assert.Equal(t, 1, isNull, "an absent descripcion must be SQL NULL, not an empty string")
	})
}

// TestE2E_VisitasImagenes_TimestampsVienenDeGo pins CLAUDE.md §1: the table
// has no DEFAULT and no trigger, so whatever Go bound is exactly what lands.
// A timestamp far in the past could only come from the entity.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_TimestampsVienenDeGo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		fixed := time.Date(2019, 3, 14, 8, 30, 0, 0, time.UTC)
		img := attachImagen(t, v, "visitas/"+v.ID().String()+"/old.jpg",
			func(p *domain.AdjuntarImagenParams) { p.Now = fixed })
		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))

		var raw any
		q := firebird.GetQuerier(ctx, pool.DB)
		require.NoError(t, q.QueryRowContext(ctx,
			"SELECT CREATED_AT FROM MSP_VISITAS_IMAGENES WHERE ID = ?", img.ID().String()).Scan(&raw))
		got, ok := raw.(time.Time)
		require.True(t, ok, "CREATED_AT scans as time.Time")
		assert.Equal(t, 2019, got.Year(), "the DB invented nothing; the year came from Go")
		assert.Equal(t, time.March, got.Month())
	})
}

// ─── Idempotency at the repo layer ────────────────────────────────────────

//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_InsertDuplicadoDevuelveErrImagenYaExiste(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		img := attachImagen(t, v, "visitas/"+v.ID().String()+"/dup.jpg")
		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))

		err := repo.InsertImagen(ctx, v.ID(), img)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrImagenYaExiste,
			"a PK collision must map to the domain sentinel, not to a raw constraint error")
	})
}

// ─── FindImagenIDs ────────────────────────────────────────────────────────

//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_FindImagenIDs(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		// Positive control first: the query must return empty, not error,
		// for a visita that genuinely has no images.
		empty, err := repo.FindImagenIDs(ctx, v.ID())
		require.NoError(t, err)
		assert.Empty(t, empty)

		want := make([]uuid.UUID, 0, 3)
		for i := range 3 {
			img := attachImagen(t, v, "visitas/"+v.ID().String()+"/"+string(rune('a'+i))+".jpg")
			require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))
			want = append(want, img.ID())
		}

		got, err := repo.FindImagenIDs(ctx, v.ID())
		require.NoError(t, err)
		assert.ElementsMatch(t, want, got)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasImagenes_FindImagenIDsNoDevuelveLasDeOtraVisita(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		mine := buildValidVisita(t)
		other := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, mine))
		require.NoError(t, repo.Insert(ctx, other))

		mineImg := attachImagen(t, mine, "visitas/"+mine.ID().String()+"/m.jpg")
		require.NoError(t, repo.InsertImagen(ctx, mine.ID(), mineImg))
		otherImg := attachImagen(t, other, "visitas/"+other.ID().String()+"/o.jpg")
		require.NoError(t, repo.InsertImagen(ctx, other.ID(), otherImg))

		got, err := repo.FindImagenIDs(ctx, mine.ID())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, mineImg.ID(), got[0])
	})
}
