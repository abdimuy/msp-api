//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Límites de GET /desempeno/usuarios/{uid}/actividad (contrato §4).
const (
	LimitePorOmision = 100
	LimiteMaximo     = 500
	RangoMaximo      = 92 * 24 * time.Hour
)

// Órdenes admitidos.
const (
	OrdenHora          = "hora"
	OrdenCliente       = "cliente"
	OrdenMonto         = "monto"
	OrdenTipo          = "tipo"
	OrdenRegistradoPor = "registrado_por"
	registradoOficina  = "oficina"
)

// FiltroActividadInput lleva los parámetros crudos de la consulta. Los repetibles
// llegan como lista.
type FiltroActividadInput struct {
	Desde         string
	Hasta         string
	Tipos         []string
	FormasCobro   []string
	TiposVisita   []string
	RegistradoPor []string
	MontoMin      string
	MontoMax      string
	ConFotos      string
	ConGPS        string
	Q             string
	Orden         string
	Dir           string
	Cursor        string
	Limit         string
}

// TotalesActividad son del filtro completo, no de la página.
type TotalesActividad struct {
	N       int
	Importe decimal.Decimal
	PorTipo map[domain.TipoActividad]int
}

// PaginaActividad es la respuesta de la consulta.
type PaginaActividad struct {
	Items           []domain.Actividad
	SiguienteCursor string // "" = no hay más
	Totales         TotalesActividad
}

type filtroActividad struct {
	desde, hasta time.Time
	tipos        map[string]bool
	formas       map[string]bool
	tiposVisita  map[string]bool
	registrado   map[string]bool
	montoMin     *decimal.Decimal
	montoMax     *decimal.Decimal
	conFotos     *bool
	conGPS       *bool
	q            string
	orden        string
	desc         bool
	cursor       *cursorActividad
	limit        int
}

type cursorActividad struct {
	Orden string `json:"o"`
	Dir   string `json:"d"`
	Hora  string `json:"h"`
	Texto string `json:"t"`
	Monto string `json:"m"`
	ID    string `json:"i"`
}

func invalido(param string) error { return domain.ErrParametroInvalido.WithField("parametro", param) }

func conjunto(valores []string, param string, permitidos ...string) (map[string]bool, error) {
	if len(valores) == 0 {
		return nil, nil //nolint:nilnil // sin filtro = nil
	}
	ok := map[string]bool{}
	for _, p := range permitidos {
		ok[p] = true
	}
	out := map[string]bool{}
	for _, v := range valores {
		if len(permitidos) > 0 && !ok[v] {
			return nil, invalido(param)
		}
		out[v] = true
	}
	return out, nil
}

func boolOpcional(s, param string) (*bool, error) {
	switch s {
	case "":
		return nil, nil //nolint:nilnil // sin filtro = nil
	case "true":
		v := true
		return &v, nil
	case "false":
		v := false
		return &v, nil
	}
	return nil, invalido(param)
}

func montoOpcional(s, param string) (*decimal.Decimal, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // sin filtro = nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil, invalido(param)
	}
	return &d, nil
}

func (s *Service) parseFiltro(in FiltroActividadInput, c contexto) (filtroActividad, error) {
	f := filtroActividad{orden: OrdenHora, desc: true, limit: LimitePorOmision}
	var err error
	if f.desde, f.hasta, err = parseRango(in.Desde, in.Hasta, c); err != nil {
		return f, err
	}
	if err := f.parseConjuntos(in); err != nil {
		return f, err
	}
	if err := f.parseOpcionales(in); err != nil {
		return f, err
	}
	if err := f.parseOrden(in.Orden, in.Dir); err != nil {
		return f, err
	}
	err = f.parsePaginacion(in.Limit, in.Cursor)
	return f, err
}

// parseRango: por omisión [inicio de semana, ahora]; máximo 92 días. Sin desde
// explícito y con un inicio de semana de hace más de 92 días, desde se recorta
// a hasta − 92 d (el usuario no pidió ese rango: no es un 422).
func parseRango(desdeS, hastaS string, c contexto) (time.Time, time.Time, error) {
	var desde, hasta time.Time
	var err error
	hasta = c.ahora
	if hastaS != "" {
		if hasta, err = time.Parse(time.RFC3339, hastaS); err != nil {
			return desde, hasta, invalido("hasta")
		}
	}
	switch {
	case desdeS != "":
		if desde, err = time.Parse(time.RFC3339, desdeS); err != nil {
			return desde, hasta, invalido("desde")
		}
	case c.usuario.InicioSemana == nil:
		return desde, hasta, domain.ErrUsuarioSinInicioSemana
	default:
		desde = *c.usuario.InicioSemana
		if minimo := hasta.Add(-RangoMaximo); desde.Before(minimo) {
			desde = minimo
		}
	}
	desde, hasta = desde.UTC(), hasta.UTC()
	if hasta.Before(desde) || hasta.Sub(desde) > RangoMaximo {
		return desde, hasta, invalido("desde/hasta")
	}
	return desde, hasta, nil
}

