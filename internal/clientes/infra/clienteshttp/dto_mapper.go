// Package clienteshttp — dto_mapper.go contains entity→DTO mapping functions.
//
//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ─── Endpoint 1: directory item (from Meilisearch DirectorioDoc) ─────────────

// dirDocToClienteListItemDTO maps a flat DirectorioDoc returned by Meilisearch
// to its wire DTO. Saldo is rendered as a 2-decimal string.
// Cobranza signals (tier_riesgo, pct_pagos_a_tiempo, fecha_prox_pago) are
// populated only when TienePulso is true.
func dirDocToClienteListItemDTO(doc outbound.DirectorioDoc) ClienteListItemDTO {
	dto := ClienteListItemDTO{
		ClienteID:      doc.ClienteID,
		Nombre:         doc.Nombre,
		Zona:           doc.ZonaNombre,
		Telefono:       doc.Telefono,
		DireccionCorta: doc.DireccionCorta,
		TienePulso:     doc.TienePulso,
		Saldo:          doc.Saldo.StringFixed(moneyScale),
	}
	if doc.TienePulso {
		dto.Score = doc.Score
		dto.Segmento = doc.Segmento
		dto.EstadoPago = doc.EstadoPago
		dto.RecenciaDias = doc.RecenciaDias
		dto.TierRiesgo = doc.TierRiesgo
		dto.PctPagosATiempo = doc.PctPagosATiempo.StringFixed(pctScale)
		dto.FechaProxPago = formatTime(doc.FechaProxPago)
		dto.BandaCredito = doc.BandaCredito
		dto.ScoreCredito = doc.ScoreCredito
		dto.BandaRecompra = doc.BandaRecompra
		dto.ScoreRecompra = doc.ScoreRecompra
		dto.BandaCLV = doc.BandaCLV
		if doc.BandaCLV != "" {
			dto.CLV = doc.CLVStr
		}
	}
	return dto
}

// ─── Decimal scale constants ──────────────────────────────────────────────────

const (
	// moneyScale is the number of decimal places for monetary fields.
	moneyScale int32 = 2
	// cantidadScale is the number of decimal places for quantity fields.
	cantidadScale int32 = 5
	// pctScale is the number of decimal places for percentage fields.
	pctScale int32 = 2
)

// ─── Time helpers ─────────────────────────────────────────────────────────────

// formatTime renders a timestamp as RFC3339Nano in UTC. Zero values map to the
// empty string so optional date fields remain absent in the JSON response.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// ─── CLV helpers ──────────────────────────────────────────────────────────────

// clvString returns the CLV as a 2-decimal peso string when BandaCLV is set,
// or "" when no aplica (BandaCLV == ""). Avoids emitting "0.00" for clients
// with no CLV signal so the frontend can cleanly hide the field.
func clvString(p analytics.ClientePulsoContract) string {
	if p.BandaCLV == "" {
		return ""
	}
	return p.MontoCLV.StringFixed(moneyScale)
}

// ─── Endpoint 2: ficha ───────────────────────────────────────────────────────

