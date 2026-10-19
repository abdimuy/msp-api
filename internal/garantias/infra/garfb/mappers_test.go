//nolint:paralleltest,testpackage // Mapper unit test needs package access.
package garfb

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// filaFalsa answers row.Scanner with a prebuilt list of values, in the exact
// order each scan* passes its destinations. It exists because the branches
// that matter here — a persisted value the domain will not accept — can only
// be reached with a corrupt row, and the integration tests run inside a
// rollback-only transaction against a shared database that cannot be given
// those rows (no DDL, and the repo itself never writes them).
type filaFalsa struct {
	valores []any
}

func (f *filaFalsa) Scan(dest ...any) error {
	if len(dest) != len(f.valores) {
		return errColumnas
	}

	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = f.valores[i].(string)
		case *[]byte:
			*p = f.valores[i].([]byte)
		case *any:
			*p = f.valores[i]
		case *sql.NullString:
			if v, ok := f.valores[i].(sql.NullString); ok {
				*p = v
			}
		case *sql.NullInt64:
			if v, ok := f.valores[i].(sql.NullInt64); ok {
				*p = v
			}
		case *sql.NullFloat64:
			if v, ok := f.valores[i].(sql.NullFloat64); ok {
				*p = v
			}
		case *sql.NullTime:
			if v, ok := f.valores[i].(sql.NullTime); ok {
				*p = v
			}
		}
	}

	return nil
}

var errColumnas = errors.New("columnas no coinciden")

// filaGarantia builds the value list scanGarantia expects. The zero value is a
// valid, complete folio; tests corrupt one field at a time.
type filaGarantia struct {
	id, folio, origen string
	clienteID         sql.NullInt64
	ventaID           sql.NullInt64
	estadoCuenta      sql.NullString
	estado            string
	description       []byte
	vigenciaHasta     sql.NullTime
	direccion         sql.NullString
	gpsLat            sql.NullFloat64
	gpsLon            sql.NullFloat64
	abiertoPor        string
	cerrado, created  any
	updated           any
}

func (g filaGarantia) valores() []any {
	return []any{
		g.id,
		g.folio,
		g.origen,
		g.clienteID,
		g.ventaID,
		g.estadoCuenta,
		g.estado,
		g.description,
		g.vigenciaHasta,
		g.direccion,
		g.direccion,
		g.direccion,
		g.direccion,
		g.direccion,
		g.direccion,
		g.gpsLat,
		g.gpsLon,
		g.abiertoPor,
		g.cerrado,
		g.created,
		g.updated,
	}
}

func garantiaValida() filaGarantia {
	return filaGarantia{
		id:     uuid.NewString(),
		folio:  "GA-000001",
		origen: string(domain.OrigenFolioPiso),
		estadoCuenta: sql.NullString{
			String: string(domain.EstadoCuentaLiquidada),
			Valid:  true,
		},
		estado:      string(domain.EstadoFolioAbierto),
		description: []byte("Garantía de prueba"),
		abiertoPor:  "ruben",
		cerrado:     nil,
		created:     time.Now(),
		updated:     time.Now(),
	}
}

func TestHydrateGarantia_CaminoFeliz(t *testing.T) {
	t.Parallel()

	g, err := hydrateGarantia(&garantiaRow{}, nil)
	require.Error(t, err, "una fila vacía no puede hidratar")
	_ = g
}

func TestHydrateGarantia_RoundTrip(t *testing.T) {
	t.Parallel()

	raw := garantiaValida()
	row, err := scanGarantia(&filaFalsa{valores: raw.valores()})
	require.NoError(t, err)

	g, err := hydrateGarantia(row, nil)
	require.NoError(t, err)
	require.Equal(t, raw.id, g.ID().String())
	require.Equal(t, domain.OrigenFolioPiso, g.Origen())
	require.Equal(t, domain.EstadoFolioAbierto, g.Estado())
	require.Equal(t, "ruben", g.AbiertoPor())
}

func TestHydrateGarantia_ValoresInvalidos(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre   string
		corrompe func(*filaGarantia)
	}{
		{"id", func(g *filaGarantia) { g.id = "no-es-uuid" }},
		{"folio", func(g *filaGarantia) { g.folio = "ZZZ" }},
		{"origen", func(g *filaGarantia) { g.origen = "inventado" }},
		{"estado_cuenta", func(g *filaGarantia) {
			g.estadoCuenta = sql.NullString{String: "inventado", Valid: true}
		}},
		{"estado", func(g *filaGarantia) { g.estado = "inventado" }},
		{"cerrado", func(g *filaGarantia) { g.cerrado = 42 }},
		{"created", func(g *filaGarantia) { g.created = 42 }},
		{"updated", func(g *filaGarantia) { g.updated = 42 }},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()

			raw := garantiaValida()
			c.corrompe(&raw)

			row, err := scanGarantia(&filaFalsa{valores: raw.valores()})
			require.NoError(t, err)

			_, err = hydrateGarantia(row, nil)
			require.Error(t, err, "un valor corrupto debe impedir la hidratación")
		})
	}
}

