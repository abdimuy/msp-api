# Caso de negocio — ventas y cobranza con IA (msp-api)

> **Objetivo doble:** (1) aprobar el proyecto y (2) justificar **aumento + bono**
> del desarrollador, convenciendo a la vez a un **dueño no técnico** (le importan
> pesos) y a su **hermano técnico** (DBA de CDMX, escruta el rigor).
>
> **El criterio que manda** (afinado en análisis del 26-may-2026): la palanca que
> justifica el sueldo del desarrollador debe ser **(a) dinero NUEVO** (no ahorro que
> otro se cuelga), **(b) atribuible a software que él construye**, y **(c) medible
> con grupo de control** para atar el bono a la cifra. Cobranza y condonaciones NO
> cumplen (a)/(b) → son "valor agregado", no la base del sueldo.

## Cómo se obtuvieron y cómo re-consultar (para retomar después)
Todos los números **VALIDADO** salen de la copia Microsip en Docker. Receta read-only
(nunca escribir, nunca reiniciar el motor Firebird Super compartido):
```
docker exec -i mueblera-firebird /usr/local/firebird/bin/isql \
  -u SYSDBA -p masterkey -ch UTF8 -q /firebird/data/MUEBLERA.FDB <<<'SELECT ...;'
```
Etiquetas: **VALIDADO** (de su data) · **BENCHMARK** (industria) · **POR PROBAR**
(requiere piloto/modelo). Datos de copia con corte feb-2026; refrescar de producción
para la propuesta final.

### Mapa de tablas clave (memoria para retomar)
- `DOCTOS_CC` + `IMPORTES_DOCTOS_CC` — movimientos de cuentas por cobrar. Monto en
  `IMPORTES_DOCTOS_CC.IMPORTE`; abonos ligan al cargo vía `DOCTO_CC_ACR_ID`.
- `CONCEPTOS_CC`: **5**=Venta a crédito (cargo), 11/155=Cobro, 24533=Enganche,
  7=Interés moratorio (**no se usa**), 27966=Cancelaciones, 27967=Fugas,
  27968=Mal Cliente, 27969=Condonaciones.
- `LIBRES_CARGOS_CC` (keyed por `DOCTO_CC_ID` del cargo) — **datos particulares que
  MSP captura por venta, que Microsip no tiene de fábrica**: `PRECIO_DE_CONTADO`,
  `CREDITO_EN_MESES`, `TIEMPO_A_CORTO_PLAZOMESES`, `MONTO_A_CORTO_PLAZO`, `ENGANCHE`,
  `PARCIALIDAD`, `VENDEDOR_1/2/3`, `NUMERO_DE_VENDEDORES`. **Poblado desde 2024;
  estándar en 2025 (85% cobertura) y 2026 (82%).** Las ventas <2024 no lo tienen.
- `PRECIOS_EMPRESA` (listas) + `PRECIOS_ARTICULOS` — escalera de precios por plazo:
  Contado (8941), 1..12 Meses, Precio de lista (42), Precio mínimo (43 = costo/piso).
- `CLIENTES`: `TIPO_CLIENTE_ID` (27770 = MAL CLIENTE), `ESTATUS`, `LIMITE_CREDITO`.
- `COBRADOR_ID` en `DOCTOS_CC`; rutas en `GRUPOS_RUTAS`; `ZONAS_CLIENTES`.
- Cómo se da de alta una venta MSP completa (incl. `LIBRES_CARGOS_CC`):
  `docs/microsip-crear-venta-paso-a-paso.md`, Fase 7.

## Baseline del negocio (VALIDADO)
| Métrica | Valor | Nota |
|---|---|---|
| Crédito otorgado / año | 2021 $71.0M · 2022 $83.4M · 2023 $94.9M · 2024 $104.6M · **2025 $117.3M** | crece ~10%/año |
| Ventas a crédito / año | ~14k-16k | concepto 5 |
| Ticket promedio | ~$7,338 | |
| Recompra orgánica | ~46.8% compra más de una vez | avg ~2.4 compras/cliente; ciclo ~297 días |
| Cartera viva actual | ~$33M | 88.6% corriente |
| Premio de financiamiento (spread crédito vs contado, 2025) | **$26.6M (28.4%)** | VALIDADO con `PRECIO_DE_CONTADO` real |

