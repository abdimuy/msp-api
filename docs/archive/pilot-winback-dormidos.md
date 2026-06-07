# Piloto — Reactivación de dormidos por AI/WhatsApp (winback con grupo de control)

> **Objetivo:** generar **venta incremental medible y atribuible al software** reactivando
> clientes que ya probaron que pagan y dejaron de comprar, midiendo el efecto contra un
> grupo de control. Es la base aritmética para convertir el aumento del desarrollador
> (de $5,500 a $12,000/sem ≈ **+$338k/año**) en "genera $X, cobra el Y%".
>
> Acompaña al [`business-case-ventas-cobranza-ia.md`](./business-case-ventas-cobranza-ia.md).
> Datos validados de la copia Microsip (corte 2026-06-01), receta read-only.

## 1. Por qué esta palanca y no otra

| Criterio | Por qué gana el winback de dormidos |
|---|---|
| **Atribución limpia** | Un dormido **no iba a comprar solo** (por eso está dormido). Lo que convierta vs. el control es casi 100% atribuible al software, no al mercado ni a la inflación. |
| **Sin pleito interno** | Ningún vendedor está trabajando hoy a estos clientes → no se le quita comisión a nadie. |
| **Doble valor** | Cada venta cerrada por AI trae margen + **28% de premio de financiamiento** **y** ahorra **~5% de comisión** (la cerró el software, no una persona). |
| **El motor es tuyo** | El 78% del efectivo del negocio ($81.9M en 2025) ya pasa por software que el desarrollador opera (cobranza en ruta concepto 87327 + enganche). |

## 2. Población (medida en la DB)

Definición de "dormido bueno": tiene historial de crédito (concepto 5), **saldo liquidado**
(≤ $50), **última compra hace 90–540 días**, y **sin castigo** (excluidos conceptos 27967
Fugas / 27968 Mal Cliente).

| Métrica | Valor |
|---|---|
| Dormidos totales (B+C) | 5,523 |
| **Contactables (con teléfono ≥10 díg.)** | **4,090** (74%) |
| Segmento B — dormido 3–9 meses | 1,837 |
| Segmento C — dormido 9–18 meses | 3,686 |
| Días dormido (promedio) | 337 (~11 meses) |
| **Compras históricas (promedio)** | **4.0 — recompradores leales, no one-timers** |
| Valor histórico de crédito (promedio) | $32,527 / cliente |

Lista operativa extraída: `.pilot-data/winback-dormidos.csv` (gitignored, contiene PII).
Teléfono: `DIRS_CLIENTES.TELEFONO1` (única fuente con cobertura; los campos de móvil de
Microsip están vacíos). **Enriquecer** con `MSP_VENTAS.TELEFONO` (la app de campo tiene
móviles más frescos) antes de ejecutar.

## 3. Diseño experimental

- **Tipo:** A/B aleatorizado, paralelo, una cohorte.
- **Unidad:** cliente.
- **Asignación:** determinística y reproducible — `MOD(CLIENTE_ID × 2654435761, 100)`,
  `<60 → tratamiento`, `≥60 → control` (hash de Knuth, bien distribuido).
- **Brazos resultantes:** **tratamiento 2,479 · control 1,611** (60/40).
- **Estratos:** el hash es independiente del segmento, así que B/C y valor histórico quedan
  balanceados entre brazos por construcción (verificar balance antes de arrancar).
- **Control:** **cero contacto** (business-as-usual). Pueden regresar orgánicamente — ese
  es justamente el contrafactual que queremos medir.

### Poder estadístico
Con control n=1,611 y tratamiento n=2,479, a α=5% y potencia 80%, el piloto **detecta un
lift de ~2 puntos porcentuales** en conversión. Suficiente: esperamos un efecto mayor en
dormidos leales (4 compras promedio).

## 4. Intervención (brazo de tratamiento)

Vendedor de AI por WhatsApp, tono respetuoso (base sensible a inflación — ver guardarraíles):