// toFichaDTO maps a FichaCliente to its full wire representation.
func toFichaDTO(ficha clientesapp.FichaCliente) FichaDTO {
	c := ficha.Cliente
	r := ficha.Resumen

	dto := FichaDTO{
		ClienteID: c.ClienteID(),
		Nombre:    c.Nombre(),
		Direccion: DireccionDTO{
			Calle:     c.Direccion().Calle(),
			Colonia:   c.Direccion().Colonia(),
			Poblacion: c.Direccion().Poblacion(),
			Estado:    c.Direccion().Estado(),
		},
		Ubicacion: UbicacionDTO{
			Lat:        c.Ubicacion().Lat,
			Lng:        c.Ubicacion().Lng,
			Disponible: c.Ubicacion().Disponible,
		},
		Telefono:      c.Telefono(),
		LimiteCredito: c.LimiteCredito().StringFixed(moneyScale),
		Notas:         c.Notas(),
		Zona:          c.ZonaNombre(),
		Cobrador:      c.CobradorNombre(),
		Estatus:       c.Estatus(),
		Resumen: ResumenDTO{
			TotalComprado:  r.TotalComprado.StringFixed(moneyScale),
			TotalAbonado:   r.TotalAbonado.StringFixed(moneyScale),
			Saldo:          r.SaldoTotal.StringFixed(moneyScale),
			PctLiquidado:   r.PctLiquidado.StringFixed(pctScale),
			TicketPromedio: r.TicketPromedio.StringFixed(moneyScale),
			NumVentas:      r.NumVentas,
			NumPagos:       r.NumPagos,
		},
		Series: toSeriesDTO(r),
	}

	if ficha.TienePulso {
		p := ficha.Pulso
		dto.Pulso = &PulsoDTO{
			Score:             p.Score,
			Segmento:          p.Segmento,
			EstadoPago:        p.EstadoPago,
			RecenciaDias:      p.RecenciaDias,
			Frecuencia:        p.Frecuencia,
			Monetary:          p.Monetary.StringFixed(moneyScale),
			Saldo:             p.Saldo.StringFixed(moneyScale),
			PorLiquidarPct:    p.PorLiquidarPct.StringFixed(pctScale),
			FechaUltimaCompra: formatTime(p.FechaUltimaCompra),
			FechaUltimoPago:   formatTime(p.FechaUltimoPago),
			NextBestProduct:   p.NextBestProduct,
			NumPagos:          p.NumPagos,
			CadenciaDias:      p.CadenciaDias,
			DiasAtrasoProm:    p.DiasAtrasoProm,
			PctPagosATiempo:   p.PctPagosATiempo.StringFixed(pctScale),
			FechaProxPago:     formatTime(p.FechaProxPago),
			MontoProxPago:     p.MontoProxPago.StringFixed(moneyScale),
			TierRiesgo:        p.TierRiesgo,
			BandaCredito:      p.BandaCredito,
			ScoreCredito:      p.ScoreCredito,
			CreditoDrivers:    p.CreditoDrivers,
			BandaRecompra:     p.BandaRecompra,
			ScoreRecompra:     p.ScoreRecompra,
			RecompraDrivers:   p.RecompraDrivers,
			BandaCLV:          p.BandaCLV,
			CLV:               clvString(p),
			CLVDrivers:        p.CLVDrivers,
			CreditoResumen:    p.CreditoResumen,
			RecompraResumen:   p.RecompraResumen,
			CLVResumen:        p.CLVResumen,
			Narrativa:         p.Narrativa,
			RasgosIA:          p.RasgosIA,
			ContextoOperativo: p.ContextoOperativo,
		}
	}

	return dto
}

// toSeriesDTO maps the ResumenFicha time-series slices to their wire DTOs.
func toSeriesDTO(r outbound.ResumenFicha) SeriesDTO {
	abonos := make([]PuntoMensualDTO, 0, len(r.AbonosPorMes))
	for _, p := range r.AbonosPorMes {
		abonos = append(abonos, PuntoMensualDTO{
			Anio:  p.Anio,
			Mes:   p.Mes,
			Monto: p.Monto.StringFixed(moneyScale),
		})
	}

	cvsa := make([]PuntoCompradoAbonadoDTO, 0, len(r.CompradoVsAbonado))
	for _, p := range r.CompradoVsAbonado {
		cvsa = append(cvsa, PuntoCompradoAbonadoDTO{
			Anio:        p.Anio,
			Mes:         p.Mes,
			Comprado:    p.Comprado.StringFixed(moneyScale),
			Cobranza:    p.Cobranza.StringFixed(moneyScale),
			Enganche:    p.Enganche.StringFixed(moneyScale),
			Condonacion: p.Condonacion.StringFixed(moneyScale),
			Perdida:     p.Perdida.StringFixed(moneyScale),
			Otro:        p.Otro.StringFixed(moneyScale),
		})
	}

	return SeriesDTO{
		AbonosPorMes:      abonos,
		CompradoVsAbonado: cvsa,
	}
}

// ─── Endpoint 5: ritmo-pago ───────────────────────────────────────────────────