## Pérdida real — corregida (VALIDADO)
La pérdida **no es ~2%** como decía la versión anterior (eso era una métrica
"same-year" que subestima porque el castigo va con 1-3 años de retraso y el libro
crece ~10%/año). Trazando el castigo al **año de la venta original (vintage)**:

| Añada (madura) | Crédito | Mal Cliente+Fugas | tasa | + Cancelaciones | total |
|---|---|---|---|---|---|
| 2021 | $71.0M | $1.66M | 2.34% | $0.68M | **3.30%** |
| 2022 | $83.4M | $2.15M | 2.58% | $0.84M | **3.59%** |

- **Pérdida económica honesta: ~2.5% (solo castigos) a ~3.0%** (incl. cancelaciones,
  que son **recuperación parcial**: recogen la mercancía usada — "REFRI 2DA" — no es
  pérdida total).
- Dato clave: en "Mal Cliente" el cliente **ya había pagado ~61%** antes de caerse →
  la pérdida es **recuperable** (pagaban y se callaron), no irremediable. Eso apunta
  a **cobranza temprana**, no a scoring al prestar.
- Aun a ~3%, el negocio gestiona bien el riesgo. **El upside grande NO es reducir
  pérdida** (es modesto y político) — es **vender más**.

## ⚠️ Corrección importante — Condonaciones NO son palanca
La versión previa (y un análisis intermedio) trató las condonaciones (~$14M/año, 12%
de la venta) como posible fuga. **Se descartó, por dos razones VALIDADAS:**
1. **Es el modelo de precios, por diseño.** El descuento por condonar escala con la
   rapidez de pago: paga en 0-1 mes → ~40% off; 3-6 meses → 26%; +12 meses → 4%. Y
   coincide exacto con el spread de las listas (Contado = 37.3% bajo el precio a 12
   meses). La condonación = aplicar el precio de contado a quien liquida anticipado.
2. **La oficina las revisa y autoriza antes de aplicarlas** (confirmado por el
   usuario). No es discrecional sin control: es una decisión consciente (cobrar algo
   > nada).

→ **No es dinero recuperable. No se pitchea como ahorro.** Queda solo como contexto
de que MSP ya captura el precio de contado real por venta (ver `LIBRES_CARGOS_CC`),
que es el **activo de datos** para scoring y pricing.

## El activo oculto: el dataset que MSP ya construyó
Las ventas nuevas (2024+) capturan por venta lo que Microsip no tiene:
`PRECIO_DE_CONTADO`, plazo, enganche, parcialidad y **vendedores**. Implicaciones:
- **Comisiones SÍ están en la data** (vía `VENDEDOR_*` / `NUMERO_DE_VENDEDORES`) —
  corrige el supuesto previo de "no está en la DB". (Falta la *tasa*, que la da el
  usuario.)
- Es la materia prima para **scoring de crédito, optimización de precio y análisis
  de comisiones** — hoy nadie lo usa.

## Mapa de valor — ordenado por "dónde el desarrollador saca dinero"

### 🟢 Ingreso NUEVO, atribuible al software (la base del sueldo)
| Palanca | Oportunidad | Por qué te paga |
|---|---|---|
| **1. Desbloqueo de límite de crédito a buenos pagadores** | ~4,051 clientes operan al tope de su línea; ~1,219 con demanda por encima del límite (brecha medida ~$26M) | Demanda **ya probada** (intentan comprar más y no pueden). Riesgo mínimo (pagadores probados). Atribución perfecta: "subí el límite con mi scoring → compró $X más". POR PROBAR la captura |
| **2. Reactivación de dormidos** | pool caliente ~817 buenos pagadores que liquidaron (~$6M potencial); +6,334 que compraron 2024 y no 2025 | Ventas que **no existían**; canal (WhatsApp + segmentación) que tú construyes. Conversión POR PROBAR (benchmark winback 5-10%) |
| **3. Cross-sell en punto de venta** | 84% de tickets son de **1 solo artículo**; con 2 sube 30% (~$2.4M) | Margen extra por ticket, atribuible a la función que construyes |

