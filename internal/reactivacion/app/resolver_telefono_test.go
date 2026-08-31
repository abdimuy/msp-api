//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var resolverNow = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

// mustCohorteCliente builds a domain.CohorteCliente fixture for clienteID
// with telefono, panicking on an invalid input (test-only).
func mustCohorteCliente(clienteID int, telefono string) *domain.CohorteCliente {
	c, err := domain.CrearCohorteCliente(domain.CrearCohorteClienteParams{
		ClienteID:    clienteID,
		Nombre:       "cliente de prueba",
		Telefono:     telefono,
		Segmento:     domain.SegmentoRecienLiquidado,
		CohorteFecha: resolverNow,
		Saldo:        decimal.Zero,
		Now:          resolverNow,
	})
	if err != nil {
		panic(err)
	}
	return c
}

// newResolverService builds a plain (non-copiloto) Service wired against a
// fakeCohorteRepo seeded with cohorte — enough for
// ResolverClienteIDPorTelefono, which only reads s.repo.ListarCohorte.
func newResolverService(cohorte []*domain.CohorteCliente) *reactivacionapp.Service {
	repo := &fakeCohorteRepo{listResult: cohorte}
	return reactivacionapp.NewService(&fakeUniversoReader{}, repo, fixedClock{now: resolverNow}, nil, reactivacionapp.Config{})
}

func TestResolverClienteIDPorTelefono_ExactMatch_Resuelve(t *testing.T) {
	t.Parallel()
	svc := newResolverService([]*domain.CohorteCliente{
		mustCohorteCliente(101, "238 123 4567"),
		mustCohorteCliente(102, "238 999 0000"),
	})

	// Meta sends digits-only with country code; the cohorte stores the
	// local, spaced format — the suffix match must bridge both.
	clienteID, err := svc.ResolverClienteIDPorTelefono(context.Background(), "522381234567", "wamid.1")
	require.NoError(t, err)
	assert.Equal(t, 101, clienteID)
}

func TestResolverClienteIDPorTelefono_NoDigitsInCommon_NoResuelto(t *testing.T) {
	t.Parallel()
	svc := newResolverService([]*domain.CohorteCliente{
		mustCohorteCliente(101, "238 123 4567"),
	})

	_, err := svc.ResolverClienteIDPorTelefono(context.Background(), "525599998888", "wamid.2")
	require.ErrorIs(t, err, reactivacionapp.ErrTelefonoNoResuelto)
}

func TestResolverClienteIDPorTelefono_EmptyCohorte_NoResuelto(t *testing.T) {
	t.Parallel()
	svc := newResolverService(nil)

	_, err := svc.ResolverClienteIDPorTelefono(context.Background(), "522381234567", "wamid.3")
	require.ErrorIs(t, err, reactivacionapp.ErrTelefonoNoResuelto)
}

func TestResolverClienteIDPorTelefono_TwoClientesSameTelefono_Ambiguo(t *testing.T) {
	t.Parallel()
	svc := newResolverService([]*domain.CohorteCliente{
		mustCohorteCliente(101, "238 123 4567"),
		mustCohorteCliente(103, "238 123 4567"), // shared household landline
	})

	_, err := svc.ResolverClienteIDPorTelefono(context.Background(), "522381234567", "wamid.4")
	require.ErrorIs(t, err, reactivacionapp.ErrTelefonoAmbiguo)
}

func TestResolverClienteIDPorTelefono_TelefonoDemasiadoCorto_Invalido(t *testing.T) {
	t.Parallel()
	svc := newResolverService([]*domain.CohorteCliente{
		mustCohorteCliente(101, "238 123 4567"),
	})

	_, err := svc.ResolverClienteIDPorTelefono(context.Background(), "12345", "wamid.5")
	require.ErrorIs(t, err, reactivacionapp.ErrTelefonoInvalido)
}

func TestResolverClienteIDPorTelefono_ListarCohorteError_Propagates(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{listErr: errors.New("boom")}
	svc := reactivacionapp.NewService(&fakeUniversoReader{}, repo, fixedClock{now: resolverNow}, nil, reactivacionapp.Config{})

	_, err := svc.ResolverClienteIDPorTelefono(context.Background(), "522381234567", "wamid.6")
	require.Error(t, err)
}
