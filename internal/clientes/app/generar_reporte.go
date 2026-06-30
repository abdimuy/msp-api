//nolint:misspell // Spanish domain vocabulary per project convention.
package app

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// GenerarReporteCliente assembles the full read-model for a client PDF report.
// It fetches the client identity, financial summary, and per-sale payment
// details from the repository.
//
// ventaIDs, when non-empty, restricts the ventas included in the report to
// those whose DoctoPVID is in the set, preserving the repository's date-desc
// order. When ventaIDs is nil or empty, all ventas are included.
//
// N+1 note: one ObtenerVentaDetalle call per venta — acceptable for a single
// client report (clients average 1-3 sales/year). Batch optimization deferred
// as a future improvement if volume grows significantly.
func (s *Service) GenerarReporteCliente(ctx context.Context, clienteID int, ventaIDs []int) (outbound.ReporteCliente, error) {
	const source = "clientes.GenerarReporteCliente"

	cliente, err := s.repo.ObtenerCliente(ctx, clienteID)
	if err != nil {
		return outbound.ReporteCliente{}, wrapRepoErr(err, source, "reporte_cliente_fetch_failed",
			"error al obtener el cliente")
	}

	resumen, err := s.repo.ObtenerResumenFicha(ctx, clienteID, outbound.RangoFechas{})
	if err != nil {
		return outbound.ReporteCliente{}, wrapRepoErr(err, source, "reporte_resumen_fetch_failed",
			"error al obtener el resumen financiero del cliente")
	}

	allVentas, err := s.fetchAllVentas(ctx, clienteID, source)
	if err != nil {
		return outbound.ReporteCliente{}, err
	}

	filtered := filterVentas(allVentas, ventaIDs)

	reporteVentas, err := s.buildReporteVentas(ctx, filtered, source)
	if err != nil {
		return outbound.ReporteCliente{}, err
	}

	total := len(allVentas)
	liquidadas := 0
	for _, v := range allVentas {
		if v.SaldoVenta().IsZero() {
			liquidadas++
		}
	}

	return outbound.ReporteCliente{
		Cliente: outbound.ReporteClienteDatos{
			ID:        cliente.ClienteID(),
			Nombre:    cliente.Nombre(),
			Direccion: formatDireccion(cliente.Direccion()),
			Telefono:  cliente.Telefono(),
			Zona:      cliente.ZonaNombre(),
			Cobrador:  cliente.CobradorNombre(),
			Notas:     cliente.Notas(),
		},
		Resumen:          resumen,
		Ventas:           reporteVentas,
		TotalVentas:      total,
		VentasLiquidadas: liquidadas,
		VentasActivas:    total - liquidadas,
	}, nil
}

// fetchAllVentas collects all ventas for a client by iterating keyset-paginated
// pages until NextCursor is empty.
func (s *Service) fetchAllVentas(ctx context.Context, clienteID int, source string) ([]*domain.VentaCliente, error) {
	const maxPageSize = 200
	var all []*domain.VentaCliente
	cursor := ""
	for {
		page, err := s.repo.ListarVentas(ctx, clienteID, outbound.ListParams{
			Cursor:   cursor,
			PageSize: maxPageSize,
		})
		if err != nil {
			return nil, wrapRepoErr(err, source, "reporte_ventas_fetch_failed",
				"error al obtener las ventas del cliente")
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return all, nil
}

// filterVentas returns the subset of ventas whose DoctoPVID is in ventaIDs.
// When ventaIDs is empty, all ventas are returned unchanged.
func filterVentas(ventas []*domain.VentaCliente, ventaIDs []int) []*domain.VentaCliente {
	if len(ventaIDs) == 0 {
		return ventas
	}
	set := make(map[int]struct{}, len(ventaIDs))
	for _, id := range ventaIDs {
		set[id] = struct{}{}
	}
	out := make([]*domain.VentaCliente, 0, len(ventaIDs))
	for _, v := range ventas {
		if _, ok := set[v.DoctoPVID()]; ok {
			out = append(out, v)
		}
	}
	return out
}

// buildReporteVentas fetches payment details for each venta and maps them to
// the report read-model slice. One ObtenerVentaDetalle call per venta (N+1;
// acceptable for a single-client report).
func (s *Service) buildReporteVentas(ctx context.Context, ventas []*domain.VentaCliente, source string) ([]outbound.ReporteVenta, error) {
	out := make([]outbound.ReporteVenta, 0, len(ventas))
	for _, v := range ventas {
		rv, err := s.buildReporteVenta(ctx, v, source)
		if err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, nil
}

// buildReporteVenta fetches payment detail for a single venta and maps it to
// the ReporteVenta read-model.
func (s *Service) buildReporteVenta(ctx context.Context, v *domain.VentaCliente, source string) (outbound.ReporteVenta, error) {
	detalle, err := s.repo.ObtenerVentaDetalle(ctx, v.DoctoPVID())
	if err != nil {
		return outbound.ReporteVenta{}, wrapRepoErr(err, source, "reporte_venta_detalle_failed",
			"error al obtener el detalle de una venta")
	}

	pagos := make([]outbound.ReportePago, 0, len(detalle.Pagos))
	for _, p := range detalle.Pagos {
		pagos = append(pagos, outbound.ReportePago{
			Fecha:     p.Fecha(),
			Concepto:  p.Concepto(),
			Cobrador:  p.Cobrador(),
			Importe:   p.Importe(),
			EsIngreso: p.Categoria().EsIngreso(),
			Categoria: string(p.Categoria()),
		})
	}

	productos := make([]outbound.ReporteProducto, 0, len(detalle.Productos))
	for _, p := range detalle.Productos {
		productos = append(productos, outbound.ReporteProducto{
			Nombre:         p.Nombre(),
			Cantidad:       p.Unidades(),
			PrecioUnitario: p.PrecioUnitario(),
			Importe:        p.PrecioTotalNeto(),
			PctDescuento:   p.PctjeDscto(),
		})
	}

	var credito *outbound.ReporteCredito
	if c := detalle.Contrato; c != nil {
		credito = &outbound.ReporteCredito{
			Parcialidad:     c.Parcialidad,
			FormaPago:       c.FormaDePago,
			PlazoMeses:      c.PlazoMeses,
			Enganche:        c.Enganche,
			PrecioContado:   c.PrecioDeContado,
			MontoCortoPlazo: c.MontoCortoPlazo,
			Vendedores:      c.Vendedores,
		}
	}

	return outbound.ReporteVenta{
		DoctoPvID: v.DoctoPVID(),
		Folio:     v.Folio(),
		Fecha:     v.Fecha(),
		Almacen:   v.Almacen(),
		Total:     v.Total(),
		Saldo:     v.SaldoVenta(),
		Liquidada: v.SaldoVenta().IsZero(),
		Productos: productos,
		Credito:   credito,
		Pagos:     pagos,
	}, nil
}

// wrapRepoErr converts a repository error into a typed apperror.Error.
// If the error is already an *apperror.Error, it is returned with source attached.
// Otherwise a new internal error wrapping the cause is returned.
func wrapRepoErr(err error, source, code, msg string) error {
	if appErr, ok := apperror.As(err); ok {
		return appErr.WithSource(source)
	}
	return apperror.NewInternal(code, msg).WithSource(source).WithError(err)
}

// formatDireccion formats a domain.Direccion as a single-line address
// "calle, colonia, ciudad" omitting empty or whitespace-only components.
func formatDireccion(d domain.Direccion) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{d.Calle(), d.Colonia(), d.Poblacion()} {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ", ")
}
