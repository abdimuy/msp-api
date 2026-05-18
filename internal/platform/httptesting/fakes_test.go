package httptesting_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/httptesting"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

func TestFakeFirebase_VerifyIDTokenDefaultsToFixedUID(t *testing.T) {
	t.Parallel()
	fb := httptesting.NewFakeFirebase("uid-1")
	tok, err := fb.VerifyIDToken(context.Background(), "any-bearer-value")
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "uid-1", tok.UID)
}

func TestFakeFirebase_VerifyOverrideIsHonored(t *testing.T) {
	t.Parallel()
	fb := httptesting.NewFakeFirebase("default")
	sentinel := errors.New("rejected by override")
	fb.Verify = func(string) (*outbound.FirebaseToken, error) { return nil, sentinel }

	_, err := fb.VerifyIDToken(context.Background(), "x")
	assert.ErrorIs(t, err, sentinel)
}

func TestFakeUsuarioRepo_AddLookupByID_FirebaseUID_Email(t *testing.T) {
	t.Parallel()
	repo := httptesting.NewFakeUsuarioRepo()
	id := uuid.New()
	u := repo.AddUsuario(httptesting.AddUsuarioParams{
		ID:          id,
		FirebaseUID: "fb-1",
		Email:       "u@example.invalid",
		Nombre:      "Tester",
		Activo:      true,
	})
	require.NotNil(t, u)

	got, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID())

	got, err = repo.FindByFirebaseUID(context.Background(), "fb-1")
	require.NoError(t, err)
	assert.Equal(t, id, got.ID())

	got, err = repo.FindByEmail(context.Background(), "u@example.invalid")
	require.NoError(t, err)
	assert.Equal(t, id, got.ID())

	_, err = repo.FindByID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, authdomain.ErrUsuarioNotFound)
}

func TestFakeUsuarioRepo_PermissionsAndSetPermissions(t *testing.T) {
	t.Parallel()
	repo := httptesting.NewFakeUsuarioRepo()
	id := uuid.New()
	repo.AddUsuario(httptesting.AddUsuarioParams{
		ID:          id,
		FirebaseUID: "fb-2",
		Permissions: []authdomain.Permission{authdomain.PermFailedIntentsVer},
	})

	perms, err := repo.PermisosFor(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, []authdomain.Permission{authdomain.PermFailedIntentsVer}, perms)

	repo.SetPermissions(id, []authdomain.Permission{authdomain.PermFailedIntentsResolver})
	perms, err = repo.PermisosFor(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, []authdomain.Permission{authdomain.PermFailedIntentsResolver}, perms)
}

func TestInMemoryIdempotencyStore_GetMissReturnsNilNil(t *testing.T) {
	t.Parallel()
	s := httptesting.NewInMemoryIdempotencyStore()
	rec, err := s.Get(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestInMemoryIdempotencyStore_SaveThenGet_RoundTrip(t *testing.T) {
	t.Parallel()
	s := httptesting.NewInMemoryIdempotencyStore()
	rec := idempotency.Record{Key: "k", Method: "POST", Path: "/x", RequestHash: "h", ResponseStatus: 201, ResponseBody: []byte(`{}`)}
	require.NoError(t, s.Save(context.Background(), rec))

	got, err := s.Get(context.Background(), "k")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "POST", got.Method)
	assert.Equal(t, 201, got.ResponseStatus)
}

func TestNewE2ERequest_DefaultsAndOptions(t *testing.T) {
	t.Parallel()

	req := httptesting.NewE2ERequest(http.MethodPost, "/v2/x", `{"a":1}`)
	assert.Equal(t, "Bearer e2e-token", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	req2 := httptesting.NewE2ERequest(http.MethodGet, "/v2/x", "", httptesting.NoBearer())
	assert.Empty(t, req2.Header.Get("Authorization"))
	assert.Empty(t, req2.Header.Get("Content-Type"), "GET without body should not set Content-Type")

	req3 := httptesting.NewE2ERequest(http.MethodPost, "/v2/x", `{}`,
		httptesting.WithBearer("custom"),
		httptesting.WithIdempotencyKey("idem-1"),
		httptesting.WithHeader("X-Test", "yes"))
	assert.Equal(t, "Bearer custom", req3.Header.Get("Authorization"))
	assert.Equal(t, "idem-1", req3.Header.Get("Idempotency-Key"))
	assert.Equal(t, "yes", req3.Header.Get("X-Test"))
}