1. **Mensaje 1 (día 0):** saludo personalizado que referencia su relación ("ya nos
   compró N veces"), sin presión. Pregunta abierta de necesidad.
2. **Oferta (día 0–2):** producto relevante + **pre-aprobación de crédito** (su historial
   ya lo respalda) + plazo/enganche claros.
3. **Seguimiento (día 4 y día 10):** 2 recordatorios máximo. Después, alto.
4. **Cierre / handoff:** si el cliente acepta, el AI agenda. **Registrar si la venta se
   cierra full-AI** (sin vendedor → comisión ahorrada) **o con handoff a vendedor** (comisión
   se paga). Los pasos físicos (INE, enganche, entrega) siguen el flujo normal.

## 5. Métricas

| Tipo | Métrica | Cómo se calcula |
|---|---|---|
| **Primaria** | Conversión incremental | `tasa_compra(tratamiento) − tasa_compra(control)` en la ventana, con IC 95% (prueba de 2 proporciones) |
| Secundaria | Ingreso incremental | Σ ventas nuevas (concepto 5) del tratamiento − tasa_control × n_tratamiento, × ticket |
| Secundaria | **Comisión ahorrada** | Ventas cerradas **full-AI** × valor × ~5% |
| Secundaria | Premio de financiamiento | originación incremental × 28% |
| Secundaria | % cierre full-AI vs handoff | de las ventas del tratamiento |
| Salud | Opt-outs / quejas | tasa de STOP, reportes |

**Conversión = una nueva venta a crédito** (nuevo cargo concepto 5 en `DOCTOS_CC` con
`FECHA > fecha_de_contacto`), atribuida al `CLIENTE_ID`. Para el control se usa la misma
ventana calendario.

- **Ventana de medición:** 60–90 días desde el primer contacto.
- **Ticket esperado:** $7,322 (promedio de venta a crédito 2025).

## 6. Resultado esperado del piloto (conservador)

Sobre el brazo de tratamiento (2,479), una sola oleada:

| Supuesto de lift incremental | Ventas nuevas | Originación | Premio financ. (28%) | Comisión ahorrada (5%) |
|---|---|---|---|---|
| 5 pp | 124 | $0.91M | $254k | $45k |
| 10 pp | 248 | $1.82M | $508k | $91k |

Incluso el escenario de 5 pp en **una sola oleada** ya se acerca a cubrir el aumento
($338k), **medido contra control**. Escalando al pool completo (4,090) y repitiendo el
ciclo, el caso se vuelve holgado. *(Los % de lift son supuestos a probar — los benchmarks
externos de winback no resistieron verificación; el número real lo fija este piloto.)*

## 7. Criterios de decisión (go / no-go + trigger de compensación)

- **Éxito:** lift incremental **estadísticamente significativo** (IC 95% no cruza 0) **y**
  valor anualizado proyectado > costo del aumento.
- **Acción si éxito:** escalar a todo el pool + activar la conversación de comp con el
  número medido en mano.
- **Si no significativo:** iterar mensaje/oferta o reconocer que la palanca no rinde en este
  negocio (y pivotar a cobranza temprana / cross-sell) — barato y sin daño.

## 8. Guardarraíles

- **Opt-out:** respetar "STOP"/baja inmediata; mantener lista de no-contactar.
- **Frecuencia:** máximo 3 mensajes en el piloto; nunca spam.
- **Legal/PROFECO:** mensajería comercial con consentimiento y baja clara.
- **Tono:** respetuoso, sin presión — base de clientes de bajos ingresos sensible a inflación;
  proteger la relación y la recompra (46.8%) vale más que una venta forzada.
- **Sin tocar lo fiscal:** el piloto solo lee CxC y crea ventas por el flujo normal.

## 9. Wiring en `msp-api` (alto nivel, respetando CLAUDE.md)

Módulo nuevo en vertical slice, p. ej. `internal/reactivacion/`:

- **`mirror`/proyección:** read-model de dormidos derivado de Firebird (CxC) — saldo,
  última compra, # compras, valor histórico, teléfono. Sin lógica en la DB.
- **Entidad de experimento (Postgres):** `experimento_winback` con `cliente_id`, `brazo`,
  `cohorte`, `asignado_at`, `hash_seed`. IDs y timestamps en Go (`uuid.New()`, `time.Now()`),
  UTC + RFC3339 — nada de `DEFAULT now()`.
- **Log de contacto:** `contacto_winback` (cliente, canal, mensaje, enviado_at, respuesta,
  resultado: full-AI | handoff | sin_respuesta | opt_out).
- **Atribución (query):** join del read-model de ventas (nuevos cargos concepto 5 post-contacto)
  contra la tabla de experimento → tasa por brazo y delta.
- Teléfonos a E.164 (+52) y encoding NFC/UTF-8 (ver `ENCODING_HANDLING.md`).

## 10. Timeline y costo

- **Build:** ~2 semanas (read-model dormidos + tabla experimento + integración WhatsApp + bot).
- **Corrida:** 8–12 semanas (contacto + ventana de 60–90 días).
- **Costo variable:** WhatsApp Business API por conversación + tokens de AI — menor frente al
  valor en juego.

## 11. El trato de compensación (acordar ANTES de correr)

Para blindar el resultado: **fijar la fórmula antes del piloto**, no después. Ejemplo:
*"si genero $X de venta incremental medida contra control, mi aumento es Y / mi bono es Z% de
esa venta nueva"*. Así el riesgo de que muevan la portería se neutraliza, y el aumento se
vuelve aritmética, no negociación.

## 12. Riesgos

| Riesgo | Mitigación |
|---|---|
| El AI por WhatsApp convierte poco | Pool leal (4 compras prom.); iterar mensaje/oferta; el piso es "poco", no "cero" |
| No terminar / no shippear | Es el riesgo #1. Build acotado a 2 semanas; alcance mínimo (solo endpoint + bot + medición) |
| El dueño atribuye al crecimiento orgánico | El grupo de control lo neutraliza por diseño |
| Teléfonos desactualizados | Enriquecer con `MSP_VENTAS.TELEFONO`; medir tasa de entrega |
| Cobertura de teléfono (74%) | El 26% sin teléfono queda fuera del piloto; documentar para no inflar |
