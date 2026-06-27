// Package analyticshttp — cartera_dtos.go contains the wire DTOs, Input, and
// Output types for the cartera dashboard endpoints.
// Money fields are strings to preserve decimal precision in JSON transport.
// Date fields are RFC3339 UTC strings.
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

// ─── Cartera DTOs ─────────────────────────────────────────────────────────────

// SaludCarteraDTO is the wire representation of the portfolio health KPI set.
type SaludCarteraDTO struct {
	SaldoTotal       string `json:"saldo_total"        doc:"Total outstanding balance (2 dp)"`
	SaldoMoroso      string `json:"saldo_moroso"       doc:"Balance in delinquent buckets 31+ days (2 dp)"`
	PAR              string `json:"par"                doc:"Portfolio-at-Risk = SaldoMoroso/SaldoTotal [0,1] (4 dp)"`
	CEIRate          string `json:"cei_rate"           doc:"Collection Effectiveness Index = Collected/SaldoTotal [0,1] (4 dp)"`
	ImporteColectado string `json:"importe_colectado"  doc:"Total collected in the CEI period (2 dp)"`
	CuentasTotal     int    `json:"cuentas_total"      doc:"Total active credit accounts"`
	CuentasEnMora    int    `json:"cuentas_en_mora"    doc:"Accounts in delinquent buckets 31+ days"`
	MargenRealProxy  string `json:"margen_real_proxy"  doc:"Margin proxy: 0.528×Collected − PAR×SaldoTotal×0.70; floored at 0 (2 dp)"`
}

// AgingBucketDTO is the wire representation of one aging-bucket distribution row.
type AgingBucketDTO struct {
	Bucket   string `json:"bucket"    doc:"Aging bucket: '0-30' | '31-60' | '61-90' | '90+'"`
	Saldo    string `json:"saldo"     doc:"Outstanding balance in this bucket (2 dp)"`
	Conteo   int    `json:"conteo"    doc:"Active accounts in this bucket"`
	PctSaldo string `json:"pct_saldo" doc:"Proportion of total portfolio saldo [0,1] (4 dp)"`
}

// CosechaDTO is the wire representation of one vintage cohort row.
type CosechaDTO struct {
	CohortMonth int    `json:"cohort_month" doc:"Cohort ordinal: year×12+month (e.g. 24318 = June 2026)"`
	AgeMonths   int    `json:"age_months"   doc:"Months since cohort origin relative to now"`
	Saldo       string `json:"saldo"        doc:"Outstanding balance from this cohort (2 dp)"`
	Conteo      int    `json:"conteo"       doc:"Active accounts from this cohort"`
}

// CobradorPerformanceDTO is the wire representation of one cobrador's portfolio performance.
type CobradorPerformanceDTO struct {
	CobradorID       int    `json:"cobrador_id"       doc:"Cobrador numeric ID; 0 = accounts with no cobrador assigned"`
	ZonaClienteID    int    `json:"zona_cliente_id"   doc:"Zone of this cobrador's portfolio"`
	CEI              string `json:"cei"               doc:"Collection Effectiveness Index [0,1] (4 dp)"`
	PAR              string `json:"par"               doc:"Portfolio-at-Risk for this cobrador's cartera [0,1] (4 dp)"`
	PctCorriente     string `json:"pct_corriente"     doc:"Proportion of accounts in the 0-30 current bucket [0,1] (4 dp)"`
	SaldoTotal       string `json:"saldo_total"       doc:"Total outstanding balance managed (2 dp)"`
	SaldoMoroso      string `json:"saldo_moroso"      doc:"Delinquent balance in buckets 31+ (2 dp)"`
	CuentasTotal     int    `json:"cuentas_total"     doc:"Total accounts managed"`
	ImporteColectado string `json:"importe_colectado" doc:"Amount collected in the CEI period (2 dp)"`
}

