package firebase_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

func firebaseCfg(projectID string, devMode, allowUnconfigured bool) config.Firebase {
	return config.Firebase{
		ProjectID:          projectID,
		ServiceAccountPath: "/nonexistent/test/path",
		DevMode:            devMode,
		AllowUnconfigured:  allowUnconfigured,
	}
}

func TestNewFirebaseClient_Matrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		env            config.Environment
		cfg            config.Firebase
		wantClientType string // type name as printf %T
		wantErr        bool
		wantErrCode    string // asserted when non-empty
	}{
		{
			name:           "dev_devmode",
			env:            config.EnvDevelopment,
			cfg:            firebaseCfg("", true, false),
			wantClientType: "*firebase.DevModeClient",
			wantErr:        false,
		},
		{
			name:           "dev_devmode_outside_dev",
			env:            config.EnvStaging,
			cfg:            firebaseCfg("", true, false),
			wantClientType: "",
			wantErr:        true,
		},
		{
			name:        "projectid_set_real",
			env:         config.EnvProduction,
			cfg:         firebaseCfg("proj-id", false, false),
			wantErr:     true,
			wantErrCode: "firebase_service_account_missing",
		},
		{
			name:           "allow_unconfigured",
			env:            config.EnvStaging,
			cfg:            firebaseCfg("", false, true),
			wantClientType: "*firebase.NotConfiguredClient",
			wantErr:        false,
		},
		{
			name:           "nothing_set",
			env:            config.EnvStaging,
			cfg:            firebaseCfg("", false, false),
			wantClientType: "",
			wantErr:        true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, err := firebase.NewFirebaseClient(tc.cfg, tc.env)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, client)
				appErr, ok := apperror.As(err)
				assert.True(t, ok, "factory errors must be apperror.Error, got %T", err)
				if tc.wantErrCode != "" {
					require.True(t, ok, "expected apperror for code assertion")
					assert.Equal(t, tc.wantErrCode, appErr.Code)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, client)
			assert.Equal(t, tc.wantClientType, fmt.Sprintf("%T", client))
		})
	}
}
