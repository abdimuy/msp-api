package garfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestGarantiaRepo_OperacionesDeEscrituraRequierenTransaccion(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	ctx := context.Background()

	numero, err := folios.Siguiente(ctx)
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	origen, err := domain.ParseOrigenFolio("cliente")
	require.NoError(t, err)

	estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
	require.NoError(t, err)

	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)

	clienteID := 930001
	ventaID := 930002

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:          folio,
		Origen:         origen,
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estadoCuenta,
		Description:    "Garantía para probar transacciones",
		Calle:          "Calle 3",
		NumeroExterior: "300",
		Colonia:        "Centro",
		Localidad:      "Puebla",
		Ciudad:         "Puebla",
		CodigoPostal:   "72000",
		AbiertoPor:     "ruben",
		Now:            now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	// Crear must fail because there is no transaction in the context.
	err = repo.Crear(ctx, g)
	require.ErrorIs(t, err, firebird.ErrNoTx)

	// Since Crear failed before writing, the warranty must not exist.
	_, err = repo.Obtener(ctx, g.ID())
	require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)

	// Guardar also requires a transaction.
	err = repo.Guardar(ctx, g)
	require.ErrorIs(t, err, firebird.ErrNoTx)

	// ObtenerParaActualizar uses WITH LOCK and also requires a transaction.
	_, err = repo.ObtenerParaActualizar(ctx, g.ID())
	require.ErrorIs(t, err, firebird.ErrNoTx)
}
