//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// RangoFechasRitmo is an optional inclusive date range for the BuildRitmoPago
// window. It mirrors outbound.RangoFechas but lives in domain to avoid an import
// cycle (domain must not import outbound). The app layer maps outbound.RangoFechas
// to this type before calling BuildRitmoPago.
type RangoFechasRitmo struct {
	Desde *time.Time
	Hasta *time.Time
}

// EventoTipo enumerates the types of events that can appear in a RitmoPago series.
type EventoTipo string

const (
	// EventoVentaCredito denotes a credit sale that adds to the outstanding balance.
	EventoVentaCredito EventoTipo = "venta_credito"
	// EventoVentaContado denotes a cash sale (does not affect the credit balance).
	EventoVentaContado EventoTipo = "venta_contado"
	// EventoLiquidacion denotes the week in which the outstanding balance reached zero.
	EventoLiquidacion EventoTipo = "liquidacion"
)

// PagoCrudo is the raw payment data read from Firebird by the repository before
// being passed to BuildRitmoPago. Fields use UTC time.
type PagoCrudo struct {
	Fecha        time.Time       // UTC
	Hora         string          // "HH:MM:SS" wall-clock (Microsip local, display only)
	Importe      decimal.Decimal // positive amount
	DoctoCCID    int             // DOCTOS_CC primary key of the abono document
	ConceptoCCID int             // CONCEPTO_CC_ID of the abono
	Concepto     string          // human-readable concepto name (Win1252-decoded)
	DoctoPVID    int             // linked sale's DOCTO_PV_ID (0 when not resolvable)
	Folio        string          // linked sale's FOLIO (empty when not resolvable)
	Articulo     string          // name of the first J/N article of the linked sale (Win1252-decoded; empty when not resolvable)
}

// PagoRitmo is an enriched payment entry within a SemanaRitmo bucket.
// It carries full context — date/time, amount, concepto, category, and the
// linked sale — so the frontend can render meaningful payment entries without
// additional round-trips.
type PagoRitmo struct {
	DoctoCCID    int
	Fecha        time.Time // UTC date of the abono
	Hora         string    // "HH:MM:SS" local wall-clock (Microsip, display only — not UTC)
	Importe      decimal.Decimal
	ConceptoCCID int
	Concepto     string    // human-readable concepto name
	Categoria    Categoria // ClasificarConcepto(ConceptoCCID)
	DoctoPVID    int       // sale this payment was applied to (0 when not resolvable)
	Folio        string    // folio of the sale (empty when not resolvable)
	Articulo     string    // name of the first J/N article of the linked sale (empty when not resolvable)
}

// VentaCruda is the raw sale header read from Firebird by the repository before
// being passed to BuildRitmoPago. Fields use UTC time.
type VentaCruda struct {
	Fecha      time.Time       // UTC
	Total      decimal.Decimal // net invoice amount
	DoctoPvID  int
	Folio      string
	EsCredito  bool
	PlazoMeses int // 0 for contado
}

// SemanaRitmo is a single weekly bucket in the RitmoPago series.
type SemanaRitmo struct {
	// SemanaInicio is the Monday-anchored (or ruta-day-anchored) start of this
	// week at 00:00 UTC.
	SemanaInicio time.Time
	// MontoAbonado is the total payments received in this week.
	MontoAbonado decimal.Decimal
	// Saldo is the reconstructed outstanding balance at the end of this week.
	Saldo decimal.Decimal
	// NumPagos is the count of individual payments in this week.
	NumPagos int
	// Pagos holds the enriched detail of each payment applied in this week,
	// in the same order they appear in the input pagos slice. An empty week
	// carries an empty (non-nil) slice so that the JSON serialization is []
	// rather than null.
	Pagos []PagoRitmo
}

// EventoRitmo is a notable event that occurred within the payment rhythm window.
type EventoRitmo struct {
	// Fecha is the UTC date/time of the event.
	Fecha time.Time
	// Tipo is the event kind (venta_credito, venta_contado, liquidacion).
	Tipo EventoTipo
	// Monto is the monetary amount (sale total for ventas; 0 for liquidacion).
	Monto decimal.Decimal
	// DoctoPvID is the Microsip DOCTOS_PV primary key (0 for liquidacion if unknown).
	DoctoPvID int
	// Folio is the Microsip folio number ("" for liquidacion if unknown).
	Folio string
	// PlazoMeses is the credit term in months (0 for contado and liquidacion).
	PlazoMeses int
}

