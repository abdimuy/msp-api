package domain

import (
	"strings"
	"time"

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
	if strings.TrimSpace(p.Usuario) == "" {
		return nil, ErrEventoUsuarioObligatorio
	}
	if strings.TrimSpace(p.ClaveIdempotencia) == "" {
		return nil, ErrEventoClaveIdempotenciaObligatoria
	}
	if p.DeviceCreatedAt.IsZero() {
		return nil, ErrEventoDeviceCreatedAtObligatorio
	}
	return &Evento{
		id:                uuid.New(),
		garantiaID:        p.GarantiaID,
		articuloRef:       p.ArticuloRef,
		tipo:              p.Tipo,
		description:       p.Description,
		etapaDesde:        p.EtapaDesde,
		etapaHasta:        p.EtapaHasta,
		usuario:           strings.TrimSpace(p.Usuario),
		rolDecisor:        p.RolDecisor,
		gpsLat:            p.GPSLat,
		gpsLon:            p.GPSLon,
		createdAt:         p.CreatedAt,
		deviceCreatedAt:   p.DeviceCreatedAt,
		claveIdempotencia: p.ClaveIdempotencia,
	}, nil
}

// EventoParamsOut is the persisted shape used by the repository over Firebird.
type EventoParamsOut struct {
	ID                uuid.UUID
	GarantiaID        uuid.UUID
	ArticuloRef       *uuid.UUID
	Tipo              TipoEvento
	Description       string
	EtapaDesde        *Etapa
	EtapaHasta        *Etapa
	Usuario           string
	RolDecisor        *RolDecisor
	GPSLat, GPSLon    *float64
	CreatedAt         time.Time
	DeviceCreatedAt   time.Time
	ClaveIdempotencia string
}

// HydrateEvento rebuilds an Evento from a persisted row without any
// validation; the repository only calls it with rows it already wrote.
func HydrateEvento(p EventoParamsOut) *Evento {
	return &Evento{
		id:                p.ID,
		garantiaID:        p.GarantiaID,
		articuloRef:       p.ArticuloRef,
		tipo:              p.Tipo,
		description:       p.Description,
		etapaDesde:        p.EtapaDesde,
		etapaHasta:        p.EtapaHasta,
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