// filaArticulo builds the value list scanArticulo expects.
type filaArticulo struct {
	id, garantiaID, rol string
	articuloID          sql.NullInt64
	clave               sql.NullString
	description         string
	ruta                sql.NullString
	etapa               string
	ubicacion           string
	dictamen            sql.NullString
	desenlace           sql.NullString
	cerrado, created    any
	updated             any
	reemplazaA          sql.NullString
}

func (a filaArticulo) valores() []any {
	return []any{
		a.id,
		a.garantiaID,
		a.rol,
		a.articuloID,
		a.clave,
		a.description,
		a.ruta,
		a.etapa,
		a.ubicacion,
		a.dictamen,
		a.desenlace,
		a.cerrado,
		a.created,
		a.updated,
		a.reemplazaA,
	}
}

func articuloValido() filaArticulo {
	return filaArticulo{
		id:          uuid.NewString(),
		garantiaID:  uuid.NewString(),
		rol:         string(domain.RolArticuloOriginal),
		description: "Sillón",
		etapa:       string(domain.EtapaRecolectado),
		ubicacion:   string(domain.UbicacionAlmacenRevision),
		cerrado:     nil,
		created:     time.Now(),
		updated:     time.Now(),
	}
}

func TestHydrateArticulo_RoundTrip(t *testing.T) {
	t.Parallel()

	raw := articuloValido()
	row, err := scanArticulo(&filaFalsa{valores: raw.valores()})
	require.NoError(t, err)

	a, err := hydrateArticulo(row)
	require.NoError(t, err)
	require.Equal(t, raw.id, a.ID().String())
	require.Equal(t, domain.RolArticuloOriginal, a.Rol())
	require.Equal(t, domain.EtapaRecolectado, a.Etapa())
	require.Nil(t, a.Ruta())
	require.Nil(t, a.Dictamen())
	require.Nil(t, a.Desenlace())
}

func TestHydrateArticulo_ValoresInvalidos(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre   string
		corrompe func(*filaArticulo)
	}{
		{"id", func(a *filaArticulo) { a.id = "no-es-uuid" }},
		{"garantia_id", func(a *filaArticulo) { a.garantiaID = "no-es-uuid" }},
		{"rol", func(a *filaArticulo) { a.rol = "inventado" }},
		{"ruta", func(a *filaArticulo) {
			a.ruta = sql.NullString{String: "inventada", Valid: true}
		}},
		{"etapa", func(a *filaArticulo) { a.etapa = "inventada" }},
		{"ubicacion", func(a *filaArticulo) { a.ubicacion = "inventada" }},
		{"dictamen", func(a *filaArticulo) {
			a.dictamen = sql.NullString{String: "inventado", Valid: true}
		}},
		{"desenlace", func(a *filaArticulo) {
			a.desenlace = sql.NullString{String: "inventado", Valid: true}
		}},
		{"reemplaza_a", func(a *filaArticulo) {
			a.reemplazaA = sql.NullString{String: "no-es-uuid", Valid: true}
		}},
		{"cerrado", func(a *filaArticulo) { a.cerrado = 42 }},
		{"created", func(a *filaArticulo) { a.created = 42 }},
		{"updated", func(a *filaArticulo) { a.updated = 42 }},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()

			raw := articuloValido()
			c.corrompe(&raw)

			row, err := scanArticulo(&filaFalsa{valores: raw.valores()})
			require.NoError(t, err)

			_, err = hydrateArticulo(row)
			require.Error(t, err, "un valor corrupto debe impedir la hidratación")
		})
	}
}

// filaEvento builds the value list scanEvento expects.
type filaEvento struct {
	id, garantiaID string
	articuloRef    sql.NullString
	tipo           string
	description    []byte
	etapaDesde     sql.NullString
	etapaHasta     sql.NullString
	ubicacionDesde sql.NullString
	ubicacionHasta sql.NullString
	usuario        string
	rolDecisor     sql.NullString
	gpsLat, gpsLon sql.NullFloat64
	created        any
	deviceCreated  any
	clave          string
}

func (e filaEvento) valores() []any {
	return []any{
		e.id,
		e.garantiaID,
		e.articuloRef,
		e.tipo,
		e.description,
		e.etapaDesde,
		e.etapaHasta,
		e.ubicacionDesde,
		e.ubicacionHasta,
		e.usuario,
		e.rolDecisor,
		e.gpsLat,
		e.gpsLon,
		e.created,
		e.deviceCreated,
		e.clave,
	}
}

