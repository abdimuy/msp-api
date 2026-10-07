// Package garfb implements Firebird persistence for the warranties module.
//
// Write operations require an active transaction. A write method may leave
// partial changes in that transaction if it returns an error, so the caller
// must roll the transaction back before continuing.
package garfb