### 🔵 Valor agregado (NO la base del sueldo — político/no atribuible)
| Palanca | Pesos/año | Por qué no lidera |
|---|---|---|
| Eficiencia de cobradores (5 peores rutas → 2%; spread 30x, 0.07% vs 6.44%) | $357-446k VALIDADO | ahorro que el área de cobranza se cuelga; cambio de proceso/gente |
| Alerta temprana de cobranza (73.7% del castigo viene de >3m callados) | ~$495k (señal VALIDADA, rescate BENCHMARK) | recuperación, no ingreso nuevo |
| Ingreso financiero: **no cobran interés moratorio** (concepto 7 vacío) | ~$1.3M BENCHMARK | requiere cambiar política comercial |
| Productividad: automatización de alta de venta + OCR INE | capacidad/errores | fundación (Fase 1, ya hay spec) |

## Cómo se convierte en TU dinero (estrategia de comp)
1. **Elige UNA palanca de ingreso nuevo** (recomendado: #1 límite o #2 reactivación).
2. **Piloto con grupo de control:** intervienes a un grupo, dejas otro igual, mides la
   venta extra → número **tuyo**, indiscutible.
3. **Estructura:** base corregida a **~$45-55k/mes** (por alcance: senior + DBA +
   integración + arquitecto + único dueño de lo crítico; hoy ~$23.8k/mes = junior)
   **+ bono = % de la venta nueva atribuible** a tu sistema. El dueño paga solo si
   entra dinero (le quita riesgo).
4. **Pedir DESPUÉS de un win medible.** Con el piloto ganado, el aumento es aritmética,
   no negociación: "generé $X que no existían; pido el Y% de eso".
5. **Secuencia de convencimiento:** ganar al hermano con el rigor → presentar al dueño
   en pesos → base + bono atado a pilotos.

## Plan para volver POR PROBAR → PROBADO
1. **Piloto de límite de crédito:** scoring sube el límite a un grupo de buenos
   pagadores topados, control queda igual; medir venta incremental a 60-90 días.
2. **Piloto de reactivación:** campaña WhatsApp a los ~817 más frescos; medir
   conversión y ventas; extrapolar al pool.
3. **(Secundario) A/B de cobranza temprana:** intervenir al mes 3 a la mitad de los
   callados; medir recuperación vs control.
4. Baselines acordados ANTES; todo reversible, piloto-primero, sin tocar lo fiscal.

## Honestidad / supuestos / correcciones de esta sesión
- **Pérdida corregida** de ~2% (same-year) a **~2.5-3%** (vintage). El 2% subestima.
- **Condonaciones descartadas** como palanca: es modelo de precio + se revisan en
  oficina. No es fuga.
- **Comisiones**: la estructura de vendedores SÍ está en `LIBRES_CARGOS_CC`; falta la
  tasa (la da el usuario, ~5-7%).
- **Reactivación:** pool a reconciliar — el análisis fino dio ~817 calientes / ~1,465
  dormidos estrictos, vs los "8,600" de versiones previas (definiciones distintas).
- Reactivación: venta ≠ utilidad (la ganancia es margen + financiamiento; descontar
  canibalización del ~46.8% que recompra solo).
- `PRECIO_DE_CONTADO` solo existe en ventas 2024+; análisis de pricing limitado a esas.
- Motor Firebird Super compartido: **nunca reiniciar**.

## Fuera de alcance
- Reemplazar lo fiscal/contable/CFDI de Microsip (se queda como ancla).
- Implementar el código del caso de negocio en esta fase. La materialización de ventas
  ya tiene spec (`docs/superpowers/specs/2026-05-22-aplicar-venta-local-microsip-design.md`)
  y es la **Fase 1 de construcción**.
