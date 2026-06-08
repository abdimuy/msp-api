package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// fixedNow is a deterministic timestamp used across all domain tests.
// Never use time.Now() in tests per DATETIME_HANDLING.md.
var fixedNow = time.Date(2026, 6, 8, 18, 0, 0, 0, time.UTC)

// validDetalles builds a minimal slice of one valid detalle input.
func validDetalles(t *testing.T) []domain.CrearTraspasoDetalleInput {
	t.Helper()
	c, err := domain.NewCantidad(decimal.NewFromInt(1))
	require.NoError(t, err)
	return []domain.CrearTraspasoDetalleInput{
		{ID: uuid.New(), ArticuloID: 1, Cantidad: c},
	}
}

// validFolio returns a valid Folio for test use.
func validFolio(t *testing.T) domain.Folio {
	t.Helper()
	f, err := domain.NewFolio("MST000001")
	require.NoError(t, err)
	return f
}

// validCrearParams returns a valid set of CrearTraspasoParams.
func validCrearParams(t *testing.T) domain.CrearTraspasoParams {
	t.Helper()
	return domain.CrearTraspasoParams{
		ID:             uuid.New(),
		Folio:          validFolio(t),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "traslado mensual",
		Detalles:       validDetalles(t),
		CreatedBy:      uuid.New(),
		Now:            fixedNow,
	}
}

func TestCrearTraspaso_HappyPath(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	tr, err := domain.CrearTraspaso(p)
	require.NoError(t, err)
	require.NotNil(t, tr)

	if tr.ID() != p.ID {
		t.Fatalf("ID mismatch: want %v got %v", p.ID, tr.ID())
	}
	if tr.AlmacenOrigen() != 1 {
		t.Fatalf("almacen_origen mismatch")
	}
	if tr.AlmacenDestino() != 2 {
		t.Fatalf("almacen_destino mismatch")
	}
	if tr.TipoReverso() {
		t.Fatal("new traspaso should not be tipoReverso=true")
	}
	if tr.DoctoInID() != nil {
		t.Fatal("new traspaso should have nil DoctoInID")
	}
	if tr.VentaID() != nil {
		t.Fatal("expected nil VentaID when not provided")
	}

	evs := tr.PendingEvents()
	if len(evs) != 1 {
		t.Fatalf("expected 1 pending event, got %d", len(evs))
	}
	if evs[0].EventType() != domain.EventTypeTraspasoCreado {
		t.Fatalf("expected event type %q, got %q", domain.EventTypeTraspasoCreado, evs[0].EventType())
	}
}

func TestCrearTraspaso_WithVentaID(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	vid := uuid.New()
	p.VentaID = &vid
	tr, err := domain.CrearTraspaso(p)
	require.NoError(t, err)
	if tr.VentaID() == nil || *tr.VentaID() != vid {
		t.Fatal("ventaID not set correctly")
	}
}

