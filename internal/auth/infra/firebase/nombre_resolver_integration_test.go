package firebase_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// TestIntegration_NombreResolver_ReadsFirestore hits the real Firestore
// `users` collection. It is gated on FB_FIRESTORE_INTEGRATION so the normal
// suite skips it; run it manually with the project's service account:
//
//	FB_FIRESTORE_INTEGRATION=1 \
//	FB_FIRESTORE_PROJECT_ID=msp-dev-96ff5 \
//	FB_FIRESTORE_SERVICE_ACCOUNT=/abs/path/serviceAccountKey.json \
//	FB_FIRESTORE_TEST_UID=<a uid that has a users/{uid}.NOMBRE doc> \
//	go test -run TestIntegration_NombreResolver_ReadsFirestore ./internal/auth/infra/firebase/...
func TestIntegration_NombreResolver_ReadsFirestore(t *testing.T) {
	t.Parallel()
	if os.Getenv("FB_FIRESTORE_INTEGRATION") == "" {
		t.Skip("FB_FIRESTORE_INTEGRATION not set; skipping Firestore integration test")
	}
	projectID := os.Getenv("FB_FIRESTORE_PROJECT_ID")
	saPath := os.Getenv("FB_FIRESTORE_SERVICE_ACCOUNT")
	uid := os.Getenv("FB_FIRESTORE_TEST_UID")
	require.NotEmpty(t, projectID, "FB_FIRESTORE_PROJECT_ID required")
	require.NotEmpty(t, saPath, "FB_FIRESTORE_SERVICE_ACCOUNT required")
	require.NotEmpty(t, uid, "FB_FIRESTORE_TEST_UID required")

	r, err := firebase.NewNombreResolver(context.Background(), config.Firebase{
		ProjectID:          projectID,
		ServiceAccountPath: saPath,
	})
	require.NoError(t, err)

	nombre, err := r.ResolveNombre(context.Background(), uid)
	require.NoError(t, err)
	assert.NotEmpty(t, nombre, "the test uid must have a users/{uid}.NOMBRE in Firestore")
	t.Logf("resolved NOMBRE = %q", nombre)

	// A uid with no document resolves to "" without error.
	missing, err := r.ResolveNombre(context.Background(), "definitely-not-a-real-uid-000")
	require.NoError(t, err)
	assert.Empty(t, missing)
}
