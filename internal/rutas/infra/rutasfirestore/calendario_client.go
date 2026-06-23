//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfirestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Compile-time check.
var _ outbound.CalendarioCobradorClient = (*CalendarioClient)(nil)

// usersCollection is the Firestore collection holding cobrador profiles.
const usersCollection = "users"

// CalendarioClient reads FECHA_CARGA_INICIAL + COBRADOR_ID from the Firestore
// `users` collection and returns a COBRADOR_ID → time.Time map.
type CalendarioClient struct {
	fs *firestore.Client
}

// NewCalendarioClient builds a CalendarioClient backed by the given Firestore client.
func NewCalendarioClient(fs *firestore.Client) *CalendarioClient {
	return &CalendarioClient{fs: fs}
}

// FechaInicioPorCobrador iterates all documents in the `users` collection and
// returns a map from COBRADOR_ID (int) to FECHA_CARGA_INICIAL (time.Time UTC).
// Documents that lack either field are silently skipped.
// A Firestore read error is surfaced — the caller (app service) treats it as
// "no calendar" and returns nil metrics without failing the request.
func (c *CalendarioClient) FechaInicioPorCobrador(ctx context.Context) (map[int]time.Time, error) {
	docs, err := c.fs.Collection(usersCollection).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	result := make(map[int]time.Time, len(docs))
	for _, doc := range docs {
		data := doc.Data()

		cobradorRaw, ok := data["COBRADOR_ID"]
		if !ok {
			continue
		}
		cobradorID, ok := toInt(cobradorRaw)
		if !ok || cobradorID <= 0 {
			continue
		}

		fechaRaw, ok := data["FECHA_CARGA_INICIAL"]
		if !ok {
			continue
		}
		t, ok := toTime(fechaRaw)
		if !ok {
			continue
		}
		result[cobradorID] = t.UTC()
	}
	return result, nil
}

// toInt converts a Firestore numeric value (float64 or int64) to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// toTime converts a Firestore Timestamp value to time.Time.
func toTime(v any) (time.Time, bool) {
	t, ok := v.(time.Time)
	return t, ok
}

// NoopCalendarioClient returns an empty map. Used in dev mode / unconfigured.
// Compile-time check.
var _ outbound.CalendarioCobradorClient = NoopCalendarioClient{}

// NoopCalendarioClient is the fallback when Firestore is unavailable.
type NoopCalendarioClient struct{}

// FechaInicioPorCobrador always returns an empty map without error.
func (NoopCalendarioClient) FechaInicioPorCobrador(_ context.Context) (map[int]time.Time, error) {
	return map[int]time.Time{}, nil
}