// CuentaRiesgoDTO is the wire representation of one at-risk credit account.
type CuentaRiesgoDTO struct {
	ClienteID       int    `json:"cliente_id"         doc:"Microsip client ID"`
	Nombre          string `json:"nombre"             doc:"Client name"`
	Zona            string `json:"zona"               doc:"Sales zone name"`
	TierRiesgo      string `json:"tier_riesgo"        doc:"Risk tier: AL_DIA | VIGILANCIA | EN_RIESGO | CRITICO"`
	Segmento        string `json:"segmento"           doc:"RFM segment"`
	EstadoPago      string `json:"estado_pago"        doc:"Solvency signal: SIN_CREDITO | LIQUIDADO | AL_CORRIENTE | ATRASADO | MOROSO"`
	Saldo           string `json:"saldo"              doc:"Outstanding balance (2 dp)"`
	DiasAtrasoProm  int    `json:"dias_atraso_prom"   doc:"Average days late, read-time adjusted for open gap"`
	PctPagosATiempo string `json:"pct_pagos_a_tiempo" doc:"Proportion of on-time payments [0,100] (2 dp)"`
	CadenciaDias    int    `json:"cadencia_dias"      doc:"Typical payment cadence in days"`
	FechaUltimoPago string `json:"fecha_ultimo_pago"  format:"date-time" doc:"RFC3339 UTC of last payment; empty if no payment history"`
	FechaProxPago   string `json:"fecha_prox_pago"    format:"date-time" doc:"RFC3339 UTC of expected next payment; empty if no cadencia"`
}

// RollRateDTO is the wire representation of the roll-rate computation.
// Disponible=false indicates the system is still accumulating snapshot cuts.
type RollRateDTO struct {
	Disponible         bool    `json:"disponible"           doc:"false when fewer than 2 snapshot cuts exist ('acumulando datos')"`
	RollRate           float64 `json:"roll_rate"            doc:"Signed net delinquency migration scalar in [-1,+1]; 0 when Disponible=false"`
	FechaCorteAnterior string  `json:"fecha_corte_anterior" format:"date-time" doc:"RFC3339 UTC of the older snapshot cut; empty when Disponible=false"`
	FechaCorteReciente string  `json:"fecha_corte_reciente" format:"date-time" doc:"RFC3339 UTC of the newer snapshot cut; empty when Disponible=false"`
}

// ─── Input / Output types ─────────────────────────────────────────────────────

// CarteraQueryInput groups the optional filter params shared by all cartera
// endpoints.
// zona: numeric zone ID (Firebird ZONA_CLIENTE_ID); absent = all zones.
// cobrador: numeric cobrador ID string (e.g. "3"); absent = all cobradores.
// periodo: YYYY-MM collection window for CEI metrics; absent = last 30 days.
// Numeric params use string type because Huma v2 does not support *int for
// query parameters; an empty string is treated as "not provided".
type CarteraQueryInput struct {
	Zona     string `query:"zona"     doc:"ID numérico de la zona como cadena (ej. '7'); ausente = toda la cartera"`
	Cobrador string `query:"cobrador" doc:"ID numérico del cobrador como cadena (ej. '3'); ausente = todos los cobradores"`
	Periodo  string `query:"periodo"  doc:"Periodo CEI en formato YYYY-MM (ej. 2026-06); ausente = últimos 30 días"`
}

// SaludCarteraOutput is the response for GET /cartera/salud.
type SaludCarteraOutput struct {
	Body SaludCarteraDTO
}

// AgingOutput is the response for GET /cartera/aging.
type AgingOutput struct {
	Body struct {
		Items []AgingBucketDTO `json:"items"`
	}
}

// CosechasOutput is the response for GET /cartera/cosechas.
type CosechasOutput struct {
	Body struct {
		Items []CosechaDTO `json:"items"`
	}
}

// CobradorRankingOutput is the response for GET /cartera/cobradores.
type CobradorRankingOutput struct {
	Body struct {
		Items []CobradorPerformanceDTO `json:"items"`
	}
}

// CuentasRiesgoOutput is the response for GET /cartera/cuentas-riesgo.
type CuentasRiesgoOutput struct {
	Body struct {
		Items []CuentaRiesgoDTO `json:"items"`
	}
}

// RollRateOutput is the response for GET /cartera/roll-rate.
type RollRateOutput struct {
	Body RollRateDTO
}
