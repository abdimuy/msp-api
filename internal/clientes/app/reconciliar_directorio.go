//nolint:misspell // Spanish domain vocabulary (directorio, clientes, pulso, etc.) per project convention.
package app

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ReconciliarDirectorio materializes the full active customer directory into
// the Meilisearch index. It:
//  1. Fetches all ESTATUS A+B clients with saldo from the repo (full unbounded list).
//  2. Batch-fetches analytics pulso for all client IDs.
//  3. Projects each (DirectorioItem, pulso) pair to an outbound.DirectorioDoc.
//  4. Calls dirIndex.Reconciliar to bulk-upsert the full set.
//
// Returns the number of documents sent to the index.
// Called by DirectoryReconcileWorker on each background tick.
func (s *Service) ReconciliarDirectorio(ctx context.Context) (int, error) {
	const source = "clientes.ReconciliarDirectorio"

	// Step 1: fetch all active clients (ESTATUS A+B) with saldo. Empty
	// FiltroDirectorio means no additional filtering beyond the repo defaults.
	items, err := s.repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{})
	if err != nil {
		return 0, apperror.NewInternal(
			"reconciliar_directorio_list_failed",
			"error al listar el directorio completo de clientes",
		).WithSource(source).WithError(err)
	}

	if len(items) == 0 {
		return 0, nil
	}

	// Step 2: collect client IDs for bulk pulse fetch.
	ids := make([]int, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.Cliente.ClienteID())
	}

	pulsos, err := s.analytics.ObtenerPulsos(ctx, ids)
	if err != nil {
		return 0, apperror.NewInternal(
			"reconciliar_directorio_pulsos_failed",
			"error al obtener pulsos de analytics para el directorio",
		).WithSource(source).WithError(err)
	}

	// Step 3: project to DirectorioDoc.
	docs := make([]outbound.DirectorioDoc, 0, len(items))
	for _, it := range items {
		c := it.Cliente
		dir := c.Direccion()

		// Build searchable full-text direccion by joining non-empty parts.
		direccionCompleta := buildDireccionCompleta(dir.Calle(), dir.Colonia(), dir.Poblacion())

		pulso, tienePulso := pulsos[c.ClienteID()]

		doc := outbound.DirectorioDoc{
			ClienteID:          c.ClienteID(),
			Nombre:             c.Nombre(),
			ZonaID:             c.ZonaClienteID(),
			ZonaNombre:         c.ZonaNombre(),
			CobradorID:         c.CobradorID(),
			Estatus:            c.Estatus(),
			Telefono:           c.Telefono(),
			Direccion:          direccionCompleta,
			DireccionCalle:     dir.Calle(),
			DireccionColonia:   dir.Colonia(),
			DireccionPoblacion: dir.Poblacion(),
			DireccionCorta:     dir.Corta(),
			Saldo:              it.SaldoTotal,
			ConSaldo:           it.SaldoTotal.IsPositive(),
			TienePulso:         tienePulso,
		}

		if tienePulso {
			doc.Score = pulso.Score
			doc.Segmento = pulso.Segmento
			doc.EstadoPago = pulso.EstadoPago
			doc.RecenciaDias = pulso.RecenciaDias
			doc.Frecuencia = pulso.Frecuencia
			doc.Monetary = pulso.Monetary
			doc.NextBestProduct = pulso.NextBestProduct
			// Cobranza intelligence signals (B2).
			doc.TierRiesgo = pulso.TierRiesgo
			doc.PctPagosATiempo = pulso.PctPagosATiempo
			doc.FechaProxPago = pulso.FechaProxPago
		}

		docs = append(docs, doc)
	}

	// Step 4: bulk-upsert into the Meilisearch index.
	if err := s.dirIndex.Reconciliar(ctx, docs); err != nil {
		return 0, apperror.NewInternal(
			"reconciliar_directorio_index_failed",
			"error al reconciliar el índice de directorio en meilisearch",
		).WithSource(source).WithError(err)
	}

	return len(docs), nil
}

// buildDireccionCompleta joins non-empty, trimmed address parts into a single
// searchable string. This mirrors the logic in domain.Direccion.Corta() but
// includes all three components (calle, colonia, poblacion) without exclusions.
func buildDireccionCompleta(calle, colonia, poblacion string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{calle, colonia, poblacion} {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}
