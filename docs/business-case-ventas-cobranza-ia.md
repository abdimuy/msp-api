# Caso de negocio — Sistema de ventas y cobranza con IA (msp-api)

> Propuesta para llevar a dirección. Todos los números salen de la **propia base
> de la empresa** (copia Microsip, ventana 12 meses feb-2025 → feb-2026; copia con
> corte feb-2026, magnitudes representativas del año).

## Resumen ejecutivo (1 párrafo)
La empresa **pierde ~$2.15M MXN al año** por colocar crédito a clientes que no
pagan (Mal Cliente + Fugas), y tiene **~$2.3M de cartera en riesgo** (clientes que
dejaron de pagar hace meses, de los cuales ~$1.3M ya es prácticamente incobrable).
Microsip —por diseño— **no puede** atacar esto: es un sistema de registro
contable/fiscal, no de inteligencia. Proponemos construir, **por fases y sin tocar
lo fiscal**, una capa propia de ventas y cobranza que: (1) **automatiza** la captura
y el alta de ventas, (2) **predice el riesgo de crédito** antes de prestar,
(3) **cobra de forma inteligente** (recordatorios automáticos + alerta temprana), y
(4) **reactiva ventas** a la base de clientes que ya pagaron bien (vía WhatsApp/web).
Recupera **~$1.0M/año** de dinero que hoy se pierde **y** habilita **~$3.2M+/año en
ventas nuevas** de bajo riesgo — todo por mucho más de lo que cuesta construirlo.

## El problema, en pesos (datos de la empresa)
| Métrica | Valor / año | Qué significa |
|---|---|---|
| **Mal Cliente + Fugas** | **$2,149,738** | pérdida real por mal crédito (~3% de la facturación) |
| Cartera total (saldo vivo) | ~$35M | el libro de crédito vigente |
| **Cartera en riesgo (>3m sin pagar)** | **~$2.3M** | dinero atorado que se está perdiendo |
| Probable pérdida (>12m + sin pago) | ~$1.3M | clientes que ya no pagan, aún no castigados |
| Tasa base de default | ~6% | 667 de 11,079 clientes a crédito se vuelven malos |

> Nota: las **condonaciones ($14.5M/año)** NO son pérdida — son **descuentos por
> pronto pago, contractuales y condicionados a que el cliente pague a tiempo**. Se
> excluyen a propósito del caso (un buen pagador se las gana). Mencionarlas como
> "ahorro" sería incorrecto.

## Por qué Microsip no lo resuelve
Microsip es un **ledger fiscal/contable**: la lógica vive en la base, no tiene API,
ni eventos en tiempo real, ni dónde guardar datos de comportamiento, y corre en un
motor compartido frágil. No tiene scoring, ni cobranza inteligente, ni IA. Para
potenciar ventas y cobranza se necesita una capa **fuera** de Microsip. Lo fiscal y
contable **se quedan en Microsip** (no se reconstruyen — sería caro y riesgoso).

## La solución: 3 palancas que forman un sistema
```
1. Automatizar la venta  →  captura/aprueba/materializa sin trabajo manual + datos limpios
2. Scoring de crédito    →  no prestar a las peores apuestas (usa esos datos)
3. Cobranza con IA       →  cobrar antes de que el saldo se vuelva incobrable
```
Cada palanca **alimenta a la siguiente** (la automatización genera los datos que el
scoring y la cobranza necesitan).

### Palanca 1 — Automatización de ventas
- Pipeline `MSP_VENTAS → Microsip` (materialización automática; ya diseñado, ver `docs/superpowers/specs/2026-05-22-aplicar-venta-local-microsip-design.md`).
- OCR de INE (autollenado), pre-revisión automática (acelera la aprobación).
- **Valor:** productividad (más ventas por vendedor, menos errores, ciclo más corto). Mide: ventas/vendedor, tiempo de ciclo, % errores.

### Palanca 2 — Scoring de crédito
- Modelo que predice impago **antes de aprobar**, con historial de pago + atributos del cliente.
- **Evidencia (de la base):** los clientes que se volvieron malos ya pagaban claramente peor — **65% de lo que debían vs 82% los buenos** — con una sola variable cruda. Un modelo completo discrimina mucho más.
- **Prueba sin riesgo (backtest):** correr el modelo sobre los clientes del último año y medir cuánta pérdida se habría evitado.
- **Valor:** reducir el $2.15M. Conservador: −25% = **~$537k/año**.