func (f *filtroActividad) parseConjuntos(in FiltroActividadInput) error {
	var err error
	if f.tipos, err = conjunto(in.Tipos, "tipo",
		string(domain.TipoPago), string(domain.TipoVisita), string(domain.TipoCondonacion)); err != nil {
		return err
	}
	if f.formas, err = conjunto(in.FormasCobro, "forma_cobro", domain.EtiquetaEfectivo, domain.EtiquetaTransferencia,
		domain.EtiquetaCheque, domain.EtiquetaCondonacion, domain.EtiquetaOtra); err != nil {
		return err
	}
	if f.tiposVisita, err = conjunto(in.TiposVisita, "tipo_visita"); err != nil {
		return err
	}
	f.registrado, err = conjunto(in.RegistradoPor, "registrado_por")
	return err
}

func (f *filtroActividad) parseOpcionales(in FiltroActividadInput) error {
	var err error
	if f.montoMin, err = montoOpcional(in.MontoMin, "monto_min"); err != nil {
		return err
	}
	if f.montoMax, err = montoOpcional(in.MontoMax, "monto_max"); err != nil {
		return err
	}
	if f.conFotos, err = boolOpcional(in.ConFotos, "con_fotos"); err != nil {
		return err
	}
	if f.conGPS, err = boolOpcional(in.ConGPS, "con_gps"); err != nil {
		return err
	}
	f.q = normalizar(in.Q)
	return nil
}

func (f *filtroActividad) parseOrden(orden, dir string) error {
	switch orden {
	case "":
	case OrdenHora, OrdenCliente, OrdenMonto, OrdenTipo, OrdenRegistradoPor:
		f.orden = orden
	default:
		return invalido("orden")
	}
	switch dir {
	case "", "desc":
	case "asc":
		f.desc = false
	default:
		return invalido("dir")
	}
	return nil
}

func (f *filtroActividad) parsePaginacion(limit, cursor string) error {
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 || n > LimiteMaximo {
			return invalido("limit")
		}
		f.limit = n
	}
	if cursor == "" {
		return nil
	}
	cur, err := decodificarCursor(cursor)
	if err != nil || cur.Orden != f.orden || cur.Dir != dirDe(f.desc) {
		return invalido("cursor")
	}
	f.cursor = &cur
	return nil
}

