//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	cobranzamicrosip "github.com/abdimuy/msp-api/internal/cobranza/infra/microsip"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// provideCobranzaSaldosRepo builds the Firebird-backed SaldosRepo.
func provideCobranzaSaldosRepo(p *firebird.Pool) cobranzaoutbound.SaldosRepo {
	return cobranzaventfb.NewSaldosRepo(p)
}

// provideCobranzaPagosRepo builds the Firebird-backed PagosRepo.
func provideCobranzaPagosRepo(p *firebird.Pool) cobranzaoutbound.PagosRepo {
	return cobranzaventfb.NewPagosRepo(p)
}

// provideCobranzaVentasRepo builds the Firebird-backed VentasRepo (enriched
// JOIN over MSP_SALDOS_VENTAS + CLIENTES + DIRS_CLIENTES + ZONAS_CLIENTES +
// COBRADORES + LIBRES_CARGOS_CC + DOCTOS_PV).
func provideCobranzaVentasRepo(p *firebird.Pool) cobranzaoutbound.VentasRepo {
	return cobranzaventfb.NewVentasRepo(p)
}

// provideCobranzaRecomputer builds the Firebird-backed SaldosRecomputer.
// The repo is injected so the re-read step shares the same pool.
func provideCobranzaRecomputer(p *firebird.Pool, repo cobranzaoutbound.SaldosRepo) cobranzaoutbound.SaldosRecomputer {
	return cobranzaventfb.NewRecomputer(p, repo)
}

// provideCobranzaPagosRecomputer builds the Firebird-backed PagosRecomputer.
func provideCobranzaPagosRecomputer(p *firebird.Pool) cobranzaoutbound.PagosRecomputer {
	return cobranzaventfb.NewPagosRecomputer(p)
}

// provideCobranzaSaldosLister builds the Firebird-backed SaldosLister.
func provideCobranzaSaldosLister(p *firebird.Pool) cobranzaoutbound.SaldosLister {
	return cobranzaventfb.NewSaldosLister(p)
}

// provideCobranzaPagosLister builds the Firebird-backed PagosLister.
func provideCobranzaPagosLister(p *firebird.Pool) cobranzaoutbound.PagosLister {
	return cobranzaventfb.NewPagosLister(p)
}

// provideCobranzaErrorsRepo builds the Firebird-backed ErrorsRepo.
func provideCobranzaErrorsRepo(p *firebird.Pool) cobranzaoutbound.ErrorsRepo {
	return cobranzaventfb.NewErrorsRepo(p)
}

// provideCobranzaClock returns the production UTC clock for the cobranza module.
func provideCobranzaClock() cobranzaoutbound.Clock {
	return cobranzaoutbound.ProductionClock{}
}

// provideCobranzaPagosRecibidosRepo builds the Firebird-backed
// PagosRecibidosRepo (write-side outbox MSP_PAGOS_RECIBIDOS).
func provideCobranzaPagosRecibidosRepo(p *firebird.Pool) *cobranzaventfb.PagosRecibidosRepo {
	return cobranzaventfb.NewPagosRecibidosRepo(p)
}

// provideCobranzaPagosRecibidosPort exposes the concrete repo as the
// outbound port interface.
func provideCobranzaPagosRecibidosPort(r *cobranzaventfb.PagosRecibidosRepo) cobranzaoutbound.PagosRecibidosRepo {
	return r
}

// provideCobranzaPagosImagenesPort exposes the same concrete repo as the
// imágenes child-collection port.
func provideCobranzaPagosImagenesPort(r *cobranzaventfb.PagosRecibidosRepo) cobranzaoutbound.PagosImagenesRepo {
	return r
}

// provideCobranzaMicrosipPagoWriter builds the Microsip writer that
// materializes a pago into DOCTOS_CC / IMPORTES_DOCTOS_CC / FORMAS_COBRO_DOCTOS.
func provideCobranzaMicrosipPagoWriter(p *firebird.Pool) cobranzaoutbound.MicrosipPagoWriter {
	return cobranzamicrosip.NewPagoWriter(p)
}

// provideCobranzaStorage wraps the shared ventas FilesystemProvider in a
// cobranza-shaped adapter. We share the same on-disk directory (STORAGE_DIR)
// so cobranza comprobantes and ventas evidencia live under one filesystem
// tree, but the two modules see their own port interface (vertical-slice).
func provideCobranzaStorage(ventasStorage ventasoutbound.StorageProvider) cobranzaoutbound.StorageProvider {
	return &cobranzaStorageAdapter{inner: ventasStorage}
}

// (No provideCobranzaImageProcessor: cobranzaoutbound.ImageProcessor is a
// type alias to imageprocessor.Processor — the same type ventas already
// provides via provideVentasImageProcessor — so fx resolves the cobranza
// consumer transparently. Adding a second provider would duplicate the
// type registration and fx aborts on startup.)

// provideCobranzaService assembles the cobranza query + command service.
func provideCobranzaService(
	saldos cobranzaoutbound.SaldosRepo,
	pagos cobranzaoutbound.PagosRepo,
	ventas cobranzaoutbound.VentasRepo,
	clock cobranzaoutbound.Clock,
	pagosRecibidos cobranzaoutbound.PagosRecibidosRepo,
	pagosImagenes cobranzaoutbound.PagosImagenesRepo,
	microsipPago cobranzaoutbound.MicrosipPagoWriter,
	storage cobranzaoutbound.StorageProvider,
	imageProc cobranzaoutbound.ImageProcessor,
	txMgr *firebird.TxManager,
	pagosReconcile cobranzaoutbound.PagosReconcileRepo,
	saldosReconcile cobranzaoutbound.SaldosReconcileRepo,
) *cobranzaapp.Service {
	svc := cobranzaapp.NewService(saldos, pagos, ventas, clock, pagosRecibidos, pagosImagenes, microsipPago, storage, imageProc, txMgr)
	svc.WithReconcilePorts(pagosReconcile, saldosReconcile)
	return svc
}

