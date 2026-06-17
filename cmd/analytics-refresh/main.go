// Command analytics-refresh triggers a synchronous rebuild of the analytics
// read model (MSP_AN_WINBACK_CANDIDATOS), including the cobranza signals and
// RFM anchors, by calling Service.RefrescarCandidatos directly — no HTTP, no
// auth. It exists so an operator/developer can materialize analytics in a dev
// or on-prem environment without minting a Firebase token for the protected
// POST /analytics/winback/refresh endpoint.
//
// Dev/ops tooling (mirrors cmd/seed-cobrador). Writes to the configured
// Firebird DB — take a snapshot first (make fb-snapshot).
//
// Usage:
//
//	source .env && go run ./cmd/analytics-refresh           # incremental
//	source .env && go run ./cmd/analytics-refresh --full    # full rebuild
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	analyticsfb "github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	analyticsoutbound "github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func main() {
	full := flag.Bool("full", false, "run a full rebuild instead of an incremental refresh")
	distOnly := flag.Bool("dist", false, "skip the refresh; just log the current credit-band distribution")
	flag.Parse()

	ctx := context.Background()

	cfg, err := config.Load()
	must(err)

	pool, err := firebird.New(cfg.Firebird)
	must(err)
	must(pool.Start(ctx))
	defer func() { _ = pool.Stop(ctx) }()

	repo := analyticsfb.NewRepo(pool)
	txMgr := firebird.NewTxManager(pool.DB)
	svc := analyticsapp.NewService(repo, repo, analyticsoutbound.ProductionClock{}, txMgr)

	if !*distOnly {
		_, _ = fmt.Printf("▶ analytics refresh (full=%t) starting…\n", *full)
		start := time.Now()
		result, err := svc.RefrescarCandidatos(ctx, *full)
		must(err)
		_, _ = fmt.Printf("✔ done in %s: procesados=%d watermark=%s\n",
			time.Since(start).Round(time.Second), result.Procesados, result.Watermark.Format(time.RFC3339))
	}

	// Log the credit-band distribution over the materialized candidatos (drift
	// monitoring; also confirms the serve-time scorer is not degenerate).
	svc.LogDistribucionBandasCredito(ctx)
}

func must(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "✘ %v\n", err)
		os.Exit(1)
	}
}
