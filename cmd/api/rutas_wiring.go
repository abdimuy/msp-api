//nolint:misspell // rutas vocabulary is Spanish per project convention.
package main

import (
	"context"
	"log/slog"

	firebasesdk "firebase.google.com/go/v4"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	rutasfb "github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfirestore"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// provideRutasRepo builds the Firebird-backed RutasRepo for the rutas module.
func provideRutasRepo(pool *firebird.Pool) *rutasfb.RutasRepo {
	return rutasfb.NewRutasRepo(pool)
}

// provideCobranzaRutasRepo builds the Firebird-backed CobranzaRepo for the rutas module.
func provideCobranzaRutasRepo(pool *firebird.Pool) *rutasfb.CobranzaRepo {
	return rutasfb.NewCobranzaRepo(pool)
}

// provideCalendarioCobradorClient builds the Firestore-backed CalendarioCobradorClient.
// Returns a noop implementation when Firestore is unavailable (dev mode / unconfigured).
// Failures are logged but never fatal — missing calendar → nil metrics, not a crash.
func provideCalendarioCobradorClient(cfg *config.Config) outbound.CalendarioCobradorClient {
	if cfg.Firebase.DevMode || cfg.Firebase.ProjectID == "" {
		slog.Info("rutas.calendario: firestore no configurado; usando noop")
		return rutasfirestore.NoopCalendarioClient{}
	}
	ctx := context.Background()
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.Firebase.ProjectID},
		option.WithCredentialsFile(cfg.Firebase.ServiceAccountPath),
	)
	if err != nil {
		slog.Error("rutas.calendario: no se pudo inicializar firebase; usando noop", "error", err)
		return rutasfirestore.NoopCalendarioClient{}
	}
	fs, err := app.Firestore(ctx)
	if err != nil {
		slog.Error("rutas.calendario: no se pudo obtener cliente firestore; usando noop", "error", err)
		return rutasfirestore.NoopCalendarioClient{}
	}
	return rutasfirestore.NewCalendarioClient(fs)
}

// provideRutasService assembles the rutas read-only query service.
func provideRutasService(
	repo *rutasfb.RutasRepo,
	cobranza *rutasfb.CobranzaRepo,
	calendario outbound.CalendarioCobradorClient,
) *rutasapp.Service {
	return rutasapp.NewService(repo, cobranza, calendario)
}
