// Package invfb is the Firebird-backed adapter for the inventario module. It
// implements outbound.TraspasoRepo, outbound.ExistenciaQuery,
// outbound.FolioMinter and outbound.AlmacenRepo against Microsip's DOCTOS_IN /
// DOCTOS_IN_DET / SUB_MOVTOS_IN / SALDOS_IN tables, plus the MSP_VENTAS_TRASPASOS
// lookup table owned by this service.
//
// All writes go through the caller's ambient transaction via
// [firebird.GetQuerier] so the full venta + traspaso flow is atomic.
//
//nolint:misspell // Microsip column names are Spanish by convention.
package invfb
