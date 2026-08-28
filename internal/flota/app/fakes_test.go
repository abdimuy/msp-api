//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package app_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	flotadomain "github.com/abdimuy/msp-api/internal/flota/domain"
	"github.com/abdimuy/msp-api/internal/flota/ports/outbound"
)

// errFirestore stands in for any transport failure of the roster read.
var errFirestore = errors.New("firestore no responde")

// errEscritura stands in for a mid-plan database failure.
var errEscritura = errors.New("firebird rechazó la escritura")

// intPtr is the roster's nullable camioneta field.
func intPtr(n int) *int { return &n }

// ── reloj ────────────────────────────────────────────────────────────────────

// relojFijo is a Clock that returns a value the test controls, so every
// assertion about a window is exact instead of approximate.
type relojFijo struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relojFijo) Now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relojFijo) avanzar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

// ── roster ───────────────────────────────────────────────────────────────────

// rosterFake serves a scripted sequence of photographs. Each call to
// LeerRoster consumes the next one; the last is repeated once exhausted so a
// worker ticking freely does not run off the end.
type rosterFake struct {
	mu     sync.Mutex
	fotos  [][]outbound.RosterUsuario
	idx    int
	err    error
	leidas int
}

func nuevoRosterFake(fotos ...[]outbound.RosterUsuario) *rosterFake {
	return &rosterFake{fotos: fotos}
}

func (r *rosterFake) LeerRoster(context.Context) ([]outbound.RosterUsuario, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leidas++
	if r.err != nil {
		return nil, r.err
	}
	if len(r.fotos) == 0 {
		return nil, nil
	}
	foto := r.fotos[min(r.idx, len(r.fotos)-1)]
	r.idx++
	return foto, nil
}

// Leidas returns how many full-roster reads have been issued — the module's
// Firestore bill, in the unit Firestore charges in.
func (r *rosterFake) Leidas() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.leidas
}

// fallarCon makes every subsequent read fail; nil restores service.
func (r *rosterFake) fallarCon(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// ── repositorio ──────────────────────────────────────────────────────────────

// repoFake is an in-memory outbound.Repo. It keeps the mirror in a map and
// the history and ledger in slices, exactly as the Firebird tables do, so the
// service can be exercised end to end without a database.
//
// fallarEn makes a chosen method fail, which is how the "does not leave the
// table half written" test is built: combined with txFake's rollback, the
// state must come back untouched.
type repoFake struct {
	mu       sync.Mutex
	espejo   map[string]*flotadomain.Asignacion
	cambios  []*flotadomain.Cambio
	fotos    []*flotadomain.Foto
	fallarEn string
}

func nuevoRepoFake(previas ...*flotadomain.Asignacion) *repoFake {
	r := &repoFake{espejo: make(map[string]*flotadomain.Asignacion, len(previas))}
	for _, a := range previas {
		r.espejo[a.UsuarioUID()] = a
	}
	return r
}

func (r *repoFake) falla(metodo string) error {
	if r.fallarEn == metodo {
		return errEscritura
	}
	return nil
}

func (r *repoFake) UltimaFoto(context.Context) (time.Time, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("UltimaFoto"); err != nil {
		return time.Time{}, false, err
	}
	if len(r.fotos) == 0 {
		return time.Time{}, false, nil
	}
	return r.fotos[len(r.fotos)-1].EjecutadoEn(), true, nil
}

func (r *repoFake) ListarAsignaciones(context.Context) ([]*flotadomain.Asignacion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("ListarAsignaciones"); err != nil {
		return nil, err
	}
	uids := make([]string, 0, len(r.espejo))
	for uid := range r.espejo {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	out := make([]*flotadomain.Asignacion, 0, len(uids))
	for _, uid := range uids {
		out = append(out, r.espejo[uid])
	}
	return out, nil
}

func (r *repoFake) InsertarAsignaciones(_ context.Context, altas []*flotadomain.Asignacion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("InsertarAsignaciones"); err != nil {
		return err
	}
	for _, a := range altas {
		r.espejo[a.UsuarioUID()] = a
	}
	return nil
}

func (r *repoFake) ActualizarAsignaciones(_ context.Context, cambiadas []*flotadomain.Asignacion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("ActualizarAsignaciones"); err != nil {
		return err
	}
	for _, a := range cambiadas {
		r.espejo[a.UsuarioUID()] = a
	}
	return nil
}

func (r *repoFake) EliminarAsignaciones(_ context.Context, uids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("EliminarAsignaciones"); err != nil {
		return err
	}
	for _, uid := range uids {
		delete(r.espejo, uid)
	}
	return nil
}

func (r *repoFake) InsertarCambios(_ context.Context, cambios []*flotadomain.Cambio) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("InsertarCambios"); err != nil {
		return err
	}
	r.cambios = append(r.cambios, cambios...)
	return nil
}

func (r *repoFake) RegistrarFoto(_ context.Context, foto *flotadomain.Foto) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("RegistrarFoto"); err != nil {
		return err
	}
	r.fotos = append(r.fotos, foto)
	return nil
}

