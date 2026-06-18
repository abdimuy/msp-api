// Package clienteshttp — dto_mapper.go contains entity→DTO mapping functions.
//
//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp

import (
	"time"

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
			Anio:     p.Anio,
			Mes:      p.Mes,
			Comprado: p.Comprado.StringFixed(moneyScale),
			Abonado:  p.Abonado.StringFixed(moneyScale),
		})
	}

	return SeriesDTO{
		AbonosPorMes:      abonos,
		CompradoVsAbonado: cvsa,
	}
}

// ─── Endpoint 3: venta list item ─────────────────────────────────────────────

// toVentaListItemDTO maps a VentaCliente to its list-row wire DTO.
func toVentaListItemDTO(v *domain.VentaCliente) VentaListItemDTO {
	return VentaListItemDTO{
		DoctoPVID:  v.DoctoPVID(),
		Fecha:      formatTime(v.Fecha()),
		Folio:      v.Folio(),
		Tipo:       v.Tipo().String(),
		Total:      v.Total().StringFixed(moneyScale),
		SaldoVenta: v.SaldoVenta().StringFixed(moneyScale),
		NumPagos:   v.NumPagos(),
	}
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
			DoctoCCID:  pago.DoctoCCID(),
			Fecha:      formatTime(pago.Fecha()),
			Importe:    pago.Importe().StringFixed(moneyScale),
			FormaCobro: pago.FormaCobro(),
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