// ResumenRitmo is the aggregated summary of the payment rhythm over the window.
type ResumenRitmo struct {
	// TotalAbonado is the sum of income movements in the window (pago, enganche, otro —
	// i.e. any movement where EsIngreso() is true). Condonacion and perdida are excluded.
	TotalAbonado decimal.Decimal
	// TotalPerdonado is the sum of forgiveness/write-off movements (condonacion and
	// perdida). These reduce the balance but are not actual cash inflow.
	TotalPerdonado decimal.Decimal
	// SemanasConPago is the count of weeks with at least one movement (any category,
	// including condonacion/perdida — "semana con movimiento").
	SemanasConPago int
	// SemanasActivas is the span from the first week with a payment to the last
	// week of the window (inclusive). Zero when no payments exist.
	SemanasActivas int
	// RachaActualSem is the count of consecutive weeks with a payment counting
	// backwards from the last week of the window.
	RachaActualSem int
	// ConstanciaPct is SemanasConPago / SemanasActivas * 100, rounded to 2 decimal
	// places. Zero when SemanasActivas == 0.
	ConstanciaPct decimal.Decimal
	// SaldoActual is the live outstanding balance (clamped >= 0).
	SaldoActual decimal.Decimal
}

// RitmoPago is the assembled weekly payment-rhythm result for a single client.
type RitmoPago struct {
	// AnclaDiaRuta is the mode weekday of the client's payment dates (the
	// "ruta day"). time.Monday when no payments exist.
	AnclaDiaRuta time.Weekday
	// Semanas is the ordered (ascending) list of weekly buckets.
	Semanas []SemanaRitmo
	// Eventos is the ordered (ascending by Fecha) list of notable events.
	Eventos []EventoRitmo
	// Resumen holds the aggregated summary statistics.
	Resumen ResumenRitmo
}

