//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// fixedClock is a controllable Clock returning a fixed instant.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// fakeUniversoReader returns a preset universe (or a preset error).
type fakeUniversoReader struct {
	universo []outbound.ClienteUniverso
	err      error
	// gate, when non-nil, blocks LeerUniversoTehuacan until it is closed. Used to
	// exercise the single-flight guard of ConstruirEnSegundoPlano.
	gate <-chan struct{}
	// calls counts how many times LeerUniversoTehuacan was invoked. Atomic so the
	// test goroutine can poll it while a background build runs.
	calls atomic.Int32
}

// Calls returns the number of LeerUniversoTehuacan invocations.
func (r *fakeUniversoReader) Calls() int { return int(r.calls.Load()) }

func (r *fakeUniversoReader) LeerUniversoTehuacan(_ context.Context) ([]outbound.ClienteUniverso, error) {
	r.calls.Add(1)
	if r.gate != nil {
		<-r.gate
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.universo, nil
}

// fakeCohorteRepo is an in-memory CohorteRepo. It captures upserts and serves
// preset flags and cohorte rows.
type fakeCohorteRepo struct {
	controlFlags    map[int]bool
	contactadoFlags map[int]bool
	listResult      []*domain.CohorteCliente

	controlErr          error
	contactadoErr       error
	upsertErr           error
	listErr             error
	marcarContactadoErr error

	// listGate, when non-nil, blocks ListarCohorte until it is closed. Used to
	// exercise the single-flight guard of EncolarEnSegundoPlano.
	listGate <-chan struct{}
	// listCalls counts how many times ListarCohorte was invoked. Atomic so the
	// test goroutine can poll it while a background run is in flight.
	listCalls atomic.Int32

	mu           sync.Mutex
	upserted     []*domain.CohorteCliente
	lastListParm outbound.ListarCohorteParams
	contactados  []contactadoCall
}

// ListCalls returns the number of ListarCohorte invocations.
func (r *fakeCohorteRepo) ListCalls() int { return int(r.listCalls.Load()) }

func (r *fakeCohorteRepo) UpsertCohorte(_ context.Context, cohorte []*domain.CohorteCliente) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upserted = append(r.upserted, cohorte...)
	return nil
}

// upsertedCount returns the number of rows captured so far (thread-safe).
func (r *fakeCohorteRepo) upsertedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.upserted)
}

func (r *fakeCohorteRepo) ListarCohorte(_ context.Context, p outbound.ListarCohorteParams) ([]*domain.CohorteCliente, error) {
	r.listCalls.Add(1)
	if r.listGate != nil {
		<-r.listGate
	}
	r.lastListParm = p
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listResult, nil
}

func (r *fakeCohorteRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	if r.controlErr != nil {
		return nil, r.controlErr
	}
	if r.controlFlags == nil {
		return map[int]bool{}, nil
	}
	return r.controlFlags, nil
}

func (r *fakeCohorteRepo) ExistingContactadoFlags(_ context.Context) (map[int]bool, error) {
	if r.contactadoErr != nil {
		return nil, r.contactadoErr
	}
	if r.contactadoFlags == nil {
		return map[int]bool{}, nil
	}
	return r.contactadoFlags, nil
}

// MarcarContactado records the clienteID/now pair marked contacted and
// serves marcarContactadoErr when set (so tests can exercise error
// propagation from EnvioService.DrenarCola).
func (r *fakeCohorteRepo) MarcarContactado(_ context.Context, clienteID int, now time.Time) error {
	if r.marcarContactadoErr != nil {
		return r.marcarContactadoErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contactados = append(r.contactados, contactadoCall{clienteID: clienteID, now: now})
	return nil
}

// contactadosCount returns the number of MarcarContactado calls so far
// (thread-safe).
func (r *fakeCohorteRepo) contactadosCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.contactados)
}

// wasContactado reports whether clienteID was passed to MarcarContactado.
func (r *fakeCohorteRepo) wasContactado(clienteID int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.contactados {
		if c.clienteID == clienteID {
			return true
		}
	}
	return false
}

// contactadoCall records one MarcarContactado invocation.
type contactadoCall struct {
	clienteID int
	now       time.Time
}

// ─── fakeMensajeRepo ────────────────────────────────────────────────────────

