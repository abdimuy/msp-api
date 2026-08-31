# ADR 0010 — WhatsApp Cloud API as the channel, and the always-on edge it requires

- **Status:** Accepted
- **Date:** 2026-08-30
- **Amends:** [`2026-06-06-ventas-ai-winback-system-design.md`](../superpowers/specs/2026-06-06-ventas-ai-winback-system-design.md) §3.1 (whatsmeow for the demo) and [`2026-07-21-reactivacion-r7-fase3-design.md`](../superpowers/specs/2026-07-21-reactivacion-r7-fase3-design.md) §3b (whatsmeow adapter as the real channel)
- **Decision drivers:** the reactivación module is finished except for the channel; the same channel will carry `comprobantes`, which are financial documents at a volume of thousands per week; and production runs on a machine that is neither always up nor publicly reachable.

## Context

`internal/reactivacion/` is built: cohorte, atribución, the LLM copiloto, the allowlist and its debt-figure backstop, the gobernador, the send queue, and the `bandeja` frontend. `GET /winback` and `/winback/attribution` are tested. The one thing that does not exist is a real channel — `reactivacionsender/whatsmeow_stub.go` returns `whatsmeow_no_configurado`, and `whatsmeow` is not in `go.mod`.

Two facts about production frame everything below.

**The server is not always up.** Per `docs/ops/despliegue-produccion.md`, the API is started by hand from `run.bat` on a Windows Server 2016 console, and remote access runs through a free pinggy tunnel that **expires after ~60 minutes and rotates both host and port** on every relaunch.

**And `cmd/winback/` was designed but never built.** The June spec already called for a separate always-on binary deployed as a second `nssm` service, precisely to isolate the WhatsApp/LLM connection from the API core. `cmd/` contains `api`, `analytics-refresh`, `analytics-export-creditdata`, `fscfg` and `seed-cobrador`. It does not contain `winback`.

## Decision

### 1. The pilot channel is the WhatsApp Cloud API, not whatsmeow

The June spec chose `whatsmeow` for the demo on the assumption that the official API was blocked behind the owner's tax documents. **It is not.** An unverified business portfolio can open **250 business-initiated conversations to unique recipients per rolling 24 h**, answer user-initiated conversations without limit, and register up to two numbers. The Tehuacán pilot is dozens of clients with at most two touches — it fits several times over.

`whatsmeow` is rejected for the pilot because **a ban is unrecoverable and would end the measurement mid-flight**, and the pilot's entire purpose is to produce an attributed enganche number. A channel that can disappear cannot carry an experiment whose output is a number for a meeting.

### 2. `cmd/winback/` runs on a VPS with a stable public HTTPS URL

This is a **new requirement**, and it does not come from the June spec being wrong — it comes from choosing the official API.

`whatsmeow` opens an *outbound* connection; "always up" is sufficient and reachability never matters. **The Cloud API is the opposite: Meta pushes inbound webhooks, and there is no endpoint to poll for new messages.** If Meta cannot reach us, the customer's reply is lost, not delayed.

So the requirement is stronger than availability:

> **A process behind NAT, on a tunnel that rotates every 60 minutes, is always up and still unreachable. It cannot serve as a webhook — it does not even pass Meta's initial verification challenge.**

Outbound sends need none of this. Only inbound does.

### 3. The VPS is never a source of truth

It is an **outbound relay and a durable inbound mailbox**. It receives Meta's webhook, validates the signature, persists the payload and answers 200 immediately — without depending on the store's server being alive. Everything that owns business truth stays on-premise with Firebird.

### 4. Cohort snapshot, not a Microsip replica

No copy of Microsip exists, and building one is a separate project with its own ETL and freshness problem. Instead, when the on-premise server builds a cohorte it pushes the **bounded fact set the AI is permitted to use**: name, phone, segmento, product paid off, next-best-product, the real parcialidad and cadence, the suggested enganche, and a catalogue slice.

This is sufficient **because `app/allowlist.go` already forbids everything a live query would have been needed for.** `razonCifraDeuda` is a last-line backstop against the LLM ever stating a debt figure; `razonFueraAllowlist` escalates anything off the permitted ground; the Fase 3 spec already lists stock availability among the things the copiloto may not assert. Every question that would require the live database already escalated to a human by design. **The snapshot removes nothing the AI was allowed to do.**

Attribution stays on-premise: it reads Microsip sales in batch, after the fact, and never needs to be live during a conversation.

### 5. Anything carrying an exact amount is composed on-premise

`comprobantes` are already designed this way and the design is correct: the message is fully rendered on-premise, at the moment of the Firebird event, from the exact data, and is a finished payload before it reaches the channel. **The VPS never learns what anyone paid.**

This is what makes the snapshot safe. The snapshot only ever feeds the conversational improvisation, which is fenced by the allowlist. **No figure that represents money passes through it.**

## The receipt invariant

Restated here as a hard rule rather than a description, because a wrong or duplicated comprobante is a document with a customer's name and money on it, sent over WhatsApp, that cannot be withdrawn.

> ### A comprobante is sent if and only if the fact already exists in Microsip
>
> **Payment:** a row exists in `MSP_PAGOS_CHANGELOG` with the payment `APLICADO='S'`.
> **Sale:** `venta.aplicada` was emitted — the sale is materialised in Microsip.
>
> **No other path produces a comprobante. None.**

