//nolint:misspell // Spanish domain vocabulary (directorio, clientes, etc.) by project convention.
package outbound

import (
	"context"

	"github.com/shopspring/decimal"
)

// DirectorioDoc is the ports-level contract document for the Meilisearch
// directory index. It is a plain struct with no JSON tags and no meilisearch
// import — infra maps it to the wire shape (clientessearch.ClienteDoc).
//
// Fields mirror clientessearch.ClienteDoc one-to-one so the mapping in the
// infra adapter is trivial. Pulse-derived fields are zero-valued when
// TienePulso is false.
type DirectorioDoc struct {
	// Identity fields from domain.Cliente.
	ClienteID  int
	Nombre     string
	ZonaID     int
	ZonaNombre string
	CobradorID int
	Estatus    string
	Telefono   string

	// Address fields from domain.Direccion.
	Direccion          string // searchable full-text (calle + colonia + poblacion)
	DireccionCalle     string
	DireccionColonia   string
	DireccionPoblacion string
	DireccionCorta     string // Direccion.Corta() one-liner

	// Balance.
	Saldo    decimal.Decimal
	ConSaldo bool

	// Pulse-derived fields. Zero when TienePulso is false.
	Score           int
	Segmento        string
	EstadoPago      string
	RecenciaDias    int
	Frecuencia      int
	Monetary        decimal.Decimal
	NextBestProduct string
	TienePulso      bool
}

// DirectoryIndex is the outbound port for the Meilisearch customer directory
// index. The implementation lives in infra/clientessearch.
type DirectoryIndex interface {
	// Reconciliar bulk-upserts the full active client set into the index.
	// It is an additive reconcile only: clients that transition out of ESTATUS
	// A/B are near-zero in practice and out-of-scope for deletion here.
	Reconciliar(ctx context.Context, docs []DirectorioDoc) error
}
