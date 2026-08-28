//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package main

import (
	"context"
	"log/slog"

	firebasesdk "firebase.google.com/go/v4"
	"go.uber.org/fx"
	"google.golang.org/api/option"

	flotaapp "github.com/abdimuy/msp-api/internal/flota/app"
	flotafb "github.com/abdimuy/msp-api/internal/flota/infra/flotafb"
	"github.com/abdimuy/msp-api/internal/flota/infra/flotafirestore"
	flotaoutbound "github.com/abdimuy/msp-api/internal/flota/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
)

// provideFlotaRepo builds the Firebird-backed Repo over the three tables of
// migration 000062.
func provideFlotaRepo(pool *firebird.Pool) flotaoutbound.Repo {
	return flotafb.New(pool)
}

// provideFlotaTxRunner exposes *firebird.TxManager as the module's TxRunner
// port. *firebird.TxManager satisfies it implicitly, but fx resolves by exact
// type, so the interface resolution has to be made explicit here.
func provideFlotaTxRunner(m *firebird.TxManager) flotaoutbound.TxRunner {
	return m
}

// provideFlotaClock returns the production UTC clock for the flota module.
func provideFlotaClock() flotaoutbound.Clock {
	return flotaoutbound.ProductionClock{}
}

// provideFlotaRosterReader builds the Firestore-backed roster reader.
//
// It builds its own Firebase app rather than sharing one, which is the
// established convention in this codebase (auth's NombreResolver and rutas'
// CalendarioCobradorClient do the same): each module's wiring provides a
// PORT-typed value, never a bare *firestore.Client. That is what keeps two
// modules from fighting over one fx type, and it is why adding this module
// needed no change to anybody else's wiring.
//
// When Firestore is unavailable the Noop reader is provided and the worker is
// not started, so no snapshot is ever attempted against a roster we cannot
// read.
func provideFlotaRosterReader(cfg *config.Config) flotaoutbound.RosterReader {
	if !firestoreDisponible(cfg) {
		slog.Info("flota.roster: firestore no configurado; el worker no arrancará")
		return flotafirestore.NoopRosterClient{}
	}
	ctx := context.Background()
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.Firebase.ProjectID},
		option.WithCredentialsFile(cfg.Firebase.ServiceAccountPath),
	)
	if err != nil {
		slog.Error("flota.roster: no se pudo inicializar firebase; usando noop", "error", err)
		return flotafirestore.NoopRosterClient{}
	}
	fs, err := app.Firestore(ctx)
	if err != nil {
		slog.Error("flota.roster: no se pudo obtener cliente firestore; usando noop", "error", err)
		return flotafirestore.NoopRosterClient{}
	}
	return flotafirestore.NewRosterClient(fs)
}

// firestoreDisponible reports whether there is a real Firestore to read.
func firestoreDisponible(cfg *config.Config) bool {
	return !cfg.Firebase.DevMode && cfg.Firebase.ProjectID != ""
}

// provideFlotaService assembles the flota module's snapshot service.
func provideFlotaService(
	roster flotaoutbound.RosterReader,
	repo flotaoutbound.Repo,
	tx flotaoutbound.TxRunner,
	clock flotaoutbound.Clock,
) *flotaapp.Service {
	return flotaapp.NewService(roster, repo, tx, clock)
}

// provideFlotaSnapshotWorker builds the background worker that photographs
// the roster every FLOTA_SNAPSHOT_INTERVAL (15m by default).
//
// It is enabled only when the operator asked for it AND there is a Firestore
// to read. Without the second condition a dev machine would tick against the
// Noop reader forever, logging an error every fifteen minutes.
func provideFlotaSnapshotWorker(
	svc *flotaapp.Service,
	cfg *config.Config,
	logger *slog.Logger,
) *flotaapp.SnapshotWorker {
	return flotaapp.NewSnapshotWorker(svc, flotaapp.SnapshotWorkerConfig{
		Intervalo:  cfg.Flota.SnapshotInterval,
		Habilitado: cfg.Flota.SnapshotEnabled && firestoreDisponible(cfg),
	}, logger)
}

// registerFlotaSnapshotWorkerLifecycle hooks the snapshot worker into the fx
// lifecycle so it starts with the application and drains on shutdown.
func registerFlotaSnapshotWorkerLifecycle(lc fx.Lifecycle, w *flotaapp.SnapshotWorker) {
	lifecycle.Append(lc, "flota-snapshot-worker", w)
}
