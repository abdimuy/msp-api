// Package domain holds the garantías module's aggregates, entities, value
// objects, and sentinel errors. Garantías is a sealed vertical slice
// (ADR-0009): this package imports nothing beyond the standard library and
// internal/platform/apperror. It has zero awareness of Firebird, HTTP, or
// any other module — firebird.ToWallClock and firebird.ScanUTCTime never
// appear here; that translation lives entirely in infra/garfb.
package domain
