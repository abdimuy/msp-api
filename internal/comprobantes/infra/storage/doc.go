// Package storage implements the [outbound.StorageProvider] port used by the
// comprobantes module to persist rendered receipt PDFs.
//
// The single implementation is [FilesystemProvider], which writes blobs under
// a local directory with a sidecar `.meta` file holding content-type and
// size. The on-prem Windows Server target reads and writes the local disk
// directly; cloud object storage is intentionally not part of the v1 design
// (ADR-0003).
//
// If a different backend is ever required, add a new implementation
// alongside this one rather than reintroducing a selector abstraction.
package storage
