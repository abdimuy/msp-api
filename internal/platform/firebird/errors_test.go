package firebird

// White-box package so we can reference the unexported GDS constants and
// construct *firebirdsql.FbError values without a live database.
// These tests are pure unit tests: no FIREBIRD=1 gate required, no TestMain.

import (
	"errors"
	"io"
	"testing"

	"github.com/nakagami/firebirdsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// makeFbErr constructs a *firebirdsql.FbError with the given GDS codes.
func makeFbErr(codes ...int) error {
	return &firebirdsql.FbError{GDSCodes: codes, Message: "fb err"}
}

// mapErrorCase is a single row in the MapError table test.
type mapErrorCase struct {
	name     string
	input    error
	wantNil  bool // expect nil result
	wantCode string
	wantKind apperror.Kind
	// passThrough true means the original error is returned unchanged.
	passThrough bool
}

func TestMapError(t *testing.T) {
	t.Parallel()

	cases := []mapErrorCase{
		{
			name:    "nil_returns_nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:     "gds_unique_primary_violation",
			input:    makeFbErr(gdsUniquePrimary),
			wantCode: "firebird_unique_violation",
			wantKind: apperror.KindConflict,
		},
		{
			name:     "gds_unique_dup_violation",
			input:    makeFbErr(gdsUniqueDup),
			wantCode: "firebird_unique_violation",
			wantKind: apperror.KindConflict,
		},
		{
			name:     "gds_foreign_key_violation",
			input:    makeFbErr(gdsForeignKey),
			wantCode: "firebird_fk_violation",
			wantKind: apperror.KindConflict,
		},
		{
			name:     "gds_check_constraint",
			input:    makeFbErr(gdsCheck),
			wantCode: "firebird_check_failed",
			wantKind: apperror.KindValidation,
		},
		{
			name:     "gds_deadlock",
			input:    makeFbErr(gdsDeadlock),
			wantCode: "firebird_lock_conflict",
			wantKind: apperror.KindConflict,
		},
		{
			name:     "gds_lock_no_wait",
			input:    makeFbErr(gdsLockNoWait),
			wantCode: "firebird_lock_conflict",
			wantKind: apperror.KindConflict,
		},
		{
			name:     "gds_lock_timeout",
			input:    makeFbErr(gdsLockTimeout),
			wantCode: "firebird_lock_conflict",
			wantKind: apperror.KindConflict,
		},
		{
			name:     "gds_io_error",
			input:    makeFbErr(gdsIOError),
			wantCode: "firebird_io_error",
			wantKind: apperror.KindInternal,
		},
		{
			name:     "gds_conn_lost_pipe",
			input:    makeFbErr(gdsConnLostPipe),
			wantCode: "firebird_connection_lost",
			wantKind: apperror.KindInternal,
		},
		{
			name:     "gds_conn_lost_db",
			input:    makeFbErr(gdsConnLostDB),
			wantCode: "firebird_connection_lost",
			wantKind: apperror.KindInternal,
		},
		{
			name:     "gds_unknown_code",
			input:    makeFbErr(999999),
			wantCode: "firebird_error",
			wantKind: apperror.KindInternal,
		},
		{
			name:     "io_eof_connection_lost",
			input:    io.EOF,
			wantCode: "firebird_connection_lost",
			wantKind: apperror.KindInternal,
		},
		{
			name:        "plain_error_passes_through",
			input:       errors.New("random error"),
			passThrough: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := MapError(tc.input)

			if tc.wantNil {
				require.NoError(t, got)
				return
			}

			if tc.passThrough {
				// The original error is returned unchanged.
				require.Equal(t, tc.input, got)
				return
			}

			require.Error(t, got)
			appErr, ok := apperror.As(got)
			require.True(t, ok, "expected an *apperror.Error, got %T: %v", got, got)
			assert.Equal(t, tc.wantCode, appErr.Code)
			assert.Equal(t, tc.wantKind, appErr.Kind)
		})
	}
}

// isTransientCase is a single row in the IsTransient table test.
type isTransientCase struct {
	name  string
	input error
	want  bool
}

func TestIsTransient(t *testing.T) {
	t.Parallel()

	// Pre-build mapped apperrors for the table.
	mappedUnique := MapError(makeFbErr(gdsUniquePrimary))
	mappedFK := MapError(makeFbErr(gdsForeignKey))
	mappedCheck := MapError(makeFbErr(gdsCheck))
	mappedLock := MapError(makeFbErr(gdsDeadlock))
	mappedIO := MapError(makeFbErr(gdsIOError))
	mappedConnLost := MapError(makeFbErr(gdsConnLostPipe))

	cases := []isTransientCase{
		{name: "nil_is_not_transient", input: nil, want: false},
		{name: "mapped_unique_violation_not_transient", input: mappedUnique, want: false},
		{name: "mapped_fk_violation_not_transient", input: mappedFK, want: false},
		{name: "mapped_check_not_transient", input: mappedCheck, want: false},
		{name: "mapped_lock_conflict_is_transient", input: mappedLock, want: true},
		{name: "mapped_io_error_is_transient", input: mappedIO, want: true},
		{name: "mapped_connection_lost_is_transient", input: mappedConnLost, want: true},
		{
			name:  "raw_fb_error_deadlock_is_transient",
			input: makeFbErr(gdsDeadlock),
			want:  true,
		},
		{
			name:  "raw_fb_error_unique_not_transient",
			input: makeFbErr(gdsUniquePrimary),
			want:  false,
		},
		{name: "io_eof_is_transient", input: io.EOF, want: true},
		{name: "random_error_not_transient", input: errors.New("random"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsTransient(tc.input))
		})
	}
}
