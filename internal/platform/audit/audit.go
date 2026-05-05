// Package audit provides the audit/timestamp/sync value objects embedded in
// every domain entity.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Auditable embeds creation/update timestamps + the user IDs that performed
// each action. Used by Type A and Type C entities.
type Auditable struct {
	createdAt time.Time
	updatedAt time.Time
	createdBy uuid.UUID
	updatedBy uuid.UUID
}

// NewAuditable initializes an Auditable for a fresh entity.
func NewAuditable(now time.Time, userID uuid.UUID) Auditable {
	return Auditable{createdAt: now, updatedAt: now, createdBy: userID, updatedBy: userID}
}

// HydrateAuditable rebuilds an Auditable from persistence without validation.
func HydrateAuditable(createdAt, updatedAt time.Time, createdBy, updatedBy uuid.UUID) Auditable {
	return Auditable{createdAt: createdAt, updatedAt: updatedAt, createdBy: createdBy, updatedBy: updatedBy}
}

// MarkUpdated stamps the audit with a new updatedAt and updatedBy.
func (a *Auditable) MarkUpdated(userID uuid.UUID) {
	a.updatedAt = time.Now()
	a.updatedBy = userID
}

// CreatedAt returns the original creation time.
func (a *Auditable) CreatedAt() time.Time { return a.createdAt }

// UpdatedAt returns the latest update time.
func (a *Auditable) UpdatedAt() time.Time { return a.updatedAt }

// CreatedBy returns the user ID that created the entity.
func (a *Auditable) CreatedBy() uuid.UUID { return a.createdBy }

// UpdatedBy returns the user ID that last updated the entity.
func (a *Auditable) UpdatedBy() uuid.UUID { return a.updatedBy }

// Timestamped is the lighter sibling of Auditable for system-created entities
// (Type B pipelines, where there's no human user behind a transition).
type Timestamped struct {
	createdAt time.Time
	updatedAt time.Time
}

// NewTimestamped initializes a Timestamped at the given moment.
func NewTimestamped(now time.Time) Timestamped {
	return Timestamped{createdAt: now, updatedAt: now}
}

// HydrateTimestamped rebuilds a Timestamped from persistence.
func HydrateTimestamped(createdAt, updatedAt time.Time) Timestamped {
	return Timestamped{createdAt: createdAt, updatedAt: updatedAt}
}

// MarkUpdated bumps updatedAt to the current time.
func (t *Timestamped) MarkUpdated() { t.updatedAt = time.Now() }

// CreatedAt returns the original creation time.
func (t *Timestamped) CreatedAt() time.Time { return t.createdAt }

// UpdatedAt returns the latest update time.
func (t *Timestamped) UpdatedAt() time.Time { return t.updatedAt }

// MicrosipSync tracks the bidirectional state of an entity that mirrors a
// Microsip/Firebird record. Used by Type C entities.
//
//	microsipID — the Firebird PK once a push has succeeded; nil before.
//	pulledAt   — last successful pull from Microsip.
//	pushedAt   — last successful push to Microsip.
type MicrosipSync struct {
	microsipID *int
	pulledAt   *time.Time
	pushedAt   *time.Time
}

// NewMicrosipSync initializes an empty sync record (entity created locally,
// not yet pushed to Microsip).
func NewMicrosipSync() MicrosipSync { return MicrosipSync{} }

// NewMicrosipSyncFromPull builds a sync record from an entity that just came
// in from a Microsip pull.
func NewMicrosipSyncFromPull(microsipID int, now time.Time) MicrosipSync {
	id := microsipID
	pulled := now
	return MicrosipSync{microsipID: &id, pulledAt: &pulled}
}

// HydrateMicrosipSync rebuilds a sync record from persistence.
func HydrateMicrosipSync(microsipID *int, pulledAt, pushedAt *time.Time) MicrosipSync {
	return MicrosipSync{microsipID: microsipID, pulledAt: pulledAt, pushedAt: pushedAt}
}

// SetMicrosipID assigns the Firebird PK after the first successful push.
func (s *MicrosipSync) SetMicrosipID(id int) { s.microsipID = &id }

// MarkPulled stamps the sync with a fresh pull time.
func (s *MicrosipSync) MarkPulled() {
	now := time.Now()
	s.pulledAt = &now
}

// MarkPushed stamps the sync with a fresh push time.
func (s *MicrosipSync) MarkPushed() {
	now := time.Now()
	s.pushedAt = &now
}

// MicrosipID returns the Firebird PK or nil if never pushed.
func (s *MicrosipSync) MicrosipID() *int { return s.microsipID }

// PulledAt returns the last pull time or nil if never pulled.
func (s *MicrosipSync) PulledAt() *time.Time { return s.pulledAt }

// PushedAt returns the last push time or nil if never pushed.
func (s *MicrosipSync) PushedAt() *time.Time { return s.pushedAt }
