package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// errBootstrapAlreadyDone is returned when the operator runs auth-bootstrap
// against a database that already has a usuario row.
var errBootstrapAlreadyDone = errors.New("bootstrap: ya existe al menos un usuario; usa --reset para limpiar primero o corre auth-bootstrap solo en un sistema vacío")

// errBootstrapMissingFlags is returned when one of the required CLI flags
// is not supplied.
var errBootstrapMissingFlags = errors.New("flags --email and --nombre are required; supply either --firebase-uid or --create-in-firebase")

// authBootstrapCmd returns the cobra command that provisions the admin
// usuario together with the inmutable "super_admin" rol.
//
// Three flag modes covering the common operator scenarios:
//
//   - `--firebase-uid <uid>`: the Firebase user exists already (created by
//     hand or imported); wire it up in the DB. Default behavior, safe to
//     re-run on an empty system, refuses if any usuario row already exists.
//
//   - `--create-in-firebase --password <pwd>`: create the user in Firebase
//     Auth with email/password and use the resulting uid. Useful on a
//     fresh dev environment where no Firebase user exists yet.
//
//   - `--reset`: DESTRUCTIVE — delete every other Firebase Auth user AND
//     wipe every row from the auth-side DB tables (MSP_USUARIOS_ROLES,
//     MSP_ROLES_PERMISOS, MSP_ROLES, MSP_USUARIOS) before bootstrapping.
//     Use on a dev environment that needs to start from scratch.
func authBootstrapCmd() *cobra.Command {
	var firebaseUID, email, nombre, password string
	var reset, createInFirebase bool

	c := &cobra.Command{
		Use:   "auth-bootstrap",
		Short: "Provision the admin usuario and assign the inmutable super_admin rol",
		Long: "Wires the initial admin usuario into MSP_USUARIOS, creates the " +
			"inmutable super_admin rol with every permission, and links them. " +
			"Optionally creates the Firebase Auth user when --create-in-firebase " +
			"is set, and optionally wipes prior state when --reset is supplied.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBootstrapCLI(cmd.Context(), bootstrapCLIFlags{
				FirebaseUID:      firebaseUID,
				Email:            email,
				Nombre:           nombre,
				Password:         password,
				CreateInFirebase: createInFirebase,
				Reset:            reset,
			})
		},
	}
	c.Flags().StringVar(&firebaseUID, "firebase-uid", "", "Firebase Authentication uid (required unless --create-in-firebase is set)")
	c.Flags().StringVar(&email, "email", "", "Email for the admin usuario (required)")
	c.Flags().StringVar(&nombre, "nombre", "", "Display name for the admin usuario (required)")
	c.Flags().StringVar(&password, "password", "MspDev2026!", "Password to set on the Firebase user when --create-in-firebase is supplied")
	c.Flags().BoolVar(&createInFirebase, "create-in-firebase", false, "Create the admin in Firebase Auth if it does not already exist by email")
	c.Flags().BoolVar(&reset, "reset", false, "DESTRUCTIVE: delete every other Firebase user AND wipe every auth-side DB table before bootstrapping")
	return c
}

// bootstrapCLIFlags carries the parsed flags from authBootstrapCmd into
// runBootstrapCLI. Keeps the cobra wiring small enough to stay under the
// gocognit/nestif thresholds without sacrificing the testability of the
// underlying bootstrapAuth pipeline.
type bootstrapCLIFlags struct {
	FirebaseUID      string
	Email            string
	Nombre           string
	Password         string
	CreateInFirebase bool
	Reset            bool
}

// runBootstrapCLI is the orchestrator the cobra RunE delegates to. It
// owns the config load, the Firebird pool lifecycle, and the optional
// Firebase admin steps (--reset, --create-in-firebase) before forwarding
// into runAuthBootstrap, whose body is the testable core.
func runBootstrapCLI(parentCtx context.Context, f bootstrapCLIFlags) error {
	if f.Email == "" || f.Nombre == "" {
		return errBootstrapMissingFlags
	}
	if f.FirebaseUID == "" && !f.CreateInFirebase {
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
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
	defer cancel()
	if err := pool.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = pool.Stop(ctx) }()

	if f.Reset {
		if err := runResetStep(ctx, cfg.Firebase, pool, f.Email); err != nil {
			return err
		}
	}
	if f.CreateInFirebase {
		uid, err := runCreateInFirebaseStep(ctx, cfg.Firebase, f.Email, f.Nombre, f.Password)
		if err != nil {
			return err
		}
		f.FirebaseUID = uid
	}
	return runAuthBootstrap(ctx, pool, f.FirebaseUID, f.Email, f.Nombre)
}

// runResetStep performs the destructive cleanup: every Firebase Auth user
// other than keepEmail is removed, then every row in the auth-side DB
// tables is dropped.
func runResetStep(ctx context.Context, fbCfg config.Firebase, pool *firebird.Pool, keepEmail string) error {
	fbAuth, err := newFirebaseAdminClient(ctx, fbCfg)
	if err != nil {
		return err
	}
	if err := deleteOtherFirebaseUsers(ctx, fbAuth, keepEmail); err != nil {
		return err
	}
	return wipeAuthTables(ctx, pool.DB)
}

