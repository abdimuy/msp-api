// Package app_test contains tests for application commands.
package app_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// relojBase is the instant setupService starts the test clock at; seeded folios
// are built with it so their rows and the clock the commands read never disagree.
var relojBase = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// opcionesSemilla is what a seeded folio needs beyond the state it stands in:
// the origin (R1 decides which edges a folio may walk) and, when a command
// requires one, the repair route already recorded on its articles.
type opcionesSemilla struct {
	origen domain.OrigenFolio
	ruta   *domain.RutaReparacion
}

// piso is the floor-origin seed almost every command test uses.
func piso() opcionesSemilla { return opcionesSemilla{origen: domain.OrigenFolioPiso} }

// cliente is the customer-origin seed; its trunk runs through the collection
// stages and it — not the floor folio — may reach entregado.
func cliente() opcionesSemilla { return opcionesSemilla{origen: domain.OrigenFolioCliente} }

// conRuta returns a seed option whose articles already carry the repair route.
// RegistrarDictamen only accepts a folio whose route was recorded first.
func conRuta(o opcionesSemilla, ruta domain.RutaReparacion) opcionesSemilla {
	o.ruta = &ruta
	return o
}

// semilla is a folio already stored in the fake repository, with the ids of the
// articles it was seeded with.
type semilla struct {
	g    *domain.Garantia
	arts []uuid.UUID
}

// art returns the id of the i-th seeded article.
func (s semilla) art(i int) uuid.UUID { return s.arts[i] }

// primero returns the id of the first seeded article.
func (s semilla) primero() uuid.UUID { return s.arts[0] }

// articuloDe returns the article with the given id inside an aggregate. The
// repository hands out clones and keeps the stored rows, so state is asserted on
// the folio the command returned, never on the semilla it was built from.
func articuloDe(t *testing.T, g *domain.Garantia, id uuid.UUID) *domain.Articulo {
	t.Helper()
	for _, a := range g.ArticulosForRepo() {
		if a.ID() == id {
			return a
		}
	}
	t.Fatalf("el articulo %s no esta en el folio", id)
	return nil
}

// reemplazoDe returns the replacement article a physical swap appends to the
// folio; there is exactly one.
func reemplazoDe(t *testing.T, g *domain.Garantia) *domain.Articulo {
	t.Helper()
	for _, a := range g.ArticulosForRepo() {
		if a.Rol() == domain.RolArticuloReemplazo {
			return a
		}
	}
	t.Fatal("el cambio fisico no dejo articulo de reemplazo")
	return nil
}

// eventosEscritos returns the events the command left pending on the aggregate
// it handed to the store. The repository stores and returns clones without
// pending events, so this is the only place they survive.
func eventosEscritos(t *testing.T, repo *fakeGarantiaRepo) []*domain.Evento {
	t.Helper()
	if repo.lastSaved == nil {
		t.Fatal("el comando no llego a guardar")
	}
	return repo.lastSaved.EventosPendientesForRepo()
}

// eventoEscrito returns the single event a command recorded.
func eventoEscrito(t *testing.T, repo *fakeGarantiaRepo) *domain.Evento {
	t.Helper()
	eventos := eventosEscritos(t, repo)
	if len(eventos) != 1 {
		t.Fatalf("el comando dejo %d eventos, want 1", len(eventos))
	}
	return eventos[0]
}

// sembrar stores a folio standing exactly where a command expects it, with one
// article per requested stage.
//
// The folio is built with HydrateGarantia/HydrateArticulo — the same
// constructors the real repository uses to rebuild a row — so a command test
// never walks the stage machine. That machine is covered by the domain package;
// walking it here would fail this test for a reason unrelated to the command
// being tested.
func sembrar(t *testing.T, repo *fakeGarantiaRepo, estado domain.EstadoFolio, ops opcionesSemilla, etapas ...domain.Etapa) semilla {
	t.Helper()
	id := uuid.New()
	folio, err := domain.NewFolio(3000 + repo.seeded)
	repo.seeded++
	if err != nil {
		t.Fatalf("NewFolio: %v", err)
	}
	arts := make([]*domain.Articulo, 0, len(etapas))
	ids := make([]uuid.UUID, 0, len(etapas))
	for i, etapa := range etapas {
		artID := uuid.New()
		ids = append(ids, artID)
		arts = append(arts, domain.HydrateArticulo(domain.HydrateArticuloParams{
			ID:          artID,
			GarantiaID:  id,
			Rol:         domain.RolArticuloOriginal,
			Clave:       string(rune('A' + i)),
			Description: "articulo " + string(rune('A'+i)),
			Ruta:        ops.ruta,
			Etapa:       etapa,
			Ubicacion:   ubicacionDeEtapa(etapa),
			CreatedAt:   relojBase,
			UpdatedAt:   relojBase,
		}))
	}
	g := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:          id,
		Folio:       folio,
		Origen:      ops.origen,
		Estado:      estado,
		Description: "silla de oficina desarmada",
		AbiertoPor:  "Juan",
		CreatedAt:   relojBase,
		UpdatedAt:   relojBase,
		Articulos:   arts,
	})
	repo.obtained[id] = g
	repo.forUpdate[id] = g
	repo.byFolio[folio] = g
	return semilla{g: g, arts: ids}
}

// ubicacionDeEtapa mirrors the R3 table (transiciones.go) so a seeded article
// already holds the location its stage implies — Cancelar's guard (R5) reads it.
func ubicacionDeEtapa(e domain.Etapa) domain.Ubicacion {
	switch e { //nolint:exhaustive // partial on purpose: the stages absent from the table mean "no location change"
	case domain.EtapaRecolectado:
		return domain.UbicacionEnTransito
	case domain.EtapaEnviadoProveedor:
		return domain.UbicacionProveedor
	case domain.EtapaEnTaller:
		return domain.UbicacionTaller
	case domain.EtapaEntregado:
		return domain.UbicacionEntregado
	case domain.EtapaSegundaMano:
		return domain.UbicacionAlmacenSegundaMano
	case domain.EtapaDesarmado, domain.EtapaMerma:
		return domain.UbicacionBaja
	default:
		return domain.UbicacionAlmacenRevision
	}
}

// rolDecisorPrueba is the decider role a command test sends; the domain only
// requires that one was sent, never which.
func rolDecisorPrueba() string { return string(domain.RolDecisorOficina) }

// gpsPrueba returns the coordinates of a delivery; Entregar refuses to run
// without them (R9).
func gpsPrueba() (*float64, *float64) {
	lat, lon := 19.4326, -99.1332
	return &lat, &lon
}

// sinEscrituras reports that a command never reached the transaction or the
// store: no transaction was opened and nothing was staged by Guardar or Crear.
func sinEscrituras(tx *fakeTxRunner, repo *fakeGarantiaRepo) bool {
	return tx.calls == 0 && len(repo.saved) == 0 && len(repo.created) == 0
}

// sinFilas reports that nothing reached the store, even though the transaction
// may have opened: an idempotency key refused inside it must roll back without
// leaving the folio rows behind.
func sinFilas(repo *fakeGarantiaRepo) bool {
	return len(repo.saved) == 0 && len(repo.created) == 0
}
