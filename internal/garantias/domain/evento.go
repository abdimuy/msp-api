package domain

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Evento is the immutable timeline entry of a garantía folio. Every fact the
// folio registers becomes one row in MSP_GA_EVENTO (spec §3.3, §4.4). It has
// no setters and no audit embed: it is created once and never mutated. All
// NOT NULL columns of the table are enforced here so the repository can
// never persist an incomplete row (CLAUDE.md §1).
type Evento struct {
	id                uuid.UUID
	garantiaID        uuid.UUID
	articuloRef       *uuid.UUID
	tipo              TipoEvento
	description       string
	etapaDesde        *Etapa
	etapaHasta        *Etapa
	ubicacionDesde    *Ubicacion
	ubicacionHasta    *Ubicacion
	usuario           string
	rolDecisor        *RolDecisor
	gpsLat            *float64
	gpsLon            *float64
	createdAt         time.Time
	deviceCreatedAt   time.Time
	claveIdempotencia string
}

// EventoParams carries everything newEvento needs. The caller (the
// aggregate) supplies the actor, the idempotency key and the device
// timestamp; the aggregate never invents them (decision 8).
type EventoParams struct {
	GarantiaID        uuid.UUID
	ArticuloRef       *uuid.UUID
	Tipo              TipoEvento
	Description       string
	EtapaDesde        *Etapa
	EtapaHasta        *Etapa
	UbicacionDesde    *Ubicacion
	UbicacionHasta    *Ubicacion
	Usuario           string
	RolDecisor        *RolDecisor
	GPSLat, GPSLon    *float64
	CreatedAt         time.Time
	DeviceCreatedAt   time.Time
	ClaveIdempotencia string
}

// newEvento is the package-private constructor used by the aggregate. The
// event ID is generated here via uuid.New(). Tipo is a validated closed enum
// fed by typed constants, so an invalid value can never reach this function.
func newEvento(p EventoParams) (*Evento, error) {
	usuario := normalizarNFC(strings.TrimSpace(p.Usuario))
	if usuario == "" {
		return nil, ErrEventoUsuarioObligatorio
	}
	if utf8.RuneCountInString(usuario) > 64 {
		return nil, ErrEventoUsuarioMuyLargo
	}
	clave := strings.TrimSpace(p.ClaveIdempotencia)
	if clave == "" {
		return nil, ErrEventoClaveIdempotenciaObligatoria
	}
	// uuid.Parse also accepts "{…}", "urn:uuid:…" and the 32 hex digits
	// without dashes. All four spell the same retry, but stored verbatim
	// they would be four different values for the UNIQUE index and
	// idempotency would silently stop working. The canonical String()
	// form is always 36 lowercase ASCII characters, so it also fits the
	// CHAR(36) column without truncating.
	parsed, err := uuid.Parse(clave)
	if err != nil {
		return nil, ErrEventoClaveIdempotenciaInvalida
	}
	if p.RolDecisor != nil && !p.RolDecisor.IsValid() {
		return nil, ErrRolDecisorInvalido
	}
	if p.DeviceCreatedAt.IsZero() {
		return nil, ErrEventoDeviceCreatedAtObligatorio
	}
	if !gpsValido(p.GPSLat, p.GPSLon) {
		return nil, ErrEventoGPSInvalido
	}
	return &Evento{
		id:                uuid.New(),
		garantiaID:        p.GarantiaID,
		articuloRef:       p.ArticuloRef,
		tipo:              p.Tipo,
		description:       normalizarNFC(p.Description),
		etapaDesde:        p.EtapaDesde,
		etapaHasta:        p.EtapaHasta,
		ubicacionDesde:    p.UbicacionDesde,
		ubicacionHasta:    p.UbicacionHasta,
		usuario:           usuario,
		rolDecisor:        p.RolDecisor,
		gpsLat:            p.GPSLat,
		gpsLon:            p.GPSLon,
		createdAt:         p.CreatedAt,
		deviceCreatedAt:   p.DeviceCreatedAt,
		claveIdempotencia: parsed.String(),
	}, nil
}

// espacioDeEventos is the fixed namespace under which the derived keys of a
// multi-event command are minted. It exists so that Entregar can write one
// event per article without every one of them carrying the command's single
// CLAVE_IDEMPOTENCIA: the column is UNIQUE, and two articles delivered by the
// same call would otherwise collide on the second INSERT.
var espacioDeEventos = uuid.MustParse("6f6b0a7c-1f2e-5a3d-9b4c-7d8e9f0a1b2c")

