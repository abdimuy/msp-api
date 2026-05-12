// Package storage implements the [outbound.StorageProvider] port used by the
// ventas module to persist binary blobs (image uploads such as evidencia de
// cobranza and INE photos).
//
// The single implementation is [FilesystemProvider], which writes blobs under
// a local directory with a sidecar `.meta` file holding content-type and
// size. The on-prem Windows Server target reads and writes the local disk
// directly; cloud object storage is intentionally not part of the v1 design.
//
// The factory [New] is a thin wrapper that builds a FilesystemProvider from
// config.Storage. If a different backend is ever required, add a new
// implementation alongside this one rather than reintroducing a selector
// abstraction.
package storage