// ritmoPagosToDTOs converts a slice of domain.PagoRitmo to []PagoRitmoDTO.
// A nil or empty input always returns a non-nil empty slice ([] in JSON, never null).
func ritmoPagosToDTOs(pagos []domain.PagoRitmo) []PagoRitmoDTO {
	result := make([]PagoRitmoDTO, 0, len(pagos))
	for _, p := range pagos {
		result = append(result, PagoRitmoDTO{
			DoctoCCID:    p.DoctoCCID,
			Fecha:        formatTime(p.Fecha),
			Hora:         p.Hora,
			Importe:      p.Importe.StringFixed(moneyScale),
			ConceptoCCID: p.ConceptoCCID,
			Concepto:     p.Concepto,
			Categoria:    string(p.Categoria),
			EsIngreso:    p.Categoria.EsIngreso(),
			DoctoPVID:    p.DoctoPVID,
			Folio:        p.Folio,
			Articulo:     p.Articulo,
		})
	}
	return result
}

// diasSemanaES maps time.Weekday to its Spanish lowercase name.
func diasSemanaES(d time.Weekday) string {
	switch d {
	case time.Monday:
		return "lunes"
	case time.Tuesday:
		return "martes"
	case time.Wednesday:
		return "miércoles"
	case time.Thursday:
		return "jueves"
	case time.Friday:
		return "viernes"
	case time.Saturday:
		return "sábado"
	case time.Sunday:
		return "domingo"
	}
	return "lunes"
}

// ritmoPagoToDTO maps a domain.RitmoPago to its wire DTO.
func ritmoPagoToDTO(r domain.RitmoPago) RitmoPagoDTO {
	semanas := make([]SemanaRitmoDTO, 0, len(r.Semanas))
	for _, s := range r.Semanas {
		pagos := ritmoPagosToDTOs(s.Pagos)
		semanas = append(semanas, SemanaRitmoDTO{
			SemanaInicio: formatTime(s.SemanaInicio),
			MontoAbonado: s.MontoAbonado.StringFixed(moneyScale),
			Saldo:        s.Saldo.StringFixed(moneyScale),
			NumPagos:     s.NumPagos,
			Pagos:        pagos,
		})
	}

	eventos := make([]EventoRitmoDTO, 0, len(r.Eventos))
	for _, e := range r.Eventos {
		eventos = append(eventos, EventoRitmoDTO{
			Fecha:      formatTime(e.Fecha),
			Tipo:       string(e.Tipo),
			Monto:      e.Monto.StringFixed(moneyScale),
			DoctoPvID:  e.DoctoPvID,
			Folio:      e.Folio,
			PlazoMeses: e.PlazoMeses,
		})
	}

	return RitmoPagoDTO{
		AnclaDiaRuta: diasSemanaES(r.AnclaDiaRuta),
		Semanas:      semanas,
		Eventos:      eventos,
		Resumen: ResumenRitmoDTO{
			TotalAbonado:   r.Resumen.TotalAbonado.StringFixed(moneyScale),
			TotalPerdonado: r.Resumen.TotalPerdonado.StringFixed(moneyScale),
			SemanasConPago: r.Resumen.SemanasConPago,
			SemanasActivas: r.Resumen.SemanasActivas,
			RachaActualSem: r.Resumen.RachaActualSem,
			ConstanciaPct:  r.Resumen.ConstanciaPct.StringFixed(pctScale),
			SaldoActual:    r.Resumen.SaldoActual.StringFixed(moneyScale),
		},
	}
}

// ─── Endpoint N: pago detalle ────────────────────────────────────────────────

// toPagoDetalleDTO maps an outbound.PagoDetalle to its wire DTO.
func toPagoDetalleDTO(d outbound.PagoDetalle) PagoDetalleDTO {
	dto := PagoDetalleDTO{
		Importe:        d.Importe.StringFixed(moneyScale),
		IVA:            d.IVA.StringFixed(moneyScale),
		Fecha:          formatTime(d.Fecha),
		FormaCobroID:   d.FormaCobroID,
		FormaCobro:     d.FormaCobro,
		Referencia:     d.Referencia,
		CobradorID:     d.CobradorID,
		Cobrador:       d.Cobrador,
		ConceptoCCID:   d.ConceptoCCID,
		Concepto:       d.Concepto,
		Categoria:      d.Categoria,
		EsIngreso:      domain.Categoria(d.Categoria).EsIngreso(),
		Folio:          d.Folio,
		AplicaACargoID: d.AplicaACargoID,
		DoctoPVID:      d.DoctoPVID,
		Cancelado:      d.Cancelado,
		Aplicado:       d.Aplicado,
		Origen:         d.Origen,
	}
	if d.Lat != nil {
		s := d.Lat.String()
		dto.Lat = &s
	}
	if d.Lon != nil {
		s := d.Lon.String()
		dto.Lon = &s
	}
	if d.SaldoCargo != nil {
		s := d.SaldoCargo.StringFixed(moneyScale)
		dto.SaldoCargo = &s
	}
	if !d.RecibidoAt.IsZero() {
		dto.RecibidoAt = formatTime(d.RecibidoAt)
	}
	if !d.AplicadoAt.IsZero() {
		dto.AplicadoAt = formatTime(d.AplicadoAt)
	}
	return dto
}

