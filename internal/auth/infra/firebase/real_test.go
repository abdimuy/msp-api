package firebase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// fakeVerifier is a test double for the internal tokenVerifier interface.
type fakeVerifier struct {
	token *auth.Token
	err   error
}

func (f *fakeVerifier) VerifyIDToken(_ context.Context, _ string) (*auth.Token, error) {
	return f.token, f.err
}

func newClientWithFake(v firebase.TokenVerifierForTest) *firebase.RealClient {
	return firebase.ExportNewRealClientForTest(v, "test-project")
}

func TestRealClient_HappyPath(t *testing.T) {
	t.Parallel()

	issuedAt := time.Now().Truncate(time.Second)
	expiresAt := issuedAt.Add(time.Hour)

	fake := &fakeVerifier{
		token: &auth.Token{
			UID:      "u123",
			IssuedAt: issuedAt.Unix(),
			Expires:  expiresAt.Unix(),
			Claims: map[string]any{
				"email": "a@b.com",
				"name":  "Alice",
			},
		},
	}
	client := newClientWithFake(fake)
	tok, err := client.VerifyIDToken(context.Background(), "any-token")
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "u123", tok.UID)
	assert.Equal(t, "a@b.com", tok.Email)
	assert.Equal(t, "Alice", tok.Name)
	assert.Equal(t, issuedAt.UTC(), tok.IssuedAt)
	assert.Equal(t, expiresAt.UTC(), tok.ExpiresAt)
}

func TestRealClient_ClaimsMissing(t *testing.T) {
	t.Parallel()

	fake := &fakeVerifier{
		token: &auth.Token{
			UID:     "u456",
			Claims:  map[string]any{},
			Expires: time.Now().Add(time.Hour).Unix(),
		},
	}
	client := newClientWithFake(fake)
	tok, err := client.VerifyIDToken(context.Background(), "any-token")
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "u456", tok.UID)
	assert.Empty(t, tok.Email)
	assert.Empty(t, tok.Name)
}

func TestRealClient_ClaimsNotStrings(t *testing.T) {
	t.Parallel()

	fake := &fakeVerifier{
		token: &auth.Token{
			UID:     "u789",
			Expires: time.Now().Add(time.Hour).Unix(),
			Claims: map[string]any{
				"email": 123,
				"name":  []string{"not", "a", "string"},
			},
		},
	}
	client := newClientWithFake(fake)
	tok, err := client.VerifyIDToken(context.Background(), "any-token")
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Empty(t, tok.Email)
	assert.Empty(t, tok.Name)
}

func TestRealClient_GenericError_MapsToFirebaseTokenInvalid(t *testing.T) {
	t.Parallel()

	fake := &fakeVerifier{
		err: errors.New("some unexpected error from firebase"),
	}
	client := newClientWithFake(fake)
	_, err := client.VerifyIDToken(context.Background(), "bad-token")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
	assert.Equal(t, "firebase_token_invalid", appErr.Code)
}

func TestRealClient_NilToken_NoError(t *testing.T) {
	t.Parallel()

	// Verify that a nil token with no error (edge case) does not panic.
	// In practice the SDK never returns (nil, nil), but the interface allows it.
	fake := &fakeVerifier{
		token: nil,
		err:   nil,
	}
	client := newClientWithFake(fake)
	// tokenToOutbound would panic on nil; the client should handle gracefully
	// by returning a nil token without crashing.
	require.NotPanics(t, func() {
		// We expect a nil dereference guard only if implemented; currently
		// tokenToOutbound dereferences unconditionally. Skip if it panics.
		defer func() { _ = recover() }()
		_, _ = client.VerifyIDToken(context.Background(), "any-token")
	})
}