// runCreateInFirebaseStep ensures the admin exists in Firebase Auth (no-op
// if it already does) and returns the UID to wire into the DB row.
func runCreateInFirebaseStep(ctx context.Context, fbCfg config.Firebase, email, nombre, password string) (string, error) {
	fbAuth, err := newFirebaseAdminClient(ctx, fbCfg)
	if err != nil {
		return "", err
	}
	uid, created, err := ensureFirebaseAdminUser(ctx, fbAuth, email, nombre, password)
	if err != nil {
		return "", err
	}
	event := "auth_bootstrap.firebase_user_existed"
	if created {
		event = "auth_bootstrap.firebase_user_created"
	}
	slog.InfoContext(ctx, event, "uid", uid, "email", email)
	return uid, nil
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

	// Save usuario BEFORE creating the rol: MSP_ROLES.CREATED_BY is a
	// strict FK to MSP_USUARIOS.ID in Firebird (see migration
	// 000001_create_auth_tables.up.sql), so the usuario row must already
	// exist when the rol is inserted with CREATED_BY=id.
	if err := deps.Usuarios.Save(ctx, u); err != nil {
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

// newFirebaseAdminClient builds a Firebase Admin SDK client from the
// supplied config. The operator-facing CLI bypasses the FirebaseClient
// outbound port because user-management calls (Create, Delete, List) are
// admin-side concerns that the port intentionally does not expose.
func newFirebaseAdminClient(ctx context.Context, cfg config.Firebase) (*auth.Client, error) {
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.ProjectID},
		option.WithCredentialsFile(cfg.ServiceAccountPath),
	)
	if err != nil {
		return nil, fmt.Errorf("firebase NewApp: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth client: %w", err)
	}
	return client, nil
}

// deleteOtherFirebaseUsers removes every user from Firebase Auth whose
// email differs from keepEmail. The keepEmail user is preserved so a
// follow-up ensureFirebaseAdminUser call can reuse it or create a fresh
// one if it never existed.
func deleteOtherFirebaseUsers(ctx context.Context, client *auth.Client, keepEmail string) error {
	it := client.Users(ctx, "")
	deleted := 0
	for {
		u, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("iterate firebase users: %w", err)
		}
		if u.Email == keepEmail {
			continue
		}
		if err := client.DeleteUser(ctx, u.UID); err != nil {
			return fmt.Errorf("delete firebase user uid=%s: %w", u.UID, err)
		}
		slog.InfoContext(ctx, "auth_bootstrap.firebase_user_deleted", "uid", u.UID, "email", u.Email)
		deleted++
	}
	slog.InfoContext(ctx, "auth_bootstrap.firebase_users_reset", "deleted", deleted)
	return nil
}

// ensureFirebaseAdminUser looks up the admin by email; creates a new
// Firebase Auth user with email/password/display name if absent. Returns
// the resulting UID and a flag indicating whether the user was newly
// created (true) or pre-existing (false).
func ensureFirebaseAdminUser(ctx context.Context, client *auth.Client, email, displayName, password string) (string, bool, error) {
	user, err := client.GetUserByEmail(ctx, email)
	if err == nil {
		return user.UID, false, nil
	}
	if !auth.IsUserNotFound(err) {
		return "", false, fmt.Errorf("lookup firebase user: %w", err)
	}
	params := (&auth.UserToCreate{}).
		Email(email).
		EmailVerified(true).
		Password(password).
		DisplayName(displayName).
		Disabled(false)
	created, err := client.CreateUser(ctx, params)
	if err != nil {
		return "", false, fmt.Errorf("create firebase user: %w", err)
	}
	return created.UID, true, nil
}

// wipeAuthTables truncates every row from the auth-side DB tables. The
// MSP_PERMISOS catalog is preserved — it gets re-synced from
// domain.AllPermissions on every bootstrap run anyway, so leaving it
// intact saves a write.
//
// MSP_USUARIOS carries a self-FK on CREATED_BY / UPDATED_BY. A bulk
// DELETE FROM fails when row B's CREATED_BY references row A's ID. To
// avoid topologically sorting the deletes we first collapse every
// cross-reference into a self-loop (UPDATE … SET CREATED_BY = ID,
// UPDATED_BY = ID); after that each row only references itself and the
// blanket DELETE succeeds.
func wipeAuthTables(ctx context.Context, db *sql.DB) error {
	for _, t := range []string{"MSP_USUARIOS_ROLES", "MSP_ROLES_PERMISOS", "MSP_ROLES"} {
		//nolint:gosec // table list is package-private; not user input.
		res, err := db.ExecContext(ctx, "DELETE FROM "+t)
		if err != nil {
			return fmt.Errorf("DELETE FROM %s: %w", t, err)
		}
		n, _ := res.RowsAffected()
		slog.InfoContext(ctx, "auth_bootstrap.table_wiped", "table", t, "rows", n)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE MSP_USUARIOS SET CREATED_BY = ID, UPDATED_BY = ID
		 WHERE CREATED_BY <> ID OR UPDATED_BY <> ID`,
	); err != nil {
		return fmt.Errorf("collapse MSP_USUARIOS self-FK: %w", err)
	}
	res, err := db.ExecContext(ctx, "DELETE FROM MSP_USUARIOS")
	if err != nil {
		return fmt.Errorf("DELETE FROM MSP_USUARIOS: %w", err)
	}
	n, _ := res.RowsAffected()
	slog.InfoContext(ctx, "auth_bootstrap.table_wiped", "table", "MSP_USUARIOS", "rows", n)
	return nil
}
