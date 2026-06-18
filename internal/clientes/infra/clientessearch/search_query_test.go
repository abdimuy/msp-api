// Package clientessearch_test — search_query_test.go tests the filter and sort
// translation logic for B2 cobranza intelligence signals (tier_riesgo,
// puntualidad, prox_pago).
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package clientessearch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ── buildFilter: tier_riesgo ──────────────────────────────────────────────────

func TestBuildFilter_TierRiesgo_IncludesClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{TierRiesgo: "EN_RIESGO"}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `tier_riesgo = "EN_RIESGO"`)
}

func TestBuildFilter_TierRiesgo_Empty_OmitsClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{TierRiesgo: ""}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "tier_riesgo")
}

func TestBuildFilter_TierRiesgo_CombinesWithOtherFilters(t *testing.T) {
	t.Parallel()
	conSaldo := true
	q := outbound.DirectorioQuery{
		ConSaldo:   conSaldo,
		TierRiesgo: "CRITICO",
	}
	filter := clientessearchmeili.BuildFilterForTest(q)
	// Both clauses must appear joined by AND.
	assert.Contains(t, filter, "con_saldo = true")
	assert.Contains(t, filter, `tier_riesgo = "CRITICO"`)
	assert.Contains(t, filter, " AND ", "clauses must be joined with AND")
}

func TestBuildFilter_AllTierValues_Accepted(t *testing.T) {
	t.Parallel()
	tiers := []string{"AL_DIA", "VIGILANCIA", "EN_RIESGO", "CRITICO"}
	for _, tier := range tiers {
		tier := tier
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			q := outbound.DirectorioQuery{TierRiesgo: tier}
			filter := clientessearchmeili.BuildFilterForTest(q)
			assert.Contains(t, filter, tier)
		})
	}
}

// ── buildFilter: banda_credito ────────────────────────────────────────────────

func TestBuildFilter_BandaCredito_IncludesClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{BandaCredito: "ALTO"}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `banda_credito = "ALTO"`)
}

func TestBuildFilter_BandaCredito_Empty_OmitsClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{BandaCredito: ""}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "banda_credito")
}

func TestBuildFilter_BandaCredito_AllValues_Accepted(t *testing.T) {
	t.Parallel()
	for _, band := range []string{"BAJO", "MEDIO", "ALTO", "CRITICO"} {
		band := band
		t.Run(band, func(t *testing.T) {
			t.Parallel()
			q := outbound.DirectorioQuery{BandaCredito: band}
			filter := clientessearchmeili.BuildFilterForTest(q)
			assert.Contains(t, filter, band)
		})
	}
}

// ── buildFilter: banda_recompra ───────────────────────────────────────────────

func TestBuildFilter_BandaRecompra_IncludesClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{BandaRecompra: "ALTA"}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `banda_recompra = "ALTA"`)
}

func TestBuildFilter_BandaRecompra_Empty_OmitsClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{BandaRecompra: ""}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "banda_recompra")
}

func TestBuildFilter_BandaRecompra_AllValues_Accepted(t *testing.T) {
	t.Parallel()
	for _, band := range []string{"ALTA", "MEDIA", "BAJA"} {
		band := band
		t.Run(band, func(t *testing.T) {
			t.Parallel()
			q := outbound.DirectorioQuery{BandaRecompra: band}
			filter := clientessearchmeili.BuildFilterForTest(q)
			assert.Contains(t, filter, band)
		})
	}
}

// ── buildSort: score_credito ──────────────────────────────────────────────────

func TestBuildSort_ScoreCredito_MapsToAttribute(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("score_credito", "desc", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "score_credito:desc", sort[0])
	}
}

func TestBuildSort_ScoreCredito_DefaultsToAsc(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("score_credito", "", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "score_credito:asc", sort[0])
	}
}

// ── buildSort: puntualidad + prox_pago ───────────────────────────────────────

func TestBuildSort_Puntualidad_MapsToAttribute(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("puntualidad", "desc", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "pct_pagos_a_tiempo:desc", sort[0])
	}
}

func TestBuildSort_Puntualidad_DefaultsToAsc(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("puntualidad", "", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "pct_pagos_a_tiempo:asc", sort[0])
	}
}

func TestBuildSort_ProxPago_MapsToAttribute(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("prox_pago", "asc", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "fecha_prox_pago_ts:asc", sort[0])
	}
}

func TestBuildSort_ProxPago_Desc(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("prox_pago", "desc", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "fecha_prox_pago_ts:desc", sort[0])
	}
}

// ── buildSort: score_recompra ─────────────────────────────────────────────────

func TestBuildSort_ScoreRecompra_MapsToAttribute(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("score_recompra", "desc", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "score_recompra:desc", sort[0])
	}
}

func TestBuildSort_ScoreRecompra_DefaultsToAsc(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("score_recompra", "", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "score_recompra:asc", sort[0])
	}
}

// ── buildFilter: banda_clv ───────────────────────────────────────────────────

func TestBuildFilter_BandaCLV_IncludesClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{BandaCLV: "ALTO"}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `banda_clv = "ALTO"`)
}

func TestBuildFilter_BandaCLV_Empty_OmitsClause(t *testing.T) {
	t.Parallel()
	q := outbound.DirectorioQuery{BandaCLV: ""}
	filter := clientessearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "banda_clv")
}

func TestBuildFilter_BandaCLV_AllValues_Accepted(t *testing.T) {
	t.Parallel()
	for _, band := range []string{"ALTO", "MEDIO", "BAJO"} {
		band := band
		t.Run(band, func(t *testing.T) {
			t.Parallel()
			q := outbound.DirectorioQuery{BandaCLV: band}
			filter := clientessearchmeili.BuildFilterForTest(q)
			assert.Contains(t, filter, band)
		})
	}
}

// ── buildSort: clv ───────────────────────────────────────────────────────────

func TestBuildSort_CLV_MapsToAttribute(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("clv", "desc", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "clv:desc", sort[0])
	}
}

func TestBuildSort_CLV_DefaultsToAsc(t *testing.T) {
	t.Parallel()
	sort := clientessearchmeili.BuildSortForTest("clv", "", "")
	if assert.Len(t, sort, 1) {
		assert.Equal(t, "clv:asc", sort[0])
	}
}
