package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// RegistrarVisitaInput carries the business inputs for RegistrarVisita. Fecha
// is the client-captured visit timestamp, NOT the reference "now" — the
// service resolves "now" itself via its Clock port (per
// docs/module-standards/DATETIME_HANDLING.md, domain code never calls
// time.Now()).
type RegistrarVisitaInput struct {
	ID             uuid.UUID
	Cobrador       string
	CobradorID     int
	Fecha          time.Time
	FormaCobroID   int
	Lat            float64
	Lng            float64
	Nota           string
	TipoVisita     string
	ZonaClienteID  int
	ClienteID      int
	ImpteDoctoCCID *int
}

// RegistrarVisita builds and persists a new Visita. It is idempotent on ID:
// the mobile app generates the UUID offline and may retry the request freely
// — a duplicate ID returns the already-stored visita instead of an error, so
// a retried request and its original both resolve to the same success
// response.
func (s *Service) RegistrarVisita(ctx context.Context, in RegistrarVisitaInput, by uuid.UUID) (*domain.Visita, error) {
	v, err := domain.NewVisita(domain.CrearVisitaParams{
		ID:             in.ID,
		Cobrador:       in.Cobrador,
		CobradorID:     in.CobradorID,
		Fecha:          in.Fecha,
		FormaCobroID:   in.FormaCobroID,
		Lat:            in.Lat,
		Lng:            in.Lng,
		Nota:           in.Nota,
		TipoVisita:     in.TipoVisita,
		ZonaClienteID:  in.ZonaClienteID,
		ClienteID:      in.ClienteID,
		ImpteDoctoCCID: in.ImpteDoctoCCID,
		CreatedBy:      by,
		Now:            s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}

	if err := s.repo.Insert(ctx, v); err != nil {
		if errors.Is(err, domain.ErrVisitaYaExiste) {
			return s.repo.FindByID(ctx, in.ID)
		}
		return nil, err
	}
	return v, nil
}
