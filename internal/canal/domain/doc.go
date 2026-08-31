// Package domain holds the canal module's entity, value objects, domain
// events and sentinel errors.
//
// canal is the always-on WhatsApp edge that runs on the store's Linux VPS
// (docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md): Meta pushes
// inbound webhooks, the VPS validates and persists them in a durable mailbox,
// answers 200 immediately, and later forwards each one to the store's
// on-premise server. The VPS is never a source of truth — it is an outbound
// relay and a durable inbox — so this package models exactly that: one
// entity (MensajeEntrante) and the state of its delivery to the store, never
// the conversation, the customer, or the sale behind it.
//
// canal is a sealed module (ADR-0009, same seal as flota and garantías):
// this package imports only the standard library, uuid, decimal, and
// internal/platform/{audit,apperror}. It does not import
// internal/platform/whatsapp — the domain does not know about transports.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain
