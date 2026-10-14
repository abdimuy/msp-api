//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

// DTOs del contrato docs/superpowers/specs/2026-10-14-desempeno-contratos.md,
// byte a byte. Regla §0: null explícito, NUNCA omitempty (lo vigila
// TestDTOs_SinOmitempty). Dinero: string con 2 decimales. Horas: RFC3339 UTC.

// RegistradoPorDTO — contrato §1 RegistradoPor.
type RegistradoPorDTO struct {
	UsuarioUID *string `json:"usuario_uid"`
	CobradorID *int    `json:"cobrador_id"`
	Nombre     string  `json:"nombre"`
	Origen     string  `json:"origen"`
}

// PagoDTO — contrato §1 Actividad.pago.
type PagoDTO struct {
	Importe        string  `json:"importe"`
	FormaCobro     string  `json:"forma_cobro"`
	FormaCobroID   *int    `json:"forma_cobro_id"`
	Aplicado       bool    `json:"aplicado"`
	Rechazado      bool    `json:"rechazado"`
	DoctoCCID      *int    `json:"docto_cc_id"`
	PagoRecibidoID *string `json:"pago_recibido_id"`
}

// VisitaDTO — contrato §1 Actividad.visita.
type VisitaDTO struct {
	VisitaID   string  `json:"visita_id"`
	TipoVisita string  `json:"tipo_visita"`
	Nota       *string `json:"nota"`
	Estado     string  `json:"estado"`
	Trato      string  `json:"trato"`
	Etiqueta   string  `json:"etiqueta"`
}

// ActividadDTO — contrato §1 Actividad.
type ActividadDTO struct {
	ID            string           `json:"id"`
	Tipo          string           `json:"tipo"`
	Hora          string           `json:"hora"`
	RecibidoAt    *string          `json:"recibido_at"`
	ClienteID     int              `json:"cliente_id"`
	ClienteNombre string           `json:"cliente_nombre"`
	VentaID       *int             `json:"venta_id"`
	Folio         *string          `json:"folio"`
	RegistradoPor RegistradoPorDTO `json:"registrado_por"`
	Lat           *float64         `json:"lat"`
	Lng           *float64         `json:"lng"`
	ConGPS        bool             `json:"con_gps"`
	NumFotos      int              `json:"num_fotos"`
	DoctoPVID     *int             `json:"docto_pv_id"`
	CapturaTardia bool             `json:"captura_tardia"`
	Pago          *PagoDTO         `json:"pago"`
	Visita        *VisitaDTO       `json:"visita"`
}

// ConteoImporteDTO es `{n, importe}`.
type ConteoImporteDTO struct {
	N       int    `json:"n"`
	Importe string `json:"importe"`
}

// PausaDTO — contrato §1 Pausa.
type PausaDTO struct {
	Desde       string  `json:"desde"`
	Hasta       string  `json:"hasta"`
	Minutos     int     `json:"minutos"`
	AnteriorID  *string `json:"anterior_id"`
	SiguienteID *string `json:"siguiente_id"`
	EnCurso     bool    `json:"en_curso"`
}

// TramoDTO — contrato §1 Tramo.
type TramoDTO struct {
	Desde         string           `json:"desde"`
	Hasta         string           `json:"hasta"`
	Pagos         ConteoImporteDTO `json:"pagos"`
	Visitas       int              `json:"visitas"`
	Condonaciones int              `json:"condonaciones"`
	Clientes      int              `json:"clientes"`
}

// HoraDTO — contrato §1 Hora.
type HoraDTO struct {
	Hora          int    `json:"hora"`
	Pagos         int    `json:"pagos"`
	Visitas       int    `json:"visitas"`
	Condonaciones int    `json:"condonaciones"`
	Importe       string `json:"importe"`
}

// SenalDTO — contrato §1 Senal.
type SenalDTO struct {
	Tipo         string   `json:"tipo"`
	ActividadIDs []string `json:"actividad_ids"`
	Desde        *string  `json:"desde"`
	Hasta        *string  `json:"hasta"`
	Detalle      string   `json:"detalle"`
	Regla        string   `json:"regla"`
}

// PuntoDTO es un punto de LineaDelDia con los datos de su popover (contrato v2).
type PuntoDTO struct {
	ID            string  `json:"id"`
	Tipo          string  `json:"tipo"`
	Hora          string  `json:"hora"`
	ClienteNombre string  `json:"cliente_nombre"`
	Importe       *string `json:"importe"`
	Resultado     *string `json:"resultado"`
	NumFotos      int     `json:"num_fotos"`
}