// BuildRitmoPago reconstructs the weekly payment-rhythm series from raw repository
// data. It is a pure, deterministic function — no clock reads, no IO.
//
// rango optionally restricts the window. Nil bounds on RangoFechasRitmo mean the
// window is derived from the first activity (earliest pago or venta) to ahora.
func BuildRitmoPago(
	pagos []PagoCrudo,
	ventas []VentaCruda,
	saldoActual decimal.Decimal,
	ahora time.Time,
	rango RangoFechasRitmo,
) RitmoPago {
	// ── 1. Compute the ruta-day anchor (mode of payment weekdays). ──────────────
	ancla := modeWeekday(pagos)

	// ── 2. Determine the window bounds. ─────────────────────────────────────────
	windowStart, windowEnd := computeWindow(pagos, ventas, ahora, rango, ancla)
	if windowStart.IsZero() {
		// No activity and no explicit desde bound → return empty result.
		return RitmoPago{
			AnclaDiaRuta: ancla,
			Semanas:      []SemanaRitmo{},
			Eventos:      []EventoRitmo{},
			Resumen: ResumenRitmo{
				SaldoActual: clampZero(saldoActual),
			},
		}
	}

	// ── 3. Build the weekly buckets (all weeks, including empty ones). ───────────
	semanas := buildSemanas(windowStart, windowEnd)

	// ── 4. Accumulate pagos and ventas into buckets. ─────────────────────────────
	indexarPagos(semanas, pagos)
	indexarVentas := indexVentasPorSemana(semanas, ventas)

	// ── 5. Reconstruct the balance series. ──────────────────────────────────────
	reconstruirSaldo(semanas, indexarVentas, saldoActual)

	// ── 6. Build eventos. ────────────────────────────────────────────────────────
	eventos := buildEventos(semanas, ventas)

	// ── 7. Compute resumen. ──────────────────────────────────────────────────────
	resumen := buildResumen(semanas, saldoActual)

	return RitmoPago{
		AnclaDiaRuta: ancla,
		Semanas:      semanas,
		Eventos:      eventos,
		Resumen:      resumen,
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// modeWeekday returns the most-frequent weekday among pago dates.
// Ties are broken by the earliest weekday in Mon..Sun order.
// Returns time.Monday when pagos is empty.
func modeWeekday(pagos []PagoCrudo) time.Weekday {
	if len(pagos) == 0 {
		return time.Monday
	}
	counts := [7]int{}
	for _, p := range pagos {
		counts[p.Fecha.UTC().Weekday()]++
	}
	// Find max count.
	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	// Walk Mon(1)..Sun(0) order: Mon=1, Tue=2, ..., Sat=6, Sun=0.
	order := []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday, time.Sunday,
	}
	for _, wd := range order {
		if counts[wd] == maxCount {
			return wd
		}
	}
	return time.Monday
}

// weekAnchorStart returns the start of the week that contains t, where "start
// of week" is defined as the most-recent occurrence of ancla at 00:00 UTC on
// or before t.
func weekAnchorStart(t time.Time, ancla time.Weekday) time.Time {
	t = t.UTC()
	// Days to subtract to reach the previous (or same) ancla day.
	diff := (int(t.Weekday()) - int(ancla) + 7) % 7
	start := t.AddDate(0, 0, -diff)
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
}

// computeWindow returns (windowStart, windowEnd) as anchor-aligned week starts.
// windowStart is zero when there is no activity and no rango.Desde.
func computeWindow(
	pagos []PagoCrudo,
	ventas []VentaCruda,
	ahora time.Time,
	rango RangoFechasRitmo,
	ancla time.Weekday,
) (time.Time, time.Time) {
	// windowEnd: week containing ahora, or rango.Hasta.
	end := ahora.UTC()
	if rango.Hasta != nil {
		end = rango.Hasta.UTC()
	}
	windowEnd := weekAnchorStart(end, ancla)

	// windowStart: earliest activity, or rango.Desde if provided.
	if rango.Desde != nil {
		return weekAnchorStart(rango.Desde.UTC(), ancla), windowEnd
	}

	// Find earliest activity.
	var earliest time.Time
	for _, p := range pagos {
		f := p.Fecha.UTC()
		if earliest.IsZero() || f.Before(earliest) {
			earliest = f
		}
	}
	for _, v := range ventas {
		f := v.Fecha.UTC()
		if earliest.IsZero() || f.Before(earliest) {
			earliest = f
		}
	}
	if earliest.IsZero() {
		return time.Time{}, windowEnd
	}
	return weekAnchorStart(earliest, ancla), windowEnd
}

// buildSemanas generates all weekly buckets from windowStart to windowEnd inclusive.
// Pagos is initialized to a non-nil empty slice so that empty weeks serialize
// as [] rather than null in JSON.
func buildSemanas(windowStart, windowEnd time.Time) []SemanaRitmo {
	if windowEnd.Before(windowStart) {
		return []SemanaRitmo{}
	}
	var semanas []SemanaRitmo
	cur := windowStart
	for !cur.After(windowEnd) {
		semanas = append(semanas, SemanaRitmo{
			SemanaInicio: cur,
			MontoAbonado: decimal.Zero,
			Saldo:        decimal.Zero,
			NumPagos:     0,
			Pagos:        []PagoRitmo{},
		})
		cur = cur.AddDate(0, 0, 7)
	}
	return semanas
}

// indexarPagos distributes pagos into their corresponding weekly buckets.
// Each pago is converted to a PagoRitmo and appended to the bucket's Pagos in input order.
func indexarPagos(semanas []SemanaRitmo, pagos []PagoCrudo) {
	for _, p := range pagos {
		f := p.Fecha.UTC()
		for i := range semanas {
			bucketEnd := semanas[i].SemanaInicio.AddDate(0, 0, 7)
			if !f.Before(semanas[i].SemanaInicio) && f.Before(bucketEnd) {
				semanas[i].MontoAbonado = semanas[i].MontoAbonado.Add(p.Importe)
				semanas[i].NumPagos++
				semanas[i].Pagos = append(semanas[i].Pagos, PagoRitmo{
					DoctoCCID:    p.DoctoCCID,
					Fecha:        p.Fecha,
					Hora:         p.Hora,
					Importe:      p.Importe,
					ConceptoCCID: p.ConceptoCCID,
					Concepto:     p.Concepto,
					Categoria:    ClasificarConcepto(p.ConceptoCCID),
					DoctoPVID:    p.DoctoPVID,
					Folio:        p.Folio,
					Articulo:     p.Articulo,
				})
				break
			}
		}
	}
}

// ventasPorSemana is the result of indexing ventas into weekly buckets.
// Only credit ventas affect the balance; contado ventas are stored here too for
// event generation but excluded from saldo math.
type ventasPorSemana struct {
	creditoTotal decimal.Decimal // sum of credit sale totals for this week
}

// indexVentasPorSemana builds a parallel slice of ventasPorSemana for each bucket.
func indexVentasPorSemana(semanas []SemanaRitmo, ventas []VentaCruda) []ventasPorSemana {
	result := make([]ventasPorSemana, len(semanas))
	for _, v := range ventas {
		if !v.EsCredito {
			continue
		}
		f := v.Fecha.UTC()
		for i := range semanas {
			bucketEnd := semanas[i].SemanaInicio.AddDate(0, 0, 7)
			if !f.Before(semanas[i].SemanaInicio) && f.Before(bucketEnd) {
				result[i].creditoTotal = result[i].creditoTotal.Add(v.Total)
				break
			}
		}
	}
	return result
}

// reconstruirSaldo walks the weekly buckets and sets Saldo for each week.
// The algorithm:
//  1. Compute saldoInicial = saldoActual + Σ abonos(ventana) − Σ totalVentasCrédito(ventana), clamp ≥ 0.
//  2. Walk forward: saldoFin = saldoPrevio + creditoEnSemana − abonosEnSemana, clamp ≥ 0.
//  3. The last week's Saldo ends up == saldoActual (clamp).
//
// Limitation: saldoActual is always today's live balance (not range-bounded). For the
// default unbounded window (no Desde/Hasta) this produces an exact saldo series because
// all history is included. When a bounded RangoFechasRitmo is supplied, saldoActual
// still reflects the full-history balance, so the reconstructed saldo curve is only
// approximate — activity outside the requested window is not visible here.
func reconstruirSaldo(semanas []SemanaRitmo, venPorSem []ventasPorSemana, saldoActual decimal.Decimal) {
	if len(semanas) == 0 {
		return
	}
	saldoActual = clampZero(saldoActual)

	// Sum totals over the whole window.
	totalAbonos := decimal.Zero
	totalCredito := decimal.Zero
	for i := range semanas {
		totalAbonos = totalAbonos.Add(semanas[i].MontoAbonado)
		totalCredito = totalCredito.Add(venPorSem[i].creditoTotal)
	}
	saldoInicial := clampZero(saldoActual.Add(totalAbonos).Sub(totalCredito))

	prev := saldoInicial
	for i := range semanas {
		prev = clampZero(prev.Add(venPorSem[i].creditoTotal).Sub(semanas[i].MontoAbonado))
		semanas[i].Saldo = prev
	}
	// Ensure last week == saldoActual (accounts for floating-point rounding or
	// out-of-window activity).
	semanas[len(semanas)-1].Saldo = saldoActual
}

// buildEventos creates the EventoRitmo slice from ventas plus liquidation events.
// Liquidation: whenever saldo transitions >0 → 0 between consecutive weeks,
// emit EventoLiquidacion at the start of that week with the last credit-sale
// folio/DoctoPvID seen before that week.
func buildEventos(semanas []SemanaRitmo, ventas []VentaCruda) []EventoRitmo {
	eventos := ventasToEventos(ventas)
	eventos = append(eventos, liquidacionEventos(semanas, ventas)...)

	// Sort by fecha ascending.
	sort.Slice(eventos, func(a, b int) bool {
		return eventos[a].Fecha.Before(eventos[b].Fecha)
	})
	return eventos
}

// ventasToEventos converts raw sale data into EventoRitmo entries.
func ventasToEventos(ventas []VentaCruda) []EventoRitmo {
	eventos := make([]EventoRitmo, 0, len(ventas))
	for _, v := range ventas {
		tipo := EventoVentaContado
		if v.EsCredito {
			tipo = EventoVentaCredito
		}
		eventos = append(eventos, EventoRitmo{
			Fecha:      v.Fecha.UTC(),
			Tipo:       tipo,
			Monto:      v.Total,
			DoctoPvID:  v.DoctoPvID,
			Folio:      v.Folio,
			PlazoMeses: v.PlazoMeses,
		})
	}
	return eventos
}

// liquidacionEventos scans weekly saldo transitions >0→0 and emits a
// EventoLiquidacion for each, carrying the last known credit-sale identifiers.
func liquidacionEventos(semanas []SemanaRitmo, ventas []VentaCruda) []EventoRitmo {
	if len(semanas) == 0 {
		return nil
	}

	// Seed: credit ventas that fell before the first semana.
	lastCreditDoctoPvID := 0
	lastCreditFolio := ""
	firstWeekStart := semanas[0].SemanaInicio
	for _, v := range ventas {
		if v.EsCredito && v.Fecha.UTC().Before(firstWeekStart) {
			lastCreditDoctoPvID = v.DoctoPvID
			lastCreditFolio = v.Folio
		}
	}

	var eventos []EventoRitmo
	for i, sem := range semanas {
		// Advance last-seen credit sale for this week.
		bucketEnd := sem.SemanaInicio.AddDate(0, 0, 7)
		for _, v := range ventas {
			if v.EsCredito {
				f := v.Fecha.UTC()
				if !f.Before(sem.SemanaInicio) && f.Before(bucketEnd) {
					lastCreditDoctoPvID = v.DoctoPvID
					lastCreditFolio = v.Folio
				}
			}
		}

		if i == 0 {
			continue
		}
		if semanas[i-1].Saldo.IsPositive() && !sem.Saldo.IsPositive() {
			eventos = append(eventos, EventoRitmo{
				Fecha:     sem.SemanaInicio,
				Tipo:      EventoLiquidacion,
				Monto:     decimal.Zero,
				DoctoPvID: lastCreditDoctoPvID,
				Folio:     lastCreditFolio,
			})
		}
	}
	return eventos
}

// buildResumen computes the aggregated ResumenRitmo from the weekly buckets.
// TotalAbonado counts only income movements (EsIngreso()==true: pago, enganche, otro).
// TotalPerdonado counts forgiveness/write-off movements (condonacion, perdida).
// MontoAbonado on each SemanaRitmo still sums ALL movements so saldo reconstruction
// remains unaffected — only the Resumen separates income from forgiveness.
func buildResumen(semanas []SemanaRitmo, saldoActual decimal.Decimal) ResumenRitmo {
	if len(semanas) == 0 {
		return ResumenRitmo{
			SaldoActual: clampZero(saldoActual),
		}
	}

	totalAbonado := decimal.Zero
	totalPerdonado := decimal.Zero
	semanasConPago := 0
	firstConPagoIdx := -1
	for i, s := range semanas {
		// Separate income from forgiveness by inspecting individual pagos.
		for _, p := range s.Pagos {
			if p.Categoria.EsIngreso() {
				totalAbonado = totalAbonado.Add(p.Importe)
			} else {
				totalPerdonado = totalPerdonado.Add(p.Importe)
			}
		}
		if s.MontoAbonado.IsPositive() {
			semanasConPago++
			if firstConPagoIdx == -1 {
				firstConPagoIdx = i
			}
		}
	}

	semanasActivas := 0
	if firstConPagoIdx != -1 {
		semanasActivas = len(semanas) - firstConPagoIdx
	}

	constancia := decimal.Zero
	if semanasActivas > 0 {
		constancia = decimal.NewFromInt(int64(semanasConPago)).
			Div(decimal.NewFromInt(int64(semanasActivas))).
			Mul(decimal.NewFromInt(100)).
			Round(2)
	}

	// Racha actual: count consecutive weeks with pago counting back from the end.
	racha := 0
	for i := len(semanas) - 1; i >= 0; i-- {
		if !semanas[i].MontoAbonado.IsPositive() {
			break
		}
		racha++
	}

	return ResumenRitmo{
		TotalAbonado:   totalAbonado,
		TotalPerdonado: totalPerdonado,
		SemanasConPago: semanasConPago,
		SemanasActivas: semanasActivas,
		RachaActualSem: racha,
		ConstanciaPct:  constancia,
		SaldoActual:    clampZero(saldoActual),
	}
}

// clampZero returns d if d >= 0, else decimal.Zero.
func clampZero(d decimal.Decimal) decimal.Decimal {
	if d.IsNegative() {
		return decimal.Zero
	}
	return d
}
