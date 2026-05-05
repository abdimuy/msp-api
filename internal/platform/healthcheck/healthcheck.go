// Package healthcheck composes named probes into HTTP /healthz and /readyz
// handlers.
//
// Convention:
//
//   - /healthz: liveness only. 200 if the process is alive. No probes.
//   - /readyz : readiness. 200 only if every registered probe passes; 503 otherwise.
package healthcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Probe is a single dependency check.
type Probe interface {
	// Name uniquely identifies the probe (e.g. "postgres", "firebird", "firebase").
	Name() string
	// Check returns nil when healthy, otherwise an error describing the failure.
	Check(ctx context.Context) error
}

// ProbeFunc adapts a (name, function) pair to the Probe interface.
type ProbeFunc struct {
	N string
	F func(ctx context.Context) error
}

// Name returns the probe name.
func (p ProbeFunc) Name() string { return p.N }

// Check invokes the wrapped function.
func (p ProbeFunc) Check(ctx context.Context) error { return p.F(ctx) }

// Service is the registry of probes evaluated by the readiness handler.
type Service struct {
	mu     sync.RWMutex
	probes []Probe
}

// New returns an empty Service.
func New() *Service { return &Service{} }

// Register adds a probe.
func (s *Service) Register(p Probe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes = append(s.probes, p)
}

// Liveness handles GET /healthz — always 200 with a simple body.
//
// This must NEVER touch any dependency. If liveness fails, orchestrators
// (Windows Service, k8s, nssm) will restart the process.
func (s *Service) Liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}

// Readiness handles GET /readyz — runs every probe in parallel with a 5s
// budget. 200 only when all pass; 503 with the failed probe details otherwise.
func (s *Service) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	s.mu.RLock()
	probes := append([]Probe(nil), s.probes...)
	s.mu.RUnlock()

	type result struct {
		name string
		err  error
	}
	ch := make(chan result, len(probes))
	for _, p := range probes {
		go func(p Probe) { ch <- result{name: p.Name(), err: p.Check(ctx)} }(p)
	}

	body := readinessBody{Status: "ok", Probes: map[string]string{}}
	allOK := true
	for range probes {
		r := <-ch
		if r.err != nil {
			body.Probes[r.name] = r.err.Error()
			allOK = false
			continue
		}
		body.Probes[r.name] = "ok"
	}

	if !allOK {
		body.Status = "unhealthy"
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

type statusBody struct {
	Status string `json:"status"`
}

type readinessBody struct {
	Status string            `json:"status"`
	Probes map[string]string `json:"probes"`
}

// writeJSON marshals first so we can react to encoding failure before
// committing a status code, satisfying errchkjson on `any` payloads.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
