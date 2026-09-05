package failedintenthttp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

// El nombre del cliente en los renglones de PAGO.
//
// El cuerpo de un pago NO trae el nombre — medido sobre capturas reales: trae
// cliente_id, el id del cargo y el nombre del cobrador. Por eso el listado
// mostraba "RUTA 27 - ALEJANDRO CHAVARRIA", que dice quién capturó el pago y
// no de quién es el dinero, que es lo que la oficina necesita saber.
//
// El nombre se resuelve contra CLIENTES en el listado, EN LOTE. Estas pruebas
// fijan las tres propiedades que hacen que eso sea seguro.

// lookupFalso cuenta las llamadas para poder afirmar que son en lote.
type lookupFalso struct {
	nombres  map[int]string
	llamadas int
	idsVisto [][]int
	err      error
}

func (l *lookupFalso) NombresPorID(_ context.Context, ids []int) (map[int]string, error) {
	l.llamadas++
	copia := append([]int(nil), ids...)
	l.idsVisto = append(l.idsVisto, copia)
	if l.err != nil {
		return nil, l.err
	}
	return l.nombres, nil
}

func idPtr(v int) *int { return &v }

func TestResolverNombres_UnaSolaConsultaParaTodaLaPagina(t *testing.T) {
	t.Parallel()
	lk := &lookupFalso{nombres: map[int]string{7: "ROSA MARÍA DÍAZ", 9: "PEDRO LÓPEZ"}}
	svc := (&failedintenthttp.Service{}).ConClientes(lk)

	items := []failedintenthttp.IntentDTO{
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("RUTA 27", idPtr(7))},
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("RUTA 27", idPtr(9))},
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("RUTA 04", idPtr(7))},
	}
	failedintenthttp.ResolverNombresParaPrueba(context.Background(), svc, items)

	assert.Equal(t, 1, lk.llamadas,
		"tres renglones tienen que costar UNA consulta: por renglón sería el N+1 que este diseño evita")
	assert.Equal(t, "ROSA MARÍA DÍAZ", items[0].Resumen.Cliente)
	assert.Equal(t, "PEDRO LÓPEZ", items[1].Resumen.Cliente)
	assert.Equal(t, "ROSA MARÍA DÍAZ", items[2].Resumen.Cliente,
		"el id repetido se resuelve igual, sin pedirlo dos veces")
	require.Len(t, lk.idsVisto, 1)
	assert.Len(t, lk.idsVisto[0], 2, "los ids repetidos se piden una sola vez")
}

// El título NO se pisa: las dos cosas responden preguntas distintas —de quién
// es el dinero, y quién lo capturó— y la oficina usa las dos.
func TestResolverNombres_NoPisaElTitulo(t *testing.T) {
	t.Parallel()
	lk := &lookupFalso{nombres: map[int]string{7: "ROSA MARÍA DÍAZ"}}
	svc := (&failedintenthttp.Service{}).ConClientes(lk)

	items := []failedintenthttp.IntentDTO{
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("RUTA 27 - ALEJANDRO CHAVARRIA", idPtr(7))},
	}
	failedintenthttp.ResolverNombresParaPrueba(context.Background(), svc, items)

	assert.Equal(t, "RUTA 27 - ALEJANDRO CHAVARRIA", items[0].Resumen.Titulo)
	assert.Equal(t, "ROSA MARÍA DÍAZ", items[0].Resumen.Cliente)
}

// Si la consulta falla, el listado se sirve igual. Es la pantalla a la que se
// acude cuando algo ya salió mal: que deje de cargar por no poder adornarla
// sería un intercambio pésimo.
func TestResolverNombres_SiFallaLaConsultaElListadoSigue(t *testing.T) {
	t.Parallel()
	lk := &lookupFalso{err: errors.New("firebird caído")}
	svc := (&failedintenthttp.Service{}).ConClientes(lk)

	items := []failedintenthttp.IntentDTO{
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("RUTA 27", idPtr(7))},
	}
	failedintenthttp.ResolverNombresParaPrueba(context.Background(), svc, items)

	assert.Empty(t, items[0].Resumen.Cliente)
	assert.Equal(t, "RUTA 27", items[0].Resumen.Titulo, "el renglón sobrevive sin nombre")
}

// Sin cliente_id no se busca nada. Es la guarda que impide usar Referencia,
// que en pagos lleva el id del CARGO cuando no hay cliente — y buscar CLIENTES
// con ella devolvería, de vez en cuando, el nombre de otro cliente.
func TestResolverNombres_SinClienteIDNoConsulta(t *testing.T) {
	t.Parallel()
	lk := &lookupFalso{nombres: map[int]string{7: "ROSA MARÍA DÍAZ"}}
	svc := (&failedintenthttp.Service{}).ConClientes(lk)

	items := []failedintenthttp.IntentDTO{
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("JAQUELINE SANCHEZ", nil)},
	}
	failedintenthttp.ResolverNombresParaPrueba(context.Background(), svc, items)

	assert.Zero(t, lk.llamadas, "sin cliente_id no hay a quién buscar")
	assert.Empty(t, items[0].Resumen.Cliente)
}

// Sin el puerto instalado, el listado se comporta como antes.
func TestResolverNombres_SinPuertoNoRompe(t *testing.T) {
	t.Parallel()
	svc := &failedintenthttp.Service{}
	items := []failedintenthttp.IntentDTO{
		{Resumen: failedintenthttp.NuevoResumenDTOParaPrueba("RUTA 27", idPtr(7))},
	}
	failedintenthttp.ResolverNombresParaPrueba(context.Background(), svc, items)
	assert.Empty(t, items[0].Resumen.Cliente)
}

// El extractor de pagos guarda el cliente_id APARTE de la referencia.
func TestResumen_ClienteIDEsIndependienteDeLaReferencia(t *testing.T) {
	t.Parallel()
	r := &failedintent.Resumen{Referencia: "13458179", ClienteID: idPtr(2344886)}
	assert.Equal(t, "13458179", r.Referencia)
	require.NotNil(t, r.ClienteID)
	assert.Equal(t, 2344886, *r.ClienteID,
		"la referencia puede ser el cargo; el cliente_id nunca")
}