// provideCobranzaPagoRetryWorker builds the background worker that drains
// the outbox.
func provideCobranzaPagoRetryWorker(
	svc *cobranzaapp.Service,
	repo cobranzaoutbound.PagosRecibidosRepo,
	clock cobranzaoutbound.Clock,
	logger *slog.Logger,
) *cobranzaapp.PagoRetryWorker {
	return cobranzaapp.NewPagoRetryWorker(svc, repo, clock, cobranzaapp.PagoRetryWorkerConfig{}, logger)
}

// registerCobranzaPagoRetryWorkerLifecycle hooks the retry worker into fx.
func registerCobranzaPagoRetryWorkerLifecycle(lc fx.Lifecycle, w *cobranzaapp.PagoRetryWorker) {
	lifecycle.Append(lc, "pago-retry-worker", w)
}

// cobranzaStorageAdapter wraps a ventas StorageProvider to satisfy the
// cobranza StorageProvider port. Both interfaces have identical method
// shapes; only the StorageObject return type differs across module
// boundaries, so we re-pack it.
type cobranzaStorageAdapter struct {
	inner ventasoutbound.StorageProvider
}

func (a *cobranzaStorageAdapter) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	return a.inner.Store(ctx, key, contentType, sizeBytes, body)
}

func (a *cobranzaStorageAdapter) Get(ctx context.Context, key string) (cobranzaoutbound.StorageObject, error) {
	obj, err := a.inner.Get(ctx, key)
	if err != nil {
		return cobranzaoutbound.StorageObject{}, err
	}
	return cobranzaoutbound.StorageObject{
		Body:        obj.Body,
		ContentType: obj.ContentType,
		SizeBytes:   obj.SizeBytes,
	}, nil
}

func (a *cobranzaStorageAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(ctx, key)
}

// provideCobranzaPagosReconcileRepo exposes *PagosRepo as a PagosReconcileRepo.
// The same concrete instance that satisfies PagosRepo also satisfies the
// reconcile interface — no extra pool connection needed.
func provideCobranzaPagosReconcileRepo(p *firebird.Pool) cobranzaoutbound.PagosReconcileRepo {
	return cobranzaventfb.NewPagosRepo(p)
}

// provideCobranzaSaldosReconcileRepo exposes *SaldosRepo as a SaldosReconcileRepo.
func provideCobranzaSaldosReconcileRepo(p *firebird.Pool) cobranzaoutbound.SaldosReconcileRepo {
	return cobranzaventfb.NewSaldosRepo(p)
}

// provideCobranzaReconcilerConfig returns the reconciler configuration.
// Hardcoded for now; promote to config.Config once a second deployment
// needs different cadence.
func provideCobranzaReconcilerConfig() cobranzaapp.ReconcilerConfig {
	return cobranzaapp.ReconcilerConfig{
		Interval:               7 * 24 * time.Hour,
		PageSize:               1000,
		DriftLog:               true,
		FixDrift:               true,
		TombstoneRetentionDays: 30,
	}
}

// provideCobranzaSaldosTombstoneCleaner exposes the SaldosRepo as a
// SaldosTombstoneCleaner port (the concrete *SaldosRepo satisfies both).
func provideCobranzaSaldosTombstoneCleaner(p *firebird.Pool) cobranzaoutbound.SaldosTombstoneCleaner {
	return cobranzaventfb.NewSaldosRepo(p)
}

// provideCobranzaPagosTombstoneCleaner exposes the PagosRepo as a
// PagosTombstoneCleaner port (the concrete *PagosRepo satisfies both).
func provideCobranzaPagosTombstoneCleaner(p *firebird.Pool) cobranzaoutbound.PagosTombstoneCleaner {
	return cobranzaventfb.NewPagosRepo(p)
}

// provideCobranzaReconciler assembles the cobranza reconciler.
func provideCobranzaReconciler(
	saldosLister cobranzaoutbound.SaldosLister,
	recomputer cobranzaoutbound.SaldosRecomputer,
	saldosRepo cobranzaoutbound.SaldosRepo,
	pagosLister cobranzaoutbound.PagosLister,
	pagosRecomputer cobranzaoutbound.PagosRecomputer,
	saldosCleaner cobranzaoutbound.SaldosTombstoneCleaner,
	pagosCleaner cobranzaoutbound.PagosTombstoneCleaner,
	clock cobranzaoutbound.Clock,
	cfg cobranzaapp.ReconcilerConfig,
	logger *slog.Logger,
) *cobranzaapp.Reconciler {
	return cobranzaapp.NewReconciler(cobranzaapp.ReconcilerDeps{
		SaldosLister:    saldosLister,
		SaldosRepo:      saldosRepo,
		Recomputer:      recomputer,
		PagosLister:     pagosLister,
		PagosRecomputer: pagosRecomputer,
		SaldosTombstone: saldosCleaner,
		PagosTombstone:  pagosCleaner,
		Clock:           clock,
		Config:          cfg,
		Logger:          logger,
	})
}

// registerCobranzaReconcilerLifecycle hooks the reconciler into the fx lifecycle.
func registerCobranzaReconcilerLifecycle(lc fx.Lifecycle, r *cobranzaapp.Reconciler) {
	lifecycle.Append(lc, "saldos-reconciler", r)
}
