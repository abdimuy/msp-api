package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// errBootstrapAlreadyDone is returned when the operator runs auth-bootstrap
// against a database that already has a usuario row.
var errBootstrapAlreadyDone = errors.New("bootstrap: ya existe al menos un usuario; auth-bootstrap solo puede ejecutarse en un sistema vacío")

// errBootstrapMissingFlags is returned when one of the required CLI flags
// is not supplied.
var errBootstrapMissingFlags = errors.New("flags --firebase-uid, --email and --nombre are all required")

// authBootstrapCmd returns the cobra command that provisions the very first
// admin usuario together with the inmutable "super_admin" rol. It is intended
// to be run once per database — the command refuses to proceed if any
// usuario row already exists.
func authBootstrapCmd() *cobra.Command {
	var firebaseUID, email, nombre string

	c := &cobra.Command{
		Use:   "auth-bootstrap",
		Short: "Create the first admin usuario and assign the inmutable super_admin rol",
		Long: "Provisions the initial admin usuario in a fresh database, " +
			"creates the inmutable super_admin rol with every permission, and " +
			"assigns it to the new usuario. Refuses to run when any usuario " +
			"already exists.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if firebaseUID == "" || email == "" || nombre == "" {
				return errBootstrapMissingFlags
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			pool, err := firebird.New(cfg.Firebird)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if err := pool.Start(ctx); err != nil {
				return err
			}
			defer func() { _ = pool.Stop(ctx) }()

			return runAuthBootstrap(ctx, pool, firebaseUID, email, nombre)
		},
	}
	c.Flags().StringVar(&firebaseUID, "firebase-uid", "", "Firebase Authentication uid for the admin usuario (required)")
	c.Flags().StringVar(&email, "email", "", "Email for the admin usuario (required)")
	c.Flags().StringVar(&nombre, "nombre", "", "Display name for the admin usuario (required)")
	return c
}

// bootstrapTxRunner is the subset of [firebird.TxManager] the bootstrap flow
// depends on. Exposing it as an interface lets tests inject a stub that
// runs the function inline without a real database.
type bootstrapTxRunner interface {
	RunInTx(ctx context.Context, fn func(context.Context) error) error
}

// bootstrapDeps groups the persistence-side collaborators bootstrapAuth
// needs. Production callers fill it from a [firebird.Pool] via
// [newBootstrapDepsFromPool]; tests pass in-memory fakes.
type bootstrapDeps struct {
	Usuarios outbound.UsuarioRepo
	Roles    outbound.RolRepo
	Permisos outbound.PermisoRepo
	TxRunner bootstrapTxRunner
	Now      func() time.Time
	NewID    func() uuid.UUID
}

// newBootstrapDepsFromPool wires the production adapters off a live Firebird
// pool. Keeping the wiring centralized here makes the runAuthBootstrap
// entry point a one-liner that's trivially audited.
func newBootstrapDepsFromPool(pool *firebird.Pool) bootstrapDeps {
	return bootstrapDeps{
		Usuarios: authfb.NewUsuarioRepo(pool),
		Roles:    authfb.NewRolRepo(pool),
		Permisos: authfb.NewPermisoRepo(pool),
		TxRunner: firebird.NewTxManager(pool.DB),
		Now:      func() time.Time { return time.Now().UTC() },
		NewID:    uuid.New,
	}
}

// runAuthBootstrap is the production entry point: wires real adapters off
// the supplied pool and forwards into [bootstrapAuth], whose body is the
// only place the bootstrap algorithm lives.
func runAuthBootstrap(ctx context.Context, pool *firebird.Pool, firebaseUID, email, nombre string) error {
	deps := newBootstrapDepsFromPool(pool)
	return bootstrapAuth(ctx, deps, firebaseUID, email, nombre, fmtWriter{})
}

// bootstrapAuth is the dependency-injected core of the bootstrap flow. It
// performs the refuse-if-bootstrapped check, validates the input via the
// domain VOs, then runs the multi-step write inside a single Firebird tx.
// The progress message is written to `out` so tests can capture it.
func bootstrapAuth(
	ctx context.Context,
	deps bootstrapDeps,
	firebaseUID, email, nombre string,
	out io.Writer,
) error {
	existing, err := deps.Usuarios.List(ctx, outbound.ListParams{PageSize: 1})
	if err != nil {
		return err
	}
	if len(existing.Items) > 0 {
		return errBootstrapAlreadyDone
	}

	fuid, err := domain.NewFirebaseUID(firebaseUID)
	if err != nil {
		return err
	}
	em, err := domain.NewEmail(email)
	if err != nil {
		return err
	}
	nm, err := domain.NewNombre(nombre)
	if err != nil {
		return err
	}

	now := deps.Now()
	id := deps.NewID()
	u := domain.NewUsuario(id, fuid, em, nm, nil, nil, id, now)

	if err := deps.TxRunner.RunInTx(ctx, func(ctx context.Context) error {
		return bootstrapWriteAll(ctx, deps, u, id, now)
	}); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, "auth-bootstrap: created usuario %s (%s) with super_admin rol\n", id, email); err != nil {
		return err
	}
	return nil
}

// bootstrapWriteAll executes the persistence work inside the open Firebird
// tx: UPSERT permission catalog, UPSERT super_admin inmutable rol, INSERT
// the admin usuario, SyncPermisos to attach every permission to the rol,
// AsignarRol to give the admin super_admin powers.
func bootstrapWriteAll(
	ctx context.Context,
	deps bootstrapDeps,
	u *domain.Usuario,
	id uuid.UUID,
	now time.Time,
) error {
	if err := deps.Permisos.UpsertCatalog(ctx, domain.AllPermissions()); err != nil {
		return err
	}

	superAdminDescription := "rol con todos los permisos del sistema"
	rol, err := domain.NewRol(deps.NewID(), "super_admin", &superAdminDescription, true, id, now)
	if err != nil {
		return err
	}
	if err := deps.Roles.UpsertInmutableByName(ctx, rol); err != nil {
		return err
	}

	if err := deps.Usuarios.Save(ctx, u); err != nil {
		return err
	}

	actual, err := deps.Roles.FindByNombre(ctx, "super_admin")
	if err != nil {
		return err
	}

	perms := domain.AllPermissions()
	codes := make([]domain.Permission, len(perms))
	for i, p := range perms {
		codes[i] = p.Code
	}
	if err := deps.Roles.SyncPermisos(ctx, actual.ID(), codes, id, now); err != nil {
		return err
	}

	return deps.Usuarios.AsignarRol(ctx, u.ID(), actual.ID(), id, now)
}

// fmtWriter is a tiny [io.Writer] that delegates to fmt.Print so we can
// keep the production code path writing to stdout without explicit
// stdlib coupling at the call site.
type fmtWriter struct{}

func (fmtWriter) Write(p []byte) (int, error) { return fmt.Print(string(p)) }
