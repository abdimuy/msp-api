package storage

import (
	"log/slog"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// New builds the filesystem-backed [outbound.StorageProvider] rooted at
// cfg.Dir. config.Load() has already validated cfg; this factory only maps a
// known-good configuration to the concrete provider.
//
// The system is intentionally filesystem-only: the on-prem Windows Server
// target writes blobs to a local directory and any retention / backup is the
// operator's responsibility. If cloud storage is ever required, add a new
// implementation alongside FilesystemProvider rather than reintroducing the
// previous selector abstraction.
func New(cfg config.Storage) (outbound.StorageProvider, error) {
	provider, err := NewFilesystemProvider(cfg.Dir)
	if err != nil {
		return nil, err
	}
	slog.Info("storage.filesystem_initialized", "dir", cfg.Dir)
	return provider, nil
}
