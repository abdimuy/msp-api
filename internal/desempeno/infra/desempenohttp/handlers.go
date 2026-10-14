//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"context"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
)

// Handlers agrupa las operaciones Huma del módulo. Todas exigen desempeno:ver.
type Handlers struct {
	svc *desempenoapp.Service
}

// TableroInput — GET /desempeno/usuarios. Todo llega como texto y se valida en
// el dominio para que el 422 lleve el código desempeno_parametro_invalido.
type TableroInput struct {
	Periodo   string `query:"periodo"    doc:"hoy | semana (por omisión hoy)"`
	PausaMin  string `query:"pausa_min"  doc:"umbral de pausa en minutos, 5–240 (por omisión 45)"`
	DesdeHora string `query:"desde_hora" doc:"HH:MM, con hasta_hora"`
	HastaHora string `query:"hasta_hora" doc:"HH:MM, con desde_hora"`
}

// TableroOutput envuelve TableroDTO.
type TableroOutput struct{ Body TableroDTO }

// JornadaInput — GET /desempeno/usuarios/{uid}/jornada.
type JornadaInput struct {
	UID       string `path:"uid"         doc:"uid de Firestore del usuario"`
	Fecha     string `query:"fecha"      doc:"YYYY-MM-DD (por omisión hoy, America/Mexico_City)"`
	PausaMin  string `query:"pausa_min"`
	DesdeHora string `query:"desde_hora"`
	HastaHora string `query:"hasta_hora"`
}

// JornadaOutput envuelve JornadaDTO.
type JornadaOutput struct{ Body JornadaDTO }

// ActividadInput — GET /desempeno/usuarios/{uid}/actividad. Los repetibles van
// "explode" (?tipo=pago&tipo=visita), como pide el contrato §4.
type ActividadInput struct {
	UID           string   `path:"uid"`
	Desde         string   `query:"desde"`
	Hasta         string   `query:"hasta"`
	Tipo          []string `query:"tipo,explode"`
	FormaCobro    []string `query:"forma_cobro,explode"`
	TipoVisita    []string `query:"tipo_visita,explode"`
	RegistradoPor []string `query:"registrado_por,explode"`
	MontoMin      string   `query:"monto_min"`
	MontoMax      string   `query:"monto_max"`
	ConFotos      string   `query:"con_fotos"`
	ConGPS        string   `query:"con_gps"`
	Q             string   `query:"q"`
	Orden         string   `query:"orden"`
	Dir           string   `query:"dir"`
	Cursor        string   `query:"cursor"`
	Limit         string   `query:"limit"`
}

// ActividadOutput envuelve PaginaActividadDTO.
type ActividadOutput struct{ Body PaginaActividadDTO }

// CuentasInput — GET /desempeno/usuarios/{uid}/cuentas.
type CuentasInput struct {
	UID      string `path:"uid"`
	Segmento string `query:"segmento"`
	Q        string `query:"q"`
	Orden    string `query:"orden"`
	Dir      string `query:"dir"`
}

// CuentasOutput envuelve CuentasDTO.
type CuentasOutput struct{ Body CuentasDTO }

// CatalogosInput — GET /desempeno/catalogos (sin parámetros).
type CatalogosInput struct{}

// CatalogosOutput envuelve CatalogosDTO.
type CatalogosOutput struct{ Body CatalogosDTO }

// Catalogos — formas de cobro y tipos de visita para armar los filtros: el
// escritorio no copia catálogos (contrato §4b).
func (h *Handlers) Catalogos(ctx context.Context, _ *CatalogosInput) (*CatalogosOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	return &CatalogosOutput{Body: aCatalogosDTO()}, nil
}

// Tablero — una fila por usuario, sin totales del equipo (D14).
func (h *Handlers) Tablero(ctx context.Context, in *TableroInput) (*TableroOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	t, err := h.svc.Tablero(ctx, desempenoapp.TableroInput{
		Periodo: in.Periodo, PausaMin: in.PausaMin, DesdeHora: in.DesdeHora, HastaHora: in.HastaHora,
	})
	if err != nil {
		return nil, mapAppError(ctx, err)
	}
	return &TableroOutput{Body: aTableroDTO(t)}, nil
}

// Jornada — actividades del usuario en un día, con pausas y señales.
func (h *Handlers) Jornada(ctx context.Context, in *JornadaInput) (*JornadaOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err := h.svc.Jornada(ctx, in.UID, desempenoapp.JornadaInput{
		Fecha: in.Fecha, PausaMin: in.PausaMin, DesdeHora: in.DesdeHora, HastaHora: in.HastaHora,
	})
	if err != nil {
		return nil, mapAppError(ctx, err)
	}
	return &JornadaOutput{Body: aJornadaDTO(v)}, nil
}

// Actividad — página de la actividad de toda la zona del usuario.
func (h *Handlers) Actividad(ctx context.Context, in *ActividadInput) (*ActividadOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	p, err := h.svc.Actividad(ctx, in.UID, desempenoapp.FiltroActividadInput{
		Desde: in.Desde, Hasta: in.Hasta, Tipos: in.Tipo, FormasCobro: in.FormaCobro, TiposVisita: in.TipoVisita,
		RegistradoPor: in.RegistradoPor, MontoMin: in.MontoMin, MontoMax: in.MontoMax,
		ConFotos: in.ConFotos, ConGPS: in.ConGPS, Q: in.Q, Orden: in.Orden, Dir: in.Dir,
		Cursor: in.Cursor, Limit: in.Limit,
	})
	if err != nil {
		return nil, mapAppError(ctx, err)
	}
	return &ActividadOutput{Body: aPaginaDTO(p)}, nil
}

// Cuentas — estado por venta de la semana del usuario.
func (h *Handlers) Cuentas(ctx context.Context, in *CuentasInput) (*CuentasOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err := h.svc.Cuentas(ctx, in.UID, desempenoapp.CuentasInput{
		Segmento: in.Segmento, Q: in.Q, Orden: in.Orden, Dir: in.Dir,
	})
	if err != nil {
		return nil, mapAppError(ctx, err)
	}
	return &CuentasOutput{Body: aCuentasDTO(v)}, nil
}