// Cambios returns a copy of the history rows written so far.
func (r *repoFake) Cambios() []*flotadomain.Cambio {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.cambios)
}

// Fotos returns a copy of the ledger rows written so far.
func (r *repoFake) Fotos() []*flotadomain.Foto {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.fotos)
}

// romperEn makes the named repo method fail on every subsequent call.
func (r *repoFake) romperEn(metodo string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallarEn = metodo
}

// uids returns the mirror's keys, sorted, for assertions.
func (r *repoFake) uids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.espejo))
	for uid := range r.espejo {
		out = append(out, uid)
	}
	slices.Sort(out)
	return out
}

// camionetaDe returns the mirrored camioneta id of one person, or 0 when they
// have none, and false when they are not mirrored at all.
func (r *repoFake) camionetaDe(uid string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.espejo[uid]
	if !ok {
		return 0, false
	}
	return a.Camioneta().ID(), true
}

// ── transacción ──────────────────────────────────────────────────────────────

// txFake models the transaction boundary with real rollback semantics: it
// snapshots the repo before running fn and restores it when fn fails. Without
// that, a test asserting "a failed pass leaves nothing half written" would be
// asserting against a fake that cannot fail the way production does.
type txFake struct {
	repo      *repoFake
	Ejecutada int
	Revertida int
}

func (tx *txFake) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx.Ejecutada++
	antes := tx.repo.snapshot()
	if err := fn(ctx); err != nil {
		tx.Revertida++
		tx.repo.restaurar(antes)
		return err
	}
	return nil
}

// estado is a deep-enough copy of the fake repo for rollback.
type estado struct {
	espejo  map[string]*flotadomain.Asignacion
	cambios []*flotadomain.Cambio
	fotos   []*flotadomain.Foto
}

func (r *repoFake) snapshot() estado {
	r.mu.Lock()
	defer r.mu.Unlock()
	espejo := make(map[string]*flotadomain.Asignacion, len(r.espejo))
	for k, v := range r.espejo {
		// Copy the entity too: Comparar mutates the *Asignacion in place when
		// it plans an update, so keeping the pointer would let a rolled-back
		// pass leak its mutation into the restored state.
		espejo[k] = flotadomain.HidratarAsignacion(flotadomain.HidratarAsignacionParams{
			UsuarioUID: v.UsuarioUID(),
			Email:      v.Email(),
			Nombre:     v.Nombre(),
			Camioneta:  v.Camioneta(),
			CreatedAt:  v.Audit().CreatedAt(),
			UpdatedAt:  v.Audit().UpdatedAt(),
		})
	}
	return estado{
		espejo:  espejo,
		cambios: slices.Clone(r.cambios),
		fotos:   slices.Clone(r.fotos),
	}
}

func (r *repoFake) restaurar(e estado) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.espejo = e.espejo
	r.cambios = e.cambios
	r.fotos = e.fotos
}

// ── constructor de escenarios ────────────────────────────────────────────────

// usuario builds one raw roster entry.
func usuario(uid, email, nombre string, camioneta *int) outbound.RosterUsuario {
	return outbound.RosterUsuario{UID: uid, Email: email, Nombre: nombre, Camioneta: camioneta}
}