Four properties protect it, all already specified in `2026-07-29-comprobantes-whatsapp-design.md`:

| | |
|---|---|
| **One mechanism per type** | The changelog for payments, the domain event for sales. Never both — two sources is a double send |
| **Cursor over `SEQ_ID`** | The durability guarantee. `POST_EVENT` only lowers latency; it is not the guarantee |
| **Group by `DOCTO_CC_ID`** | One payment document, one comprobante — never one per importe row |
| **The payment is never gated** | Only the comprobante waits. If nobody looks, it still goes out |

## Template categories

| Flow | Meta category | Why it matters |
|---|---|---|
| **Comprobantes** | **Utility** | Transactional, expected by the customer. Cheaper and far easier to approve |
| **Reactivación opener** | **Marketing** | Business-initiated outreach. Stricter opt-in scrutiny |

Both categories coexist on one number; that is normal. Submitting a comprobante as Marketing would overpay and risk rejection for nothing.

**There is no one-time "marketing permission" to obtain.** Categories are assigned and reviewed **per template**. Templates can be approved months before use and do not expire — so the opener variants should be submitted early, and **three or four variants should be approved up front** to preserve the ability to A/B the copy without waiting on review each time.

⚠️ **Templates submitted from a number without an approved display name are rejected automatically.** Display name first, always.

## Messaging limits and what they gate

| | Unique recipients / 24 h |
|---|---|
| Unverified portfolio | **250** |
| After business verification | **100,000** — Meta is removing the intermediate tiers, so verification is a jump, not a climb |

**The reactivación pilot fits under 250 and can start unverified. `comprobantes` cannot** — thousands per week is roughly 430 unique recipients per day. Business verification is therefore a hard prerequisite for comprobantes and not for the pilot, and the two should proceed in parallel rather than in series.

## Consequences

**Gained:** no ban risk; a legitimate channel that scales to 100k/day; Meta reports invalid numbers in the API response instead of failing silently.

**Lost:** the Fase 3 upgrade in which the copiloto writes the opener. Meta requires a pre-approved template for the first contact, so `app/opener.go`'s current shape — a static template per segmento with the name interpolated — **stays, and is in fact already exactly what Meta approves.** Personalisation survives through template variables (name, product paid off, next-best-product, enganche); what is lost is free-form phrasing and same-day iteration on opener copy.

**Unchanged:** the entire copiloto. It operates *after* the customer replies, inside the 24-hour free-form window — `procesar_entrante`, triage, NBP, drafting, dictado, escalation, memoria, bandeja. The AI stops being the pen for the greeting and remains the pen for everything that closes a sale.

## Degradation when the store's server is down

| | |
|---|---|
| Comprobantes | Wait in `MSP_CM_ENVIO` and go out on return. **Never go out wrong** |
| Inbound replies | Held in the VPS mailbox, processed on return |
| Live conversations | Delayed — and 🔴 **after 24 h the free-form window closes** and re-engagement needs a template again |

**That 24-hour window is the only cost of downtime that does not simply reverse itself.** It is also the strongest argument for the API core to stop being started by hand.

## What this leaves to build

1. `cmd/winback/` — already decided in the June spec, never built
2. VPS, subdomain, HTTPS (Caddy + Let's Encrypt)
3. A Cloud API adapter behind the existing `MessageSender` signature — the stub's contract does not change
4. Webhook receiver plus durable inbox
5. Cohort snapshot push from on-premise
6. Templates submitted: Utility for comprobantes, three or four Marketing variants for the opener

## Prerequisites that are not ours

Meta account ownership matters: **the Business portfolio must belong to the store, with us as administrator.** The number and the conversations are the store's, and a portfolio registered to an individual becomes a transfer problem later.

### Anexo — lo que la oficina tiene que preparar *(en español, para reenviar)*

> **1 · Una línea de celular nueva y dedicada.** Que no tenga WhatsApp instalado, y **que no sea el número que usa el negocio hoy.**
>
> **2 · Los datos del negocio:** nombre legal, dirección, giro.
>
> **3 · El nombre que verá el cliente** en WhatsApp — el nombre con el que la gente reconoce la mueblería.
>
> **4 · Una tarjeta** para el cobro por conversación de Meta.
>
> **5 · Documentos para la verificación de negocio** *(necesarios por el volumen de comprobantes)*: **Constancia de Situación Fiscal**, **comprobante de domicilio**, y **acta constitutiva** si es persona moral.
>
> **6 · Una página web sencilla** — una sola página con nombre, dirección, teléfono y el **aviso de privacidad**. Hace falta para la verificación y para que el nombre para mostrar se apruebe sin sospecha.
>
> **7 · 🔴 Agregar al contrato de crédito y al ticket una cláusula de autorización de contacto por WhatsApp**, desde ya. Y **revisar si los contratos actuales ya traen algo equivalente** — de eso depende si el piloto puede correr con clientes existentes o solo con los nuevos.

**El punto 7 es el único sin atajo.** El consentimiento no se puede retroactivar, y si se mandan mensajes de marketing sin él y algunos clientes reportan, **cae la calidad del número y se congelan los límites — incluidos los de comprobantes.**