### Palanca 3 — Cobranza con IA
- Agente de WhatsApp (recordatorios automáticos, bien temporizados; ya hay infra de WhatsApp), priorización de visitas, y **alerta temprana** del cliente que se está degradando.
- **Valor:** recuperar la cartera en riesgo (~$2.3M) antes del castigo, mejorar flujo del libro de $35M, y productividad del cobrador. Conservador: recuperar 20% del riesgo = **~$460k/año** + flujo.

### Palanca 4 — Reactivación de ventas (ingreso nuevo)
- Usa la base de clientes + historial de pago para **vender de nuevo, vía WhatsApp y web**, a quienes **ya liquidaron y pagaron bien** (los mejores prospectos, riesgo bajo). IA segmenta, ofrece y pre-aprueba; el pedido entra a `MSP_VENTAS` → se materializa (Palanca 1).
- **Datos (de la base):** ~**8,600 clientes** buenos pagadores que liquidaron y no han vuelto a comprar (3-24m); ~30,000 totales. **Ticket promedio = $7,338.**
- **Oportunidad (conservador, solo pool caliente/tibio):**

  | Conversión | Ventas nuevas | Ventas $ |
  |---|---|---|
  | 5% | 431 | ~$3.2M |
  | 10% | 862 | ~$6.3M |

- **Valor:** ingreso nuevo de bajo riesgo (clientes con pago comprobado). La utilidad es el **margen** sobre esas ventas + intereses (no el ticket completo). Se valida con un piloto de WhatsApp a los ~1,037 más frescos.
- **Riesgo:** WhatsApp oficial + opt-in (no quemar el número); la conversión es supuesto hasta el piloto.

## ROI (conservador y defendible)
**Ahorro / recuperación (dinero que hoy se pierde):**
| Palanca | Recuperable/año (conservador) |
|---|---|
| Scoring (−25% del $2.15M) | ~$537k |
| Cobranza (recuperar 20% de $2.3M en riesgo) | ~$460k |
| Automatización | productividad (no se cuenta como $ directo) |
| **Subtotal ahorro** | **~$1.0M/año** |

**Ingreso nuevo (ventas a base dormida):**
| Palanca | Ventas/año (conservador) |
|---|---|
| Reactivación (5% del pool caliente) | **~$3.2M en ventas** (utilidad = margen) |

(Sin contar productividad ni el efecto de flujo sobre el libro de $35M.)

## Plan por fases (bajo riesgo)
1. **Automatización de venta** (fundación; ya hay spec) — win tangible + datos limpios.
2. **Cobranza con IA** (WhatsApp + alerta temprana) — toca el riesgo y el flujo, muy visible.
3. **Scoring** — la reducción de pérdida más limpia (backtest primero).
- Cada fase: piloto medible, reversible, Microsip queda de ancla fiscal.

## Por qué es de bajo riesgo
- **No reemplaza lo fiscal/contable** (se queda en Microsip). Cero riesgo SAT.
- **Incremental** (strangler fig), no big-bang.
- **Piloto-primero, medible** (especialmente el scoring vía backtest).
- **Probado que se puede:** ya se hizo ingeniería inversa del alta de venta, se diseñó la integración y se resolvió un problema real de producción sin tumbarla.

## Argumentos por audiencia
**Dirección (decisión, no técnico):**
- "Perdemos $2.15M/año por mal crédito y tenemos $2.3M de cartera en riesgo. Esto lo reduce, y el resultado vale múltiplos de lo que cuesta."
- "Lo fiscal no se toca; es por fases; empezamos con un piloto medible."

**Responsable técnico (validación):**
- Números sacados de la base, con la pérdida real separada del descuento contractual (rigor, no humo).
- Señal de scoring demostrada (65% vs 82%); backtest para el número final.
- Arquitectura: capa de engagement propia, Microsip como sistema de registro; integración transaccional e idempotente.

## Honestidad / supuestos
- No todo el $2.15M es prevenible (hay impagos impredecibles); los % son conservadores y se confirman con backtest.
- El descuento por pronto pago **no** es una palanca (es contractual).
- Números de una copia con corte feb-2026 (~representativos del año); refrescar del dato más reciente para la propuesta final.
- La automatización aporta sobre todo **productividad/capacidad**, no ingresos mágicos.