func eventoValido() filaEvento {
	return filaEvento{
		id:            uuid.NewString(),
		garantiaID:    uuid.NewString(),
		tipo:          string(domain.TipoEventoFolioAbierto),
		usuario:       "ruben",
		created:       time.Now(),
		deviceCreated: time.Now(),
		clave:         uuid.NewString(),
	}
}

func TestHydrateEvento_RoundTrip(t *testing.T) {
	t.Parallel()

	raw := eventoValido()
	row, err := scanEvento(&filaFalsa{valores: raw.valores()})
	require.NoError(t, err)

	e, err := hydrateEvento(row)
	require.NoError(t, err)
	require.Equal(t, raw.id, e.ID().String())
	require.Equal(t, domain.TipoEventoFolioAbierto, e.Tipo())
	require.Nil(t, e.ArticuloRef())
	require.Nil(t, e.RolDecisor())
	require.Nil(t, e.EtapaDesde())
	require.Nil(t, e.GPSLat())
}

func TestHydrateEvento_ValoresInvalidos(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre   string
		corrompe func(*filaEvento)
	}{
		{"id", func(e *filaEvento) { e.id = "no-es-uuid" }},
		{"garantia_id", func(e *filaEvento) { e.garantiaID = "no-es-uuid" }},
		{"articulo_ref", func(e *filaEvento) {
			e.articuloRef = sql.NullString{String: "no-es-uuid", Valid: true}
		}},
		{"tipo", func(e *filaEvento) { e.tipo = "inventado" }},
		{"etapa_desde", func(e *filaEvento) {
			e.etapaDesde = sql.NullString{String: "inventada", Valid: true}
		}},
		{"etapa_hasta", func(e *filaEvento) {
			e.etapaHasta = sql.NullString{String: "inventada", Valid: true}
		}},
		{"ubicacion_desde", func(e *filaEvento) {
			e.ubicacionDesde = sql.NullString{String: "inventada", Valid: true}
		}},
		{"ubicacion_hasta", func(e *filaEvento) {
			e.ubicacionHasta = sql.NullString{String: "inventada", Valid: true}
		}},
		{"rol_decisor", func(e *filaEvento) {
			e.rolDecisor = sql.NullString{String: "inventado", Valid: true}
		}},
		{"created", func(e *filaEvento) { e.created = 42 }},
		{"device_created", func(e *filaEvento) { e.deviceCreated = 42 }},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()

			raw := eventoValido()
			c.corrompe(&raw)

			row, err := scanEvento(&filaFalsa{valores: raw.valores()})
			require.NoError(t, err)

			_, err = hydrateEvento(row)
			require.Error(t, err, "un valor corrupto debe impedir la hidratación")
		})
	}
}

func TestHydrateEvento_PunterosYGPSCargados(t *testing.T) {
	t.Parallel()

	ref := uuid.NewString()
	raw := eventoValido()
	raw.articuloRef = sql.NullString{String: ref, Valid: true}
	raw.etapaDesde = sql.NullString{String: string(domain.EtapaRecolectado), Valid: true}
	raw.etapaHasta = sql.NullString{String: string(domain.EtapaEnRevision), Valid: true}
	raw.ubicacionDesde = sql.NullString{String: string(domain.UbicacionEnTransito), Valid: true}
	raw.ubicacionHasta = sql.NullString{String: string(domain.UbicacionAlmacenRevision), Valid: true}
	raw.rolDecisor = sql.NullString{String: string(domain.RolDecisorTecnica), Valid: true}
	raw.gpsLat = sql.NullFloat64{Float64: 19.04, Valid: true}
	raw.gpsLon = sql.NullFloat64{Float64: -98.20, Valid: true}

	row, err := scanEvento(&filaFalsa{valores: raw.valores()})
	require.NoError(t, err)

	e, err := hydrateEvento(row)
	require.NoError(t, err)
	require.NotNil(t, e.ArticuloRef())
	require.Equal(t, ref, e.ArticuloRef().String())
	require.NotNil(t, e.RolDecisor())
	require.Equal(t, domain.RolDecisorTecnica, *e.RolDecisor())
	require.NotNil(t, e.GPSLat())
	require.InDelta(t, 19.04, *e.GPSLat(), 1e-9)
	require.InDelta(t, -98.20, *e.GPSLon(), 1e-9)
	require.NotNil(t, e.UbicacionDesde())
	require.Equal(t, domain.UbicacionEnTransito, *e.UbicacionDesde())
	require.NotNil(t, e.UbicacionHasta())
	require.Equal(t, domain.UbicacionAlmacenRevision, *e.UbicacionHasta())
}
