//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// telefonoSuffixLen is the number of trailing digits compared when matching
// an inbound WhatsApp sender against the cohorte's stored teléfono. Meta
// sends the sender as digits-only WITH the country code (e.g.
// "5212381234567"); MSP_RX_COHORTE.TELEFONO is copied verbatim from
// DIRS_CLIENTES.TELEFONO1, the store's local format (e.g. "238 123 4567").
// Comparing only the trailing 10 digits — the Mexican national significant
// number length since the 2021 dialing-plan simplification — matches
// regardless of country code, the historical mobile "1" prefix, or any
// spacing/punctuation on either side.
const telefonoSuffixLen = 10

// ResolverClienteIDPorTelefono finds the cliente_id the piloto cohorte
// associates with an inbound WhatsApp sender's phone number.
//
// Scoped DELIBERATELY to MSP_RX_COHORTE (the reactivación piloto's own
// snapshot), never to the live Microsip directory: ADR-0010 §4 restricts
// this channel to the bounded fact set the cohorte push already carries,
// and the piloto itself only ever contacts cohorte members — a message
// from anyone else is out of scope for this channel by design, not an
// oversight.
//
// wamid is used only for the WARN log line on failure, so an operator can
// correlate the rejection back to the exact row in the VPS's mailbox.
//
// Returns ErrTelefonoNoResuelto when telefono matches no cohorte row (an
// unrecognised number, or a real cliente who is simply not in the piloto),
// and ErrTelefonoAmbiguo when it matches more than one (e.g. a shared
// household landline). Neither is swallowed: both are logged here, and the
// caller (reactivacionhttp) turns them into a permanent (not retried) HTTP
// response — the inbound message itself is never lost, since it stays in
// the VPS's durable mailbox as EstadoReenvioFallido regardless of what this
// store returns.
func (s *Service) ResolverClienteIDPorTelefono(ctx context.Context, telefono, wamid string) (int, error) {
	const source = "reactivacion.ResolverClienteIDPorTelefono"

	suffix := telefonoSuffix(telefono)
	if suffix == "" {
		s.logger.WarnContext(ctx, "reactivacion_resolver_telefono.invalido",
			slog.String("wamid", wamid))
		return 0, ErrTelefonoInvalido
	}

	cohorte, err := s.repo.ListarCohorte(ctx, outbound.ListarCohorteParams{})
	if err != nil {
		return 0, apperror.NewInternal("resolver_telefono_listar_cohorte_failed", "error al leer la cohorte").
			WithSource(source).WithError(err)
	}

	clienteID, err := matchTelefono(cohorte, suffix)
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_resolver_telefono.no_resuelto",
			slog.String("wamid", wamid), slog.String("telefono", telefono), slog.String("error", err.Error()))
		return 0, err
	}
	return clienteID, nil
}

// matchTelefono scans cohorte for every row whose teléfono shares suffix and
// returns the single match, or the appropriate sentinel when there is none
// or more than one.
func matchTelefono(cohorte []*domain.CohorteCliente, suffix string) (int, error) {
	clienteID := 0
	matches := 0
	for _, c := range cohorte {
		if telefonoSuffix(c.Telefono()) != suffix {
			continue
		}
		matches++
		clienteID = c.ClienteID()
	}
	switch matches {
	case 0:
		return 0, ErrTelefonoNoResuelto
	case 1:
		return clienteID, nil
	default:
		return 0, ErrTelefonoAmbiguo
	}
}

// telefonoSuffix strips every non-digit rune from telefono and returns its
// trailing telefonoSuffixLen digits, or "" when fewer than that many digits
// remain — too short to identify a Mexican national number.
func telefonoSuffix(telefono string) string {
	digits := make([]byte, 0, len(telefono))
	for _, r := range telefono {
		if r >= '0' && r <= '9' {
			digits = append(digits, byte(r))
		}
	}
	if len(digits) < telefonoSuffixLen {
		return ""
	}
	return string(digits[len(digits)-telefonoSuffixLen:])
}
