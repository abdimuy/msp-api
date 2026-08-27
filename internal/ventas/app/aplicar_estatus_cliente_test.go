package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── Estatus de cliente validation tests ───────────────────────────────────
//
// validarEstatusClienteMicrosipPreExistente bloquea AplicarVenta cuando el
// cliente pre-existente en Microsip no está en ESTATUS 'A' (activo) ni 'B'
// (baja). 'V' (suspensión de ventas) y 'C' (suspensión de créditos) deben
// bloquear la aplicación: la oficina debe cambiar el estatus primero.

// TestAplicarVenta_EstatusActivo_Procede verifica que un cliente en estatus
// 'A' permite aplicar la venta con normalidad.
func TestAplicarVenta_EstatusActivo_Procede(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("A")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err, "estatus 'A' no debe bloquear la aplicación")
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion(), "la venta debe quedar aplicada")
	assert.Equal(t, 1, writer.callsCount(), "el writer de microsip debe llamarse una vez")
	assert.Equal(t, 1, estatusReader.callsCount(), "el lector de estatus debe consultarse una vez")
}

// TestAplicarVenta_EstatusBaja_Procede verifica que un cliente en estatus 'B'
// (baja) sigue permitiendo aplicar la venta.
func TestAplicarVenta_EstatusBaja_Procede(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("B")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err, "estatus 'B' (baja) debe seguir permitiendo aplicar")
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion(), "la venta debe quedar aplicada")
	assert.Equal(t, 1, writer.callsCount(), "el writer de microsip debe llamarse una vez")
	assert.Equal(t, 1, estatusReader.callsCount(), "el lector de estatus debe consultarse una vez")
}

// TestAplicarVenta_EstatusSuspensionVentas_Rechazado verifica que 'V'
// (suspensión de ventas) bloquea la aplicación y NO llama al writer.
func TestAplicarVenta_EstatusSuspensionVentas_Rechazado(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("V")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrClienteEstatusNoPermiteVenta, "estatus 'V' debe bloquear con el error de estatus")
	assert.Equal(t, 0, writer.callsCount(), "el writer de microsip NO debe llamarse en suspensión de ventas")

	v, findErr := h.svc.ObtenerVenta(t.Context(), id)
	require.NoError(t, findErr, "la venta debe seguir existiendo")
	assert.Equal(t, domain.SincronizacionPendiente, v.Sincronizacion(), "la venta NO debe quedar aplicada")
}

// TestAplicarVenta_EstatusSuspensionCreditos_Rechazado verifica que 'C'
// (suspensión de créditos) bloquea la aplicación igual que 'V'.
func TestAplicarVenta_EstatusSuspensionCreditos_Rechazado(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("C")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrClienteEstatusNoPermiteVenta, "estatus 'C' debe bloquear con el error de estatus")
	assert.Equal(t, 0, writer.callsCount(), "el writer de microsip NO debe llamarse en suspensión de créditos")

	v, findErr := h.svc.ObtenerVenta(t.Context(), id)
	require.NoError(t, findErr, "la venta debe seguir existiendo")
	assert.Equal(t, domain.SincronizacionPendiente, v.Sincronizacion(), "la venta NO debe quedar aplicada")
}

// TestAplicarVenta_EstatusConEspaciosYMinusculas_Rechazado protege la
// normalización: Firebird rellena CHAR(1) con espacios y el valor puede venir
// en minúscula; " v " debe bloquear igual que "V".
func TestAplicarVenta_EstatusConEspaciosYMinusculas_Rechazado(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("  v  ")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrClienteEstatusNoPermiteVenta, "el estatus con espacios/minúsculas debe normalizarse y bloquear igual")
	assert.Equal(t, 0, writer.callsCount(), "el writer de microsip NO debe llamarse cuando el estatus normalizado bloquea")
}

// TestAplicarVenta_EstatusLecturaFalla_Rechazado verifica que un error del
// lector de estatus bloquea la aplicación (falla cerrado) en vez de
// degradarse a "todo bien".
func TestAplicarVenta_EstatusLecturaFalla_Rechazado(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("A")
	estatusReader.Err = domain.ErrClienteNotFoundInMicrosip
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrClienteNotFoundInMicrosip, "un error de lectura debe propagarse, no degradarse a nil")
	assert.Equal(t, 0, writer.callsCount(), "el writer de microsip NO debe llamarse cuando falla la lectura del estatus")
}

// TestAplicarVenta_EstatusVacio_Rechazado verifica que un estatus vacío (no
// se pudo determinar) bloquea la aplicación en vez de dejarla pasar.
func TestAplicarVenta_EstatusVacio_Rechazado(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaCredito(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrClienteEstatusNoPermiteVenta, "un estatus vacío debe bloquear, no se pudo determinar")
	assert.Equal(t, 0, writer.callsCount(), "el writer de microsip NO debe llamarse con estatus vacío")
}

// TestAplicarVenta_AutoCrea_EstatusReaderSkipped verifica que cuando la venta
// no tiene ClienteID previo (rama de auto-creación), el lector de estatus NO
// se consulta — el cliente auto-creado nace en 'A'.
func TestAplicarVenta_AutoCrea_EstatusReaderSkipped(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	estatusReader := newFakeClienteEstatusReader("V")
	h.svc = h.svc.WithEstatusReader(estatusReader)

	id := seedAprobadaSinCliente(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err, "el cliente auto-creado nace en 'A' y no debe bloquearse")
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion(), "la venta debe quedar aplicada")
	assert.Equal(t, 1, writer.callsCount(), "el writer de microsip debe llamarse para la rama de auto-creación")
	assert.Equal(t, 0, estatusReader.callsCount(), "el lector de estatus NO debe consultarse para cliente auto-creado")
}