func TestCrearTraspaso_RejectsAlmacenOrigenZero(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.AlmacenOrigen = 0
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestCrearTraspaso_RejectsAlmacenOrigenNegative(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.AlmacenOrigen = -1
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestCrearTraspaso_RejectsAlmacenDestinoZero(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.AlmacenDestino = 0
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestCrearTraspaso_RejectsAlmacenDestinoNegative(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.AlmacenDestino = -5
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestCrearTraspaso_RejectsAlmacenesIguales(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.AlmacenOrigen = 3
	p.AlmacenDestino = 3
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestCrearTraspaso_RejectsSinDetalles(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.Detalles = nil
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)

	p2 := validCrearParams(t)
	p2.Detalles = []domain.CrearTraspasoDetalleInput{}
	_, err2 := domain.CrearTraspaso(p2)
	require.Error(t, err2)
}

func TestCrearTraspaso_RejectsDescripcionDemasiadoLarga(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.Descripcion = strings.Repeat("a", 201)
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestCrearTraspaso_AcceptsDescripcionExact200(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.Descripcion = strings.Repeat("a", 200)
	_, err := domain.CrearTraspaso(p)
	require.NoError(t, err)
}

func TestCrearTraspaso_TrimsDescripcion(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.Descripcion = "  hola mundo  "
	tr, err := domain.CrearTraspaso(p)
	require.NoError(t, err)
	if tr.Descripcion() != "hola mundo" {
		t.Fatalf("expected trimmed description, got %q", tr.Descripcion())
	}
}

func TestCrearTraspaso_RejectsInvalidArticuloCantidad(t *testing.T) {
	t.Parallel()
	// Zero-value Cantidad (not constructed via NewCantidad) triggers error in newDetalle.
	p := validCrearParams(t)
	p.Detalles = []domain.CrearTraspasoDetalleInput{
		{ID: uuid.New(), ArticuloID: 1, Cantidad: domain.Cantidad{}},
	}
	_, err := domain.CrearTraspaso(p)
	require.Error(t, err)
}

func TestTraspaso_Reversar_HappyPath(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.AlmacenOrigen = 1
	p.AlmacenDestino = 2
	p.Descripcion = "original"
	original, err := domain.CrearTraspaso(p)
	require.NoError(t, err)

	newFolio, _ := domain.NewFolio("MST000002")
	newID := uuid.New()
	reversedNow := fixedNow.Add(time.Hour)
	by := uuid.New()
	reversed, err := original.Reversar(reversedNow, by, newID, newFolio)
	require.NoError(t, err)
	require.NotNil(t, reversed)

	if reversed.ID() != newID {
		t.Fatalf("reversed ID mismatch")
	}
	if reversed.AlmacenOrigen() != 2 {
		t.Fatalf("expected reversed almacen_origen=2, got %d", reversed.AlmacenOrigen())
	}
	if reversed.AlmacenDestino() != 1 {
		t.Fatalf("expected reversed almacen_destino=1, got %d", reversed.AlmacenDestino())
	}
	if !reversed.TipoReverso() {
		t.Fatal("expected tipoReverso=true on reversed traspaso")
	}
	if !strings.HasPrefix(reversed.Descripcion(), "REVERSO: ") {
		t.Fatalf("expected descripcion to start with 'REVERSO: ', got %q", reversed.Descripcion())
	}

	evs := reversed.PendingEvents()
	if len(evs) != 1 || evs[0].EventType() != domain.EventTypeTraspasoReversado {
		t.Fatalf("expected one traspaso.reversado event, got %v", evs)
	}
}

func TestTraspaso_Reversar_CannotReverseAReverso(t *testing.T) {
	t.Parallel()
	original, _ := domain.CrearTraspaso(validCrearParams(t))
	newFolio, _ := domain.NewFolio("MST000002")
	reversed, _ := original.Reversar(fixedNow, uuid.New(), uuid.New(), newFolio)

	thirdFolio, _ := domain.NewFolio("MST000003")
	_, err := reversed.Reversar(fixedNow, uuid.New(), uuid.New(), thirdFolio)
	require.Error(t, err)
}

func TestTraspaso_Reversar_DetallesAreDeepCopied(t *testing.T) {
	t.Parallel()
	original, _ := domain.CrearTraspaso(validCrearParams(t))
	newFolio, _ := domain.NewFolio("MST000002")
	reversed, _ := original.Reversar(fixedNow, uuid.New(), uuid.New(), newFolio)

	var origDet, revDet *domain.TraspasoDetalle
	for d := range original.Detalles() {
		origDet = d
		break
	}
	for d := range reversed.Detalles() {
		revDet = d
		break
	}
	require.NotNil(t, origDet)
	require.NotNil(t, revDet)
	// Same data, different pointers.
	if origDet == revDet {
		t.Fatal("reversed detalles should be deep copies, not the same pointer")
	}
}

func TestTraspaso_MarcarAplicado_HappyPath(t *testing.T) {
	t.Parallel()
	tr, _ := domain.CrearTraspaso(validCrearParams(t))
	if tr.DoctoInID() != nil {
		t.Fatal("expected nil DoctoInID before MarcarAplicado")
	}
	err := tr.MarcarAplicado(999)
	require.NoError(t, err)
	if tr.DoctoInID() == nil || *tr.DoctoInID() != 999 {
		t.Fatalf("expected DoctoInID=999, got %v", tr.DoctoInID())
	}
}

func TestTraspaso_MarcarAplicado_RejectsDoubleApply(t *testing.T) {
	t.Parallel()
	tr, _ := domain.CrearTraspaso(validCrearParams(t))
	_ = tr.MarcarAplicado(1)
	err := tr.MarcarAplicado(2)
	require.Error(t, err)
}

func TestTraspaso_PendingEvents_DefensiveCopy(t *testing.T) {
	t.Parallel()
	tr, _ := domain.CrearTraspaso(validCrearParams(t))
	evs := tr.PendingEvents()
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	// Mutate the returned slice — should not affect the aggregate's buffer.
	evs[0] = nil
	evs2 := tr.PendingEvents()
	if evs2[0] == nil {
		t.Fatal("PendingEvents returned a reference to the internal slice")
	}
}

func TestTraspaso_ClearPendingEvents(t *testing.T) {
	t.Parallel()
	tr, _ := domain.CrearTraspaso(validCrearParams(t))
	tr.ClearPendingEvents()
	evs := tr.PendingEvents()
	if len(evs) != 0 {
		t.Fatalf("expected 0 events after ClearPendingEvents, got %d", len(evs))
	}
}

func TestHydrateTraspaso_NoValidation(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	folio := domain.HydrateFolio("MST999999")
	by := uuid.New()
	doctoID := 42
	tr := domain.HydrateTraspaso(domain.HydrateTraspasoParams{
		ID:             id,
		Folio:          folio,
		AlmacenOrigen:  5,
		AlmacenDestino: 6,
		Fecha:          fixedNow,
		Descripcion:    "hydrated",
		TipoReverso:    true,
		DoctoInID:      &doctoID,
		Detalles:       nil,
		CreatedAt:      fixedNow,
		UpdatedAt:      fixedNow,
		CreatedBy:      by,
		UpdatedBy:      by,
	})
	require.NotNil(t, tr)
	if tr.ID() != id {
		t.Fatalf("ID mismatch")
	}
	if !tr.TipoReverso() {
		t.Fatal("expected tipoReverso=true")
	}
	if tr.DoctoInID() == nil || *tr.DoctoInID() != 42 {
		t.Fatalf("doctoInID mismatch")
	}
	if tr.PendingEvents() != nil && len(tr.PendingEvents()) != 0 {
		t.Fatal("hydrated traspaso should have no pending events")
	}
}

func TestTraspaso_DetallesForRepo(t *testing.T) {
	t.Parallel()
	tr, _ := domain.CrearTraspaso(validCrearParams(t))
	detalles := tr.DetallesForRepo()
	if len(detalles) != 1 {
		t.Fatalf("expected 1 detalle, got %d", len(detalles))
	}
}

func TestTraspaso_Detalles_Iterator(t *testing.T) {
	t.Parallel()
	c, _ := domain.NewCantidad(decimal.NewFromInt(1))
	p := domain.CrearTraspasoParams{
		ID:             uuid.New(),
		Folio:          validFolio(t),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "test",
		Detalles: []domain.CrearTraspasoDetalleInput{
			{ID: uuid.New(), ArticuloID: 1, Cantidad: c},
			{ID: uuid.New(), ArticuloID: 2, Cantidad: c},
		},
		CreatedBy: uuid.New(),
		Now:       fixedNow,
	}
	tr, _ := domain.CrearTraspaso(p)
	count := 0
	for range tr.Detalles() {
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 detalles from iterator, got %d", count)
	}
}

func TestTraspaso_Accessors(t *testing.T) {
	t.Parallel()
	p := validCrearParams(t)
	p.Descripcion = "test descripcion"
	tr, _ := domain.CrearTraspaso(p)

	if tr.Folio().Value() != "MST000001" {
		t.Fatalf("Folio mismatch: got %q", tr.Folio().Value())
	}
	if tr.Fecha() != fixedNow {
		t.Fatalf("Fecha mismatch")
	}
	if tr.Descripcion() != "test descripcion" {
		t.Fatalf("Descripcion mismatch: got %q", tr.Descripcion())
	}
	// Audit should be set.
	a := tr.Audit()
	if a.CreatedAt().IsZero() {
		t.Fatal("Audit.CreatedAt should not be zero")
	}
}
