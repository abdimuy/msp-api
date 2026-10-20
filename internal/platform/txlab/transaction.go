package txlab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Esta llave se usa para guardar la transaccion dentro del contexto.
type txKey struct{}

// Querier tiene los metodos que vamos a usar tanto con la BD
// como con una transaccion.
type Querier interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)

	QueryContext(
		ctx context.Context,
		query string,
		args ...any,
	) (*sql.Rows, error)

	QueryRowContext(
		ctx context.Context,
		query string,
		args ...any,
	) *sql.Row
}

// TxManager guarda la conexion que se va a usar
// para crear las transacciones.
type TxManager struct {
	db *sql.DB
}

// Crea un nuevo manejador de transacciones.
func NewTxManager(db *sql.DB) *TxManager {
	return &TxManager{
		db: db,
	}
}

// Ejecuta una funcion dentro de una transaccion.
func (m *TxManager) RunInTx(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	return RunInTx(ctx, m.db, fn)
}

// Esta es otra forma de ejecutar una transaccion
// sin tener que crear un TxManager.
func RunInTx(
	ctx context.Context,
	db *sql.DB,
	fn func(context.Context) error,
) error {
	opts := &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	}

	return runInTx(ctx, db, opts, fn)
}

// Se usa para hacer lecturas dentro de una transaccion.
func RunInReadTx(
	ctx context.Context,
	db *sql.DB,
	fn func(context.Context) error,
) error {
	opts := &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	}

	return runInTx(ctx, db, opts, fn)
}

// Esta transaccion usa RepeatableRead para mantener
// los mismos datos durante las consultas.
func RunInSnapshotTx(
	ctx context.Context,
	db *sql.DB,
	fn func(context.Context) error,
) error {
	opts := &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
	}

	return runInTx(ctx, db, opts, fn)
}

// Aqui se maneja lo principal de la transaccion.
// Si todo sale bien hace commit y si hay error se hace rollback.
func runInTx(
	ctx context.Context,
	db *sql.DB,
	opts *sql.TxOptions,
	fn func(context.Context) error,
) error {

	// Si ya existe una transaccion, usamos esa misma
	// para no crear otra dentro.
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx)
	}

	// Iniciamos una nueva transaccion.
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf(
			"txlab: begin transaction: %w",
			err,
		)
	}

	// Guardamos la transaccion en el contexto para poder
	// recuperarla despues desde los repositorios.
	txCtx := context.WithValue(
		ctx,
		txKey{},
		tx,
	)

	// Dejamos un rollback por seguridad.
	// Si ya hubo commit simplemente no hace cambios.
	defer func() {
		_ = tx.Rollback()
	}()

	// Ejecutamos lo que se tenga que hacer dentro de la transaccion.
	if err := fn(txCtx); err != nil {
		return err
	}

	// Si no hubo errores guardamos los cambios.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"txlab: commit transaction: %w",
			err,
		)
	}

	return nil
}

// Si hay una transaccion en el contexto la devuelve.
// Si no, usa la conexion que recibimos como fallback.
func GetQuerier(
	ctx context.Context,
	fallback Querier,
) Querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}

	return fallback
}

// Revisa si actualmente hay una transaccion en el contexto.
func HasTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(*sql.Tx)
	return ok
}

// Error que se usa cuando se esperaba una transaccion
// pero no existe ninguna en el contexto.
var ErrNoTx = errors.New(
	"txlab: no active transaction in context",
)

// Devuelve la transaccion actual.
// Si no existe regresamos ErrNoTx.
func RequireTx(
	ctx context.Context,
) (*sql.Tx, error) {

	tx, ok := ctx.Value(txKey{}).(*sql.Tx)

	if !ok {
		return nil, ErrNoTx
	}

	return tx, nil
}