// claveDerivada mints the idempotency key of an extra event inside a command
// that already owns una clave. It is a UUIDv5 of the command's key and a
// discriminator, so it is deterministic: the same retry of the same command
// produces the same keys and the UNIQUE index still answers "ya lo hice"
// instead of writing the delivery twice. The caller keeps the original key for
// the event its command is named after, which is the one the app's replay
// lookup reads (revisarClave compares TIPO against it).
func claveDerivada(clave, discriminator string) string {
	return uuid.NewSHA1(espacioDeEventos, []byte(clave+"\x00"+discriminator)).String()
}

// gpsValido reports whether the event's coordinates are acceptable. They are
// optional, but they come as a pair (spec §3.3): half a position would be
// stored as a lat with no lon and read back as a place off the coast of
// Africa. Each value must also be in its own range, or the same argument
// holds for a lat of 200.
func gpsValido(lat, lon *float64) bool {
	if lat == nil && lon == nil {
		return true
	}
	if lat == nil || lon == nil {
		return false
	}
	if math.IsNaN(*lat) || math.IsNaN(*lon) || math.IsInf(*lat, 0) || math.IsInf(*lon, 0) {
		return false
	}
	return *lat >= -90 && *lat <= 90 && *lon >= -180 && *lon <= 180
}

// HydrateEventoParams is the persisted shape used by the repository over
// Firebird.
type HydrateEventoParams struct {
	ID                uuid.UUID
	GarantiaID        uuid.UUID
	ArticuloRef       *uuid.UUID
	Tipo              TipoEvento
	Description       string
	EtapaDesde        *Etapa
	EtapaHasta        *Etapa
	UbicacionDesde    *Ubicacion
	UbicacionHasta    *Ubicacion
	Usuario           string
	RolDecisor        *RolDecisor
	GPSLat, GPSLon    *float64
	CreatedAt         time.Time
	DeviceCreatedAt   time.Time
	ClaveIdempotencia string
}

// HydrateEvento rebuilds an Evento from a persisted row without any
// validation; the repository only calls it with rows it already wrote.
func HydrateEvento(p HydrateEventoParams) *Evento {
	return &Evento{
		id:                p.ID,
		garantiaID:        p.GarantiaID,
		articuloRef:       p.ArticuloRef,
		tipo:              p.Tipo,
		description:       p.Description,
		etapaDesde:        p.EtapaDesde,
		etapaHasta:        p.EtapaHasta,
		ubicacionDesde:    p.UbicacionDesde,
		ubicacionHasta:    p.UbicacionHasta,
		usuario:           p.Usuario,
		rolDecisor:        p.RolDecisor,
		gpsLat:            p.GPSLat,
		gpsLon:            p.GPSLon,
		createdAt:         p.CreatedAt,
		deviceCreatedAt:   p.DeviceCreatedAt,
		claveIdempotencia: p.ClaveIdempotencia,
	}
}

// ID returns the event's primary key.
func (e *Evento) ID() uuid.UUID { return e.id }

// GarantiaID returns the folio the event belongs to.
func (e *Evento) GarantiaID() uuid.UUID { return e.garantiaID }

// ArticuloRef returns the article the event refers to, when article-scoped.
func (e *Evento) ArticuloRef() *uuid.UUID { return e.articuloRef }

// Tipo returns the event type.
func (e *Evento) Tipo() TipoEvento { return e.tipo }

// Description returns the free-form event note.
func (e *Evento) Description() string { return e.description }

// EtapaDesde returns the origin stage for stage-bearing events, if any.
func (e *Evento) EtapaDesde() *Etapa { return e.etapaDesde }

// EtapaHasta returns the destination stage for stage-bearing events, if any.
func (e *Evento) EtapaHasta() *Etapa { return e.etapaHasta }

// UbicacionDesde returns the origin location for location-bearing events, if
// any. A move that does not change where the item is leaves it nil.
func (e *Evento) UbicacionDesde() *Ubicacion { return e.ubicacionDesde }

// UbicacionHasta returns the destination location for location-bearing events,
// if any.
func (e *Evento) UbicacionHasta() *Ubicacion { return e.ubicacionHasta }

// Usuario returns the actor who registered the event.
func (e *Evento) Usuario() string { return e.usuario }

// RolDecisor returns the decision role of the actor, when it matters.
func (e *Evento) RolDecisor() *RolDecisor { return e.rolDecisor }

// GPSLat returns the event latitude, if captured.
func (e *Evento) GPSLat() *float64 { return e.gpsLat }

// GPSLon returns the event longitude, if captured.
func (e *Evento) GPSLon() *float64 { return e.gpsLon }

// CreatedAt returns the event timestamp (UTC).
func (e *Evento) CreatedAt() time.Time { return e.createdAt }

// DeviceCreatedAt returns the device-reported creation timestamp.
func (e *Evento) DeviceCreatedAt() time.Time { return e.deviceCreatedAt }

// ClaveIdempotencia returns the idempotency key.
func (e *Evento) ClaveIdempotencia() string { return e.claveIdempotencia }
