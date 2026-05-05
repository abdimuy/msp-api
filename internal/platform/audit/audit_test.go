package audit_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

func TestNewAuditable_SetsAllFieldsToTheSameUserAndTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	user := uuid.New()
	a := audit.NewAuditable(now, user)

	assert.Equal(t, now, a.CreatedAt())
	assert.Equal(t, now, a.UpdatedAt())
	assert.Equal(t, user, a.CreatedBy())
	assert.Equal(t, user, a.UpdatedBy())
}

func TestAuditable_MarkUpdated_OnlyUpdatedFieldsChange(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	creator := uuid.New()
	updater := uuid.New()

	a := audit.NewAuditable(created, creator)
	time.Sleep(2 * time.Millisecond) // ensure later timestamp
	a.MarkUpdated(updater)

	assert.Equal(t, created, a.CreatedAt(), "createdAt must not change")
	assert.Equal(t, creator, a.CreatedBy(), "createdBy must not change")
	assert.True(t, a.UpdatedAt().After(created), "updatedAt must advance")
	assert.Equal(t, updater, a.UpdatedBy())
}

func TestHydrateAuditable_NoValidation(t *testing.T) {
	t.Parallel()
	c := time.Now()
	u := time.Now().Add(time.Hour)
	cb := uuid.New()
	ub := uuid.New()
	a := audit.HydrateAuditable(c, u, cb, ub)

	assert.Equal(t, c, a.CreatedAt())
	assert.Equal(t, u, a.UpdatedAt())
	assert.Equal(t, cb, a.CreatedBy())
	assert.Equal(t, ub, a.UpdatedBy())
}

func TestNewTimestamped(t *testing.T) {
	t.Parallel()
	now := time.Now()
	ts := audit.NewTimestamped(now)
	assert.Equal(t, now, ts.CreatedAt())
	assert.Equal(t, now, ts.UpdatedAt())
}

func TestTimestamped_MarkUpdated(t *testing.T) {
	t.Parallel()
	ts := audit.NewTimestamped(time.Now().Add(-time.Hour))
	before := ts.UpdatedAt()
	time.Sleep(2 * time.Millisecond)
	ts.MarkUpdated()
	assert.True(t, ts.UpdatedAt().After(before))
}

func TestNewMicrosipSync_Empty(t *testing.T) {
	t.Parallel()
	s := audit.NewMicrosipSync()
	assert.Nil(t, s.MicrosipID())
	assert.Nil(t, s.PulledAt())
	assert.Nil(t, s.PushedAt())
}

func TestNewMicrosipSyncFromPull_HasIDAndPulledAt(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := audit.NewMicrosipSyncFromPull(42, now)

	require.NotNil(t, s.MicrosipID())
	assert.Equal(t, 42, *s.MicrosipID())
	require.NotNil(t, s.PulledAt())
	assert.Equal(t, now, *s.PulledAt())
	assert.Nil(t, s.PushedAt(), "push has not happened yet")
}

func TestMicrosipSync_SetMicrosipIDAndMarkPushed(t *testing.T) {
	t.Parallel()
	s := audit.NewMicrosipSync()
	s.SetMicrosipID(99)
	s.MarkPushed()

	require.NotNil(t, s.MicrosipID())
	assert.Equal(t, 99, *s.MicrosipID())
	require.NotNil(t, s.PushedAt())
}

func TestMicrosipSync_MarkPulled(t *testing.T) {
	t.Parallel()
	s := audit.NewMicrosipSync()
	s.MarkPulled()
	require.NotNil(t, s.PulledAt())
}

func TestHydrateMicrosipSync(t *testing.T) {
	t.Parallel()
	id := 7
	now := time.Now()
	s := audit.HydrateMicrosipSync(&id, &now, nil)

	require.NotNil(t, s.MicrosipID())
	assert.Equal(t, 7, *s.MicrosipID())
	require.NotNil(t, s.PulledAt())
	assert.Nil(t, s.PushedAt())
}