// LineaDelDiaDTO — contrato §1 LineaDelDia.
type LineaDelDiaDTO struct {
	Dia     string     `json:"dia"`
	Puntos  []PuntoDTO `json:"puntos"`
	Tramos  []TramoDTO `json:"tramos"`
	PorHora []HoraDTO  `json:"por_hora"`
	Pausas  []PausaDTO `json:"pausas"`
}

// PausasResumenDTO es `{n, minutos, en_curso}`.
type PausasResumenDTO struct {
	N       int  `json:"n"`
	Minutos int  `json:"minutos"`
	EnCurso bool `json:"en_curso"`
}

// UltimaDTO es `ultima` del tablero.
type UltimaDTO struct {
	ID            string `json:"id"`
	Tipo          string `json:"tipo"`
	Hora          string `json:"hora"`
	ClienteNombre string `json:"cliente_nombre"`
}

// PagosPorFormaDTO cuenta solo efectivo, transferencia y cheque.
type PagosPorFormaDTO struct {
	Efectivo      ConteoImporteDTO `json:"efectivo"`
	Transferencia ConteoImporteDTO `json:"transferencia"`
	Cheque        ConteoImporteDTO `json:"cheque"`
}

// CoberturaDTO es `{con_algo, total}`.
type CoberturaDTO struct {
	ConAlgo int `json:"con_algo"`
	Total   int `json:"total"`
}

// CoberturaPorSegmentoDTO — conteos por segmento.
type CoberturaPorSegmentoDTO struct {
	SinVisitar int `json:"sin_visitar"`
	Volver     int `json:"volver"`
	Despues    int `json:"despues"`
	Pagados    int `json:"pagados"`
	SinNada    int `json:"sin_nada"`
}

// CobroDTO es `{cobrado, esperado, falta, pct}`.
type CobroDTO struct {
	Cobrado  string  `json:"cobrado"`
	Esperado string  `json:"esperado"`
	Falta    string  `json:"falta"`
	Pct      *string `json:"pct"`
}

// UsuarioTableroDTO — una fila del contrato §2.
type UsuarioTableroDTO struct {
	UID                  string                  `json:"uid"`
	Nombre               string                  `json:"nombre"`
	Email                string                  `json:"email"`
	ZonaID               *int                    `json:"zona_id"`
	ZonaNombre           *string                 `json:"zona_nombre"`
	Configurado          bool                    `json:"configurado"`
	MotivoNoConfigurado  *string                 `json:"motivo_no_configurado"`
	InicioSemana         *string                 `json:"inicio_semana"`
	VentasUniverso       int                     `json:"ventas_universo"`
	EstadoAhora          string                  `json:"estado_ahora"`
	EstadoDesde          *string                 `json:"estado_desde"`
	Ultima               *UltimaDTO              `json:"ultima"`
	PrimeraActividad     *string                 `json:"primera_actividad"`
	UltimaActividad      *string                 `json:"ultima_actividad"`
	MinutosActivos       int                     `json:"minutos_activos"`
	Pausas               PausasResumenDTO        `json:"pausas"`
	Pagos                ConteoImporteDTO        `json:"pagos"`
	PagosPorForma        PagosPorFormaDTO        `json:"pagos_por_forma"`
	Condonaciones        ConteoImporteDTO        `json:"condonaciones"`
	NoAplicados          ConteoImporteDTO        `json:"no_aplicados"`
	Visitas              int                     `json:"visitas"`
	Cobertura            CoberturaDTO            `json:"cobertura"`
	CoberturaPorSegmento CoberturaPorSegmentoDTO `json:"cobertura_por_segmento"`
	Cobro                CobroDTO                `json:"cobro"`
	Lineas               []LineaDelDiaDTO        `json:"lineas"`
}

// TableroDTO — contrato §2.
type TableroDTO struct {
	GeneradoAt string              `json:"generado_at"`
	Periodo    string              `json:"periodo"`
	PausaMin   int                 `json:"pausa_min"`
	Usuarios   []UsuarioTableroDTO `json:"usuarios"`
}

