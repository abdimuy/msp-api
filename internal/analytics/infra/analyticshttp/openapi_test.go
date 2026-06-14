//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticshttp"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── Minimal fakes ───────────────────────────────────────────────────────────

// stubClock always returns a fixed point in time.
type stubClock struct{}

func (stubClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

// stubRepo satisfies outbound.WinbackRepo with no-op implementations.
type stubRepo struct{}

func (stubRepo) UpsertCandidatos(_ context.Context, _ []*domain.WinbackCandidato) error {
	return nil
}

func (stubRepo) ListCandidatos(_ context.Context, _ outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	return outbound.Page[*domain.WinbackCandidato]{}, nil
}

func (stubRepo) GetRefreshState(_ context.Context, _ string) (outbound.RefreshState, error) {
	return outbound.RefreshState{}, domain.ErrRefreshStateNotFound
}

func (stubRepo) SaveRefreshState(_ context.Context, _ outbound.RefreshState) error { return nil }

func (stubRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	return map[int]bool{}, nil
}

// stubMicrosip satisfies outbound.MicrosipReader with a no-op implementation.
type stubMicrosip struct{}

func (stubMicrosip) LeerAnclasDesde(_ context.Context, _ *time.Time) ([]outbound.AnclaCliente, error) {
	return nil, nil
}

// buildTestService wires a *analyticsapp.Service against in-memory fakes.
// txMgr is nil so runInTx calls fn directly without a real transaction.
func buildTestService() *analyticsapp.Service {
	return analyticsapp.NewService(stubRepo{}, stubMicrosip{}, stubClock{}, nil)
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestOpenAPI_PathsRegistered verifies that MountRouter registers the expected
// paths and operationIDs in the generated OpenAPI spec.
func TestOpenAPI_PathsRegistered(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	api := analyticshttp.MountRouter(r, buildTestService())
	require.NotNil(t, api, "MountRouter must return a non-nil huma.API")

	spec := api.OpenAPI()
	require.NotNil(t, spec, "OpenAPI spec must be non-nil")
	require.NotNil(t, spec.Paths, "OpenAPI paths must be non-nil")

	type want struct {
		path        string
		method      string
		operationID string
	}

	cases := []want{
		{"/winback", "GET", "listar-winback"},
		{"/winback/attribution", "GET", "atribucion-winback"},
		{"/winback/refresh", "POST", "refrescar-candidatos-winback"},
	}

	for _, tc := range cases {
		t.Run(tc.operationID, func(t *testing.T) {
			t.Parallel()

			pathItem, ok := spec.Paths[tc.path]
			require.True(t, ok, "path %q must be registered in OpenAPI spec", tc.path)
			require.NotNil(t, pathItem, "path item for %q must not be nil", tc.path)

			var opID string
			switch tc.method {
			case "GET":
				require.NotNil(t, pathItem.Get, "GET operation must exist at %q", tc.path)
				opID = pathItem.Get.OperationID
			case "POST":
				require.NotNil(t, pathItem.Post, "POST operation must exist at %q", tc.path)
				opID = pathItem.Post.OperationID
			}

			assert.Equal(t, tc.operationID, opID,
				"operationID at %s %s must be %q", tc.method, tc.path, tc.operationID)
		})
	}
}
