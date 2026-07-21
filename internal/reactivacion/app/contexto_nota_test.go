//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var contextoNotaNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

// internalFixedClock is a minimal outbound.Clock for internal (package app)
// tests — the external fakes in fakes_test.go live in package app_test and
// are not visible here.
type internalFixedClock struct{ now time.Time }

func (c internalFixedClock) Now() time.Time { return c.now }

// internalNotaReader is a minimal outbound.NotaReader for internal tests.
type internalNotaReader struct {
	nota string
	err  error
}

func (r internalNotaReader) GetNotaCliente(_ context.Context, _ int) (string, error) {
	return r.nota, r.err
}

// internalCopilotoLLM is a minimal outbound.CopilotoLLM for internal tests —
// only DestilarNota is exercised by asegurarContextoNota; the other methods
// are unused stubs.
type internalCopilotoLLM struct {
	destilarOut   outbound.NotaOutput
	destilarErr   error
	destilarCalls int
}

func (f *internalCopilotoLLM) Analizar(context.Context, outbound.AnalizarInput) (outbound.AnalizarOutput, error) {
	return outbound.AnalizarOutput{}, nil
}

func (f *internalCopilotoLLM) DestilarNota(_ context.Context, _ outbound.NotaInput) (outbound.NotaOutput, error) {
	f.destilarCalls++
	if f.destilarErr != nil {
		return outbound.NotaOutput{}, f.destilarErr
	}
	return f.destilarOut, nil
}

func (f *internalCopilotoLLM) Redactar(context.Context, outbound.RedactarInput) (string, error) {
	return "", nil
}

func newContextoNotaService(notaReader outbound.NotaReader, llm outbound.CopilotoLLM) *Service {
	s := NewService(nil, nil, internalFixedClock{now: contextoNotaNow}, nil, Config{})
	return s.WithCopiloto(nil, nil, notaReader, llm, nil)
}

func newTestConversacion(t *testing.T) *domain.Conversacion {
	t.Helper()
	conv, err := domain.CrearConversacion(24037, contextoNotaNow)
	require.NoError(t, err)
	return conv
}

func TestAsegurarContextoNota_EmptyNota_NoOp(t *testing.T) {
	t.Parallel()
	llm := &internalCopilotoLLM{}
	svc := newContextoNotaService(internalNotaReader{nota: ""}, llm)
	conv := newTestConversacion(t)

	svc.asegurarContextoNota(context.Background(), conv, "Juan Pérez", "recien_liquidado", contextoNotaNow)
	assert.Empty(t, conv.ContextoNota())
	assert.Empty(t, conv.NotaHash())
	assert.Zero(t, llm.destilarCalls)
}

func TestAsegurarContextoNota_DistillsOnce_ThenCacheHits(t *testing.T) {
	t.Parallel()
	llm := &internalCopilotoLLM{destilarOut: outbound.NotaOutput{
		Contexto: "cliente prefiere pagos los viernes",
		Banderas: []string{"prefiere_viernes"},
	}}
	notaReader := internalNotaReader{nota: "paga siempre los viernes, muy puntual"}
	svc := newContextoNotaService(notaReader, llm)
	conv := newTestConversacion(t)

	svc.asegurarContextoNota(context.Background(), conv, "Juan Pérez", "recien_liquidado", contextoNotaNow)
	assert.Equal(t, "cliente prefiere pagos los viernes", conv.ContextoNota())
	assert.Equal(t, []string{"prefiere_viernes"}, conv.Banderas())
	assert.NotEmpty(t, conv.NotaHash())
	assert.Equal(t, 1, llm.destilarCalls)

	hashAfterFirst := conv.NotaHash()

	// Second call with the SAME nota (identical hash) must be a cache hit — no
	// second DestilarNota call, and the cached contexto/banderas are untouched.
	svc.asegurarContextoNota(context.Background(), conv, "Juan Pérez", "recien_liquidado", contextoNotaNow)
	assert.Equal(t, 1, llm.destilarCalls, "cache hit must not call DestilarNota again")
	assert.Equal(t, hashAfterFirst, conv.NotaHash())
}

func TestAsegurarContextoNota_NotaChanged_RedistillsAndUpdatesHash(t *testing.T) {
	t.Parallel()
	llm := &internalCopilotoLLM{destilarOut: outbound.NotaOutput{Contexto: "primera destilación"}}
	notaReader := internalNotaReader{nota: "nota original"}
	svc := newContextoNotaService(notaReader, llm)
	conv := newTestConversacion(t)

	svc.asegurarContextoNota(context.Background(), conv, "", "", contextoNotaNow)
	firstHash := conv.NotaHash()

	llm.destilarOut = outbound.NotaOutput{Contexto: "segunda destilación"}
	svc.notaReader = internalNotaReader{nota: "nota cambió"}

	svc.asegurarContextoNota(context.Background(), conv, "", "", contextoNotaNow)
	assert.Equal(t, 2, llm.destilarCalls)
	assert.Equal(t, "segunda destilación", conv.ContextoNota())
	assert.NotEqual(t, firstHash, conv.NotaHash())
}

func TestAsegurarContextoNota_NotaReaderError_DegradesSilently(t *testing.T) {
	t.Parallel()
	llm := &internalCopilotoLLM{}
	svc := newContextoNotaService(internalNotaReader{err: errors.New("boom")}, llm)
	conv := newTestConversacion(t)

	svc.asegurarContextoNota(context.Background(), conv, "", "", contextoNotaNow)
	assert.Empty(t, conv.ContextoNota())
	assert.Zero(t, llm.destilarCalls)
}

func TestAsegurarContextoNota_LLMError_DegradesWithoutUpdatingHash(t *testing.T) {
	t.Parallel()
	llm := &internalCopilotoLLM{destilarErr: errors.New("llm down")}
	notaReader := internalNotaReader{nota: "una nota cualquiera"}
	svc := newContextoNotaService(notaReader, llm)
	conv := newTestConversacion(t)

	svc.asegurarContextoNota(context.Background(), conv, "", "", contextoNotaNow)
	assert.Empty(t, conv.ContextoNota())
	assert.Empty(t, conv.NotaHash(), "hash must NOT update on LLM error, so a later retry re-attempts distillation")
}

func TestAsegurarContextoNota_UsesProvidedNow_NotClock(t *testing.T) {
	t.Parallel()
	// Regression: asegurarContextoNota must stamp SetContextoNota with the
	// EXPLICIT now argument threaded in by the caller, not a second,
	// independent s.clock.Now() read — one point-in-time snapshot per
	// ProcesarMensajeEntrante call. The service's clock is fixed at
	// contextoNotaNow; we pass a DIFFERENT explicit now to prove which one
	// wins.
	llm := &internalCopilotoLLM{destilarOut: outbound.NotaOutput{Contexto: "algo"}}
	svc := newContextoNotaService(internalNotaReader{nota: "una nota"}, llm)
	conv := newTestConversacion(t)

	explicitNow := contextoNotaNow.Add(48 * time.Hour)
	svc.asegurarContextoNota(context.Background(), conv, "", "", explicitNow)

	assert.True(t, conv.UpdatedAt().Equal(explicitNow),
		"UpdatedAt must equal the explicit now argument, not the service clock's fixed value")
	assert.False(t, conv.UpdatedAt().Equal(contextoNotaNow),
		"UpdatedAt must NOT come from a second, independent s.clock.Now() read")
}