// ─── Endpoint 3: venta list item ─────────────────────────────────────────────

// toVentaListItemDTO maps a VentaCliente to its list-row wire DTO.
func toVentaListItemDTO(v *domain.VentaCliente) VentaListItemDTO {
	return VentaListItemDTO{
		DoctoPVID:      v.DoctoPVID(),
		Fecha:          formatTime(v.Fecha()),
		Folio:          v.Folio(),
		Tipo:           v.Tipo().String(),
		Total:          v.Total().StringFixed(moneyScale),
		SaldoVenta:     v.SaldoVenta().StringFixed(moneyScale),
		NumPagos:       v.NumPagos(),
		Hora:           v.Hora(),
		Almacen:        v.Almacen(),
		PrimerArticulo: v.PrimerArticulo(),
		NumArticulos:   v.NumArticulos(),
	}
}

// ─── Endpoint: predicciones ───────────────────────────────────────────────────

// prediccionesToDTO maps a PrediccionesContract to its wire representation.
// CLV intervals are formatted as 2-decimal peso strings; all other intervals
// pass float64 values directly.
func prediccionesToDTO(c analytics.PrediccionesContract) PrediccionesDTO {
	return PrediccionesDTO{
		Disponible: c.Disponible,
		PAlive: IntervaloDTO{
			Punto: c.PAlive.Punto,
			Lo:    c.PAlive.Lo,
			Hi:    c.PAlive.Hi,
		},
		ComprasEsperadas12m: IntervaloDTO{
			Punto: c.ComprasEsperadas12m.Punto,
			Lo:    c.ComprasEsperadas12m.Lo,
			Hi:    c.ComprasEsperadas12m.Hi,
		},
		CLV: IntervaloMoneyDTO{
			Punto: decimal.NewFromFloat(c.CLV.Punto).StringFixed(moneyScale),
			Lo:    decimal.NewFromFloat(c.CLV.Lo).StringFixed(moneyScale),
			Hi:    decimal.NewFromFloat(c.CLV.Hi).StringFixed(moneyScale),
		},
		ProximaCompraDias: IntervaloDTO{
			Punto: c.ProximaCompraDias.Punto,
			Lo:    c.ProximaCompraDias.Lo,
			Hi:    c.ProximaCompraDias.Hi,
		},
		Draws: c.Draws,
	}
}

// ─── Endpoint: benchmark ─────────────────────────────────────────────────────

// benchmarkToDTO maps a BenchmarkContract to its wire representation.
func benchmarkToDTO(c analytics.BenchmarkContract) BenchmarkDTO {
	return BenchmarkDTO{
		Disponible:  c.Disponible,
		CohortBy:    c.CohortBy,
		Zona:        c.Zona,
		N:           c.N,
		Puntualidad: metricaToDTO(c.Puntualidad),
		CLV:         metricaMoneyToDTO(c.CLV),
		Credito:     metricaToDTO(c.Credito),
		Recompra:    metricaToDTO(c.Recompra),
	}
}

// metricaToDTO maps a MetricaBenchmark to its float64-valued wire DTO.
func metricaToDTO(m analytics.MetricaBenchmark) MetricaDTO {
	return MetricaDTO{
		Aplica:         m.Aplica,
		Valor:          m.Valor,
		Percentil:      m.Percentil,
		Mediana:        m.Mediana,
		P25:            m.P25,
		P75:            m.P75,
		N:              m.N,
		MuestraPequena: m.MuestraPequena,
	}
}

