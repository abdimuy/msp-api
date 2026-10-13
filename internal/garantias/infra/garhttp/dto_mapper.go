package garhttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func garantiaDTO(g *domain.Garantia) GarantiaDTO {
	dto := GarantiaDTO{
		ID:          g.ID().String(),
		Folio:       g.Folio().String(),
		Origen:      g.Origen().String(),
		ClienteID:   g.ClienteID(),
		VentaID:     g.VentaID(),
		Estado:      g.Estado().String(),
		Description: g.Description(),
		GPSLat:      g.GPSLat(),
		GPSLon:      g.GPSLon(),
		AbiertoPor:  g.AbiertoPor(),
		CreatedAt:   formatTime(g.CreatedAt()),
		UpdatedAt:   formatTime(g.UpdatedAt()),
		Articulos:   make([]ArticuloDTO, 0, g.ArticulosCount()),
	}

	if estadoCuenta := g.EstadoCuenta(); estadoCuenta != nil {
		v := estadoCuenta.String()
		dto.EstadoCuenta = &v
	}

	if vigencia := g.VigenciaHasta(); vigencia != nil {
		v := vigencia.Format("2006-01-02")
		dto.VigenciaHasta = &v
	}

	dto.Calle = stringOpcional(g.Calle())
	dto.NumeroExterior = stringOpcional(g.NumeroExterior())
	dto.Colonia = stringOpcional(g.Colonia())
	dto.Localidad = stringOpcional(g.Localidad())
	dto.Ciudad = stringOpcional(g.Ciudad())
	dto.CodigoPostal = stringOpcional(g.CodigoPostal())

	if cerrado := g.CerradoEn(); cerrado != nil {
		v := formatTime(*cerrado)
		dto.CerradoEn = &v
	}

	for articulo := range g.Articulos() {
		dto.Articulos = append(dto.Articulos, articuloDTO(articulo))
	}

	return dto
}

func articuloDTO(a *domain.Articulo) ArticuloDTO {
	dto := ArticuloDTO{
		ID:          a.ID().String(),
		GarantiaID:  a.GarantiaID().String(),
		Rol:         a.Rol().String(),
		ArticuloID:  a.ArticuloID(),
		Clave:       a.Clave(),
		Description: a.Description(),
		Etapa:       a.Etapa().String(),
		Ubicacion:   a.Ubicacion().String(),
		CreatedAt:   formatTime(a.CreatedAt()),
		UpdatedAt:   formatTime(a.UpdatedAt()),
	}

	if reemplazaA := a.ReemplazaA(); reemplazaA != nil {
		v := reemplazaA.String()
		dto.ReemplazaA = &v
	}

	if ruta := a.Ruta(); ruta != nil {
		v := ruta.String()
		dto.Ruta = &v
	}

	if dictamen := a.Dictamen(); dictamen != nil {
		v := dictamen.String()
		dto.Dictamen = &v
	}

	if desenlace := a.Desenlace(); desenlace != nil {
		v := desenlace.String()
		dto.Desenlace = &v
	}

	if cerrado := a.CerradoEn(); cerrado != nil {
		v := formatTime(*cerrado)
		dto.CerradoEn = &v
	}

	return dto
}

func eventoDTO(e *domain.Evento) EventoDTO {
	dto := EventoDTO{
		ID:                e.ID().String(),
		GarantiaID:        e.GarantiaID().String(),
		Tipo:              e.Tipo().String(),
		Description:       e.Description(),
		Usuario:           e.Usuario(),
		GPSLat:            e.GPSLat(),
		GPSLon:            e.GPSLon(),
		CreatedAt:         formatTime(e.CreatedAt()),
		DeviceCreatedAt:   formatTime(e.DeviceCreatedAt()),
		ClaveIdempotencia: e.ClaveIdempotencia(),
	}

	if articuloRef := e.ArticuloRef(); articuloRef != nil {
		v := articuloRef.String()
		dto.ArticuloRef = &v
	}

	if etapaDesde := e.EtapaDesde(); etapaDesde != nil {
		v := etapaDesde.String()
		dto.EtapaDesde = &v
	}

	if etapaHasta := e.EtapaHasta(); etapaHasta != nil {
		v := etapaHasta.String()
		dto.EtapaHasta = &v
	}

	if rol := e.RolDecisor(); rol != nil {
		v := rol.String()
		dto.RolDecisor = &v
	}

	return dto
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func stringOpcional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