// fakeMensajeRepo is an in-memory outbound.MensajeRepo. It captures inserted
// mensajes and lets tests preset/inspect state for EncolarCohorte/DrenarCola.
type fakeMensajeRepo struct {
	insertarErr           error
	listarPendientesErr   error
	actualizarErr         error
	listarErr             error
	contarEnviadosHoyErr  error
	clientesConMensajeErr error

	// clientesConMensaje presets the set returned by ClientesConMensaje. When
	// nil, it is derived from insertados (updated as Insertar is called).
	clientesConMensaje map[int]bool

	mu          sync.Mutex
	insertados  []*domain.Mensaje
	actualizado []*domain.Mensaje
}

func (r *fakeMensajeRepo) Insertar(_ context.Context, mensajes []*domain.Mensaje) error {
	if r.insertarErr != nil {
		return r.insertarErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insertados = append(r.insertados, mensajes...)
	return nil
}

// insertadosCount returns the number of mensajes captured by Insertar so far
// (thread-safe).
func (r *fakeMensajeRepo) insertadosCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.insertados)
}

// insertadosSnapshot returns a copy of the captured mensajes (thread-safe).
func (r *fakeMensajeRepo) insertadosSnapshot() []*domain.Mensaje {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*domain.Mensaje, len(r.insertados))
	copy(out, r.insertados)
	return out
}

func (r *fakeMensajeRepo) ListarPendientes(_ context.Context, limit int) ([]*domain.Mensaje, error) {
	if r.listarPendientesErr != nil {
		return nil, r.listarPendientesErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Mensaje
	for _, m := range r.insertados {
		if m.Estado() == domain.EstadoEncolado {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeMensajeRepo) Actualizar(_ context.Context, m *domain.Mensaje) error {
	if r.actualizarErr != nil {
		return r.actualizarErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actualizado = append(r.actualizado, m)
	return nil
}

// actualizadoCount returns the number of Actualizar calls so far (thread-safe).
func (r *fakeMensajeRepo) actualizadoCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.actualizado)
}

func (r *fakeMensajeRepo) Listar(_ context.Context, p outbound.ListarMensajesParams) ([]*domain.Mensaje, error) {
	if r.listarErr != nil {
		return nil, r.listarErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Mensaje
	for _, m := range r.insertados {
		if p.Estado != "" && m.Estado() != p.Estado {
			continue
		}
		if p.Segmento != "" && m.Segmento() != p.Segmento {
			continue
		}
		out = append(out, m)
	}
	if p.Limit > 0 && len(out) > p.Limit {
		out = out[:p.Limit]
	}
	return out, nil
}

func (r *fakeMensajeRepo) ContarEnviadosHoy(_ context.Context, desde time.Time) (int, error) {
	if r.contarEnviadosHoyErr != nil {
		return 0, r.contarEnviadosHoyErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, m := range r.insertados {
		if m.Estado() == domain.EstadoEnviado && !m.EnviadoEn().Before(desde) {
			count++
		}
	}
	return count, nil
}

func (r *fakeMensajeRepo) ClientesConMensaje(_ context.Context) (map[int]bool, error) {
	if r.clientesConMensajeErr != nil {
		return nil, r.clientesConMensajeErr
	}
	if r.clientesConMensaje != nil {
		return r.clientesConMensaje, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[int]bool, len(r.insertados))
	for _, m := range r.insertados {
		out[m.ClienteID()] = true
	}
	return out, nil
}

// ─── fakeSender ─────────────────────────────────────────────────────────────

// fakeSender is an in-memory outbound.MessageSender. It records every Enviar
// call and can be configured to fail (always, or only for specific
// clienteIDs) so tests can exercise both the success and failure paths of
// EnvioService.DrenarCola.
type fakeSender struct {
	kind domain.SenderKind

	// err, when non-nil, is returned by every Enviar call.
	err error
	// failClienteIDs marks specific clienteIDs whose Enviar call fails.
	failClienteIDs map[int]bool

	mu      sync.Mutex
	enviado []outbound.Destino
}

func (s *fakeSender) Enviar(_ context.Context, dest outbound.Destino, _ string) error {
	s.mu.Lock()
	s.enviado = append(s.enviado, dest)
	s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.failClienteIDs[dest.ClienteID] {
		return errFakeSenderRejected
	}
	return nil
}

func (s *fakeSender) Kind() domain.SenderKind {
	if s.kind == "" {
		return domain.SenderSimulado
	}
	return s.kind
}

// enviadoCount returns the number of Enviar calls so far (thread-safe).
func (s *fakeSender) enviadoCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.enviado)
}

// errFakeSenderRejected is the sentinel error returned for clienteIDs listed
// in fakeSender.failClienteIDs.
var errFakeSenderRejected = errors.New("fake sender: mensaje rechazado")