// metricaMoneyToDTO maps a MetricaBenchmark to its string-valued monetary wire DTO.
// Monetary values (valor, mediana, p25, p75) are formatted as 2-decimal peso strings.
func metricaMoneyToDTO(m analytics.MetricaBenchmark) MetricaMoneyDTO {
	return MetricaMoneyDTO{
		Aplica:         m.Aplica,
		Valor:          decimal.NewFromFloat(m.Valor).StringFixed(moneyScale),
		Percentil:      m.Percentil,
		Mediana:        decimal.NewFromFloat(m.Mediana).StringFixed(moneyScale),
		P25:            decimal.NewFromFloat(m.P25).StringFixed(moneyScale),
		P75:            decimal.NewFromFloat(m.P75).StringFixed(moneyScale),
		N:              m.N,
		MuestraPequena: m.MuestraPequena,
	}
}

// ─── Endpoint: timeline ───────────────────────────────────────────────────────

// timelineToDTO maps a slice of domain.EventoTimeline to its wire DTO.
// A nil or empty input always returns a TimelineDTO with a non-nil Eventos slice
// so the JSON response renders [] rather than null.
func timelineToDTO(eventos []domain.EventoTimeline) TimelineDTO {
	dtos := make([]EventoTimelineDTO, 0, len(eventos))
	for _, e := range eventos {
		dtos = append(dtos, EventoTimelineDTO{
			Fecha:    formatTime(e.Fecha),
			Tipo:     e.Tipo,
			Monto:    e.Monto.StringFixed(moneyScale),
			Etiqueta: e.Etiqueta,
			RefID:    e.RefID,
		})
	}
	return TimelineDTO{Eventos: dtos}
}

// ─── Endpoint 4: venta detalle ───────────────────────────────────────────────

// toVentaDetalleDTO maps an outbound.VentaDetalle to its full detail wire DTO.
func toVentaDetalleDTO(d outbound.VentaDetalle) VentaDetalleDTO {
	v := d.Venta

	productos := make([]ProductoVentaDTO, 0, len(d.Productos))
	for _, p := range d.Productos {
		productos = append(productos, ProductoVentaDTO{
			ArticuloID:      p.ArticuloID(),
			Nombre:          p.Nombre(),
			Unidades:        p.Unidades().StringFixed(cantidadScale),
			PrecioUnitario:  p.PrecioUnitario().StringFixed(moneyScale),
			PrecioTotalNeto: p.PrecioTotalNeto().StringFixed(moneyScale),
			PctjeDscto:      p.PctjeDscto().StringFixed(pctScale),
		})
	}

	pagos := make([]PagoDTO, 0, len(d.Pagos))
	for _, pago := range d.Pagos {
		pagos = append(pagos, PagoDTO{
			DoctoCCID:    pago.DoctoCCID(),
			Fecha:        formatTime(pago.Fecha()),
			Importe:      pago.Importe().StringFixed(moneyScale),
			FormaCobro:   pago.FormaCobro(),
			ConceptoCCID: pago.ConceptoCCID(),
			Concepto:     pago.Concepto(),
			Categoria:    string(pago.Categoria()),
			Cobrador:     pago.Cobrador(),
			EsIngreso:    pago.Categoria().EsIngreso(),
		})
	}

	dto := VentaDetalleDTO{
		Venta: VentaHeaderDTO{
			DoctoPVID:  v.DoctoPVID(),
			ClienteID:  v.ClienteID(),
			Fecha:      formatTime(v.Fecha()),
			Folio:      v.Folio(),
			Tipo:       v.Tipo().String(),
			Total:      v.Total().StringFixed(moneyScale),
			SaldoVenta: v.SaldoVenta().StringFixed(moneyScale),
			NumPagos:   v.NumPagos(),
		},
		Productos: productos,
		Pagos:     pagos,
	}

	if d.Contrato != nil {
		vendedores := d.Contrato.Vendedores
		if vendedores == nil {
			vendedores = []string{}
		}
		dto.Contrato = &ContratoDTO{
			Parcialidad:     d.Contrato.Parcialidad.StringFixed(moneyScale),
			Enganche:        d.Contrato.Enganche.StringFixed(moneyScale),
			PrecioDeContado: d.Contrato.PrecioDeContado.StringFixed(moneyScale),
			PlazoMeses:      d.Contrato.PlazoMeses,
			FormaDePago:     d.Contrato.FormaDePago,
			Vendedores:      vendedores,
		}
	}

	return dto
}
