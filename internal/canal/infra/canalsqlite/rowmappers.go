package canalsqlite

import (
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// rowScanner is the minimal surface satisfied by both *sql.Row and
// *sql.Rows — the scan helper below works against either.
type rowScanner interface {
	Scan(dest ...any) error
}

// mensajeRowRaw is the intermediate scan target for one mensajes_entrantes
// row, in the exact column order of mensajeColumns.
type mensajeRowRaw struct {
	idRaw            string
	wamid            string
	remitente        string
	phoneNumberID    string
	tipo             string
	contenido        string
	timestampMetaRaw string
	recibidoEnRaw    string
	estadoRaw        string
	motivoFallo      string
	createdAtRaw     string
	updatedAtRaw     string
}

// scanMensaje reads one row into a MensajeEntrante via
// domain.RehydrateMensajeEntrante — zero validation, since every value
// already passed it once on the way in.
func scanMensaje(row rowScanner) (*domain.MensajeEntrante, error) {
	var r mensajeRowRaw
	err := row.Scan(
		&r.idRaw, &r.wamid, &r.remitente, &r.phoneNumberID, &r.tipo, &r.contenido,
		&r.timestampMetaRaw, &r.recibidoEnRaw, &r.estadoRaw, &r.motivoFallo,
		&r.createdAtRaw, &r.updatedAtRaw,
	)
	if err != nil {
		return nil, mapError(err)
	}
	return assembleMensaje(&r)
}

// assembleMensaje parses every raw column and rebuilds the entity.
func assembleMensaje(r *mensajeRowRaw) (*domain.MensajeEntrante, error) {
	id, err := parseUUIDColumn("id", r.idRaw)
	if err != nil {
		return nil, err
	}
	estado, err := domain.ParseEstadoReenvio(r.estadoRaw)
	if err != nil {
		return nil, err
	}
	timestampMeta, err := parseUTC("timestamp_meta", r.timestampMetaRaw)
	if err != nil {
		return nil, err
	}
	recibidoEn, err := parseUTC("recibido_en", r.recibidoEnRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := parseUTC("created_at", r.createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseUTC("updated_at", r.updatedAtRaw)
	if err != nil {
		return nil, err
	}
	return domain.RehydrateMensajeEntrante(domain.RehydrateMensajeEntranteParams{
		ID:            id,
		Wamid:         r.wamid,
		Remitente:     r.remitente,
		PhoneNumberID: r.phoneNumberID,
		Tipo:          r.tipo,
		Contenido:     r.contenido,
		TimestampMeta: timestampMeta,
		RecibidoEn:    recibidoEn,
		Estado:        estado,
		MotivoFallo:   r.motivoFallo,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}), nil
}

// parseUUIDColumn converts a TEXT column holding a UUID string, mirroring
// the same-named helper in every Firebird repo (e.g.
// internal/reactivacion/infra/reactivacionfb/rowmappers.go) — canal keeps
// its own copy because it is a sealed module (ADR-0009) and may not import
// another module's infra.
func parseUUIDColumn(column, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewInternal(
			"canal_mailbox_uuid_invalid",
			"uuid inválido en columna del buzón",
		).WithSource("canalsqlite").WithError(err).WithField("column", column).WithField("raw_value", raw)
	}
	return id, nil
}
