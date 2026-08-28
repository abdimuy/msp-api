// Package app implements the flota module's only operation: take one
// photograph of the roster, compare it against the last one, and record what
// moved.
//
// # How this is queried
//
// There is no HTTP endpoint and no screen, deliberately. The question this
// module answers — "did somebody's camioneta change after that venta was
// captured?" — is asked a handful of times a month, by an operator who is
// already looking at a specific venta, and it is answered by a SELECT:
//
//	SELECT c.DETECTADO_EN, c.VENTANA_DESDE, c.TIPO, c.NOMBRE, c.EMAIL,
//	       c.CAMIONETA_ANTERIOR, c.CAMIONETA_NUEVA
//	  FROM MSP_FLOTA_ASIGNACION_CAMBIOS c
//	 WHERE c.DETECTADO_EN >= '2026-08-01'
//	 ORDER BY c.DETECTADO_EN DESC;
//
// Narrow it with `AND c.EMAIL = '...'` for one person, or with
// `AND (c.CAMIONETA_ANTERIOR = 11341 OR c.CAMIONETA_NUEVA = 11341)` for one
// truck. Before concluding "nothing changed", check the ledger — an empty
// result only means something if the snapshots were actually running:
//
//	SELECT MAX(EJECUTADO_EN), COUNT(*) FROM MSP_FLOTA_FOTOS
//	 WHERE EJECUTADO_EN >= '2026-08-01';
//
// Building a screen for that would be building something nobody asked for.
// If the question starts being asked weekly, the endpoint is a thin read over
// two tables and can be added then.
package app

import (
	"context"
	"time"

	flotadomain "github.com/abdimuy/msp-api/internal/flota/domain"
	"github.com/abdimuy/msp-api/internal/flota/ports/outbound"
)

// Service takes roster photographs.
type Service struct {
	roster outbound.RosterReader
	repo   outbound.Repo
	tx     outbound.TxRunner
	clock  outbound.Clock
}

// NewService wires a Service against its four ports.
func NewService(
	roster outbound.RosterReader,
	repo outbound.Repo,
	tx outbound.TxRunner,
	clock outbound.Clock,
) *Service {
	return &Service{roster: roster, repo: repo, tx: tx, clock: clock}
}

// Resultado summarises one pass, for the worker's log line and for tests.
type Resultado struct {
	// LineaBase reports whether this pass was the first-ever snapshot, which
	// records the starting state and emits no changes.
	LineaBase bool
	// UsuariosObservados is how many roster entries the photograph contained.
	UsuariosObservados int
	// Altas, Actualizaciones and Bajas count the mirror writes.
	Altas           int
	Actualizaciones int
	Bajas           int
	// CambiosDetectados is how many history rows were appended.
	CambiosDetectados int
}

// TomarFoto photographs the roster and records the difference.
//
// Ordering matters and is not incidental: the roster is read BEFORE the
// transaction opens. A Firestore call can hang for as long as its deadline
// allows, and holding a Firebird transaction open across it would pin the
// oldest active transaction for the whole wait — the exact shape of the bug
// that froze the cobranza watermark. If the read fails, nothing has been
// touched and there is nothing to unwind.
func (s *Service) TomarFoto(ctx context.Context) (Resultado, error) {
	crudos, err := s.roster.LeerRoster(ctx)
	if err != nil {
		return Resultado{}, err
	}
	observadas, err := aObservaciones(crudos)
	if err != nil {
		return Resultado{}, err
	}

	ahora := s.clock.Now()

	var res Resultado
	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		var errTx error
		res, errTx = s.registrar(ctx, observadas, ahora)
		return errTx
	})
	if err != nil {
		return Resultado{}, err
	}
	return res, nil
}

// registrar runs the whole comparison inside the caller's transaction.
func (s *Service) registrar(
	ctx context.Context, observadas []flotadomain.Observacion, ahora time.Time,
) (Resultado, error) {
	ventanaDesde, hayPrevia, err := s.repo.UltimaFoto(ctx)
	if err != nil {
		return Resultado{}, err
	}
	previas, err := s.repo.ListarAsignaciones(ctx)
	if err != nil {
		return Resultado{}, err
	}

	plan, err := flotadomain.Comparar(previas, observadas, flotadomain.ParametrosComparacion{
		// The baseline is decided by the ledger, not by the mirror being
		// empty. See ParametrosComparacion.LineaBase for why the difference
		// matters.
		LineaBase:    !hayPrevia,
		VentanaDesde: ventanaDesde,
		DetectadoEn:  ahora,
	})
	if err != nil {
		return Resultado{}, err
	}
	if err := s.aplicar(ctx, plan); err != nil {
		return Resultado{}, err
	}

	// The ledger row is written on every pass, including the ones that
	// changed nothing — that is what makes a later absence of changes a
	// finding instead of a guess.
	foto := flotadomain.NuevaFoto(ahora, len(observadas), len(plan.Cambios), !hayPrevia)
	if err := s.repo.RegistrarFoto(ctx, foto); err != nil {
		return Resultado{}, err
	}

	return Resultado{
		LineaBase:          !hayPrevia,
		UsuariosObservados: len(observadas),
		Altas:              len(plan.Altas),
		Actualizaciones:    len(plan.Actualizaciones),
		Bajas:              len(plan.Bajas),
		CambiosDetectados:  len(plan.Cambios),
	}, nil
}

// aplicar writes the plan. History goes in before the mirror moves, so that a
// failure between the two statements can only ever leave history without a
// mirror update (harmless: the next pass detects the same change again),
// never a mirror that advanced past a change nobody recorded. In practice the
// whole thing is one transaction and neither happens.
func (s *Service) aplicar(ctx context.Context, plan flotadomain.Plan) error {
	if plan.SinEscrituras() {
		return nil
	}
	if err := s.repo.InsertarCambios(ctx, plan.Cambios); err != nil {
		return err
	}
	if err := s.repo.InsertarAsignaciones(ctx, plan.Altas); err != nil {
		return err
	}
	if err := s.repo.ActualizarAsignaciones(ctx, plan.Actualizaciones); err != nil {
		return err
	}
	return s.repo.EliminarAsignaciones(ctx, plan.Bajas)
}

// aObservaciones normalises the raw roster into domain observations. A single
// unusable entry fails the whole pass rather than being skipped: the only way
// to get here is an adapter that returned an entry with no identifier, and
// silently dropping that person would make them look like a baja.
func aObservaciones(crudos []outbound.RosterUsuario) ([]flotadomain.Observacion, error) {
	observadas := make([]flotadomain.Observacion, 0, len(crudos))
	for _, u := range crudos {
		o, err := flotadomain.NuevaObservacion(
			u.UID, u.Email, u.Nombre, flotadomain.CamionetaDesdeRoster(u.Camioneta),
		)
		if err != nil {
			return nil, err
		}
		observadas = append(observadas, o)
	}
	return observadas, nil
}
