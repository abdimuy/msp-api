// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// firmaMaxBytes is the size cap of the conformity signature (spec §4.1).
const firmaMaxBytes = 1 << 20

// prefijoFirmas is where the signature blobs live under STORAGE_DIR.
const prefijoFirmas = "garantias/firmas"

// EntregarCmd contains input to hand the folio to the client.
//
// Firma is the raw image the client signed; FirmaContentType is what the
// caller declared, kept for the transport layer. The domain never sees the
// bytes — app stores the blob first and hands the aggregate its relative path.
type EntregarCmd struct {
	GarantiaID        uuid.UUID
	Firma             []byte
	FirmaContentType  string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// Entregar hands the folio to the client. The agent's GPS is the delivery
// audit of §4.1, so a delivery without coordinates is refused before the
// transaction opens: the folio is not even locked for a request that cannot be
// delivered. The client's signature is required for the same reason.
//
// The blob is written before the transaction and cleaned up afterwards, always:
// only the attempt whose image row actually landed keeps its file. The cleanup
// runs detached from the request's cancellation (context.WithoutCancel) so a
// dropped connection cannot leave an orphan behind.
func (s *Service) Entregar(ctx context.Context, cmd EntregarCmd) (*domain.Garantia, error) {
	if cmd.GPSLat == nil || cmd.GPSLon == nil {
		return nil, domain.ErrEntregaGPSRequerido
	}
	if len(cmd.Firma) == 0 {
		return nil, domain.ErrFirmaRequerida
	}
	huella := huellaDe(cmd)
	// The image id comes from the canonical key, exactly like the domain
	// derives it. An invalid key yields uuid.Nil on purpose: the blob is still
	// written and then discarded after ejecutarFolio fails with the key
	// sentinel, so identity and permission keep deciding the error first.
	imagenID := firmaIDDe(cmd.ClaveIdempotencia)
	ruta, err := s.guardarBlob(ctx, prefijoFirmas, cmd.Firma, firmaMaxBytes)
	if err != nil {
		return nil, err
	}
	defer s.descartarBlobSiNoQuedo(context.WithoutCancel(ctx), imagenID, ruta)

	return s.ejecutarFolio(ctx, cmd.GarantiaID, domain.PermisoCerrar,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon, huella),
		huella,
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return g.Entregar(ruta, actor, s.clock.Now())
		})
}

// firmaIDDe returns the image id the signature will use: FirmaImagenID over
// the canonical idempotency key. An invalid key returns uuid.Nil, which can
// never match a stored image and therefore makes the cleanup delete the blob.
func firmaIDDe(clave string) uuid.UUID {
	canonica, err := normalizarClave(clave)
	if err != nil {
		return uuid.Nil
	}
	return domain.FirmaImagenID(canonica)
}