// ResumenJornadaDTO — contrato §3 resumen.
type ResumenJornadaDTO struct {
	PrimeraActividad *string          `json:"primera_actividad"`
	UltimaActividad  *string          `json:"ultima_actividad"`
	MinutosActivos   int              `json:"minutos_activos"`
	Pausas           PausasResumenDTO `json:"pausas"`
	Pagos            ConteoImporteDTO `json:"pagos"`
	Visitas          int              `json:"visitas"`
	Condonaciones    int              `json:"condonaciones"`
	Clientes         int              `json:"clientes"`
}

// JornadaDTO — contrato §3.
type JornadaDTO struct {
	UID         string            `json:"uid"`
	Fecha       string            `json:"fecha"`
	PausaMin    int               `json:"pausa_min"`
	GeneradoAt  string            `json:"generado_at"`
	Actividades []ActividadDTO    `json:"actividades"`
	Linea       LineaDelDiaDTO    `json:"linea"`
	Senales     []SenalDTO        `json:"senales"`
	Resumen     ResumenJornadaDTO `json:"resumen"`
}

// PorTipoDTO cuenta por tipo de actividad.
type PorTipoDTO struct {
	Pago        int `json:"pago"`
	Visita      int `json:"visita"`
	Condonacion int `json:"condonacion"`
}

// TotalesActividadDTO — totales del filtro completo.
type TotalesActividadDTO struct {
	N       int        `json:"n"`
	Importe string     `json:"importe"`
	PorTipo PorTipoDTO `json:"por_tipo"`
}

// PaginaActividadDTO — contrato §4.
type PaginaActividadDTO struct {
	Items           []ActividadDTO      `json:"items"`
	SiguienteCursor *string             `json:"siguiente_cursor"`
	Totales         TotalesActividadDTO `json:"totales"`
}

// ConteosCuentasDTO — contrato §5 conteos.
type ConteosCuentasDTO struct {
	SinVisitar int `json:"sin_visitar"`
	Volver     int `json:"volver"`
	Despues    int `json:"despues"`
	Pagados    int `json:"pagados"`
	SinNada    int `json:"sin_nada"`
	Total      int `json:"total"`
}

// ActividadCuentaDTO es `ultima_actividad {id, tipo, hora}`.
type ActividadCuentaDTO struct {
	ID   string `json:"id"`
	Tipo string `json:"tipo"`
	Hora string `json:"hora"`
}

// CuentaDTO — una venta del contrato §5.
type CuentaDTO struct {
	VentaID         int                 `json:"venta_id"`
	DoctoPVID       *int                `json:"docto_pv_id"`
	Folio           *string             `json:"folio"`
	ClienteID       int                 `json:"cliente_id"`
	ClienteNombre   string              `json:"cliente_nombre"`
	Producto        *string             `json:"producto"`
	Estado          string              `json:"estado"`
	Trato           string              `json:"trato"`
	Etiqueta        string              `json:"etiqueta"`
	Detalle         string              `json:"detalle"`
	Segmento        *string             `json:"segmento"`
	ConAlgo         bool                `json:"con_algo"`
	DeterminadoPor  *string             `json:"determinado_por"`
	UltimaActividad *ActividadCuentaDTO `json:"ultima_actividad"`
	Saldo           string              `json:"saldo"`
	Parcialidad     string              `json:"parcialidad"`
	AbonoSemana     string              `json:"abono_semana"`
	NumAtrasos      int                 `json:"num_atrasos"`
	Nueva           bool                `json:"nueva"`
}

// CuentasDTO — contrato §5.
type CuentasDTO struct {
	InicioSemana string            `json:"inicio_semana"`
	Conteos      ConteosCuentasDTO `json:"conteos"`
	Cuentas      []CuentaDTO       `json:"cuentas"`
}

// FormaCobroCatalogoDTO — contrato §4b.
type FormaCobroCatalogoDTO struct {
	Codigo   string `json:"codigo"`
	Etiqueta string `json:"etiqueta"`
}

// TipoVisitaCatalogoDTO — contrato §4b.
type TipoVisitaCatalogoDTO struct {
	Literal string `json:"literal"`
	Estado  string `json:"estado"`
	Grupo   string `json:"grupo"`
}

// CatalogosDTO — contrato §4b, GET /desempeno/catalogos.
type CatalogosDTO struct {
	FormasCobro []FormaCobroCatalogoDTO `json:"formas_cobro"`
	TiposVisita []TipoVisitaCatalogoDTO `json:"tipos_visita"`
}
