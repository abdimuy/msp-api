// Package invhttp hosts the inventario module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount point. It is the outermost
// adapter layer — nothing inside the inventario module imports it.
//
//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp
