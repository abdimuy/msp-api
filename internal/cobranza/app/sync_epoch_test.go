//nolint:misspell // Spanish domain vocabulary (recurso, zona, ventas, pagos) per project convention.
package app_test

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// errEpochBoom is the transport failure the fake repo raises.
var errEpochBoom = errors.New("firebird: connection reset")

// fakeSyncEpochRepo is an in-memory outbound.SyncEpochRepo. It records every
// call so tests can assert which (recurso, zona) pair the service asked for.
type fakeSyncEpochRepo struct {
	mu    sync.Mutex
	byKey map[string]int
	err   error
	calls []string
}

func newFakeSyncEpochRepo() *fakeSyncEpochRepo {
	return &fakeSyncEpochRepo{byKey: map[string]int{}}
}

func (f *fakeSyncEpochRepo) Efectivo(_ context.Context, recurso domain.RecursoSync, zonaID int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := epochKeyOf(recurso, zonaID)
	f.calls = append(f.calls, key)
	if f.err != nil {
		// Devuelve un valor NO cero junto al error a propósito: así el test
		// de degradación distingue "el servicio ignoró el valor y devolvió 0"
		// de "el servicio devolvió lo que trajo el puerto, que casualmente
		// era 0".
		return 99, f.err
	}
	return f.byKey[key], nil
}

func (f *fakeSyncEpochRepo) set(recurso domain.RecursoSync, zonaID, epoch int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byKey[epochKeyOf(recurso, zonaID)] = epoch
}

func (f *fakeSyncEpochRepo) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// epochKeyOf builds the "recurso/zona" key used by the fake.
func epochKeyOf(recurso domain.RecursoSync, zonaID int) string {
	return recurso.String() + "/" + strconv.Itoa(zonaID)
}

// newEpochService builds a read-only Service with only the epoch port wired —
// enough to exercise Service.SyncEpoch.
func newEpochService(t *testing.T, repo *fakeSyncEpochRepo) *app.Service {
	t.Helper()
	svc := app.NewService(newFakeSaldosRepo(), nil, nil, fixedClock{}, nil, nil, nil, nil, nil, nil)
	if repo != nil {
		svc.WithSyncEpochRepo(repo, slog.New(slog.NewTextHandler(discardWriter{}, nil)))
	}
	return svc
}

// discardWriter swallows the degradation log so the test output stays clean.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestService_SyncEpoch_DevuelveElValorDelPuerto verifica el camino feliz:
// el servicio devuelve tal cual lo que reporta el repositorio, y consulta el
// (recurso, zona) que se le pidió.
func TestService_SyncEpoch_DevuelveElValorDelPuerto(t *testing.T) {
	t.Parallel()

	repo := newFakeSyncEpochRepo()
	repo.set(domain.RecursoSyncVentas, 12271, 7)
	repo.set(domain.RecursoSyncPagos, 12271, 3)
	svc := newEpochService(t, repo)

	assert.Equal(t, 7, svc.SyncEpoch(t.Context(), domain.RecursoSyncVentas, 12271))
	assert.Equal(t, 3, svc.SyncEpoch(t.Context(), domain.RecursoSyncPagos, 12271))
	assert.Equal(t, []string{"ventas/12271", "pagos/12271"}, repo.recordedCalls())
}

// TestService_SyncEpoch_SinPuertoDevuelveCero cubre el módulo cableado sin el
// repositorio de epoch (tests de solo lectura, despliegues previos a la
// migración 000055): el sync debe comportarse como antes de que el mecanismo
// existiera.
func TestService_SyncEpoch_SinPuertoDevuelveCero(t *testing.T) {
	t.Parallel()

	svc := newEpochService(t, nil)

	assert.Equal(t, 0, svc.SyncEpoch(t.Context(), domain.RecursoSyncVentas, 12271))
	assert.Equal(t, 0, svc.SyncEpoch(t.Context(), domain.RecursoSyncPagos, 12271))
}

// TestService_SyncEpoch_ErrorDegradaACero es el requisito duro: un fallo del
// repositorio (tabla ausente, pool caído) nunca puede tumbar el sync — se
// degrada a 0 sin devolver error.
func TestService_SyncEpoch_ErrorDegradaACero(t *testing.T) {
	t.Parallel()

	repo := newFakeSyncEpochRepo()
	repo.set(domain.RecursoSyncVentas, 12271, 42)
	repo.err = errEpochBoom
	svc := newEpochService(t, repo)

	assert.Equal(t, 0, svc.SyncEpoch(t.Context(), domain.RecursoSyncVentas, 12271))
}

// TestService_SyncEpoch_LoggerNilNoRompe protege contra un panic en el camino
// de degradación cuando el Service se construyó sin logger explícito.
func TestService_SyncEpoch_LoggerNilNoRompe(t *testing.T) {
	t.Parallel()

	repo := newFakeSyncEpochRepo()
	repo.err = errEpochBoom
	svc := app.NewService(newFakeSaldosRepo(), nil, nil, fixedClock{}, nil, nil, nil, nil, nil, nil)
	svc.WithSyncEpochRepo(repo, nil) // logger nil → slog.Default()

	require.NotPanics(t, func() {
		assert.Equal(t, 0, svc.SyncEpoch(t.Context(), domain.RecursoSyncVentas, 12271))
	})
}
