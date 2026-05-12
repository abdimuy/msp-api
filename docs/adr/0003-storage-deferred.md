# ADR 0003 — Filesystem-only blob storage (superseded R2 deferral)

- **Status:** Accepted (supersedes the earlier R2-deferral draft of this ADR)
- **Date:** 2026-05-11
- **Decision drivers:** the on-prem deploy target (Windows Server 2016) reads and writes its local disk directly; cloud object storage adds operational complexity (credentials, network, billing) without solving a real problem for the v1 audience. An earlier draft of this ADR introduced a `StorageProvider` selector with a Cloudflare R2 stub for future use — that abstraction was removed because the second implementation is not on any roadmap.

## Context

The `ventas` module accepts multipart image uploads via `POST /v2/ventas/{id}/imagenes`. The blobs need a backing store. The customer base runs entirely on-prem with local disk and no requirement for cloud-hosted object storage.

## Decision

A single implementation: **`FilesystemProvider`**, behind the `outbound.StorageProvider` port. Blobs are written to `<STORAGE_DIR>/<key>` with a sidecar `<key>.meta` file holding content-type and size. Path-traversal protection rejects keys containing `..`, leading `/`, null bytes, or backslashes.

Configuration is a single environment variable:

```
STORAGE_DIR=./var/uploads
```

No selector, no enum, no stub. If a future module ever needs a cloud backend, add a new concrete implementation alongside `FilesystemProvider` (the port is unchanged) and select it at the composition root.

## What about the database CHECK constraint?

The `MSP_VENTAS_IMAGENES.STORAGE_KIND` column has a `CHECK (STORAGE_KIND IN ('FILESYSTEM', 'R2'))` constraint inherited from migration 000002. The Go code only ever inserts `FILESYSTEM`, so the `R2` value in the constraint is dormant. Removing it would require a forward migration and is not worth the churn — the constraint correctly rejects everything outside the allowed set today.

If a future module ever writes `R2` directly, the constraint already permits it. No migration is needed at that point unless we want to widen the set further.

## Consequences

- **DX**: developers run the API with zero cloud dependencies.
- **Production**: ships filesystem-only. Backup / retention is the operator's responsibility (rsync, scheduled task) for v1.
- **Tests**: filesystem provider tests run in `t.TempDir()` — deterministic and fast.
- **Smaller surface**: no factory selector, no stub provider, no parallel env vars, no second config validation branch. The port + one implementation is the entire blob-storage subsystem.
