// Command seed-cobrador inserts a row in MSP_USUARIOS for a given email,
// looking up the FIREBASE_UID via Firebase Admin SDK, and assigns the
// `super_admin` role so the user passes the auth middleware and has access
// to cobranza endpoints.
//
// One-shot dev tooling: run once per developer email, then delete.
//
// Usage:
//
//	source .env && go run ./cmd/seed-cobrador <email>
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"github.com/google/uuid"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: seed-cobrador <email>")
		os.Exit(2)
	}
	email := os.Args[1]
	ctx := context.Background()

	cfg, err := config.Load()
	must(err)

	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.Firebase.ProjectID},
		option.WithCredentialsFile(cfg.Firebase.ServiceAccountPath))
	must(err)
	auth, err := app.Auth(ctx)
	must(err)

	user, err := auth.GetUserByEmail(ctx, email)
	must(err)
	fmt.Printf("firebase: email=%s uid=%s name=%q\n", email, user.UID, user.DisplayName)

	pool, err := firebird.New(cfg.Firebird)
	must(err)
	must(pool.Start(ctx))
	defer func() { _ = pool.Stop(ctx) }()

	must(seed(ctx, pool, email, user.UID, user.DisplayName))
	fmt.Println("done.")
}

func seed(ctx context.Context, pool *firebird.Pool, email, uid, nombre string) error {
	if nombre == "" {
		nombre = email
	}
	now := time.Now()

	var existingID string
	err := pool.DB.QueryRowContext(ctx,
		`SELECT ID FROM MSP_USUARIOS WHERE FIREBASE_UID = ?`, uid,
	).Scan(&existingID)

	var usuarioID string
	switch {
	case err == nil:
		usuarioID = existingID
		fmt.Printf("usuario ya existe: id=%s\n", usuarioID)
	case err == sql.ErrNoRows:
		usuarioID = uuid.New().String()
		_, err := pool.DB.ExecContext(ctx,
			`INSERT INTO MSP_USUARIOS (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			   CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, ?, TRUE, ?, ?, ?, ?)`,
			usuarioID, uid, email, nombre, firebird.ToWallClock(now),
			firebird.ToWallClock(now), usuarioID, usuarioID)
		if err != nil {
			return fmt.Errorf("insert MSP_USUARIOS: %w", err)
		}
		fmt.Printf("usuario insertado: id=%s\n", usuarioID)
	default:
		return fmt.Errorf("lookup MSP_USUARIOS: %w", err)
	}

	var rolID string
	if err := pool.DB.QueryRowContext(ctx,
		`SELECT ID FROM MSP_ROLES WHERE NOMBRE = 'super_admin'`,
	).Scan(&rolID); err != nil {
		return fmt.Errorf("lookup super_admin: %w", err)
	}

	var count int
	if err := pool.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_USUARIOS_ROLES WHERE USUARIO_ID = ? AND ROL_ID = ?`,
		usuarioID, rolID,
	).Scan(&count); err != nil {
		return fmt.Errorf("lookup MSP_USUARIOS_ROLES: %w", err)
	}
	if count > 0 {
		fmt.Println("rol super_admin ya asignado")
		return nil
	}

	if _, err := pool.DB.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS_ROLES (USUARIO_ID, ROL_ID, CREATED_AT, CREATED_BY)
		 VALUES (?, ?, ?, ?)`,
		usuarioID, rolID, firebird.ToWallClock(now), usuarioID); err != nil {
		return fmt.Errorf("insert MSP_USUARIOS_ROLES: %w", err)
	}
	fmt.Println("rol super_admin asignado")
	return nil
}

func must(err error) {
	if err != nil {
		slog.Error("seed-cobrador", "err", err)
		os.Exit(1)
	}
}
