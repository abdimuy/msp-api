//nolint:misspell // Spanish vocabulary (directorio, pulso, clientes, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// newService is a helper for tests in this file that need a Service wired with
// the provided fakes for repo, analytics, and dirIndex (search and clock get
// zero-value stubs since ReconciliarDirectorio does not use them).
func newReconcileService(
	repo outbound.ClientesRepo,
	anl outbound.AnalyticsClient,
	dirIdx outbound.DirectoryIndex,
) *app.Service {
	return app.NewService(repo, anl, &fakeSearchIndex{}, dirIdx, fixedClock{T: fixedTime})
}

func TestReconciliarDirectorio_HappyPath(t *testing.T) {
	t.Parallel()

	c1 := newCliente(1, "JUAN PEREZ")
	c2 := newCliente(2, "MARIA LOPEZ")

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c1, SaldoTotal: decimal.NewFromFloat(1000)},
			{Cliente: c2, SaldoTotal: decimal.Zero},
		},
	}

	pulso1 := analytics.ClientePulsoContract{
		ClienteID:    1,
		Score:        80,
		Segmento:     "ACTIVO",
		EstadoPago:   "AL_CORRIENTE",
		RecenciaDias: 15,
		Frecuencia:   4,
		Monetary:     decimal.NewFromFloat(12000),
	}

	anl := &fakeAnalyticsClient{
		pulsosMap: map[int]analytics.ClientePulsoContract{
			1: pulso1,
			// client 2 has no pulse
		},
	}

	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n, "should return count of docs sent to index")

	// Verify the index received both docs.
	require.Len(t, dirIdx.lastDocs, 2)

	// Verify doc for client 1 (with pulse).
	var doc1 outbound.DirectorioDoc
	for _, d := range dirIdx.lastDocs {
		if d.ClienteID == 1 {
			doc1 = d
		}
	}
	assert.Equal(t, 1, doc1.ClienteID)
	assert.Equal(t, "JUAN PEREZ", doc1.Nombre)
	assert.True(t, doc1.ConSaldo, "saldo 1000 > 0 → ConSaldo true")
	assert.True(t, doc1.TienePulso)
	assert.Equal(t, 80, doc1.Score)
	assert.Equal(t, "ACTIVO", doc1.Segmento)
	assert.Equal(t, "AL_CORRIENTE", doc1.EstadoPago)
	assert.Equal(t, 15, doc1.RecenciaDias)
	assert.Equal(t, 4, doc1.Frecuencia)
	assert.True(t, doc1.Monetary.Equal(decimal.NewFromFloat(12000)))

	// Verify doc for client 2 (no pulse).
	var doc2 outbound.DirectorioDoc
	for _, d := range dirIdx.lastDocs {
		if d.ClienteID == 2 {
			doc2 = d
		}
	}
	assert.Equal(t, 2, doc2.ClienteID)
	assert.Equal(t, "MARIA LOPEZ", doc2.Nombre)
	assert.False(t, doc2.ConSaldo, "saldo 0 → ConSaldo false")
	assert.False(t, doc2.TienePulso)
	assert.Equal(t, 0, doc2.Score, "no pulse → zero Score")
}

func TestReconciliarDirectorio_EmptyRepo(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{dirCompleto: nil}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Nil(t, dirIdx.lastDocs, "should not call Reconciliar when repo is empty")
}

func TestReconciliarDirectorio_RepoError(t *testing.T) {
	t.Parallel()

	repoErr := errors.New("firebird down")
	repo := &fakeClientesRepo{dirCompletoErr: repoErr}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciliar_directorio_list_failed")
}

func TestReconciliarDirectorio_AnalyticsError(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{
				Cliente:    newCliente(1, "TEST"),
				SaldoTotal: decimal.Zero,
			},
		},
	}
	anl := &fakeAnalyticsClient{pulsosErr: errors.New("analytics timeout")}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciliar_directorio_pulsos_failed")
}

func TestReconciliarDirectorio_IndexError(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: newCliente(1, "TEST"), SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{err: errors.New("meilisearch down")}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciliar_directorio_index_failed")
}

func TestReconciliarDirectorio_DireccionBuilt(t *testing.T) {
	t.Parallel()

	c := domain.HydrateCliente(domain.HydrateClienteParams{
		ClienteID: 99,
		Nombre:    "DIRECCION TEST",
		Direccion: domain.HydrateDireccion(domain.HydrateDireccionParams{
			Calle:     "AV. HIDALGO",
			Colonia:   "CENTRO",
			Poblacion: "GUADALAJARA",
		}),
	})

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	// Direccion (full-text): space-joined
	assert.Equal(t, "AV. HIDALGO CENTRO GUADALAJARA", doc.Direccion)
	// DireccionCorta: comma-joined (from Direccion.Corta())
	assert.Equal(t, "AV. HIDALGO, CENTRO, GUADALAJARA", doc.DireccionCorta)
	assert.Equal(t, "AV. HIDALGO", doc.DireccionCalle)
	assert.Equal(t, "CENTRO", doc.DireccionColonia)
	assert.Equal(t, "GUADALAJARA", doc.DireccionPoblacion)
}

func TestReconciliarDirectorio_UsesListarDirectorioCompleto(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{dirCompleto: nil}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, _ = svc.ReconciliarDirectorio(context.Background())
	assert.True(t, repo.listarComplCalled, "ReconciliarDirectorio must call ListarDirectorioCompleto")
}
