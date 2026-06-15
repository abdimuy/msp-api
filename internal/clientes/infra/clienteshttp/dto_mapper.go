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

// ─── Endpoint 1: directory item ──────────────────────────────────────────────

// toClienteListItemDTO maps a DirectorioClienteItem to its wire DTO.
func toClienteListItemDTO(item clientesapp.DirectorioClienteItem) ClienteListItemDTO {
	dto := ClienteListItemDTO{
		ClienteID:      item.Cliente.ClienteID(),
		Nombre:         item.Cliente.Nombre(),
		Zona:           item.Cliente.ZonaNombre(),
		Telefono:       item.Cliente.Telefono(),
		DireccionCorta: item.Cliente.Direccion().Corta(),
		TienePulso:     item.TienePulso,
		Saldo:          item.SaldoTotal.StringFixed(moneyScale),
	}
	if item.TienePulso {
		dto.Score = item.Pulso.Score
		dto.Segmento = item.Pulso.Segmento
		dto.EstadoPago = item.Pulso.EstadoPago
		dto.RecenciaDias = item.Pulso.RecenciaDias
	}
	return dto
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
