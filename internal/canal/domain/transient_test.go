package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

var errBoom = errors.New("boom")

func TestIsTransient_TrueForTransientError(t *testing.T) {
	t.Parallel()
	err := &domain.TransientError{Cause: errBoom}
	if !domain.IsTransient(err) {
		t.Errorf("IsTransient(%v) = false, want true", err)
	}
}

func TestIsTransient_FalseForPlainError(t *testing.T) {
	t.Parallel()
	if domain.IsTransient(errBoom) {
		t.Errorf("IsTransient(%v) = true, want false for a plain error", errBoom)
	}
}

func TestIsTransient_FalseForNil(t *testing.T) {
	t.Parallel()
	if domain.IsTransient(nil) {
		t.Error("IsTransient(nil) = true, want false")
	}
}

func TestIsTransient_TrueThroughFmtErrorfWrap(t *testing.T) {
	t.Parallel()
	// A TransientError wrapped again with fmt.Errorf("%w", ...) must still
	// resolve — IsTransient traverses the error chain via errors.As, not a
	// bare type assertion.
	inner := &domain.TransientError{Cause: errBoom}
	wrapped := fmt.Errorf("attempt 3: %w", inner)
	if !domain.IsTransient(wrapped) {
		t.Errorf("IsTransient(%v) = false, want true through a %%w wrap", wrapped)
	}
}

func TestTransientError_ErrorIncludesCause(t *testing.T) {
	t.Parallel()
	err := &domain.TransientError{Cause: errBoom}
	got := err.Error()
	if got == "" {
		t.Fatal("Error() returned empty string")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("errors.Is(err, errBoom) = false, want true via Unwrap")
	}
}

func TestTransientError_Unwrap(t *testing.T) {
	t.Parallel()
	err := &domain.TransientError{Cause: errBoom}
	if unwrapped := errors.Unwrap(err); !errors.Is(unwrapped, errBoom) {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, errBoom)
	}
}