func dirDe(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

//nolint:cyclop // un filtro por rama; todos son opcionales.
func (f *filtroActividad) acepta(a domain.Actividad) bool {
	if a.Hora.Before(f.desde) || a.Hora.After(f.hasta) {
		return false
	}
	if f.tipos != nil && !f.tipos[string(a.Tipo)] {
		return false
	}
	if f.formas != nil && (a.Pago == nil || !f.formas[domain.EtiquetaFormaDeActividad(a)]) {
		return false
	}
	if f.tiposVisita != nil && (a.Visita == nil || !f.tiposVisita[a.Visita.TipoVisita]) {
		return false
	}
	if f.registrado != nil && !f.registrado[claveRegistrador(a)] {
		return false
	}
	if (f.montoMin != nil || f.montoMax != nil) && a.Pago == nil {
		return false
	}
	if f.montoMin != nil && a.Importe().LessThan(*f.montoMin) {
		return false
	}
	if f.montoMax != nil && a.Importe().GreaterThan(*f.montoMax) {
		return false
	}
	if f.conFotos != nil && (a.NumFotos > 0) != *f.conFotos {
		return false
	}
	if f.conGPS != nil && a.ConGPS() != *f.conGPS {
		return false
	}
	if f.q != "" && !strings.Contains(normalizar(a.ClienteNombre), f.q) && !strings.Contains(normalizar(a.Folio), f.q) {
		return false
	}
	return true
}

// claveRegistrador devuelve "oficina" o el uid (vacío si no se resolvió, y entonces
// ningún filtro por uid lo encuentra — decisión D-API-4).
func claveRegistrador(a domain.Actividad) string {
	if a.RegistradoPor.Origen == domain.OrigenOficina {
		return registradoOficina
	}
	return a.RegistradoPor.UsuarioUID
}

type claveOrden struct {
	hora  time.Time
	texto string
	monto decimal.Decimal
	id    string
}

func claveDe(a domain.Actividad, orden string) claveOrden {
	k := claveOrden{id: a.ID, monto: decimal.Zero}
	switch orden {
	case OrdenHora:
		k.hora = a.Hora
	case OrdenCliente:
		k.texto = normalizar(a.ClienteNombre)
	case OrdenMonto:
		k.monto = a.Importe()
	case OrdenTipo:
		k.texto = string(a.Tipo)
	case OrdenRegistradoPor:
		k.texto = normalizar(a.RegistradoPor.Nombre)
	}
	return k
}

// comparar es un orden total: la clave del orden y, en empate, el id.
func comparar(a, b claveOrden) int {
	if c := a.hora.Compare(b.hora); c != 0 {
		return c
	}
	if c := strings.Compare(a.texto, b.texto); c != 0 {
		return c
	}
	if c := a.monto.Cmp(b.monto); c != 0 {
		return c
	}
	return strings.Compare(a.id, b.id)
}

func (f *filtroActividad) ordenar(acts []domain.Actividad) {
	sort.SliceStable(acts, func(i, j int) bool {
		c := comparar(claveDe(acts[i], f.orden), claveDe(acts[j], f.orden))
		if f.desc {
			return c > 0
		}
		return c < 0
	})
}

func codificarCursor(a domain.Actividad, f *filtroActividad) string {
	k := claveDe(a, f.orden)
	cur := cursorActividad{Orden: f.orden, Dir: dirDe(f.desc), Texto: k.texto, Monto: k.monto.String(), ID: k.id}
	if !k.hora.IsZero() {
		cur.Hora = k.hora.UTC().Format(time.RFC3339Nano)
	}
	b, _ := json.Marshal(cur) //nolint:errchkjson // struct de strings: no puede fallar.
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodificarCursor(s string) (cursorActividad, error) {
	var cur cursorActividad
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cur, err
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return cur, err
	}
	return cur, nil
}

func (cur cursorActividad) clave() (claveOrden, error) {
	k := claveOrden{texto: cur.Texto, id: cur.ID}
	var err error
	if cur.Hora != "" {
		if k.hora, err = time.Parse(time.RFC3339Nano, cur.Hora); err != nil {
			return k, err
		}
	}
	if k.monto, err = decimal.NewFromString(cur.Monto); err != nil {
		return k, err
	}
	return k, nil
}

// filtrar trae TODA la zona del usuario en el rango y aplica el filtro.
func (s *Service) filtrar(ctx context.Context, uid string, in FiltroActividadInput) ([]domain.Actividad, filtroActividad, error) {
	c, err := s.resolverUsuario(ctx, uid)
	if err != nil {
		return nil, filtroActividad{}, err
	}
	if c.usuario.ZonaID <= 0 {
		return nil, filtroActividad{}, domain.ErrUsuarioSinZona
	}
	if c.usuario.InicioSemanaFuturo(c.ahora) {
		return nil, filtroActividad{}, domain.ErrUsuarioInicioSemanaFuturo
	}
	f, err := s.parseFiltro(in, c)
	if err != nil {
		return nil, f, err
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, f.desde, f.hasta)
	if err != nil {
		return nil, f, err
	}
	out := make([]domain.Actividad, 0, len(acts))
	for _, a := range acts {
		if f.acepta(a) {
			out = append(out, a)
		}
	}
	f.ordenar(out)
	return out, f, nil
}

// Actividad devuelve una página de la actividad de TODA la zona del usuario (cualquier
// registrador) con totales del filtro completo y cursor estable.
func (s *Service) Actividad(ctx context.Context, uid string, in FiltroActividadInput) (PaginaActividad, error) {
	acts, f, err := s.filtrar(ctx, uid, in)
	if err != nil {
		return PaginaActividad{}, err
	}
	pag := PaginaActividad{Totales: TotalesActividad{
		N: len(acts), Importe: decimal.Zero,
		PorTipo: map[domain.TipoActividad]int{domain.TipoPago: 0, domain.TipoVisita: 0, domain.TipoCondonacion: 0},
	}}
	for _, a := range acts {
		pag.Totales.PorTipo[a.Tipo]++
		if a.CuentaComoDinero() {
			pag.Totales.Importe = pag.Totales.Importe.Add(a.Importe())
		}
	}
	inicio := 0
	if f.cursor != nil {
		k, err := f.cursor.clave()
		if err != nil {
			return PaginaActividad{}, invalido("cursor")
		}
		inicio = sort.Search(len(acts), func(i int) bool {
			c := comparar(claveDe(acts[i], f.orden), k)
			if f.desc {
				return c < 0
			}
			return c > 0
		})
	}
	fin := inicio + f.limit
	if fin > len(acts) {
		fin = len(acts)
	}
	pag.Items = acts[inicio:fin]
	if fin < len(acts) && fin > inicio {
		pag.SiguienteCursor = codificarCursor(acts[fin-1], &f)
	}
	return pag, nil
}
