//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"time"

	"github.com/shopspring/decimal"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

const escalaDinero = 2

func dinero(d decimal.Decimal) string { return d.StringFixed(escalaDinero) }

func hora(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func horaNula(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := hora(*t)
	return &s
}

func textoNulo(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func enteroNulo(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func conteo(c domain.ConteoImporte) ConteoImporteDTO {
	return ConteoImporteDTO{N: c.N, Importe: dinero(c.Importe)}
}

func aActividadDTO(a domain.Actividad) ActividadDTO {
	dto := ActividadDTO{
		ID: a.ID, Tipo: string(a.Tipo), Hora: hora(a.Hora), RecibidoAt: horaNula(a.RecibidoAt),
		ClienteID: a.ClienteID, ClienteNombre: a.ClienteNombre,
		VentaID: enteroNulo(a.VentaID), Folio: textoNulo(a.Folio),
		RegistradoPor: RegistradoPorDTO{
			UsuarioUID: textoNulo(a.RegistradoPor.UsuarioUID), CobradorID: enteroNulo(a.RegistradoPor.CobradorID),
			Nombre: a.RegistradoPor.Nombre, Origen: string(a.RegistradoPor.Origen),
		},
		ConGPS: a.ConGPS(), NumFotos: a.NumFotos,
		DoctoPVID: enteroNulo(a.DoctoPVID), CapturaTardia: a.CapturaTardia(),
	}
	if dto.ConGPS {
		dto.Lat, dto.Lng = a.Lat, a.Lng
	}
	if a.Pago != nil {
		dto.Pago = &PagoDTO{
			Importe: dinero(a.Pago.Importe), FormaCobro: domain.EtiquetaFormaCobro(a.Pago.FormaCobroID),
			FormaCobroID: enteroNulo(a.Pago.FormaCobroID), Aplicado: a.Pago.Aplicado, Rechazado: a.Pago.Rechazado,
			DoctoCCID: enteroNulo(a.Pago.DoctoCCID), PagoRecibidoID: textoNulo(a.Pago.PagoRecibidoID),
		}
	}
	if a.Visita != nil {
		// El estado, trato y etiqueta que esa visita sola le daría a su venta,
		// con las mismas reglas del teléfono (EstadoCuentaUi.kt).
		r := domain.ResultadoCuenta{Estado: domain.EstadoDeTipoVisita(a.Visita.TipoVisita, false)}
		dto.Visita = &VisitaDTO{
			VisitaID: a.Visita.VisitaID, TipoVisita: a.Visita.TipoVisita, Nota: textoNulo(a.Visita.Nota),
			Estado: string(r.Estado), Trato: string(domain.TratoDe(r)), Etiqueta: domain.EtiquetaDe(r),
		}
	}
	return dto
}

func aActividadesDTO(acts []domain.Actividad) []ActividadDTO {
	out := make([]ActividadDTO, 0, len(acts))
	for _, a := range acts {
		out = append(out, aActividadDTO(a))
	}
	return out
}

func aLineaDTO(l domain.LineaDelDia) LineaDelDiaDTO {
	dto := LineaDelDiaDTO{
		Dia: l.Dia.String(), Puntos: make([]PuntoDTO, 0, len(l.Puntos)), Tramos: make([]TramoDTO, 0, len(l.Tramos)),
		PorHora: make([]HoraDTO, 0, len(l.PorHora)), Pausas: make([]PausaDTO, 0, len(l.Pausas)),
	}
	for _, p := range l.Puntos {
		pd := PuntoDTO{
			ID: p.ID, Tipo: string(p.Tipo), Hora: hora(p.Hora), ClienteNombre: p.ClienteNombre,
			Resultado: textoNulo(p.Resultado), NumFotos: p.NumFotos,
		}
		if p.Importe != nil {
			s := dinero(*p.Importe)
			pd.Importe = &s
		}
		dto.Puntos = append(dto.Puntos, pd)
	}
	for _, t := range l.Tramos {
		dto.Tramos = append(dto.Tramos, TramoDTO{
			Desde: hora(t.Desde), Hasta: hora(t.Hasta), Pagos: conteo(t.Pagos),
			Visitas: t.Visitas, Condonaciones: t.Condonaciones, Clientes: t.Clientes,
		})
	}
	for _, h := range l.PorHora {
		dto.PorHora = append(dto.PorHora, HoraDTO{
			Hora: h.Hora, Pagos: h.Pagos, Visitas: h.Visitas, Condonaciones: h.Condonaciones, Importe: dinero(h.Importe),
		})
	}
	for _, p := range l.Pausas {
		dto.Pausas = append(dto.Pausas, PausaDTO{
			Desde: hora(p.Desde), Hasta: hora(p.Hasta), Minutos: p.Minutos,
			AnteriorID: textoNulo(p.AnteriorID), SiguienteID: textoNulo(p.SiguienteID), EnCurso: p.EnCurso,
		})
	}
	return dto
}

func aPausasDTO(p domain.ResumenPausas) PausasResumenDTO {
	return PausasResumenDTO{N: p.N, Minutos: p.Minutos, EnCurso: p.EnCurso}
}

func aTableroDTO(t desempenoapp.Tablero) TableroDTO {
	dto := TableroDTO{
		GeneradoAt: hora(t.GeneradoAt), Periodo: string(t.Periodo), PausaMin: t.PausaMin,
		Usuarios: make([]UsuarioTableroDTO, 0, len(t.Filas)),
	}
	for _, f := range t.Filas {
		dto.Usuarios = append(dto.Usuarios, aFilaDTO(f))
	}
	return dto
}

func aFilaDTO(f desempenoapp.FilaTablero) UsuarioTableroDTO {
	u := f.Usuario
	dto := UsuarioTableroDTO{
		UID: u.UID, Nombre: u.Nombre, Email: u.Email,
		ZonaID: enteroNulo(u.ZonaID), ZonaNombre: textoNulo(u.ZonaNombre),
		Configurado: f.Configurado, MotivoNoConfigurado: textoNulo(f.MotivoNoConfigurado),
		InicioSemana: horaNula(u.InicioSemana), VentasUniverso: f.VentasUniverso,
		EstadoAhora: string(f.EstadoAhora), EstadoDesde: horaNula(f.EstadoDesde),
		PrimeraActividad: horaNula(f.PrimeraActividad), UltimaActividad: horaNula(f.UltimaActividadHora),
		MinutosActivos: f.MinutosActivos, Pausas: aPausasDTO(f.Pausas),
		Pagos: conteo(f.Pagos),
		PagosPorForma: PagosPorFormaDTO{
			Efectivo: conteo(f.PagosPorForma.Efectivo), Transferencia: conteo(f.PagosPorForma.Transferencia),
			Cheque: conteo(f.PagosPorForma.Cheque),
		},
		Condonaciones: conteo(f.Condonaciones), NoAplicados: conteo(f.NoAplicados), Visitas: f.Visitas,
		Cobertura: CoberturaDTO{ConAlgo: f.CoberturaConAlgo, Total: f.CoberturaTotal},
		CoberturaPorSegmento: CoberturaPorSegmentoDTO{
			SinVisitar: f.PorSegmento.SinVisitar, Volver: f.PorSegmento.Volver, Despues: f.PorSegmento.Despues,
			Pagados: f.PorSegmento.Pagados, SinNada: f.PorSegmento.SinNada,
		},
		Cobro: CobroDTO{
			Cobrado: dinero(f.Cobro.Cobrado), Esperado: dinero(f.Cobro.Esperado), Falta: dinero(f.Cobro.Falta),
		},
		Lineas: make([]LineaDelDiaDTO, 0, len(f.Lineas)),
	}
	if f.Cobro.Pct != nil {
		s := dinero(*f.Cobro.Pct)
		dto.Cobro.Pct = &s
	}
	if f.Ultima != nil {
		dto.Ultima = &UltimaDTO{
			ID: f.Ultima.ID, Tipo: string(f.Ultima.Tipo), Hora: hora(f.Ultima.Hora), ClienteNombre: f.Ultima.ClienteNombre,
		}
	}
	for _, l := range f.Lineas {
		dto.Lineas = append(dto.Lineas, aLineaDTO(l))
	}
	return dto
}

func aJornadaDTO(v desempenoapp.VistaJornada) JornadaDTO {
	j := v.Jornada
	dto := JornadaDTO{
		UID: v.UID, Fecha: j.Dia.String(), PausaMin: v.PausaMin, GeneradoAt: hora(v.GeneradoAt),
		Actividades: aActividadesDTO(j.Actividades), Linea: aLineaDTO(j.Linea),
		Senales: make([]SenalDTO, 0, len(j.Senales)),
		Resumen: ResumenJornadaDTO{
			PrimeraActividad: horaNula(j.Resumen.PrimeraActividad), UltimaActividad: horaNula(j.Resumen.UltimaActividad),
			MinutosActivos: j.Resumen.MinutosActivos, Pausas: aPausasDTO(j.Resumen.Pausas),
			Pagos: conteo(j.Resumen.Pagos), Visitas: j.Resumen.Visitas,
			Condonaciones: j.Resumen.Condonaciones.N, Clientes: j.Resumen.Clientes,
		},
	}
	for _, s := range j.Senales {
		ids := make([]string, 0, len(s.ActividadIDs))
		ids = append(ids, s.ActividadIDs...)
		dto.Senales = append(dto.Senales, SenalDTO{
			Tipo: string(s.Tipo), ActividadIDs: ids, Desde: horaNula(s.Desde), Hasta: horaNula(s.Hasta),
			Detalle: s.Detalle, Regla: s.Regla,
		})
	}
	return dto
}

func aPaginaDTO(p desempenoapp.PaginaActividad) PaginaActividadDTO {
	return PaginaActividadDTO{
		Items: aActividadesDTO(p.Items), SiguienteCursor: textoNulo(p.SiguienteCursor),
		Totales: TotalesActividadDTO{
			N: p.Totales.N, Importe: dinero(p.Totales.Importe),
			PorTipo: PorTipoDTO{
				Pago:        p.Totales.PorTipo[domain.TipoPago],
				Visita:      p.Totales.PorTipo[domain.TipoVisita],
				Condonacion: p.Totales.PorTipo[domain.TipoCondonacion],
			},
		},
	}
}

func aCuentasDTO(v desempenoapp.VistaCuentas) CuentasDTO {
	dto := CuentasDTO{
		InicioSemana: hora(v.InicioSemana),
		Conteos: ConteosCuentasDTO{
			SinVisitar: v.Conteos.SinVisitar, Volver: v.Conteos.Volver, Despues: v.Conteos.Despues,
			Pagados: v.Conteos.Pagados, SinNada: v.Conteos.SinNada, Total: v.Conteos.Total,
		},
		Cuentas: make([]CuentaDTO, 0, len(v.Cuentas)),
	}
	for _, c := range v.Cuentas {
		cu := c.Cuenta
		cd := CuentaDTO{
			VentaID: cu.VentaID, DoctoPVID: enteroNulo(cu.DoctoPVID), Folio: textoNulo(cu.Folio),
			ClienteID: cu.ClienteID, ClienteNombre: cu.ClienteNombre, Producto: textoNulo(cu.Producto),
			Estado: string(c.Resultado.Estado), Trato: string(c.Trato), Etiqueta: c.Etiqueta, Detalle: c.Detalle,
			ConAlgo: c.Resultado.ConAlgo, DeterminadoPor: textoNulo(c.Resultado.DeterminadoPor),
			Saldo: dinero(cu.Saldo), Parcialidad: dinero(cu.Parcialidad), AbonoSemana: dinero(c.Resultado.AbonoVentana),
			NumAtrasos: c.NumAtrasos, Nueva: c.Nueva,
		}
		if c.Segmento != nil {
			s := string(*c.Segmento)
			cd.Segmento = &s
		}
		if c.UltimaActividad != nil {
			cd.UltimaActividad = &ActividadCuentaDTO{
				ID: c.UltimaActividad.ID, Tipo: string(c.UltimaActividad.Tipo), Hora: hora(c.UltimaActividad.Hora),
			}
		}
		dto.Cuentas = append(dto.Cuentas, cd)
	}
	return dto
}

func aCatalogosDTO() CatalogosDTO {
	dto := CatalogosDTO{}
	for _, f := range domain.CatalogoFormasCobro() {
		dto.FormasCobro = append(dto.FormasCobro, FormaCobroCatalogoDTO{Codigo: f.Codigo, Etiqueta: f.Etiqueta})
	}
	for _, t := range domain.CatalogoTiposVisita() {
		dto.TiposVisita = append(dto.TiposVisita, TipoVisitaCatalogoDTO{Literal: t.Literal, Estado: string(t.Estado), Grupo: t.Grupo})
	}
	return dto
}
