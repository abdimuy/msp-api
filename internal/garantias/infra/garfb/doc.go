// Package garfb implements Firebird persistence for the warranties module.
//
// Write operations require an active transaction so article stage changes and
// their events are persisted atomically. If any operation fails, the transaction
// can be rolled back without leaving partial state in the database.
package garfb
