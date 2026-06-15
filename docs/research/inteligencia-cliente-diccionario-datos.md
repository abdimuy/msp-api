# Inteligencia de Cliente — Diccionario de datos (Track B)

> **Entregable 2** del *Plan de INVESTIGACIÓN — Componente "Inteligencia de Cliente"*.
> Mapeo, contra la **base de datos recién restaurada**, de qué datos existen y dónde, y de cómo se
> deriva cada feature del perfil objetivo. Todas las cifras provienen de queries `SELECT` reales
> corridas contra el restore — no de supuestos.

## Conexión usada (read-only) y corte de datos

```bash
docker exec -i mueblera-firebird-snap /opt/firebird/bin/isql -u SYSDBA -p masterkey -ch UTF8 \
  -q /var/lib/firebird/data/MUEBLERA_SNP.fdb <<'SQL'
SELECT ...;
SQL
```

- Contenedor `mueblera-firebird-snap` · FB 5.0.4 · puerto host `3055`. Motor **embedded** vía path de
  archivo → solo un `isql` a la vez (correr queries en serie).
- **Reglas duras:** solo `SELECT`; `FIRST n` / `ROWS n` (no `LIMIT`); sumas grandes con
  `SUM(CAST(x AS DOUBLE PRECISION))`; texto legacy Microsip en Win1252.
- **Corte de datos del restore:** `MAX(FECHA)=2026-06-11` en `DOCTOS_CC`; historial desde
  `2006-03-26`; **108,077** cargos a crédito (concepto 5).

## Correcciones al mapa documentado (`ventas-ai-estrategia.md §16`)

Hallazgos que **contradicen** la documentación previa y deben corregirse en §16:

| §16 decía | Realidad verificada |
|---|---|
| Tablas `MSP_VENTAS`, `MSP_SALDOS_VENTAS` | **No existen.** Las reales son `MSP_LOCAL_SALE`, `MSP_PAGOS_RECIBIDOS` (454,838), `MSP_VISITAS` (295,871), `MSP_CHANGE_LOG`, `MSP_GARANTIAS` |
| `LIBRES_CARGOS_CC` poblado desde 2024 (~85% en 2025) | Poblado **desde 2018**, cobertura ~100% desde entonces (99,423 registros); `PRECIO_DE_CONTADO` falta en solo 30 registros |
| `LIBRES_CARGOS_CC.FREC_PAGO` | No existe en esa tabla; vive en `MSP_LOCAL_SALE` |
| `MSP_VENTAS.TELEFONO` = móviles frescos | Es `MSP_LOCAL_SALE.TELEFONO` (89.4% cobertura, pero solo sobre 6,438 ventas de campo sep-2025→jun-2026) |
| Cobertura de teléfono ~51% | Re-medida: **52.7%** (`DIRS_CLIENTES.TELEFONO1`, 23,820/45,217) |
| Concepto 5 = "Venta a crédito" | Se llama "Venta en mostrador" en el catálogo (es el cargo de crédito correcto) |

Los 9 códigos de concepto documentados (`5, 11, 155, 24533, 27966, 27967, 27968, 27969, 87327`)
quedaron **todos verificados** contra el catálogo real (ver detalle en §Bloque 4).

## Modelo de ventas Microsip (corrección estructural importante)

> **Corrección sobre la primera versión:** el RFM y la recencia se derivaban del **ledger de
> crédito** (`DOCTOS_CC`, concepto 5), que SOLO ve ventas a crédito. Eso dejaba **invisible** a
> todo cliente de contado. La venta real —contado *y* crédito— vive en **`DOCTOS_PV`** (Punto de
> Venta). El crédito/enganche/parcialidades es una **capa encima** de la venta, no la venta misma.

Estructura verificada de las ventas en Microsip:

| Concepto | Dónde vive | Notas verificadas |
|---|---|---|
| **Venta (evento de compra)** | `DOCTOS_PV` + `DOCTOS_PV_DET` | 423,284 docs, 2006→2026; `CLIENTE_ID` **100% poblado** (44,781 clientes). Ventas = `TIPO_DOCTO IN ('V','P')` y `ESTATUS='N'` (421,575); `'D'`=devoluciones; `ESTATUS='C'`=canceladas |
| **Módulo Ventas/Facturación** (`DOCTOS_VE`) | — | **Abandonado**: 59 registros, todos ene-2018. No usar |
| **Forma de venta: contado vs crédito** | `DOCTOS_PV_COBROS.FORMA_COBRO_ID` | 67=Efectivo, 68=Cheque, **71=Crédito**, 27773=Traspaso. Venta con línea forma 71 = **a crédito** |
| **Enganche** | `DOCTOS_CC` concepto 24533 / `LIBRES_CARGOS_CC.ENGANCHE` (~$35M) | **NO** está como línea de efectivo en `PV_COBROS` (ahí suma solo $400); es dato de la capa de crédito |
| **Capa de crédito** (cargo, cobros, parcialidades, castigos) | `DOCTOS_CC` + `IMPORTES_DOCTOS_CC` | aplica **solo a ventas a crédito**. Enlace a la venta por `FOLIO`+`CLIENTE_ID`+`SISTEMA_ORIGEN` (no hay FK `DOCTO_PV_ID`) |

**Magnitud de lo que se perdía el RFM solo-crédito** (medido a nivel venta):
- **Crédito:** 104,013 ventas · **$664.6M** → 24.7% por conteo, **84.4% por valor**.
- **Contado:** 317,562 ventas · $123.0M → 75.3% por conteo, 15.6% por valor.

**Impacto por cliente (el matiz que corrige la primera lectura):**
- Cohortes (44,738 clientes): **solo crédito 30,256** · **ambos 13,678** · **solo contado 804**.
- Un RFM solo-crédito captura el **98.2% de los clientes** — el problema NO es cobertura.
- El daño real es **recencia mal fechada**: de los 13,677 "ambos", **7,198 (53%)** compraron de
  contado **más reciente** que su último crédito. → **~7,200 clientes (16% de la base)** quedarían
  mal clasificados de ciclo de vida (falsos "dormido/frío") con un RFM solo-crédito. Por eso R/F/M
  y la recencia se anclan en `DOCTOS_PV`.

> El detalle re-medido (split contado/crédito, RFM sobre toda la base, cohortes
> solo-contado/solo-crédito/ambos) está en el **Bloque fuente B4** al final de este documento.

---

## Diccionario de features del perfil objetivo → fuente

Para cada feature del perfil: la tabla.columna fuente, la derivación, y su estado de soporte.
Los SQL completos y validados están en el detalle por bloque (más abajo).

| # | Feature del perfil | Fuente (tabla.columna) | Derivación | Estado |
|---|---|---|---|---|
| 1 | **RFM — Recency** | `DOCTOS_PV.FECHA` (ventas: `TIPO_DOCTO IN ('V','P')`, `ESTATUS='N'`) | `MAX(FECHA)` por `CLIENTE_ID`; días desde el corte | ✅ Soportado — **toda venta** (contado + crédito) |
| 2 | **RFM — Frequency** | `DOCTOS_PV` (ventas) | `COUNT(*)` de ventas por `CLIENTE_ID` | ✅ Soportado — **toda venta** |
| 3 | **RFM — Monetary** | `DOCTOS_PV.IMPORTE_NETO` | `SUM` por cliente (precio crédito en ventas a plazo) | ✅ Soportado — **toda venta** |
| 3b | **Tipo de venta (contado / crédito / enganche)** | `DOCTOS_PV_COBROS.FORMA_COBRO_ID` (67=Efectivo, 71=Crédito) | venta con línea forma 71 = crédito; enganche = línea efectivo en venta a crédito | ✅ Soportado |
| 4 | **RFMS — Solvency / comportamiento de pago** | `IMPORTES_DOCTOS_CC` (`DOCTO_CC_ACR_ID`) + `DOCTOS_CC.FECHA` | días-a-pagar = fecha abono − fecha cargo; % puntualidad | ✅ Soportado |
| 5 | **Saldo abierto** | `IMPORTES_DOCTOS_CC` `TIPO_IMPTE` C/R | `SUM(C: IMP+IVA) − SUM(R: IMP)` por cargo, vía `DOCTO_CC_ACR_ID` | ✅ Validado end-to-end |
| 6 | **% pagado** | idem #5 | `cobrado / facturado` por cliente | ✅ Soportado |
| 7 | **Ownership (categorías que tiene)** | `DOCTOS_PV_DET.ARTICULO_ID` → `ARTICULOS.LINEA_ARTICULO_ID` → `LINEAS_ARTICULOS` | join validado; categorías por cliente | ✅ Soportado |
| 8 | **Next-best-product / complemento faltante** | `DOCTOS_PV_DET` (historial) + mapa de pares | market basket; categorías ausentes vs pares complementarios | ✅ Soportado (mapa de pares a definir) |
| 9 | **Estado de ciclo de vida** | `DOCTOS_PV.FECHA` (recencia de **toda venta**) | recencia vs ciclo de recompra ~11 meses (335 días) | ✅ Soportado |
| 10 | **Tier de riesgo (conductual)** | comportamiento de pago (#4) + `CLIENTES.TIPO_CLIENTE_ID` (27770 Mal Cliente) + castigos (concepto 27968) | scorecard ponderado → semáforo | ✅ Soportado |
| 11 | **Propensión a recompra** | RFM (#1–3) + recencia | heurística RFM (cold-start); luego modelo | ✅ Soportado (heurística) |
| 12 | **Premio de financiamiento** | `LIBRES_CARGOS_CC.PRECIO_DE_CONTADO` vs precio crédito | spread = crédito − contado (**+45–64%** medido) | ✅ Soportado desde 2018 |
| 13 | **LTV histórico** | #3 + margen (#15) | suma de margen histórico por cliente | ⚠️ Parcial (margen aproximado) |
| 14 | **Atribución por cobrador** | `DOCTOS_CC.COBRADOR_ID` (99.5% poblado) → `AGENTES`/`RUTAS` | join de ruta/agente | ✅ Soportado |
| 15 | **Margen por venta** | `DOCTOS_IN_DET.COSTO_UNITARIO` por artículo | precio − costo; costo **puntual**, no histórico | ⚠️ Aproximado |
| 16 | **Flag fraude: teléfono compartido** | `DIRS_CLIENTES.TELEFONO1` | mismo teléfono en >1 `CLIENTE_ID` (1,615 casos / 3,391 clientes) | ⚠️ Soportado, limitado por cobertura 52.7% |
| 17 | **Flag: Mal Cliente / castigo** | `CLIENTES.TIPO_CLIENTE_ID=27770`; conceptos 27967/27968 | bandera directa | ✅ Soportado |

## Features del perfil que la DB **NO** soporta (gaps duros)

| Feature deseado (estado del arte) | Por qué no se puede |
|---|---|
| **Verificación de identidad documental** (INE / CURP / RFC) | Cobertura ≈ **0%**. `LIBRES_CLIENTES.IDENTIFICACION_OFICIAL` guarda solo el *tipo* de documento, nunca el número. Email también ≈0% |
| **Antifraude por aval/responsable** | Aval capturado en solo **3.1%** de ventas de campo; solo 8 avales repetidos → señal inservible |
| **Margen histórico exacto por venta** | El costo (`DOCTOS_IN_DET.COSTO_UNITARIO`) es puntual; `DOCTOS_PV_DET` no trae costo. El costo al momento de cada venta histórica no es recuperable con exactitud |
| **Trazado automático producto → cargo CxC** | No hay FK directa `DOCTOS_PV` → `DOCTOS_CC`; el vínculo es por `CLIENTE_ID`+fecha/folio en texto |
| **Vendedor real desde `LIBRES_CARGOS_CC.VENDEDOR_1`** | Usa IDs opacos del sistema de comisiones MSP que no enlazan a `VENDEDORES.VENDEDOR_ID` del ERP |
| **Premio de financiamiento pre-2018** | No hay `PRECIO_DE_CONTADO` antes de 2018 |
| **Identity resolution (de-dupe de clientes)** | No hay clave de identidad fuerte (sin CURP/INE); de-dupe solo heurístico por nombre+teléfono+dirección, con el ruido ya documentado |

---

## Cruce: estado del arte (Track A) ↔ soporte real en la DB (Track B)

Qué recomienda la literatura vs. qué podemos construir hoy. (Criterio de "completo" del plan.)

| Tema (Track A) | Recomendación | ¿Construible ya? |
|---|---|---|
| §2 RFM / RFMS | RFM + "S" de solvencia; M con precio crédito | ✅ **Sí, completo.** Ledger de 20 años + pagos; la "S" sale del comportamiento de pago validado |
| §6 Ciclo de vida | 7 estados anclados a recompra ~11 meses | ✅ **Sí.** Recencia desde `DOCTOS_CC` |
| §7 CLV/LTV | histórico → BG/NBD; con premio financiamiento | ✅ Histórico con premio (desde 2018). ⚠️ Margen aproximado |
| §3 Next-best-product | market basket + complemento faltante | ✅ **Sí.** Cadena de productos validada; falta definir el mapa de pares con las 47 categorías reales |
| §4 Propensión | cold-start con heurística RFM → modelo | ✅ **Sí** (heurística primero, sin etiquetas) |
| §5 Riesgo thin-file | scorecard conductual ponderado | ✅ **Sí.** Toda la data de pago existe; sin necesidad de buró |
| §9 Identidad / fraude | verificación documental + grafo de atributos | ❌ **Parcial/No.** Verificación documental imposible (INE/CURP ~0%). Solo teléfono compartido como señal débil |
| §1/§8 Customer 360 + UI | perfil materializado + ficha de cliente | ✅ **Sí.** Datos suficientes para una ficha rica; identity resolution es el punto débil |
| §10 Comparables | historial interno > buró | ✅ **Confirmado** por los datos: 20 años de historial propio es el activo |
| §11 Arquitectura Go/Firebird | read-model materializado + refresh batch/incremental | ✅ **Sí**, encaja con el stack legacy |

**Conclusión del cruce:** el **motor analítico** (segmentación, riesgo, recomendación, ciclo de vida,
propensión, LTV) es **construible ya** con la data existente. La única capa que la DB **no** soporta
es la **verificación de identidad/antifraude documental** — habría que capturarla a futuro (campo
INE/CURP en la app) para habilitarla.

---

# Detalle por bloque de tablas

> A continuación, el detalle crudo de cada bloque con columnas, tipos, cobertura medida y el SQL de
> derivación que corrió sin error contra el restore. Producido por los agentes de exploración.


---

# Bloque fuente: B1-cliente-identidad.md

# B1 — Cliente, Identidad y Señales de Fraude

**Corte de datos:** `MAX(FECHA) = 2026-06-11` en `DOCTOS_CC` (snapshot restaurado en `MUEBLERA_SNP.fdb`).

> **Nota metodológica:** algunas consultas de los pasos 6–9 no pudieron completarse
> porque un proceso `isql` quedó colgado dentro del contenedor `mueblera-firebird-snap`
> (PID 739, 99.9 % CPU), bloqueando la BD con un lock exclusivo. Las secciones
> afectadas (`MSP_GARANTIAS`, `CLAVES_CLIENTES` cobertura live, señales de fraude
> compartidas) se documentan con la SQL correcta y se marcan `[PENDIENTE]`.
> Las cifras de cobertura de `CLIENTES`, `DIRS_CLIENTES` y `MSP_LOCAL_SALE` son reales.

---

## 0. Paridad y frescura

```sql
SELECT MAX(FECHA) AS MAX_FECHA FROM DOCTOS_CC;
-- Resultado: 2026-06-11
```

**Tablas MSP presentes** (11 tablas en total):

```sql
SELECT RDB$RELATION_NAME FROM RDB$RELATIONS
WHERE RDB$RELATION_NAME LIKE 'MSP%' AND RDB$SYSTEM_FLAG = 0
ORDER BY RDB$RELATION_NAME;
```

| Tabla MSP | Descripción funcional |
|---|---|
| `MSP_CHANGE_LOG` | Log de cambios legacy (trigger `MSP_CLIENTES_LISTEN`), sin consumer activo en Go |
| `MSP_GARANTIAS` | Garantías de productos vendidos |
| `MSP_GARANTIA_EVENTOS` | Eventos del flujo de garantía |
| `MSP_GARANTIA_IMAGENES` | Imágenes adjuntas a garantías |
| `MSP_LOCAL_SALE` | Ventas de campo capturadas por la app (contiene `TELEFONO`, `AVAL_O_RESPONSABLE`) |
| `MSP_LOCAL_SALE_COMBO` | Combos de productos en ventas de campo |
| `MSP_LOCAL_SALE_IMAGES` | Imágenes de ventas de campo |
| `MSP_LOCAL_SALE_PRODUCT` | Productos en ventas de campo |
| `MSP_LOCAL_SALE_VENDEDOR` | Vendedores asignados a ventas de campo |
| `MSP_PAGOS_RECIBIDOS` | Pagos de cobranza recibidos (454,838 registros) |
| `MSP_VISITAS` | Visitas de cobranza georreferenciadas (295,871 registros) |

**Tablas del mapa documentado que NO existen:** `MSP_VENTAS`, `MSP_SALDOS_VENTAS`.
El equivalente funcional de "ventas de campo" es `MSP_LOCAL_SALE`. No existe tabla
`MSP_SALDOS_VENTAS` en este snapshot.

**Tablas adicionales relevantes Microsip (no MSP):**
- `LIBRES_CLIENTES` — tabla custom Mueblera (1:1 con `CLIENTES`): `CELULAR`, `REFERENCIA`, `IDENTIFICACION_OFICIAL`, `COMPROBANTE_DE_DOMICILIO`, `U_LATITUD`, `U_LONGITUD`
- `CLAVES_CLIENTES` — clave alfanumérica del cliente (tipo `0044523`)
- `DIRS_CLIENTES` — direcciones y teléfonos

---

## 1. CLIENTES — inventario de columnas y cobertura

### Tipos Firebird (referencia de decodificación)
| Código | Tipo SQL |
|---|---|
| 8 | INTEGER |
| 14 | CHAR |
| 16 | BIGINT (NUMERIC escalado) |
| 23 | BOOLEAN |
| 27 | DOUBLE PRECISION |
| 35 | TIMESTAMP |
| 37 | VARCHAR |
| 261 | BLOB |

### Columnas de CLIENTES

```sql
SELECT RF.RDB$FIELD_NAME, F.RDB$FIELD_TYPE, F.RDB$FIELD_LENGTH,
       F.RDB$FIELD_PRECISION, F.RDB$FIELD_SCALE, RF.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS RF
JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
WHERE RF.RDB$RELATION_NAME = 'CLIENTES'
ORDER BY RF.RDB$FIELD_POSITION;
```

| Columna | Tipo SQL | Long | NOT NULL | Notas |
|---|---|---|---|---|
| `CLIENTE_ID` | INTEGER | 4 | ✅ | PK; generator `ID_DOCTOS` (compartido) |
| `NOMBRE` | VARCHAR(200) | 200 | ✅ | Win1252; único por convención GUI (no UNIQUE constraint) |
| `CONTACTO1` | VARCHAR(50) | 50 | — | Win1252 |
| `CONTACTO2` | VARCHAR(50) | 50 | — | Win1252 |
| `ESTATUS` | CHAR(1) | 1 | — | Default `'A'`; A=activo, B=bloqueado, V=vendedor-ruta, C=cancelado |
| `CAUSA_SUSP` | VARCHAR(100) | 100 | — | Texto de suspensión |
| `FECHA_SUSP` | DATE | 4 | — | Solo si ESTATUS='S' |
| `COBRAR_IMPUESTOS` | CHAR(1) | 1 | — | Default `'S'` |
| `RETIENE_IMPUESTOS` | CHAR(1) | 1 | — | Default `'N'` |
| `SUJETO_IEPS` | CHAR(1) | 1 | ✅ | Sin default — obligatorio en INSERT |
| `GENERAR_INTERESES` | CHAR(1) | 1 | — | |
| `EMITIR_EDOCTA` | CHAR(1) | 1 | — | |
| `DIFERIR_CFDI_COBROS` | BOOLEAN | 1 | ✅ | Sin default — obligatorio en INSERT |
| `LIMITE_CREDITO` | NUMERIC(15,-2) | 8 | — | Escalado ×100; `0` = contado |
| `MONEDA_ID` | INTEGER | 4 | ✅ | FK `MONEDAS`; `1`=MXN |
| `COND_PAGO_ID` | INTEGER | 4 | ✅ | FK `CONDS_PAGO`; `21497`=Contado |
| `TIPO_CLIENTE_ID` | INTEGER | 4 | — | FK `TIPOS_CLIENTES`; ver distribución abajo |
| `ZONA_CLIENTE_ID` | INTEGER | 4 | — | FK `ZONAS_CLIENTES` |
| `COBRADOR_ID` | INTEGER | 4 | — | FK `COBRADORES` |
| `VENDEDOR_ID` | INTEGER | 4 | — | FK `VENDEDORES`; representa la **ruta**, no la persona |
| `NOTAS` | BLOB SUB_TYPE 1 | — | — | Win1252 |
| `CUENTA_CXC` | VARCHAR(30) | 30 | — | |
| `CUENTA_ANTICIPOS` | VARCHAR(30) | 30 | — | |
| `FORMATOS_EMAIL` | VARCHAR(100) | 100 | — | |
| `RECEPTOR_CFD` | VARCHAR(30) | 30 | — | |
| `NUM_PROV_CLIENTE` | VARCHAR(35) | 35 | — | |
| `CAMPOS_ADDENDA` | BLOB SUB_TYPE 1 | — | — | |
| `USUARIO_CREADOR` | VARCHAR(31) | 31 | — | |
| `FECHA_HORA_CREACION` | TIMESTAMP | 8 | — | Llenado por trigger `CLIENTES_BEFINS` |
| `USUARIO_AUT_CREACION` | VARCHAR(31) | 31 | — | |
| `USUARIO_ULT_MODIF` | VARCHAR(31) | 31 | — | |
| `FECHA_HORA_ULT_MODIF` | TIMESTAMP | 8 | — | |
| `USUARIO_AUT_MODIF` | VARCHAR(31) | 31 | — | |
| `PRECIO_EMPRESA_ID` | INTEGER | 4 | — | FK lista de precios |

### Cobertura de CLIENTES (45,216 registros totales)

```sql
SELECT
  COUNT(*) AS TOTAL,
  COUNT(NOMBRE) AS CON_NOMBRE,
  COUNT(CONTACTO1) AS CON_CONTACTO1,
  COUNT(CONTACTO2) AS CON_CONTACTO2,
  COUNT(ESTATUS) AS CON_ESTATUS,
  COUNT(CAUSA_SUSP) AS CON_CAUSA_SUSP,
  COUNT(FECHA_SUSP) AS CON_FECHA_SUSP,
  COUNT(LIMITE_CREDITO) AS CON_LIMITE_CREDITO,
  COUNT(TIPO_CLIENTE_ID) AS CON_TIPO_CLIENTE,
  COUNT(ZONA_CLIENTE_ID) AS CON_ZONA,
  COUNT(COBRADOR_ID) AS CON_COBRADOR,
  COUNT(VENDEDOR_ID) AS CON_VENDEDOR,
  COUNT(NOTAS) AS CON_NOTAS,
  COUNT(FECHA_HORA_CREACION) AS CON_FECHA_CREACION,
  COUNT(FECHA_HORA_ULT_MODIF) AS CON_FECHA_MODIF
FROM CLIENTES;
```

| Campo | No-nulo | % |
|---|---|---|
| Total registros | 45,216 | — |
| `NOMBRE` | 45,216 | 100.0% |
| `ESTATUS` | 45,216 | 100.0% |
| `LIMITE_CREDITO` | 45,216 | 100.0% |
| `FECHA_HORA_CREACION` | 45,216 | 100.0% |
| `FECHA_HORA_ULT_MODIF` | 45,216 | 100.0% |
| `TIPO_CLIENTE_ID` | 45,215 | ~100.0% |
| `ZONA_CLIENTE_ID` | 45,215 | ~100.0% |
| `COBRADOR_ID` | 45,214 | ~100.0% |
| `VENDEDOR_ID` | 45,195 | 99.9% |
| `NOTAS` | 36,687 | 81.1% |
| `CONTACTO1` | 23,563 | 52.1% |
| `CAUSA_SUSP` | 4,468 | 9.9% |
| `FECHA_SUSP` | 40,426 | 89.4% (nulos = activos, valor presente = suspendidos) |
| `CONTACTO2` | 2,174 | 4.8% |

> **Nota:** `FECHA_SUSP` no-nulo (40,426) parece alto — podría ser que el campo
> almacena una fecha de alta provisional o que muchos clientes tienen fecha histórica
> de suspensión aunque ahora estén activos. Requiere validación cruzada con ESTATUS.

### Distribución ESTATUS

```sql
SELECT ESTATUS, COUNT(*) AS CNT FROM CLIENTES GROUP BY ESTATUS ORDER BY CNT DESC;
```

| ESTATUS | Significado | Cantidad | % |
|---|---|---|---|
| `B` | Bloqueado | 28,463 | 63.0% |
| `A` | Activo | 10,207 | 22.6% |
| `V` | Vendedor-ruta (Microsip) | 6,445 | 14.3% |
| `C` | Cancelado | 101 | 0.2% |

> Los 6,445 registros con `ESTATUS='V'` son registros internos de Microsip
> que representan rutas/vendedores, no clientes reales. Los clientes activos
> reales son 10,207. Los "bloqueados" (28,463) incluyen clientes con saldo
> en mora o dados de baja sin eliminar.

### Distribución TIPO_CLIENTE_ID

```sql
SELECT TIPO_CLIENTE_ID, COUNT(*) AS CNT FROM CLIENTES GROUP BY TIPO_CLIENTE_ID ORDER BY CNT DESC;
```

| `TIPO_CLIENTE_ID` | Descripción documentada | Cantidad | % |
|---|---|---|---|
| `21499` | Público General / Particular | 40,852 | 90.3% |
| `27770` | Mal Cliente | 4,283 | 9.5% |
| `11955` | (otro tipo — no documentado) | 36 | 0.1% |
| `179164` | (otro tipo — no documentado) | 30 | 0.1% |
| `67821` | (otro tipo — no documentado) | 14 | 0.0% |
| `NULL` | Sin asignar | 1 | 0.0% |

**Verificación:** `21499`=Público General y `27770`=Mal Cliente confirman el mapa documentado.
Los tres tipos menores (`11955`, `179164`, `67821`) no estaban en el mapa — requieren
consulta a `TIPOS_CLIENTES` para obtener su nombre.

### Rango de fechas

```sql
SELECT MIN(FECHA_HORA_CREACION) AS PRIMERA_ALTA,
       MAX(FECHA_HORA_CREACION) AS ULTIMA_ALTA,
       MIN(FECHA_HORA_ULT_MODIF) AS PRIMERA_MODIF,
       MAX(FECHA_HORA_ULT_MODIF) AS ULTIMA_MODIF
FROM CLIENTES;
```

| Columna | Valor |
|---|---|
| Primera alta | 2018-01-08 18:01:10 |
| Última alta | 2026-06-11 19:04:51 |
| Primera modificación | 2018-03-23 18:37:19 |
| Última modificación | 2026-06-11 19:41:52 |

Rango de datos: **~8.4 años** de historial de clientes.

---

## 2. DIRS_CLIENTES — direcciones y teléfonos

### Columnas de DIRS_CLIENTES

```sql
SELECT RF.RDB$FIELD_NAME, F.RDB$FIELD_TYPE, F.RDB$FIELD_LENGTH, RF.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS RF
JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
WHERE RF.RDB$RELATION_NAME = 'DIRS_CLIENTES'
ORDER BY RF.RDB$FIELD_POSITION;
```

| Columna | Tipo SQL | Long | NOT NULL | Notas |
|---|---|---|---|---|
| `DIR_CLI_ID` | INTEGER | 4 | ✅ | PK; generator `ID_DOCTOS` |
| `CLIENTE_ID` | INTEGER | 4 | ✅ | FK `CLIENTES` |
| `NOMBRE_CONSIG` | VARCHAR(200) | 200 | ✅ | Hardcoded `"Dirección principal"` en altas Go |
| `CALLE` | VARCHAR(430) | 430 | — | Composición: `NOMBRE_CALLE` + `NUM_EXT` + `\n` + `COLONIA` + `, ` + `POBLACION` |
| `NOMBRE_CALLE` | VARCHAR(100) | 100 | — | Calle sin número |
| `NUM_EXTERIOR` | VARCHAR(10) | 10 | — | |
| `NUM_INTERIOR` | VARCHAR(10) | 10 | — | |
| `COLONIA` | VARCHAR(100) | 100 | — | |
| `COLONIA_CLAVE_FISCAL` | VARCHAR(4) | 4 | — | |
| `POBLACION` | VARCHAR(100) | 100 | — | |
| `POBLACION_CLAVE_FISCAL` | VARCHAR(3) | 3 | — | |
| `REFERENCIA` | VARCHAR(100) | 100 | — | Referencia de ubicación (campo distinto al de `LIBRES_CLIENTES`) |
| `CIUDAD_ID` | INTEGER | 4 | — | FK `CIUDADES`; `338`=Tehuacán |
| `ESTADO_ID` | INTEGER | 4 | — | FK `ESTADOS`; `337`=Puebla |
| `CODIGO_POSTAL` | VARCHAR(10) | 10 | — | |
| `PAIS_ID` | INTEGER | 4 | — | FK `PAISES`; `336`=México |
| `TELEFONO1` | VARCHAR(35) | 35 | — | **Columna principal de teléfono** |
| `TELEFONO2` | VARCHAR(35) | 35 | — | Rara vez poblada |
| `FAX` | VARCHAR(35) | 35 | — | Prácticamente vacío |
| `EMAIL` | VARCHAR(200) | 200 | — | Casi vacío (2 registros) |
| `RFC_CURP` | VARCHAR(18) | 18 | — | No requerido para "público general" |
| `TIPO_PERSONA` | CHAR(1) | 1 | — | |
| `CLAVE_REGIMEN_FISCAL` | CHAR(3) | 3 | — | |
| `TAX_ID` | VARCHAR(40) | 40 | — | |
| `CONTACTO` | VARCHAR(50) | 50 | — | |
| `VIA_EMBARQUE_ID` | INTEGER | 4 | — | FK catálogo; `87621`=default |
| `ES_DIR_PPAL` | CHAR(1) | 1 | — | `'S'`=principal |
| `USAR_PARA_ENVIOS` | CHAR(1) | 1 | — | |
| `USAR_PARA_FACTURAR` | CHAR(1) | 1 | — | |
| `GLN` | VARCHAR(20) | 20 | — | UTF8 charset |

### Cobertura de DIRS_CLIENTES (45,217 registros totales)

```sql
SELECT
  COUNT(*) AS TOTAL,
  COUNT(TELEFONO1) AS CON_TEL1,
  COUNT(CASE WHEN TELEFONO1 IS NOT NULL AND TELEFONO1 <> '' THEN 1 END) AS TEL1_NO_VACIO,
  COUNT(TELEFONO2) AS CON_TEL2,
  COUNT(CASE WHEN TELEFONO2 IS NOT NULL AND TELEFONO2 <> '' THEN 1 END) AS TEL2_NO_VACIO,
  COUNT(EMAIL) AS CON_EMAIL,
  COUNT(CASE WHEN EMAIL IS NOT NULL AND EMAIL <> '' THEN 1 END) AS EMAIL_NO_VACIO,
  COUNT(RFC_CURP) AS CON_RFC,
  COUNT(CASE WHEN RFC_CURP IS NOT NULL AND RFC_CURP <> '' THEN 1 END) AS RFC_NO_VACIO,
  COUNT(COLONIA) AS CON_COLONIA,
  COUNT(CASE WHEN COLONIA IS NOT NULL AND COLONIA <> '' THEN 1 END) AS COLONIA_NO_VACIO,
  COUNT(CIUDAD_ID) AS CON_CIUDAD,
  COUNT(ES_DIR_PPAL) AS CON_PPAL
FROM DIRS_CLIENTES;
```

| Campo | No-nulo o valor | % sobre 45,217 |
|---|---|---|
| Total registros | 45,217 | — |
| `TELEFONO1` no-nulo | 23,823 | 52.7% |
| **`TELEFONO1` no-nulo y no-vacío** | **23,820** | **52.7%** |
| `TELEFONO2` no-nulo y no-vacío | 1,858 | 4.1% |
| `EMAIL` no-vacío | 2 | ~0.0% |
| `RFC_CURP` no-vacío | 1 | ~0.0% |
| `COLONIA` no-vacío | 45,156 | 99.9% |
| `CIUDAD_ID` no-nulo | 45,083 | 99.7% |
| `ES_DIR_PPAL` no-nulo | 45,217 | 100.0% |

**Cobertura telefónica: 52.7% sobre todos los registros de dirección.**

La relación es prácticamente 1:1 entre `CLIENTES` y `DIRS_CLIENTES` (45,216 vs 45,217
filas). La consulta en direcciones principales (`ES_DIR_PPAL = 'S'`) también devolvió
45,216 filas con las mismas cifras de teléfono — confirma que es un registro por cliente.

**Muestra de formato de TELEFONO1:**
```
238 162 3585
238 236 7726
236 113 3320
238 208 6384
236 120 7376
```
Formato predominante: `NNN NNN NNNN` (10 dígitos separados por espacios). Lada 238 = Tehuacán.

### Distribución CIUDAD_ID (top 10)

```sql
SELECT CIUDAD_ID, COUNT(*) AS CNT FROM DIRS_CLIENTES GROUP BY CIUDAD_ID ORDER BY CNT DESC ROWS 10;
```

| CIUDAD_ID | Cantidad | Notas |
|---|---|---|
| 338 | 14,120 | Tehuacán (ciudad principal) |
| 11115 | 5,586 | |
| 11371 | 3,263 | |
| 11599 | 2,119 | |
| 22939 | 2,050 | |
| 11605 | 2,047 | |
| 11373 | 1,950 | |
| 11640 | 1,534 | |
| 11608 | 1,485 | |
| 26220 | 1,432 | |

`CIUDAD_ID=338` (Tehuacán) es la ciudad dominante (31.2% de registros). Los IDs
restantes corresponden a poblaciones de la zona de cobertura (requieren join a
`CIUDADES` para nombres).

---

## 3. Tablas MSP propias — inventario y cobertura

### MSP_LOCAL_SALE (reemplaza al documentado MSP_VENTAS)

```sql
SELECT RF.RDB$FIELD_NAME, F.RDB$FIELD_TYPE, F.RDB$FIELD_LENGTH, RF.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS RF
JOIN RDB$FIELDS F ON F.RDB$FIELD_NAME = RF.RDB$FIELD_SOURCE
WHERE RF.RDB$RELATION_NAME = 'MSP_LOCAL_SALE'
ORDER BY RF.RDB$FIELD_POSITION;
```

| Columna | Tipo SQL | Long | NOT NULL | Descripción |
|---|---|---|---|---|
| `LOCAL_SALE_ID` | CHAR(36) | 36 | ✅ | UUID PK |
| `NOMBRE_CLIENTE` | VARCHAR(200) | 200 | ✅ | Nombre capturado en el momento de la venta |
| `FECHA_VENTA` | TIMESTAMP | 8 | ✅ | |
| `LATITUD` / `LONGITUD` | DOUBLE PRECISION | 8 | ✅ | GPS del vendedor en el momento de la venta |
| `DIRECCION` | VARCHAR(300) | 300 | ✅ | Dirección de entrega libre |
| `PARCIALIDAD` | NUMERIC(12,0) | 8 | ✅ | Monto de pago periódico |
| `ENGANCHE` | NUMERIC(12,0) | 8 | — | |
| **`TELEFONO`** | **VARCHAR(30)** | **30** | **✅** | **Teléfono capturado por el vendedor en campo** |
| `FREC_PAGO` | VARCHAR(50) | 50 | ✅ | Frecuencia de pago |
| **`AVAL_O_RESPONSABLE`** | **VARCHAR(200)** | **200** | **—** | **Nombre del aval o responsable de la deuda** |
| `NOTA` | VARCHAR(500) | 500 | — | |
| `DIA_COBRANZA` | VARCHAR(20) | 20 | ✅ | Día de cobro |
| `PRECIO_TOTAL` | NUMERIC(12,0) | 8 | ✅ | |
| `TIEMPO_A_CORTO_PLAZO_MESES` | INTEGER | 4 | ✅ | |
| `MONTO_A_CORTO_PLAZO` | NUMERIC(12,0) | 8 | ✅ | |
| `ENVIADO` | BOOLEAN | 1 | — | Si fue procesada/enviada a Microsip |
| `USER_EMAIL` | VARCHAR(255) | 255 | — | Usuario vendedor |
| `ALMACEN_ID` | INTEGER | 4 | — | |
| `NUMERO` | VARCHAR(20) | 20 | — | Folio de venta |
| `COLONIA` | VARCHAR(120) | 120 | — | |
| `POBLACION` | VARCHAR(120) | 120 | — | |
| `CIUDAD` | VARCHAR(120) | 120 | — | |
| `TIPO_VENTA` | VARCHAR(20) | 20 | — | |
| `ZONA_CLIENTE_ID` | INTEGER | 4 | — | Zona de la venta |
| `ALMACEN_DESTINO_ID` | INTEGER | 4 | — | |
| `MONTO_CONTADO` | NUMERIC(0,0) | 8 | — | |

### Cobertura MSP_LOCAL_SALE (6,438 registros)

```sql
SELECT
  COUNT(*) AS TOTAL,
  COUNT(TELEFONO) AS CON_TEL,
  COUNT(CASE WHEN TELEFONO IS NOT NULL AND TELEFONO <> '' THEN 1 END) AS TEL_NO_VACIO,
  COUNT(AVAL_O_RESPONSABLE) AS CON_AVAL,
  COUNT(CASE WHEN AVAL_O_RESPONSABLE IS NOT NULL AND AVAL_O_RESPONSABLE <> '' THEN 1 END) AS AVAL_NO_VACIO,
  MIN(FECHA_VENTA) AS PRIMERA_VENTA,
  MAX(FECHA_VENTA) AS ULTIMA_VENTA
FROM MSP_LOCAL_SALE;
```

| Campo | Valor | % |
|---|---|---|
| Total ventas de campo | 6,438 | — |
| `TELEFONO` no-nulo | 6,438 | 100.0% |
| `TELEFONO` no-vacío | 5,756 | 89.4% |
| `AVAL_O_RESPONSABLE` no-nulo | 6,438 | 100.0% |
| `AVAL_O_RESPONSABLE` no-vacío | 198 | 3.1% |
| Rango de fechas | 2025-09-13 → 2026-06-11 | ~9 meses |

**`MSP_LOCAL_SALE.TELEFONO` es la fuente más fresca de móviles** (89.4% cobertura efectiva
sobre ventas de campo capturadas desde sep 2025). La cobertura baja de `AVAL_O_RESPONSABLE`
(3.1%) indica que el campo está disponible en la app pero no se captura rutinariamente.

**Enlace a CLIENTES:** `MSP_LOCAL_SALE` **no tiene `CLIENTE_ID`** — las ventas de campo
representan clientes nuevos no-capturados en Microsip aún. El enlace es por `NOMBRE_CLIENTE`
(texto libre) o por la conversión posterior cuando se registra en Microsip.

### MSP_PAGOS_RECIBIDOS (454,838 registros)

Columnas: `ID` (CHAR/UUID), `DOCTO_CC_ID` (INTEGER, FK a `DOCTOS_CC`), `FECHA` (TIMESTAMP).
Tabla de log de pagos. Enlace a `CLIENTES` via `DOCTOS_CC.CLIENTE_ID`.

### MSP_VISITAS (295,871 registros)

| Columna | Tipo | NOT NULL | Descripción |
|---|---|---|---|
| `ID` | CHAR(36) | ✅ | UUID PK |
| `COBRADOR` | VARCHAR(150) | ✅ | Nombre del cobrador |
| `COBRADOR_ID` | INTEGER | ✅ | FK `COBRADORES` |
| `FECHA` | TIMESTAMP | ✅ | |
| `FORMA_COBRO_ID` | INTEGER | ✅ | FK forma de cobro |
| `LAT` / `LNG` | DOUBLE PRECISION | ✅ | GPS de la visita |
| `NOTA` | VARCHAR(10,000) | — | |
| `TIPO_VISITA` | VARCHAR(100) | ✅ | |
| `ZONA_CLIENTE_ID` | INTEGER | ✅ | |
| `CLIENTE_ID` | INTEGER | ✅ | FK `CLIENTES` |
| `IMPTE_DOCTO_CC_ID` | INTEGER | — | FK pago específico |

Permite derivar frecuencia de visitas, patrones de pago, y correlación cobrador-cliente.

### MSP_CHANGE_LOG

Columnas: `LOG_ID` (INTEGER), `TABLE_NAME` (VARCHAR 50), `RECORD_ID` (VARCHAR 50),
`OPERATION` (VARCHAR 10), `CHANGE_TIMESTAMP` (TIMESTAMP), `PROCESSED` (INTEGER).
Producido por el trigger legacy `MSP_CLIENTES_LISTEN`. Sin consumer activo en el Go actual.

### MSP_GARANTIAS — [PENDIENTE]

Esquema no obtenido por lock de BD. Columnas esperadas según código Go: relaciona
productos con garantías post-venta. Tabla presente, filas por confirmar.

---

## 10. Identidad y señales de fraude

### CLAVES_CLIENTES (tabla Microsip)

Documentado contra código Go (`cliente_writer.go`) y doc `microsip-crear-cliente-paso-a-paso.md`.
No fue posible ejecutar consultas live por lock de BD.

| Columna | Tipo SQL | NOT NULL | Notas |
|---|---|---|---|
| `CLAVE_CLIENTE_ID` | INTEGER | ✅ | PK; trigger `CLAVES_CLIENTES_BEFINS` asigna el valor real cuando se inserta con -1 |
| `CLAVE_CLIENTE` | VARCHAR(20) | ✅ | Código alfanumérico tipo `"0044523"` (7 dígitos zero-padded para "principal") |
| `CLIENTE_ID` | INTEGER | ✅ | FK `CLIENTES` |
| `ROL_CLAVE_CLI_ID` | INTEGER | ✅ | FK `ROLES_CLAVES_CLIENTES`; `2`="Principal" |

**Nota arquitectural crítica:** La tabla tiene al menos una página corrupta que
causa error de motor en full-table scans. El código Go usa únicamente acceso
por `CLIENTE_ID` con índice (`CLAVES_CLIENTES_AK1`), evitando el scan completo.
Cualquier consulta de cobertura debe filtrar por `CLIENTE_ID` o usar un índice.

```sql
-- Cobertura estimada (NO ejecutar como full scan):
SELECT FIRST 1 CLAVE_CLIENTE FROM CLAVES_CLIENTES WHERE CLIENTE_ID = 3083038 ORDER BY CLAVE_CLIENTE_ID;
-- [PENDIENTE: confirmar con lock liberado]
```

### LIBRES_CLIENTES — tabla custom Mueblera

Columnas relevantes para identidad (documentado en `microsip-crear-cliente-paso-a-paso.md`):

| Columna | Tipo | Notas |
|---|---|---|
| `CLIENTE_ID` | INTEGER | PK + FK `CLIENTES` |
| `CELULAR` | VARCHAR(99) | Alternativo — en práctica vacío; el teléfono va en `DIRS_CLIENTES.TELEFONO1` |
| `COMPROBANTE_DE_DOMICILIO` | INTEGER | FK catálogo Mueblera; `2992`=default observado |
| `IDENTIFICACION_OFICIAL` | INTEGER | FK catálogo Mueblera; `6597`/`6598`=INE/pasaporte |
| `REFERENCIA` | VARCHAR(99) | Descripción de ubicación: "CASA COLOR AZUL, 2 PISOS" |
| `U_LATITUD` / `U_LONGITUD` | VARCHAR(99) | GPS opcional capturado en el alta |
| `LOCALIDAD` | INTEGER | FK catálogo; `-1`=sin localidad |

**Señal de identidad:** `IDENTIFICACION_OFICIAL` indica el tipo de ID presentada
pero no almacena el número de documento. No hay número de INE, CURP, ni número
de credencial en ninguna tabla Microsip — la cobertura real de identidad formal
es prácticamente nula salvo el `RFC_CURP` de `DIRS_CLIENTES` (0.002% poblado).

### Señales de fraude compartido — EJECUTADO (2026-06-13)

**Resultados agregados:**

| Señal | Casos | Clientes afectados |
|---|---|---|
| Teléfonos compartidos por >1 cliente | **1,615** números | **3,391** clientes (~14% de los 23,820 con teléfono) |
| Caso extremo | 1 teléfono → **7 clientes distintos** | (`238 380 8758`) |
| `CALLE` compartida por >1 cliente | 4,390 calles | señal **débil** — sin número exterior no discrimina hogar real |
| Aval repetido en `MSP_LOCAL_SALE` | **8** avales | señal casi inservible (cobertura aval 3.1%) |

> **Lectura:** el teléfono compartido es la única señal de atributo-compartido con volumen
> accionable, pero está limitada por la cobertura del 52.7% de teléfono. La dirección por `CALLE`
> sin número exterior es ruidosa. El aval es inservible por falta de captura.


#### Teléfono compartido entre distintos clientes

```sql
-- Clientes distintos con el mismo TELEFONO1 (señal de fraude/familia):
SELECT TELEFONO1, COUNT(DISTINCT CLIENTE_ID) AS N_CLIENTES
FROM DIRS_CLIENTES
WHERE TELEFONO1 IS NOT NULL AND TELEFONO1 <> ''
GROUP BY TELEFONO1
HAVING COUNT(DISTINCT CLIENTE_ID) > 1
ORDER BY N_CLIENTES DESC
ROWS 20;
-- [PENDIENTE: lock de BD]
```

#### Dirección compartida entre distintos clientes

```sql
-- Clientes distintos con misma CALLE (señal de hogar/fraude):
SELECT CALLE, COUNT(DISTINCT CLIENTE_ID) AS N_CLIENTES
FROM DIRS_CLIENTES
WHERE CALLE IS NOT NULL AND CALLE <> ''
GROUP BY CALLE
HAVING COUNT(DISTINCT CLIENTE_ID) > 1
ORDER BY N_CLIENTES DESC
ROWS 20;
-- [PENDIENTE: lock de BD]
```

#### Aval compartido en MSP_LOCAL_SALE

```sql
-- Mismo nombre de aval aparece en múltiples ventas de campo:
SELECT AVAL_O_RESPONSABLE, COUNT(*) AS N_VENTAS,
       COUNT(DISTINCT NOMBRE_CLIENTE) AS N_CLIENTES_DISTINTOS
FROM MSP_LOCAL_SALE
WHERE AVAL_O_RESPONSABLE IS NOT NULL AND AVAL_O_RESPONSABLE <> ''
GROUP BY AVAL_O_RESPONSABLE
HAVING COUNT(*) > 1
ORDER BY N_VENTAS DESC
ROWS 20;
-- [PENDIENTE: lock de BD]
```

---

## 11. Calidad de datos y brechas para perfilado

### Resumen de cobertura de señales clave

| Señal | Tabla | Cobertura | Calidad |
|---|---|---|---|
| Teléfono (cliente registrado) | `DIRS_CLIENTES.TELEFONO1` | **52.7%** de 45,217 | Formato heterogéneo (espacios, sin lada uniforme) |
| Teléfono (ventas de campo) | `MSP_LOCAL_SALE.TELEFONO` | 89.4% de 6,438 | Datos frescos sep 2025–jun 2026 |
| Email | `DIRS_CLIENTES.EMAIL` | ~0.0% | No usable |
| RFC/CURP | `DIRS_CLIENTES.RFC_CURP` | ~0.0% | No usable |
| Número de INE | Ninguna tabla | — | **GAP TOTAL** |
| CURP numérico | Ninguna tabla | — | **GAP TOTAL** |
| Tipo de ID presentada | `LIBRES_CLIENTES.IDENTIFICACION_OFICIAL` | Presente (tipo, no número) | FK a catálogo, no el número real |
| Dirección (colonia) | `DIRS_CLIENTES.COLONIA` | 99.9% | Texto libre, no normalizado |
| GPS de alta | `LIBRES_CLIENTES.U_LATITUD/U_LONGITUD` | Bajo — solo altas desde app | Solo desde msp-api go |
| GPS de visita | `MSP_VISITAS.LAT/LNG` | 100% en registros MSP | 295,871 visitas de cobranza |
| Aval/referente | `MSP_LOCAL_SALE.AVAL_O_RESPONSABLE` | 3.1% de ventas campo | Muy baja captura |
| Fecha de alta | `CLIENTES.FECHA_HORA_CREACION` | 100% | |
| Historial de estatus | Solo `ESTATUS` actual | Sin historial temporal | **GAP**: no hay tabla de log de cambios de estado |

### Brechas críticas para perfil de cliente-inteligencia

1. **Identidad formal inexistente:** No se almacena número de INE, CURP, NIP o biometría.
   `LIBRES_CLIENTES.IDENTIFICACION_OFICIAL` guarda solo el _tipo_ de documento, no el número.
   Imposible deduplicar clientes por identidad formal desde la BD.

2. **Email inexistente:** Sólo 2 registros en 45,217 — inutilizable para comunicación digital
   o enriquecimiento.

3. **Teléfono con cobertura parcial:** 47.3% de clientes registrados en Microsip no tiene
   teléfono en `DIRS_CLIENTES`. El 52.7% presente tiene formato heterogéneo (con/sin espacios,
   con/sin lada). Normalización requerida antes de usarlo para matching.

4. **Aval no capturado sistemáticamente:** `MSP_LOCAL_SALE.AVAL_O_RESPONSABLE` existe
   pero solo el 3.1% de ventas de campo lo registra. No hay tabla de avales estructurada.

5. **Sin historial de estados de cliente:** `CLIENTES.ESTATUS` refleja solo el estado
   actual. No existe tabla de auditoría que permita reconstruir cuándo un cliente
   pasó de activo a bloqueado o viceversa.

6. **MSP_VENTAS y MSP_SALDOS_VENTAS no existen** en este snapshot — el mapa documentado
   estaba desactualizado. La funcionalidad está en `MSP_LOCAL_SALE` (ventas campo) y
   el saldo en `DOCTOS_CC`/`IMPTS_DOCTOS_CC`.

7. **CLAVES_CLIENTES tiene corrupción de datos:** Al menos una página corrupta impide
   full-table scans. Las consultas de cobertura de claves deben hacerse por `CLIENTE_ID`
   individual o reconstruirse via backup limpio.

8. **`MSP_LOCAL_SALE` no enlaza directamente a `CLIENTES`:** Las ventas de campo
   capturan `NOMBRE_CLIENTE` como texto libre. El enriquecimiento cruzado requiere
   lógica de matching difuso o el proceso de conversión a Microsip que asigna `CLIENTE_ID`.


---

# Bloque fuente: B2-cxc-pago.md

# B2 — CxC, Comportamiento de Pago y Estructura de Atribución

> Snapshot: `MUEBLERA_SNP.fdb` · datos hasta 2026-06-11. Modo embedded (un solo lector a la vez).
> Valores monetarios en MXN. Mojibake Win1252 en campos NOMBRE con tildes (leídos con `-ch UTF8`).

---

## 4. CONCEPTOS_CC — catálogo completo reconciliado

### 4.1 SQL ejecutado

```sql
SELECT CONCEPTO_CC_ID, NOMBRE, NATURALEZA FROM CONCEPTOS_CC ORDER BY CONCEPTO_CC_ID;
-- 34 filas
```

### 4.2 Catálogo completo con estado vs documentado

| CONCEPTO_CC_ID | NOMBRE | NATURALEZA | Estado |
|---|---|---|---|
| 4 | Venta | C | — (no documentado) |
| **5** | **Venta en mostrador** | **C** | **VERIFICADO** — cargo de venta a crédito |
| 6 | Cheque devuelto | C | — |
| 7 | Interés moratorio | C | — |
| 8 | Nota de cargo | C | — |
| 9 | Saldo inicial | C | — |
| 10 | Devolución de saldo por acreditar | C | — |
| **11** | **Cobro** | **R** | **VERIFICADO** |
| 12 | Devolución | R | — |
| 13 | Devolución en mostrador | R | — |
| 14 | Nota de crédito | R | — |
| 15 | Ajuste de saldo | R | — |
| 16 | Aplicación de saldo por acreditar | R | — |
| **155** | **Cobro en mostrador** | **R** | **VERIFICADO** |
| 181 | Anticipo (uso anterior) | R | — |
| 182 | Devolución de anticipo | C | — |
| 183 | Aplicación de anticipo | R | — |
| 201 | Anticipo | C | — |
| **24533** | **Enganche** | **R** | **VERIFICADO** — abono de enganche aplicado contra cargo 5 |
| 25116 | No utilizar Condonación por pronto pago | R | — (obsoleto) |
| 25117 | No utilizar Cobro por Cancelación | R | — (obsoleto) |
| 27774 | Traspaso Efectivo Cta Ant | R | — |
| **27966** | **Cancelaciones** | **R** | **VERIFICADO** |
| **27967** | **Fugas** | **R** | **VERIFICADO** |
| **27968** | **Mal Cliente** | **R** | **VERIFICADO** |
| **27969** | **Condonaciones** | **R** | **VERIFICADO** |
| 27970 | Devolución de Efectivo por Cancelación | C | — |
| 52604 | Factoraje financiero | C | — |
| 52605 | Cesión de deuda por factoraje | R | — |
| 55334 | FCOBRADOR | R | — (cobrador fiscal) |
| 84453 | IVA Crédito aplicado del 50% | R | — |
| **87327** | **Cobranza en ruta** | **R** | **VERIFICADO** (documentado como "Cobranza ruta") |
| 247507 | Cobro Anticipo de Apartado | R | — |
| 3036289 | Ajuste de saldo de anticipo | C | — |

**Resumen reconciliación:**

| Código documentado | Nombre real | Estado |
|---|---|---|
| 5 | Venta en mostrador | VERIFICADO (nombre levemente distinto, es el cargo de crédito) |
| 11 | Cobro | VERIFICADO |
| 155 | Cobro en mostrador | VERIFICADO |
| 24533 | Enganche | VERIFICADO |
| 27966 | Cancelaciones | VERIFICADO |
| 27967 | Fugas | VERIFICADO |
| 27968 | Mal Cliente | VERIFICADO |
| 27969 | Condonaciones | VERIFICADO |
| 87327 | Cobranza en ruta | VERIFICADO |

**Ningún código documentado resultó WRONG o MISSING.**

---

### 4.3 Columnas de DOCTOS_CC

108,149 cargos (TIPO_IMPTE='C') · 2,387,184 documentos totales · rango 2006-03-26 → 2026-06-11.

| Columna | Tipo Firebird | Nullable | Notas |
|---|---|---|---|
| `DOCTO_CC_ID` | INTEGER | NOT NULL | PK |
| `CONCEPTO_CC_ID` | INTEGER | NOT NULL | FK → CONCEPTOS_CC |
| `FOLIO` | VARCHAR(9) | NULL | e.g. `AJ0002163`, `CR1864171` |
| `NATURALEZA_CONCEPTO` | CHAR(1) | NOT NULL | `C`/`R` |
| `SUCURSAL_ID` | INTEGER | NOT NULL | sucursal |
| `FECHA` | DATE | NOT NULL | fecha del documento |
| `HORA` | TIME | NULL | hora |
| `CLAVE_CLIENTE` | VARCHAR(20) | NULL | clave legible |
| `IMPORTE_COBRO` | NUMERIC(15,2) | NULL | solo en cobros |
| `FECHA_HORA_PAGO` | TIMESTAMP | NULL | timestamp pago |
| `CLIENTE_ID` | INTEGER | NOT NULL | FK → CLIENTES |
| `TIPO_CAMBIO` | NUMERIC(18,6) | NULL | tipo de cambio |
| `CANCELADO` | CHAR(1) | NULL | `N`=vigente, `S`=cancelado |
| `APLICADO` | CHAR(1) | NOT NULL | `S` (único valor observado) |
| `DESCRIPCION` | VARCHAR(200) | NULL | descripción libre |
| `COBRADOR_ID` | INTEGER | NULL | FK → COBRADORES (99.5% poblado en cobros 87327) |
| `COND_PAGO_ID` | INTEGER | NULL | FK → CONDICIONES_PAGO |
| `FECHA_DSCTO_PPAG` | DATE | NULL | fecha límite descuento PPago |
| `ESTATUS` | CHAR(1) | NULL | estatus |
| `METODO_PAGO_SAT` | CHAR(3) | NULL | clave SAT |
| `USO_CFDI` | CHAR(4) | NULL | uso CFDI |
| `LAT` / `LON` | VARCHAR(100) | NULL | coordenadas GPS (app móvil) |
| `USUARIO_CREADOR` | VARCHAR(31) | NULL | usuario creador |
| `FECHA_HORA_CREACION` | TIMESTAMP | NULL | timestamp creación |
| *(+ 35 columnas de auditoría, CFDI, contabilidad)* | | | |

**Total:** 59 columnas.

---

### 4.4 Columnas de IMPORTES_DOCTOS_CC

**2,392,535 registros totales.** Distribución TIPO_IMPTE: C=108,149 (cargos raíz) · R=2,284,359 (abonos aplicados) · NULL=27 (sin clasificar).

| Columna | Tipo | Nullable | Notas |
|---|---|---|---|
| `IMPTE_DOCTO_CC_ID` | INTEGER | NOT NULL | PK |
| `DOCTO_CC_ID` | INTEGER | NOT NULL | FK → DOCTOS_CC (documento contenedor) |
| `FECHA` | DATE | NOT NULL | fecha del importe |
| `CANCELADO` | CHAR(1) | NULL | `N`=vigente |
| `APLICADO` | CHAR(1) | NOT NULL | `S` (único valor) |
| `ESTATUS` | CHAR(1) | NOT NULL | estatus |
| **`TIPO_IMPTE`** | CHAR(1) | NULL | **`C`=cargo raíz, `R`=abono/crédito aplicado** |
| **`DOCTO_CC_ACR_ID`** | INTEGER | NULL | **ID del cargo al que se aplica (clave del linkage)** |
| `IMPORTE` | NUMERIC(15,2) | NULL | importe sin IVA |
| `IMPUESTO` | NUMERIC(15,2) | NULL | IVA |
| `IVA_RETENIDO` | NUMERIC(15,2) | NULL | IVA retenido |
| `ISR_RETENIDO` | NUMERIC(15,2) | NULL | ISR retenido |
| `DSCTO_PPAG` | NUMERIC(15,2) | NULL | descuento pronto pago |
| `PCTJE_COMIS_COB` | NUMERIC(9,6) | NULL | % comisión cobrador |

#### Patrón DOCTO_CC_ACR_ID verificado

```sql
SELECT TIPO_IMPTE,
  SUM(CASE WHEN DOCTO_CC_ACR_ID = DOCTO_CC_ID THEN 1 ELSE 0 END) AS self_link,
  SUM(CASE WHEN DOCTO_CC_ACR_ID <> DOCTO_CC_ID THEN 1 ELSE 0 END) AS cross_link
FROM IMPORTES_DOCTOS_CC GROUP BY TIPO_IMPTE;
```

| TIPO_IMPTE | self_link (cargo raíz) | cross_link (abono aplicado a cargo) |
|---|---|---|
| C | 108,149 | 0 |
| R | 0 | 2,284,359 |

**Regla:** `TIPO_IMPTE='C'` → `DOCTO_CC_ACR_ID = DOCTO_CC_ID` (auto-referencia: es el importe del cargo original). `TIPO_IMPTE='R'` → `DOCTO_CC_ACR_ID` = ID del DOCTO_CC de concepto 5 al que este abono aplica.

---

### 4.5 Derivación: saldo abierto y % pagado por cliente (verificado)

#### SQL validado en 5 clientes muestra

```sql
SELECT
  c.CLIENTE_ID,
  COUNT(DISTINCT CASE WHEN i.TIPO_IMPTE = 'C' THEN i.DOCTO_CC_ACR_ID END) AS num_cargos,
  SUM(CASE WHEN i.TIPO_IMPTE = 'C'
        THEN CAST(i.IMPORTE + i.IMPUESTO AS DOUBLE PRECISION) ELSE 0 END) AS total_cargo,
  SUM(CASE WHEN i.TIPO_IMPTE = 'R'
        THEN CAST(i.IMPORTE AS DOUBLE PRECISION) ELSE 0 END)              AS total_abonado,
  SUM(CASE WHEN i.TIPO_IMPTE = 'C'
        THEN CAST(i.IMPORTE + i.IMPUESTO AS DOUBLE PRECISION) ELSE 0 END)
  - SUM(CASE WHEN i.TIPO_IMPTE = 'R'
        THEN CAST(i.IMPORTE AS DOUBLE PRECISION) ELSE 0 END)              AS saldo_pendiente
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC c ON c.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
WHERE c.CONCEPTO_CC_ID = 5 AND c.CANCELADO = 'N' AND i.CANCELADO = 'N'
  AND c.CLIENTE_ID IN (2782515, 1267580, 2629511, 1464283, 3092126)
GROUP BY c.CLIENTE_ID ORDER BY c.CLIENTE_ID;
```

**Resultado medido:**

| CLIENTE_ID | num_cargos | total_cargo | total_abonado | saldo_pendiente | % pagado |
|---|---|---|---|---|---|
| 1267580 | 4 | $16,700 | $15,150 | $1,550 | 90.7% |
| 1464283 | 9 | $43,000 | $33,300 | $9,700 | 77.4% |
| 2629511 | 3 | $33,200 | $24,200 | $9,000 | 72.9% |
| 2782515 | 4 | $32,200 | $20,550 | $11,650 | 63.8% |
| 3092126 | 1 | $8,600 | $500 | $8,100 | 5.8% |

**Verificación detallada — cargo 14357102 (cliente 2782515, venta 2025-10-30):**

```sql
SELECT i.DOCTO_CC_ID, i.DOCTO_CC_ACR_ID, i.TIPO_IMPTE, i.IMPORTE, i.IMPUESTO, i.CANCELADO,
       d.CONCEPTO_CC_ID, d.FECHA
FROM IMPORTES_DOCTOS_CC i JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = i.DOCTO_CC_ID
WHERE i.DOCTO_CC_ACR_ID = 14357102 AND i.CANCELADO = 'N'
ORDER BY i.DOCTO_CC_ID;
-- 24 filas: 1 cargo C + 23 abonos R (cobros semanales ruta 87327)
```

Cargo: $8,100.00 (incl. IVA). Total abonado: $4,550.00 (23 pagos de $150–$200). Saldo: $3,550.00.

```sql
SELECT
  SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN CAST(i.IMPORTE + i.IMPUESTO AS DOUBLE PRECISION) ELSE 0 END) AS total_cargo,
  SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN CAST(i.IMPORTE AS DOUBLE PRECISION) ELSE 0 END) AS total_abonado,
  SUM(CASE WHEN i.TIPO_IMPTE = 'C' THEN CAST(i.IMPORTE + i.IMPUESTO AS DOUBLE PRECISION) ELSE 0 END) -
  SUM(CASE WHEN i.TIPO_IMPTE = 'R' THEN CAST(i.IMPORTE AS DOUBLE PRECISION) ELSE 0 END) AS saldo
FROM IMPORTES_DOCTOS_CC i WHERE i.DOCTO_CC_ACR_ID = 14357102 AND i.CANCELADO = 'N';
-- total_cargo=8100, total_abonado=4550, saldo=3550
```

#### Comportamiento de pago: días a pagar

```sql
SELECT
  cargo.FECHA AS fecha_cargo, cobro.FECHA AS fecha_cobro,
  cobro.CONCEPTO_CC_ID, i.IMPORTE,
  CAST(cobro.FECHA - cargo.FECHA AS INTEGER) AS dias_a_pagar
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC cargo ON cargo.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
JOIN DOCTOS_CC cobro ON cobro.DOCTO_CC_ID = i.DOCTO_CC_ID
WHERE i.DOCTO_CC_ACR_ID = 14357102 AND i.TIPO_IMPTE = 'R' AND i.CANCELADO = 'N'
ORDER BY cobro.FECHA;
```

Patrón cliente 2782515 (cargo oct-2025): cobros semanales cada 7 días, días_a_pagar del primer cobro = 5, del último (jun-2026) = 222. El cliente está activo pagando; no hay señal de mora en el ERP porque `COND_PAGO_ID=21497` = "CREDITO 12 MESES" (365 días de plazo, DIAS_PLAZO=365 en CONDICIONES_PAGO/PLAZOS_COND_PAG).

#### Aggregate total portafolio

```sql
SELECT
  COUNT(DISTINCT c.CLIENTE_ID) AS num_clientes,
  SUM(CASE WHEN i.TIPO_IMPTE = 'C'
        THEN CAST(i.IMPORTE + i.IMPUESTO AS DOUBLE PRECISION) ELSE 0 END) AS total_facturado,
  SUM(CASE WHEN i.TIPO_IMPTE = 'R'
        THEN CAST(i.IMPORTE AS DOUBLE PRECISION) ELSE 0 END)              AS total_cobrado,
  SUM(CASE WHEN i.TIPO_IMPTE = 'C'
        THEN CAST(i.IMPORTE + i.IMPUESTO AS DOUBLE PRECISION) ELSE 0 END)
  - SUM(CASE WHEN i.TIPO_IMPTE = 'R'
        THEN CAST(i.IMPORTE AS DOUBLE PRECISION) ELSE 0 END)              AS saldo_abierto_total
FROM IMPORTES_DOCTOS_CC i
JOIN DOCTOS_CC c ON c.DOCTO_CC_ID = i.DOCTO_CC_ACR_ID
WHERE c.CONCEPTO_CC_ID = 5 AND c.CANCELADO = 'N' AND i.CANCELADO = 'N';
```

| Métrica | Valor |
|---|---|
| Clientes con al menos un cargo histórico | 44,774 |
| Total facturado (cargo incl. IVA, histórico) | $753,295,512 |
| Total cobrado (abonos no cancelados) | $698,530,529 |
| **Saldo abierto neto total** | **$54,764,983** |
| % cobrado sobre facturado | **92.7%** |

Cargos vigentes (concepto 5, no cancelados): **107,535** · rango 2006-03-26 → 2026-06-11.

#### Pagos por concepto (abonos R, no cancelados)

```sql
SELECT d.CONCEPTO_CC_ID, COUNT(*) AS cnt,
  SUM(CAST(i.IMPORTE AS DOUBLE PRECISION)) AS total_importe
FROM IMPORTES_DOCTOS_CC i JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = i.DOCTO_CC_ID
WHERE i.CANCELADO = 'N' AND d.CANCELADO = 'N' AND i.TIPO_IMPTE = 'R'
GROUP BY d.CONCEPTO_CC_ID ORDER BY total_importe DESC;
```

| Concepto | Nombre | Registros | Importe total |
|---|---|---|---|
| 87327 | Cobranza en ruta | 1,810,852 | $376,394,663 |
| 155 | Cobro en mostrador | 319,940 | $106,091,789 |
| 27969 | Condonaciones | 60,486 | $80,599,137 |
| 11 | Cobro | 13,834 | $71,997,603 |
| 24533 | Enganche | 67,015 | $34,435,273 |
| 27968 | Mal Cliente | 3,391 | $9,320,499 |
| 12 | Devolución | 1,218 | $6,377,440 |
| 27966 | Cancelaciones | 983 | $4,847,748 |
| 27967 | Fugas | 1,322 | $4,530,977 |
| otros | — | ~2,300 | ~$3,478,864 |

**Nota crítica:** Condonaciones ($80.6M, concepto 27969) es el tercer mayor flujo de crédito — supera a los cobros ERP tradicionales (concepto 11, $72M). Relevante para análisis de pérdidas por política de condonación.

---

### Calidad de datos de crédito/pagos (medido jun-2026)

> Hallazgos empíricos del módulo `internal/analytics` (R1/R1+). Los implementadores de R2/R3 deben
> conocerlos antes de tocar datos de pago.

#### 1. La fecha en `DOCTOS_CC` es la de validación en oficina, no la de cobro real

Microsip sobrescribe la fecha al momento de validar el pago en oficina.
Medido sobre el solape de **334,757 pagos** con `MSP_PAGOS_RECIBIDOS.FECHA` (timestamp de captura
real en app) vs. `DOCTOS_CC.FECHA` (fecha de validación):

| Métrica | Valor |
|---|---|
| Coincidencia mismo día | **96.7 %** |
| Desfase promedio | **0.07 días** |
| Desfase máximo observado | 13 días (1 solo caso >7) |

**Conclusión:** el ruido es despreciable para features a nivel semana/mes; usar `DOCTOS_CC.FECHA` es
aceptable. La fecha **real** de cobro (+ GPS lat/lon) vive en `MSP_PAGOS_RECIBIDOS.FECHA`, pero solo
para pagos capturados por la app (desde sep-2024); los históricos caen al `COALESCE` con la fecha
de Microsip.

#### 2. El plazo del crédito no se guarda de forma confiable

`DOCTOS_CC.COND_PAGO_ID → PLAZOS_COND_PAG.DIAS_PLAZO` no es fiable como plazo real del crédito, y
`LIBRES_CARGOS_CC.TIEMPO_A_CORTO_PLAZOMESES` es la porción **contable** "a corto plazo", no el plazo
pactado. → **Inferir** el plazo del plan de pagos: `pagos_esperados ≈ total_venta ÷ cuota`, donde la
cuota es `LIBRES_CARGOS_CC.PARCIALIDAD` (monto de pago periódico contratado), o el abono mediano
de `MSP_PAGOS_VENTAS.IMPORTE` como fallback si falta.

#### 3. Cadencias mixtas: inferir por crédito, no asumir uniform

Distribución medida sobre la cartera activa: **71 % semanal, 23 % quincenal, 6 % mensual**. Cada
crédito tiene su propia frecuencia de abono. La cadencia correcta se infiere como el gap modal entre
abonos del crédito, encajado a {7, 14, 30} días.

La puntualidad normalizada por cadencia (sin usar el plazo de Microsip) es:

```
stretch = lapso_real ÷ (n_pagos × cadencia_inferida)
```

Donde 1.0 = pagó clavado; ≥ 2.0 = atraso crónico. Este es el componente de solvencia en el score.

#### 4. Usar los caches materializados — no joins a nivel de fila

Los joins de `IMPORTES_DOCTOS_CC` sobre `DOCTOS_PV` a nivel de fila **explotan** (P ventas × K
cargos ≈ 450 M filas a escala completa) e inflan `MONETARY`/`SALDO`. Bug real encontrado y corregido
en el módulo analytics (R1).

Usar los caches materializados:

| Tabla | Grano | Columnas clave |
|---|---|---|
| `MSP_SALDOS_VENTAS` | 1 fila por cargo | `SALDO`, `NUM_PAGOS`, `FECHA_ULT_PAGO`, `IMPTE_REST`, `PRECIO_TOTAL`, `CARGO_CANCELADO` |
| `MSP_PAGOS_VENTAS` | 1 fila por abono | — |

Estas tablas son las que lee el módulo `internal/analytics` en R1/R1+.

#### 5. Separabilidad de la señal de solvencia (medido sobre 87,958 créditos)

| Segmento | Valor |
|---|---|
| Créditos liquidados | **88 %** |
| Tiempo-a-liquidar | spread **20×** entre rápido y lento |
| Puntuales (stretch ≈ 1.0) | **~56 %** |
| Atraso crónico (stretch ≥ 2) | **~10 %** |
| Con saldo abierto | **10,590** |
| Sin un solo pago en >6 meses | **409** |

→ La señal de solvencia es real y separable pese al ruido de fecha y plazo. Features a nivel
semana/mes son limpias para scoring.

---

## 5b. MSP_PAGOS_RECIBIDOS — captura de campo (app móvil)

### Columnas (solo 3)

```sql
SELECT rf.RDB$FIELD_NAME, rf.RDB$FIELD_POSITION, f.RDB$FIELD_TYPE, f.RDB$FIELD_LENGTH, rf.RDB$NULL_FLAG
FROM RDB$RELATION_FIELDS rf JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
WHERE rf.RDB$RELATION_NAME = 'MSP_PAGOS_RECIBIDOS' ORDER BY rf.RDB$FIELD_POSITION;
```

| Columna | Tipo | Nullable | Descripción |
|---|---|---|---|
| `ID` | CHAR(36) | NOT NULL | UUID v4 PK (generado en Go) |
| `DOCTO_CC_ID` | INTEGER | NOT NULL | FK → DOCTOS_CC (1:1 con cobro) |
| `FECHA` | TIMESTAMP | NOT NULL | timestamp exacto de captura en app móvil |

### Cobertura

```sql
SELECT COUNT(*) AS total_rows, MIN(FECHA) AS fecha_min, MAX(FECHA) AS fecha_max,
  COUNT(DISTINCT DOCTO_CC_ID) AS distinct_doctos
FROM MSP_PAGOS_RECIBIDOS;
```

| total_rows | fecha_min | fecha_max | distinct_doctos |
|---|---|---|---|
| 454,838 | 2024-09-15 13:13 | 2026-06-11 20:02 | 454,838 (1:1) |

Cada fila corresponde a un `DOCTO_CC_ID` único — no hay duplicados.

### Composición por concepto linkado

```sql
SELECT d.CONCEPTO_CC_ID, COUNT(*) AS cnt
FROM MSP_PAGOS_RECIBIDOS p JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = p.DOCTO_CC_ID
GROUP BY d.CONCEPTO_CC_ID ORDER BY cnt DESC;
```

| CONCEPTO_CC_ID | Nombre | Registros | % |
|---|---|---|---|
| 87327 | Cobranza en ruta | 437,742 | 96.2% |
| 27969 | Condonaciones | 65 | 0.01% |
| *(sin match en DOCTOS_CC)* | orphans | 17,031 | 3.7% |

### Orphans (cobros capturados sin sync al ERP)

```sql
SELECT EXTRACT(YEAR FROM p.FECHA) AS yr, COUNT(*) AS cnt
FROM MSP_PAGOS_RECIBIDOS p
WHERE NOT EXISTS (SELECT 1 FROM DOCTOS_CC d WHERE d.DOCTO_CC_ID = p.DOCTO_CC_ID)
GROUP BY yr ORDER BY yr;
```

| Año | Orphans |
|---|---|
| 2024 | 2,020 |
| 2025 | 9,779 |
| 2026 | 5,232 |
| **Total** | **17,031** |

### Registros matched por año

```sql
SELECT EXTRACT(YEAR FROM p.FECHA) AS yr, COUNT(*) AS cnt
FROM MSP_PAGOS_RECIBIDOS p JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = p.DOCTO_CC_ID
GROUP BY yr ORDER BY yr;
```

| Año | Matched |
|---|---|
| 2024 | 15,938 |
| 2025 | 274,193 |
| 2026 | 147,676 |

### Rol vs DOCTOS_CC — no es duplicativa

| Tabla | Rol | Escala 87327 |
|---|---|---|
| `DOCTOS_CC` | Registro contable del cobro; lo genera Microsip al confirmar el pago | 1,810,718 cobros |
| `MSP_PAGOS_RECIBIDOS` | Registro de captura de campo (timestamp móvil); confirma que la app recibió el cobro | 437,742 (desde sep-2024) |

`MSP_PAGOS_RECIBIDOS` permite calcular: (a) latencia app→ERP (`MSP.FECHA` vs `DOCTOS_CC.FECHA`), (b) cobros capturados pero no confirmados al ERP (orphans = 17,031), (c) fuente de verdad para el dashboard de cobranza en tiempo real. Escala: cubre ~24% del total histórico de cobros 87327, pero representa prácticamente el 100% desde la adopción de la app (sep-2024).

---

## 7. LIBRES_CARGOS_CC — datos de venta: enganche, plazo, vendedor, precio contado

### Columnas

```sql
SELECT rf.RDB$FIELD_NAME, rf.RDB$FIELD_POSITION, f.RDB$FIELD_TYPE, f.RDB$FIELD_PRECISION, f.RDB$FIELD_SCALE
FROM RDB$RELATION_FIELDS rf JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
WHERE rf.RDB$RELATION_NAME = 'LIBRES_CARGOS_CC' ORDER BY rf.RDB$FIELD_POSITION;
```

| Columna | Tipo | Descripción |
|---|---|---|
| `DOCTO_CC_ID` | INTEGER NOT NULL | PK/FK → DOCTOS_CC concepto 5 |
| `FORMA_DE_PAGO` | INTEGER | ID de forma de cobro (IDs ~33824–840308; referencia no resuelta en este snapshot) |
| `PARCIALIDAD` | SMALLINT(4) | Monto del pago periódico en MXN (ej. $100, $150, $200) |
| `CREDITO_EN_MESES` | INTEGER | ID del plan de crédito (FK opaca; valores dominantes: 33828, 840309, 33830, 33829) |
| `TIEMPO_A_CORTO_PLAZOMESES` | SMALLINT(2) | Meses del período a corto plazo |
| `MONTO_A_CORTO_PLAZO` | INTEGER(5) | Monto total del período corto |
| `VENDEDOR_1` | INTEGER | ID vendedor principal (sistema de comisiones; no enlaza a VENDEDORES.VENDEDOR_ID directamente) |
| `VENDEDOR_2` | INTEGER | ID vendedor secundario |
| `VENDEDOR_3` | INTEGER | ID vendedor terciario |
| `ENGANCHE` | NUMERIC(6,2) | Monto de enganche en MXN |
| `NUMERO_DE_VENDEDORES` | INTEGER | ID opaco (valores: 47559 dominante, 47558, 47560; no es un conteo) |
| `PRECIO_DE_CONTADO` | NUMERIC(17,2) | Precio si el cliente pagara de contado |
| `AVAL_O_RESPONSABLE` | VARCHAR(50) | Nombre del aval o responsable |
| `OBSERVACIONES` | VARCHAR(99) | Notas libres |

**Total:** 14 columnas. **99,423 registros.**

**Nota:** `FREC_PAGO` no existe en LIBRES_CARGOS_CC; está en `MSP_LOCAL_SALE` (app MSP). El equivalente aquí es `PARCIALIDAD` (monto por cobro) combinado con el plazo `CREDITO_EN_MESES`.

### Cobertura por año

```sql
SELECT EXTRACT(YEAR FROM d.FECHA) AS yr, COUNT(*) AS num_cargos,
  SUM(CASE WHEN lc.PRECIO_DE_CONTADO IS NOT NULL THEN 1 ELSE 0 END) AS has_precio_contado,
  SUM(CASE WHEN lc.ENGANCHE IS NOT NULL THEN 1 ELSE 0 END) AS has_enganche,
  SUM(CASE WHEN lc.VENDEDOR_1 IS NOT NULL THEN 1 ELSE 0 END) AS has_vendedor1,
  SUM(CASE WHEN lc.CREDITO_EN_MESES IS NOT NULL THEN 1 ELSE 0 END) AS has_credito_meses
FROM LIBRES_CARGOS_CC lc JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = lc.DOCTO_CC_ID
WHERE d.CONCEPTO_CC_ID = 5
GROUP BY yr ORDER BY yr;
```

| Año | Cargos LIBRES | PRECIO_CONTADO | ENGANCHE | VENDEDOR_1 | CREDITO_MESES |
|---|---|---|---|---|---|
| 2006–2017 | 10 | 10 | 10 | 10 | 10 |
| 2018 | 7,381 | 7,381 | 7,381 | 7,381 | 7,381 |
| 2019 | 9,706 | 9,706 | 9,706 | 9,706 | 9,706 |
| 2020 | 9,105 | 9,105 | 9,105 | 9,105 | 9,105 |
| 2021 | 11,424 | 11,424 | 11,424 | 11,424 | 11,424 |
| 2022 | 12,286 | 12,286 | 12,286 | 12,286 | 12,286 |
| 2023 | 13,201 | 13,201 | 13,201 | 13,201 | 13,201 |
| 2024 | 13,995 | 13,976 (−19) | 13,995 | 13,995 | 13,995 |
| 2025 | 15,240 | 15,229 (−11) | 15,240 | 15,240 | 15,240 |
| 2026 (parcial) | 7,061 | 7,061 | 7,061 | 7,061 | 7,061 |

**La tabla existe desde 2018, no desde 2024.** Cobertura ~100% desde 2018; los 30 faltantes de PRECIO_DE_CONTADO en 2024–2025 son ruido.

### Prima de financiamiento (precio crédito − precio contado)

```sql
SELECT FIRST 10
  d.DOCTO_CC_ID, d.FECHA, d.CLIENTE_ID,
  lc.PRECIO_DE_CONTADO, lc.ENGANCHE, lc.PARCIALIDAD,
  i.IMPORTE + i.IMPUESTO AS precio_credito_total
FROM LIBRES_CARGOS_CC lc
JOIN DOCTOS_CC d ON d.DOCTO_CC_ID = lc.DOCTO_CC_ID
JOIN IMPORTES_DOCTOS_CC i ON i.DOCTO_CC_ID = lc.DOCTO_CC_ID AND i.TIPO_IMPTE = 'C'
WHERE d.CONCEPTO_CC_ID = 5 AND d.CANCELADO = 'N'
ORDER BY d.FECHA DESC;
```

**Muestra jun-2026 (7 ventas):**

| precio_contado | precio_credito | enganche | parcialidad | spread abs | spread% |
|---|---|---|---|---|---|
| $6,200 | $9,400 | $600 | $200/semana | $3,200 | +51.6% |
| $2,800 | $4,600 | $300 | $100 | $1,800 | +64.3% |
| $4,900 | $7,400 | $400 | $150 | $2,500 | +51.0% |
| $3,800 | $5,900 | $300 | $100 | $2,100 | +55.3% |
| $5,000 | $8,200 | $0 | $150 | $3,200 | +64.0% |
| $9,900 | $14,700 | $1,000 | $300 | $4,800 | +48.5% |
| $9,500 | $13,800 | $0 | $300 | $4,300 | +45.3% |

**Prima de financiamiento típica: +45%–64% sobre precio de contado.** Base para calcular la rentabilidad del financiamiento vs el costo de cobranza en ruta.

### Distribución de PARCIALIDAD

| PARCIALIDAD | Registros | % |
|---|---|---|
| $100 | 32,504 | 32.7% |
| $150 | 22,990 | 23.1% |
| $200 | 12,539 | 12.6% |
| $1 (enganche único) | 8,382 | 8.4% |
| $300 | 4,342 | 4.4% |
| $50 | 2,990 | 3.0% |
| $250 | 2,895 | 2.9% |
| otros | 13,781 | 13.9% |

---

## 8. Vendedores, Cobradores, Rutas y Zonas

### 8.1 COBRADORES

**Columnas:** `COBRADOR_ID` (INTEGER PK NOT NULL), `NOMBRE` (VARCHAR 100 NOT NULL), `POLITICA_COMIS_COB_ID` (FK), `ES_PREDET` (CHAR 1), `OCULTO` (CHAR 1 NOT NULL), + auditoría (6 campos timestamp/usuario).

**Total:** 50 registros. Activos (OCULTO='N'): 43. Inactivos: 7.

Nomenclatura: `RUTA XX - NOMBRE COMPLETO`. Ejemplos representativos:

| COBRADOR_ID | Nombre | OCULTO |
|---|---|---|
| 11294 | RUTA 01 - JUAN CARLOS CASTRO | N |
| 11335 | RUTA 11 - PABLO VARGAS | N |
| 11499 | R/MSP_MATRIZ | N |
| 11505 | R/MSP_ZOQUITLAN | N |
| 23302 | MAYOREO | N |
| 77486 | RUTA 36 - OSCAR ROQUE | N |
| 179183 | R/MSP_TEHUACAN | N |
| 841525 | RUTA 39 - EMMANUEL MENDEZ | N |

**Uso en DOCTOS_CC (cobros concepto 87327):**

```sql
SELECT d.COBRADOR_ID, c.NOMBRE, COUNT(*) AS cnt
FROM DOCTOS_CC d LEFT JOIN COBRADORES c ON c.COBRADOR_ID = d.COBRADOR_ID
WHERE d.CONCEPTO_CC_ID = 87327 AND d.CANCELADO = 'N'
GROUP BY d.COBRADOR_ID, c.NOMBRE ORDER BY cnt DESC ROWS 1 TO 5;
```

| Cobrador | Nombre | Cobros históricos |
|---|---|---|
| 11335 | RUTA 11 - PABLO VARGAS | 68,815 |
| 11491 | RUTA 14 - MIGUEL ANGEL SUAREZ | 56,964 |
| 77486 | RUTA 36 - OSCAR ROQUE | 56,335 |
| 320087 | RUTA 38 - ALEXIS MARTINEZ | 55,943 |
| 11494 | RUTA 17 - JUAN ROGELIO MARTINEZ | 54,814 |

Cobertura `COBRADOR_ID` en cobros 87327: **99.5%** poblado (null: 8,272 / 1,810,718).

### 8.2 ZONAS_CLIENTES

**Columnas:** `ZONA_CLIENTE_ID` (INTEGER PK NOT NULL), `NOMBRE` (VARCHAR 50 NOT NULL), `CUENTA_CXC` (VARCHAR 30), `CUENTA_ANTICIPOS` (VARCHAR 30), `ES_PREDET` (CHAR 1), `OCULTO` (CHAR 1 NOT NULL), + auditoría.

**Total:** 46 zonas, todas activas (OCULTO='N'). Nomenclatura espeja COBRADORES 1:1: `R/01`–`R/39`, `R/MSP_MATRIZ`, `R/MSP_ZOQUITLAN`, `R/MSP_TEHUACAN`, `MAYOREO`, `MEDIO MAYOREO`, `TRABAJADORES MSP`, `R/ABOGADO`.

**Linkage → CLIENTES.ZONA_CLIENTE_ID:** 10,206/10,207 clientes activos con zona asignada (99.99% cobertura).

### 8.3 GRUPOS_RUTAS

**Columnas:** `GRUPO_RUTAS_ID` (INTEGER PK), `NOMBRE` (VARCHAR 30 NOT NULL), + auditoría, `VERSION_REGISTRO`.

**1 solo registro:** `GRUPO_RUTAS_ID=87342`, `NOMBRE='RUTAS'`. Categoría contenedora única.

### 8.4 RUTAS y RUTAS_DET

**RUTAS columnas:** `RUTA_ID` (PK), `NOMBRE` (VARCHAR 100 NOT NULL), `CLAVE` (VARCHAR 20), `ESTATUS` (CHAR 1 NOT NULL — `A`=activa), `AGENTE_ID` (FK → AGENTES), `GRUPO_RUTAS_ID` (FK), + auditoría, `VERSION_REGISTRO`, `DIARIA`.

**RUTAS_DET columnas:** `RUTA_DET_ID` (PK), `RUTA_ID` (FK → RUTAS), `DIA` (INTEGER — día de semana), `DIA_POSICION` (orden en ruta), `CLIENTE_ID` (FK → CLIENTES), `CLAVE_CLIENTE` (VARCHAR 20). Define qué cliente visita cada ruta en cada día de la semana.

**Muestra rutas activas:**

```sql
SELECT r.RUTA_ID, r.NOMBRE, a.NOMBRE AS agente, a.COBRADOR_ID, a.VENDEDOR_ID
FROM RUTAS r LEFT JOIN AGENTES a ON a.AGENTE_ID = r.AGENTE_ID
WHERE r.ESTATUS = 'A' ORDER BY r.RUTA_ID ROWS 1 TO 5;
```

| RUTA_ID | Nombre | Agente | COBRADOR_ID | VENDEDOR_ID |
|---|---|---|---|---|
| 87343 | R3 | PATRICIO DE LA LUZ | 11296 | 87340 |
| 87642 | R16 | GERARDO GINES | 11493 | 87639 |
| 99035 | R18 | RIGOBERTO LINARES | 11495 | 88259 |
| 102312 | R25 | NOE CORTERO | 11502 | 88266 |
| 104857 | R1 | JUAN CARLOS CASTRO | 11294 | 88240 |

### 8.5 VENDEDORES

**Columnas:** `VENDEDOR_ID` (INTEGER PK NOT NULL), `NOMBRE` (VARCHAR 50 NOT NULL), `POLITICA_COMIS_VEN_ID` (FK), `ES_PREDET`, `OCULTO` (NOT NULL), + auditoría.

**Total:** 46 activos. Nomenclatura: `RUTA01`–`RUTA39` + especiales (`MAYOREO`, `RUTA_MSP_MATRIZ`, `TRABAJADORES MSP`, `RUTA_ABOGADO`, `OLIVER VAZQUEZ`). Los IDs de VENDEDORES (ej. 87340, 88240) son distintos de los IDs en `LIBRES_CARGOS_CC.VENDEDOR_1` (ej. 289817, 3944703).

### 8.6 AGENTES — tabla puente de atribución

**Columnas clave:** `AGENTE_ID` (PK), `NOMBRE`, `CLAVE`, `USUARIO`, `VENDEDOR_ID` (FK → VENDEDORES), `COBRADOR_ID` (FK → COBRADORES), + permisos app móvil (30+ campos de permisos).

Cada agente de campo (cobrador en ruta) tiene asignado un `COBRADOR_ID` (para registrar cobros en Microsip) y un `VENDEDOR_ID` (para comisiones de venta).

### 8.7 Cadena completa de atribución

```
CLIENTES.COBRADOR_ID    → COBRADORES (ruta asignada al cliente)
CLIENTES.ZONA_CLIENTE_ID → ZONAS_CLIENTES (territorio)

RUTAS_DET.RUTA_ID       → RUTAS.AGENTE_ID → AGENTES
                                             ├─ AGENTES.COBRADOR_ID → COBRADORES
                                             └─ AGENTES.VENDEDOR_ID → VENDEDORES

DOCTOS_CC.COBRADOR_ID   → COBRADORES (quién hizo el cobro en esa visita)

LIBRES_CARGOS_CC.VENDEDOR_1 → ID opaco del sistema de comisiones MSP
                               (NO enlaza a VENDEDORES.VENDEDOR_ID en este snapshot)
```

**Atribución confiable en ERP:** `DOCTOS_CC.COBRADOR_ID` (quién cobró) y `CLIENTES.ZONA_CLIENTE_ID` (a qué ruta pertenece el cliente). `LIBRES_CARGOS_CC.VENDEDOR_1` es la referencia al sistema de comisiones de la app, que requiere resolverse contra la tabla de agentes de la app móvil.

---

*Generado: 2026-06-13 · Fuente: MUEBLERA_SNP.fdb (snapshot Firebird 5.0 embedded) · solo lectura*


---

# Bloque fuente: B3-productos-margen.md

# B3 — Productos por cliente, catálogo y margen

> Datos al: 2026-06-11. Motor: Firebird 5.0.4 (ODS 13.1). Snapshot: `MUEBLERA_SNP.fdb`.
> Todas las consultas son SELECT-only sobre el snapshot.

---

## Bloque 5 — Historial de compras por cliente: `DOCTOS_PV` + `DOCTOS_PV_DET`

### 5.1 Tabla `DOCTOS_PV` — encabezado de venta de punto de venta

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `DOCTO_PV_ID` | INTEGER | — | NOT NULL PK | ID interno del documento PV |
| `CAJA_ID` | INTEGER | — | NOT NULL | Caja registradora |
| `TIPO_DOCTO` | CHAR(1) | — | NOT NULL | Tipo: `V`=venta, `D`=devolución, `P`=pedido |
| `SUCURSAL_ID` | INTEGER | — | NOT NULL | Sucursal |
| `FOLIO` | CHAR(9) | — | NOT NULL | Folio impreso en ticket (texto) |
| `FECHA` | DATE | — | NOT NULL | Fecha de la venta |
| `HORA` | TIME | — | NOT NULL | Hora de la venta |
| `CAJERO_ID` | INTEGER | — | nullable | Usuario cajero |
| `CLIENTE_ID` | INTEGER | — | **nullable** | FK → `CLIENTES.CLIENTE_ID` (NULL = mostrador sin cliente) |
| `CLAVE_CLIENTE` | VARCHAR(20) | — | nullable | Clave textual del cliente |
| `CLIENTE_FAC_ID` | INTEGER | — | nullable | Cliente de facturación (puede diferir) |
| `DIR_CLI_ID` | INTEGER | — | nullable | Dirección de entrega del cliente |
| `ALMACEN_ID` | INTEGER | — | nullable | Almacén que surte |
| `VENDEDOR_ID` | INTEGER | — | nullable | Vendedor asignado al documento |
| `ESTATUS` | CHAR(1) | — | NOT NULL | `A`=activo, `C`=cancelado, `P`=pendiente |
| `APLICADO` | CHAR(1) | — | nullable | Flag de aplicación contable |
| `IMPORTE_NETO` | NUMERIC(15,2) | 15,2 | nullable | Subtotal neto (sin impuestos) |
| `TOTAL_IMPUESTOS` | NUMERIC(15,2) | 15,2 | nullable | IVA total del documento |
| `TOTAL_RETENCIONES` | NUMERIC(15,2) | 15,2 | nullable | Retenciones |
| `TOTAL_FPGC` | NUMERIC(15,2) | 15,2 | nullable | Total con impuesto incluido |
| `DSCTO_IMPORTE` | NUMERIC(15,2) | 15,2 | nullable | Descuento en importe |
| `DSCTO_PCTJE` | NUMERIC(9,6) | 9,6 | nullable | Porcentaje de descuento global |
| `FECHA_VIGENCIA` | DATE | — | nullable | Vigencia del pedido (si aplica) |
| `METODO_PAGO_SAT` | CHAR(3) | — | nullable | Clave SAT de método de pago |
| `SISTEMA_ORIGEN` | CHAR(2) | — | NOT NULL | Origen del registro |
| `FECHA_HORA_CREACION` | TIMESTAMP | — | nullable | Timestamp de creación |
| `FECHA_HORA_ULT_MODIF` | TIMESTAMP | — | nullable | Timestamp última modificación |
| `FECHA_HORA_CANCELACION` | TIMESTAMP | — | nullable | Timestamp de cancelación |

**Cobertura medida:**
- Total de registros no cancelados (`ESTATUS <> 'C'`): **422,003**
- Rango de fechas: histórico hasta 2026-06-11

**Vínculo a `DOCTOS_CC`:** `DOCTOS_CC` no tiene `DOCTO_PV_ID`. El enlace lógico es por `CLIENTE_ID` + `FECHA` o por `FOLIO` (columna texto, no integridad referencial). No existe FK directa de PV→CC, lo que significa que la **cadena PV → cargo CxC no se puede unir de forma determinista** sin conocer la convención de folio usada en cada caso. Ver Brecha 5-B.

### 5.2 Tabla `DOCTOS_PV_DET` — líneas del documento de venta

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `DOCTO_PV_DET_ID` | INTEGER | — | NOT NULL PK | ID de línea |
| `DOCTO_PV_ID` | INTEGER | — | NOT NULL FK | → `DOCTOS_PV.DOCTO_PV_ID` |
| `ARTICULO_ID` | INTEGER | — | NOT NULL FK | → `ARTICULOS.ARTICULO_ID` |
| `CLAVE_ARTICULO` | VARCHAR(20) | — | nullable | Clave textual del artículo |
| `UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Cantidad vendida |
| `UNIDADES_DEV` | NUMERIC(18,5) | 18,5 | nullable | Cantidad devuelta |
| `UNIDADES_SURT` | NUMERIC(18,5) | 18,5 | nullable | Unidades surtidas |
| `PRECIO_UNITARIO` | NUMERIC(18,6) | 18,6 | nullable | Precio unitario sin IVA |
| `PRECIO_UNITARIO_IMPTO` | NUMERIC(18,6) | 18,6 | nullable | Precio unitario con IVA |
| `IMPUESTO_POR_UNIDAD` | NUMERIC(18,6) | 18,6 | nullable | IVA por unidad |
| `PCTJE_DSCTO` | NUMERIC(9,6) | 9,6 | nullable | % descuento en línea |
| `PRECIO_TOTAL_NETO` | NUMERIC(15,2) | 15,2 | nullable | Total de la línea sin IVA |
| `DSCTO_ART` | NUMERIC(15,2) | 15,2 | nullable | Descuento artículo |
| `DSCTO_EXTRA` | NUMERIC(15,2) | 15,2 | nullable | Descuento extra |
| `VENDEDOR_ID` | INTEGER | — | nullable | Vendedor de la línea (puede diferir del header) |
| `PCTJE_COMIS` | NUMERIC(9,6) | 9,6 | nullable | % comisión vendedor en línea |
| `POSICION` | INTEGER | — | NOT NULL | Orden de la línea en el documento |
| `ROL` | CHAR(1) | — | NOT NULL | Rol de la partida |
| `NOTAS` | BLOB TEXT | — | nullable | Notas de la línea |

**Cobertura medida:** 150,877 líneas en total.

### 5.3 Cadena de join validada

```sql
-- Cadena PV → PV_DET → ARTICULOS: validada con datos reales
SELECT FIRST 5
  pv.DOCTO_PV_ID,
  pv.CLIENTE_ID,
  pv.FECHA,
  det.ARTICULO_ID,
  det.UNIDADES,
  det.PRECIO_UNITARIO,
  det.PRECIO_TOTAL_NETO,
  a.NOMBRE
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
WHERE pv.CLIENTE_ID IS NOT NULL AND pv.ESTATUS <> 'C'
ORDER BY pv.FECHA DESC;
```

**Resultado real (2026-06-11):**

| DOCTO_PV_ID | CLIENTE_ID | FECHA | ARTICULO_ID | UNIDADES | PRECIO_UNITARIO | PRECIO_TOTAL_NETO | NOMBRE |
|---|---|---|---|---|---|---|---|
| 15542211 | 12387 | 2026-06-11 | 710396 | 1.00000 | 1668.000000 | 1668.00 | BASE DE CAMA INDIVIDUAL ANTONI CAFE PAMBAZO TACTOPIEL DUELA |
| 15542211 | 12387 | 2026-06-11 | 897 | 1.00000 | 7674.000000 | 7674.00 | ROPERO MONARCA RAMAS 3 PIEZAS SAN BERNARDO CHOCOLATE |
| 15542211 | 12387 | 2026-06-11 | 3027845 | 1.00000 | 3586.206897 | 3586.21 | TOCADOR GESSEL MARMOL BLANCO MDF SARAI |
| 15542211 | 12387 | 2026-06-11 | 93259 | 1.00000 | 3051.724138 | 3051.72 | LAVADORA REDONDA ACROS 22 KG. MOD. ALF2253EC |
| 15543397 | 34084 | 2026-06-11 | 483 | 1.00000 | 1560.000000 | 1560.00 | BASE DE MADERA MATRIMONIAL CHOCOLATE |

**Cadena VALIDADA.** Join funciona sin ambigüedades: `DOCTO_PV_ID` es la FK entre ambas tablas; `ARTICULO_ID` liga a catálogo.

### 5.4 Artículos y categorías por cliente

```sql
-- Artículos comprados por cliente con categoría
SELECT
  pv.CLIENTE_ID,
  la.NOMBRE AS LINEA,
  a.ARTICULO_ID,
  a.NOMBRE AS ARTICULO,
  COUNT(*) AS VECES_COMPRADO,
  SUM(CAST(det.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS TOTAL_GASTADO
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.CLIENTE_ID = :cliente_id AND pv.ESTATUS <> 'C'
GROUP BY pv.CLIENTE_ID, la.NOMBRE, a.ARTICULO_ID, a.NOMBRE
ORDER BY TOTAL_GASTADO DESC;
```

### 5.5 Cliente más activo medido

```sql
SELECT FIRST 1 CLIENTE_ID, COUNT(*) AS VENTAS
FROM DOCTOS_PV WHERE ESTATUS <> 'C' AND CLIENTE_ID IS NOT NULL
GROUP BY CLIENTE_ID ORDER BY VENTAS DESC;
```
**Resultado:** `CLIENTE_ID=12387`, **2,383 tickets** de venta no cancelados. Es el cliente con mayor frecuencia de compra en la BD.

---

## Bloque 6 — Catálogo: `ARTICULOS` + `LINEAS_ARTICULOS`

### 6.1 Tabla `ARTICULOS`

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `ARTICULO_ID` | INTEGER | — | NOT NULL PK | ID interno |
| `NOMBRE` | VARCHAR(200) | — | NOT NULL | Nombre completo (Win1252 legacy, nombres en mayúsculas) |
| `LINEA_ARTICULO_ID` | INTEGER | — | nullable FK | → `LINEAS_ARTICULOS.LINEA_ARTICULO_ID` (categoría) |
| `ESTATUS` | CHAR(1) | — | NOT NULL | `A`=activo, `S`=suspendido |
| `CAUSA_SUSP` | VARCHAR(100) | — | nullable | Motivo de suspensión |
| `FECHA_SUSP` | DATE | — | nullable | Fecha de suspensión |
| `ES_ALMACENABLE` | CHAR(1) | — | nullable | Si lleva inventario físico |
| `ES_JUEGO` | CHAR(1) | — | nullable | Si es conjunto/juego de piezas |
| `UNIDAD_VENTA` | VARCHAR(20) | — | nullable | Unidad de venta (pieza, par, etc.) |
| `UNIDAD_COMPRA` | VARCHAR(20) | — | nullable | Unidad de compra |
| `DIAS_GARANTIA` | INTEGER | — | nullable | Días de garantía |
| `ES_IMPORTADO` | CHAR(1) | — | nullable | Flag de artículo importado |
| `PESO_UNITARIO` | NUMERIC(18,5) | 18,5 | nullable | Peso en kg |
| `CUENTA_ALMACEN` | VARCHAR(30) | — | nullable | Cuenta contable de almacén |
| `CUENTA_COSTO_VENTA` | VARCHAR(30) | — | nullable | Cuenta contable de costo de venta |
| `CUENTA_VENTAS` | VARCHAR(30) | — | nullable | Cuenta contable de ventas |
| `FACTOR_VENTA` | NUMERIC(18,5) | 18,5 | NOT NULL | Factor de conversión venta |
| `NOTAS_VENTAS` | BLOB TEXT | — | nullable | Notas de ventas (imprimibles en ticket) |
| `FECHA_HORA_CREACION` | TIMESTAMP | — | nullable | Creación |
| `FECHA_HORA_ULT_MODIF` | TIMESTAMP | — | nullable | Última modificación |

**Cobertura medida:** 6,319 artículos en total (activos e inactivos).

### 6.2 Tabla `LINEAS_ARTICULOS` — categorías de producto

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `LINEA_ARTICULO_ID` | INTEGER | — | NOT NULL PK | ID de categoría |
| `NOMBRE` | VARCHAR(50) | — | NOT NULL | Nombre de la línea/categoría |
| `GRUPO_LINEA_ID` | INTEGER | — | nullable | Agrupador superior (subcategorías) |
| `CLAVE` | VARCHAR(20) | — | nullable | Clave textual alternativa |
| `ES_PREDET` | CHAR(1) | — | nullable | Si es la línea predeterminada |
| `OCULTO` | CHAR(1) | — | NOT NULL | Si está oculta en el POS |
| `FACTOR_VENTA` | NUMERIC(18,5) | 18,5 | NOT NULL | Factor de precio heredable |
| `CUENTA_ALMACEN` | VARCHAR(30) | — | nullable | Cuenta contable |
| `FECHA_HORA_CREACION` | TIMESTAMP | — | nullable | Creación |

**Cobertura medida:** 47 categorías.

### 6.3 Mapa completo de categorías (`LINEA_ARTICULO_ID` → nombre)

```sql
SELECT LINEA_ARTICULO_ID, NOMBRE FROM LINEAS_ARTICULOS ORDER BY NOMBRE;
```

**Resultado real (47 categorías):**

| ID | Nombre |
|---|---|
| 772151 | ALACENAS |
| 379 | ARTICULOS DE 2DA |
| 349 | BICICLETAS |
| 376 | BLANCOS |
| 350 | BLU-RAY |
| 351 | BOCINAS PORTATILES |
| 729401 | CALENTADORES SOLARES |
| 352 | CAMAS Y LITERAS |
| 11574 | CAMPANAS |
| 353 | CAR AUDIO |
| 123054 | CASITAS |
| 146542 | CELULAR |
| 354 | COLCHONES Y BOXES |
| 355 | COMEDORES Y ANTECOMEDORES |
| 772157 | COMODAS Y CAJONERAS |
| 356 | CUNAS |
| 357 | DVD |
| 375 | ENSERES DOMESTICOS |
| 358 | ESTUFAS Y PARRILLAS |
| 359 | HORNOS DE MICROONDAS |
| 11774 | KIT CAMAS COMPLETAS |
| 22580 | KIT COMEDORES COMPLETOS |
| 23522 | KIT MESA INFANTIL |
| 360 | LAPTOPS Y PC'S DE ESCRITORIO |
| 361 | LAVADORAS |
| 772155 | LIBREROS Y CENTROS DE ENTRETENIMIENTO |
| 362 | LICUADORAS |
| 378 | MAQUINAS DE COSER |
| 374 | METALICOS |
| 363 | MINICOMPONENTES Y MODULARES |
| 123058 | MONTABLES |
| 364 | MUEBLES DE OFICINA |
| 365 | MUEBLES EN GENERAL |
| 366 | MUEBLES INFANTILES |
| 367 | PANTALLAS |
| 123056 | PISTAS DE CARROS |
| 368 | PLANCHAS |
| 373 | PLASTICOS |
| 772245 | PORTA MICROONDAS, ESPECIEROS, DISPENSEROS |
| 1796075 | PRODUCTOS ELECTRONICOS |
| 369 | RECAMARAS |
| 370 | REFRIGERADORES Y CONGELADORES |
| 772149 | ROPEROS |
| 371 | SALAS Y ESTANCIAS |
| 772153 | TOCADORES |
| 12456 | VARIOS |
| 12053 | VENTILADORES |

### 6.4 Top 15 categorías por volumen de ventas (importe neto)

```sql
SELECT FIRST 15
  la.LINEA_ARTICULO_ID,
  la.NOMBRE AS LINEA,
  COUNT(DISTINCT pv.DOCTO_PV_ID) AS NUM_VENTAS,
  COUNT(det.DOCTO_PV_DET_ID) AS NUM_PARTIDAS,
  SUM(CAST(det.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS TOTAL_VENTAS
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.ESTATUS <> 'C'
GROUP BY la.LINEA_ARTICULO_ID, la.NOMBRE
ORDER BY TOTAL_VENTAS DESC;
```

**Resultado real:**

| Rank | Categoría | Ventas (docs) | Partidas | Total Ventas (MXN) |
|---|---|---|---|---|
| 1 | ROPEROS | 14,021 | 14,525 | $97,330,671 |
| 2 | KIT CAMAS COMPLETAS | 9,637 | 9,675 | $76,588,012 |
| 3 | LAVADORAS | 11,455 | 11,665 | $72,096,187 |
| 4 | COLCHONES Y BOXES | 24,292 | 25,363 | $69,867,637 |
| 5 | PANTALLAS | 8,242 | 8,354 | $63,134,984 |
| 6 | REFRIGERADORES Y CONGELADORES | 5,364 | 5,472 | $55,707,863 |
| 7 | ESTUFAS Y PARRILLAS | 3,715 | 3,754 | $27,983,390 |
| 8 | ALACENAS | 3,799 | 3,860 | $23,204,563 |
| 9 | BOCINAS PORTATILES | 5,416 | 5,615 | $21,788,221 |
| 10 | SALAS Y ESTANCIAS | 1,559 | 1,592 | $21,310,839 |
| 11 | TOCADORES | 3,353 | 3,498 | $17,186,515 |
| 12 | CELULAR | 2,940 | 3,149 | $17,090,092 |
| 13 | ENSERES DOMESTICOS | 4,599 | 5,450 | $12,347,051 |
| 14 | CAMAS Y LITERAS | 12,967 | 14,278 | $10,855,103 |
| 15 | COMODAS Y CAJONERAS | 2,300 | 2,432 | $10,041,977 |

### 6.5 Mapa de pares complementarios propuesto

Definido sobre los nombres reales de categorías de la BD:

| Categoría comprada | Complemento natural | Lógica de recomendación |
|---|---|---|
| KIT CAMAS COMPLETAS / CAMAS Y LITERAS | COLCHONES Y BOXES | Cama sin colchón → oportunidad inmediata |
| COLCHONES Y BOXES | KIT CAMAS COMPLETAS / ROPEROS | Colchón solo → probable upgrade a recámara |
| REFRIGERADORES Y CONGELADORES | ESTUFAS Y PARRILLAS | Clásico par de electrodomésticos de cocina |
| ESTUFAS Y PARRILLAS | ALACENAS / PORTA MICROONDAS, ESPECIEROS, DISPENSEROS | Equipamiento de cocina completo |
| LAVADORAS | REFRIGERADORES Y CONGELADORES | Segunda compra de blanco más común |
| PANTALLAS | BOCINAS PORTATILES / MINICOMPONENTES Y MODULARES | Upgrade de audio para sala con TV nueva |
| SALAS Y ESTANCIAS | PANTALLAS / LIBREROS Y CENTROS DE ENTRETENIMIENTO | Sala → equipo de entretenimiento |
| COMEDORES Y ANTECOMEDORES | ALACENAS / HORNOS DE MICROONDAS | Comedor → equipamiento de comedor/cocina |
| RECAMARAS | COLCHONES Y BOXES / TOCADORES / ROPEROS / COMODAS Y CAJONERAS | Juego de recámara completo |
| MUEBLES INFANTILES / CUNAS | KIT MESA INFANTIL / PISTAS DE CARROS / CASITAS | Cuarto infantil completo |

> **Nota:** los pares son hipótesis sobre comportamiento de compra. Para validarlos estadísticamente se requiere análisis de co-ocurrencia dentro del mismo `CLIENTE_ID` (ver SQL en Brecha 6-A).

---

## Bloque 9 — Inventario y costo: `SALDOS_IN` + `DOCTOS_IN_DET`

### 9.1 Tabla `SALDOS_IN` — saldos mensuales de inventario

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `ARTICULO_ID` | INTEGER | — | NOT NULL PK† | FK → `ARTICULOS.ARTICULO_ID` |
| `ALMACEN_ID` | INTEGER | — | NOT NULL PK† | FK → almacén |
| `ANO` | SMALLINT | — | NOT NULL PK† | Año del saldo |
| `MES` | SMALLINT | — | NOT NULL PK† | Mes del saldo (1–12) |
| `ULTIMO_DIA` | SMALLINT | — | NOT NULL PK† | Último día hábil del mes |
| `ENTRADAS_UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Unidades que entraron en el mes |
| `SALIDAS_UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Unidades que salieron en el mes |
| `ENTRADAS_COSTO` | NUMERIC(15,2) | 15,2 | nullable | Costo total de entradas del mes |
| `SALIDAS_COSTO` | NUMERIC(15,2) | 15,2 | nullable | Costo total de salidas del mes |

† PK compuesta implícita por combinación de los 5 campos.

> **CAVEAT:** `SALDOS_IN` es un resumen mensual de movimientos, no un saldo actual. No contiene `COSTO_PROMEDIO` ni `ULTIMO_COSTO` directamente; para el costo promedio ponderado hay que calcular `ENTRADAS_COSTO / ENTRADAS_UNIDADES` por período.

### 9.2 Tabla `DOCTOS_IN_DET` — líneas de movimientos de inventario

| Columna | Tipo FB | Precisión | Nulable | Descripción |
|---|---|---|---|---|
| `DOCTO_IN_DET_ID` | INTEGER | — | NOT NULL PK | ID de línea |
| `DOCTO_IN_ID` | INTEGER | — | NOT NULL FK | → encabezado del movimiento (`DOCTOS_IN`) |
| `ALMACEN_ID` | INTEGER | — | NOT NULL | Almacén |
| `CONCEPTO_IN_ID` | INTEGER | — | NOT NULL | Concepto de movimiento (compra, ajuste, traspaso…) |
| `ARTICULO_ID` | INTEGER | — | NOT NULL FK | → `ARTICULOS.ARTICULO_ID` |
| `CLAVE_ARTICULO` | VARCHAR(20) | — | nullable | Clave textual |
| `TIPO_MOVTO` | CHAR(1) | — | NOT NULL | `E`=entrada, `S`=salida |
| `UNIDADES` | NUMERIC(18,5) | 18,5 | nullable | Cantidad del movimiento |
| `COSTO_UNITARIO` | NUMERIC(18,6) | 18,6 | nullable | **Costo unitario en el momento del movimiento** |
| `COSTO_TOTAL` | NUMERIC(15,2) | 15,2 | nullable | Costo total de la línea |
| `METODO_COSTEO` | CHAR(1) | — | NOT NULL | Método: `P`=promedio, `F`=FIFO, etc. |
| `CANCELADO` | CHAR(1) | — | nullable | Si el movimiento fue cancelado |
| `APLICADO` | CHAR(1) | — | nullable | Si fue aplicado contablemente |
| `FECHA` | DATE | — | NOT NULL | Fecha del movimiento |
| `ROL` | CHAR(1) | — | NOT NULL | Rol del movimiento |
| `CENTRO_COSTO_ID` | INTEGER | — | nullable | Centro de costo |

### 9.3 Dónde vive el costo unitario

El costo unitario real está en **`DOCTOS_IN_DET.COSTO_UNITARIO`** (NUMERIC 18,6). Es el costo **en el momento del movimiento de entrada** — refleja el precio de compra registrado en esa entrada de inventario.

`SALDOS_IN` permite calcular costo promedio mensual pero no tiene columna directa de costo por unidad.

### 9.4 SQL para obtener costo por artículo (más reciente entrada)

```sql
-- Ultimo costo de entrada registrado por artículo
SELECT
  did.ARTICULO_ID,
  a.NOMBRE,
  FIRST 1 did.COSTO_UNITARIO AS ULTIMO_COSTO,
  did.FECHA AS FECHA_ULTIMO_COSTO
FROM DOCTOS_IN_DET did
JOIN ARTICULOS a ON a.ARTICULO_ID = did.ARTICULO_ID
WHERE did.TIPO_MOVTO = 'E'
  AND (did.CANCELADO IS NULL OR did.CANCELADO <> 'S')
ORDER BY did.ARTICULO_ID, did.FECHA DESC;
-- NOTA: Firebird no soporta DISTINCT ON; usar subconsulta con MAX(FECHA) por ARTICULO_ID.
```

Versión funcional en Firebird:

```sql
-- Costo más reciente por artículo (subconsulta correlacionada)
SELECT
  did.ARTICULO_ID,
  a.NOMBRE,
  did.COSTO_UNITARIO AS ULTIMO_COSTO_ENTRADA,
  did.FECHA
FROM DOCTOS_IN_DET did
JOIN ARTICULOS a ON a.ARTICULO_ID = did.ARTICULO_ID
WHERE did.TIPO_MOVTO = 'E'
  AND did.FECHA = (
    SELECT MAX(did2.FECHA)
    FROM DOCTOS_IN_DET did2
    WHERE did2.ARTICULO_ID = did.ARTICULO_ID
      AND did2.TIPO_MOVTO = 'E'
      AND (did2.CANCELADO IS NULL OR did2.CANCELADO <> 'S')
  )
  AND (did.CANCELADO IS NULL OR did.CANCELADO <> 'S');
```

### 9.5 SQL para margen por venta (precio − costo)

```sql
-- Margen por línea de venta usando último costo de entrada
SELECT
  pv.FECHA,
  pv.CLIENTE_ID,
  a.NOMBRE AS ARTICULO,
  la.NOMBRE AS LINEA,
  det.UNIDADES,
  det.PRECIO_UNITARIO,
  costo.ULTIMO_COSTO,
  det.PRECIO_UNITARIO - costo.ULTIMO_COSTO AS MARGEN_UNITARIO,
  CASE WHEN det.PRECIO_UNITARIO > 0
    THEN (det.PRECIO_UNITARIO - costo.ULTIMO_COSTO) / det.PRECIO_UNITARIO * 100
    ELSE NULL END AS MARGEN_PCT
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
JOIN (
  -- subconsulta de último costo por artículo
  SELECT did.ARTICULO_ID,
         did.COSTO_UNITARIO AS ULTIMO_COSTO
  FROM DOCTOS_IN_DET did
  WHERE did.TIPO_MOVTO = 'E'
    AND (did.CANCELADO IS NULL OR did.CANCELADO <> 'S')
    AND did.FECHA = (
      SELECT MAX(did2.FECHA)
      FROM DOCTOS_IN_DET did2
      WHERE did2.ARTICULO_ID = did.ARTICULO_ID
        AND did2.TIPO_MOVTO = 'E'
        AND (did2.CANCELADO IS NULL OR did2.CANCELADO <> 'S')
    )
) costo ON costo.ARTICULO_ID = det.ARTICULO_ID
WHERE pv.CLIENTE_ID = :cliente_id AND pv.ESTATUS <> 'C'
ORDER BY pv.FECHA DESC;
```

> **Advertencia de costo promedio:** `SALDOS_IN` permite calcular costo promedio mensual ponderado pero requiere acumulación:
> ```sql
> SELECT ARTICULO_ID, ALMACEN_ID, ANO, MES,
>        CAST(ENTRADAS_COSTO AS DOUBLE PRECISION) /
>          NULLIF(CAST(ENTRADAS_UNIDADES AS DOUBLE PRECISION), 0) AS COSTO_PROM_MES
> FROM SALDOS_IN
> WHERE ENTRADAS_UNIDADES > 0;
> ```
> Este costo promedio mensual es más representativo para margen histórico que el último costo puntual.

---

## Brechas identificadas

### Brecha 5-A — `CLIENTE_ID` nulo en ventas de mostrador — ❌ RETRACTADA (ver Bloque B4)
> **Corrección:** medido contra el restore, `DOCTOS_PV.CLIENTE_ID` está **100% poblado**
> (423,284/423,284, 44,781 clientes distintos). No hay ventas sin cliente; las de mostrador van a
> clientes reales con nombre. Esta brecha NO aplica.

~~`DOCTOS_PV.CLIENTE_ID` es nullable. Ventas a "público general" no tienen cliente asignado y no pueden agregarse al perfil.~~ La proporción exacta (resultó 0 nulos):
```sql
SELECT COUNT(*) FROM DOCTOS_PV WHERE ESTATUS <> 'C' AND CLIENTE_ID IS NULL;
```

### Brecha 5-B — Sin FK directa PV → DOCTOS_CC (CxC)
`DOCTOS_CC` no tiene `DOCTO_PV_ID`. El vínculo es por `CLIENTE_ID` + fecha/folio en texto, sin integridad referencial. Esto impide construir un join determinista entre "artículo vendido" y "cargo generado en CxC". La cadena PV → cargo CxC → cobranza **no es directamente trazable** para análisis automatizado.

### Brecha 6-A — Análisis de co-ocurrencia de categorías (pares complementarios)
El mapa de pares propuesto en §6.5 es heurístico. Para validarlo estadísticamente se requiere:
```sql
-- Co-ocurrencia de categorías en el historial de un cliente
SELECT a1.LINEA_ARTICULO_ID AS LINEA_A, a2.LINEA_ARTICULO_ID AS LINEA_B, COUNT(*) AS CLIENTES_CON_AMBAS
FROM (SELECT DISTINCT pv.CLIENTE_ID, ar.LINEA_ARTICULO_ID
      FROM DOCTOS_PV pv
      JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
      JOIN ARTICULOS ar ON ar.ARTICULO_ID = det.ARTICULO_ID
      WHERE pv.ESTATUS <> 'C' AND pv.CLIENTE_ID IS NOT NULL) a1
JOIN (SELECT DISTINCT pv.CLIENTE_ID, ar.LINEA_ARTICULO_ID
      FROM DOCTOS_PV pv
      JOIN DOCTOS_PV_DET det ON det.DOCTO_PV_ID = pv.DOCTO_PV_ID
      JOIN ARTICULOS ar ON ar.ARTICULO_ID = det.ARTICULO_ID
      WHERE pv.ESTATUS <> 'C' AND pv.CLIENTE_ID IS NOT NULL) a2
  ON a1.CLIENTE_ID = a2.CLIENTE_ID AND a1.LINEA_ARTICULO_ID < a2.LINEA_ARTICULO_ID
GROUP BY a1.LINEA_ARTICULO_ID, a2.LINEA_ARTICULO_ID
ORDER BY CLIENTES_CON_AMBAS DESC;
```
Esta consulta es costosa sobre 422K docs; se recomienda ejecutar en ventana de mantenimiento o en una proyección materializada.

### Brecha 9-A — Costo es puntual, no histórico por venta
`DOCTOS_IN_DET.COSTO_UNITARIO` registra el costo en el momento del movimiento de **entrada** de inventario, no en el momento de la **venta**. Para ventas antiguas, el "último costo" actual puede diferir significativamente del costo vigente cuando se realizó la venta. El margen calculado con costo actual es una **aproximación**, no el margen real de la transacción original.

### Brecha 9-B — Sin costo_unitario en `DOCTOS_PV_DET`
`DOCTOS_PV_DET` no tiene columna de costo. El costo unitario en el momento de la venta no queda registrado en el documento PV. Microsip guarda el costo en los movimientos de inventario generados por la venta (en `DOCTOS_IN_DET` con `TIPO_MOVTO='S'`), pero ligar la salida de inventario al renglón específico de la venta requiere un join adicional no trivial.

### Brecha 9-C — IVA no distinguible en precio de venta
`PRECIO_UNITARIO` en `DOCTOS_PV_DET` puede incluir o excluir IVA según el flag `IMPUESTO_INCLUIDO` de `DOCTOS_PV`. El margen real requiere normalizar el precio a base (sin IVA) antes de comparar contra el costo (que es siempre sin IVA).

### Brecha 9-D — Artículos sin línea de categoría
`ARTICULOS.LINEA_ARTICULO_ID` es nullable. Artículos sin categoría asignada no aparecen en análisis por `LINEAS_ARTICULOS`. Su proporción debe verificarse:
```sql
SELECT COUNT(*) FROM ARTICULOS WHERE LINEA_ARTICULO_ID IS NULL;
```


---

# Bloque fuente: B4-ventas-microsip.md

# B4 — Diccionario de datos corregido: VENTAS sobre `DOCTOS_PV` (Microsip)

> **Corrección estructural.** El análisis previo derivó el RFM de clientes desde el ledger de **crédito** (`DOCTOS_CC` concepto 5), que solo ve ventas a crédito y omite ~75 % de los eventos de compra (ventas de contado). Este bloque re-ancla el análisis de ventas sobre la tabla real de ventas de Microsip `DOCTOS_PV`.
>
> **Fuente:** `MUEBLERA_SNP.fdb` (snapshot). Corte de datos `MAX(FECHA) = 2026-06-11`. Solo lecturas (`SELECT`).
>
> **Ground truth heredado (no se re-deriva):** `DOCTOS_VE` está abandonada (59 filas, ene-2018) → ignorada. `DOCTOS_PV` es LA tabla de ventas: 423,284 filas, 2006-03-26 → 2026-06-11, `CLIENTE_ID` 100 % poblado, 44,781 clientes distintos. Ventas reales = `TIPO_DOCTO IN ('V','P')` + `ESTATUS='N'` = 421,575 filas. Contado vs crédito vive en `DOCTOS_PV_COBROS` (`FORMA_COBRO_ID`: 67=Efectivo, 68=Cheque, 71=Crédito, 27773=Traspaso). Cadena de productos: `DOCTOS_PV → DOCTOS_PV_DET → ARTICULOS → LINEAS_ARTICULOS`.

---

## 1. Clasificación de la venta: CONTADO vs CRÉDITO

Una venta PV es **CRÉDITO** si tiene una línea de cobro con `FORMA_COBRO_ID = 71` en `DOCTOS_PV_COBROS`; en caso contrario es **CONTADO**. Validado primero en muestra (las 10 ventas más recientes traían las 10 con línea 71 = crédito).

### CRÉDITO

```sql
SELECT
  COUNT(*) AS N_CREDITO,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS IMPORTE_CREDITO
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
  AND EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c
              WHERE c.DOCTO_PV_ID = pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID = 71);
```

| N_CREDITO | IMPORTE_CREDITO (IMPORTE_NETO) |
|-----------|-------------------------------:|
| 104,013   | $664,649,206.89 |

### CONTADO

```sql
SELECT
  COUNT(*) AS N_CONTADO,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS IMPORTE_CONTADO
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
  AND NOT EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c
                  WHERE c.DOCTO_PV_ID = pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID = 71);
```

| N_CONTADO | IMPORTE_CONTADO (IMPORTE_NETO) |
|-----------|-------------------------------:|
| 317,562   | $123,011,986.21 |

### Split final

| Clase | Ventas (count) | % count | Importe (IMPORTE_NETO) | % valor |
|-------|---------------:|--------:|-----------------------:|--------:|
| **CONTADO** | 317,562 | **75.3 %** | $123.0 M | 15.6 % |
| **CRÉDITO** | 104,013 | **24.7 %** | $664.6 M | 84.4 % |
| **Total** | **421,575** | 100 % | $787.7 M | 100 % |

`104,013 + 317,562 = 421,575` → cuadra exacto con el ground truth. **Confirma la tesis de la corrección:** el contado es el **75 %** de los eventos de compra (lo que el RFM de crédito no veía), aunque solo ~16 % del valor.

### ENGANCHE — corrección de un supuesto del ground truth

El supuesto heredado decía que "el enganche es la línea efectivo (67) en la misma venta a crédito". **Esto es FALSO en `DOCTOS_PV_COBROS`.** Las líneas de cobro de las ventas a crédito son casi exclusivamente forma 71:

```sql
SELECT c.FORMA_COBRO_ID, COUNT(*) AS N, SUM(CAST(c.IMPORTE AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV_COBROS c
WHERE EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c2
              WHERE c2.DOCTO_PV_ID = c.DOCTO_PV_ID AND c2.FORMA_COBRO_ID = 71)
  AND EXISTS (SELECT 1 FROM DOCTOS_PV pv
              WHERE pv.DOCTO_PV_ID = c.DOCTO_PV_ID AND pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N')
GROUP BY c.FORMA_COBRO_ID ORDER BY 2 DESC;
```

| FORMA_COBRO_ID | N | IMPORTE |
|---------------:|--:|--------:|
| 71 (crédito) | 104,013 | $733,118,777.50 |
| 67 (efectivo) | **1** | $400.00 |

- El **enganche NO se registra como línea efectivo (67) separada** en el PV. La línea 71 lleva el **monto financiado completo** ($733.1 M, incluye enganche + saldo a plazos), mientras `SUM(IMPORTE_NETO)` de las mismas ventas es $664.6 M (precio de lista).
- No existe columna `ENGANCHE` en `DOCTOS_PV` (solo `DSCTO_IMPORTE`, `IMPORTE_DONATIVO`, `IMPORTE_NETO`).
- La columna `TIPO` de `DOCTOS_PV_COBROS` es C (cobro) / A (abono), **no** distingue enganche.

**Total efectivo de contado vs financiado:** todas las líneas efectivo (67) caen en ventas de **contado**, no en crédito:

```sql
SELECT c.FORMA_COBRO_ID, COUNT(*) AS N_LINES, SUM(CAST(c.IMPORTE AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV_COBROS c
JOIN DOCTOS_PV pv ON pv.DOCTO_PV_ID = c.DOCTO_PV_ID
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
GROUP BY c.FORMA_COBRO_ID ORDER BY 2 DESC;
```

| FORMA_COBRO_ID | N_LINES | IMPORTE |
|---------------:|--------:|--------:|
| 67 (efectivo) | 317,735 | $124,720,531.00 |
| 71 (crédito)  | 104,013 | $733,118,777.50 |
| 68 (cheque)   | 13      | $2,150.00 |

> El detalle de enganche por crédito (cuánto se dio de inicial en cada venta a plazos) **no vive en el PV**; vive en el ledger de crédito `DOCTOS_CC` (concepto enganche/abono), aplicable solo a la cohorte de crédito (ver §4).

---

## 2. RFM sobre la base COMPLETA de ventas (contado + crédito), por cliente

Definiciones:
- **Recency** = días de `MAX(FECHA)` del cliente hasta `2026-06-11`.
- **Frequency** = número de ventas (`COUNT(*)`) del cliente.
- **Monetary** = `SUM(IMPORTE_NETO)` (precio de lista; para crédito es el precio a crédito).

```sql
SELECT pv.CLIENTE_ID,
  DATEDIFF(DAY FROM MAX(pv.FECHA) TO DATE '2026-06-11') AS RECENCY_DAYS,
  COUNT(*) AS FREQUENCY,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS MONETARY
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS = 'N'
GROUP BY pv.CLIENTE_ID;
```

Muestra (top frecuencia): `12387 → R=0 F=2381 M=$32.7M`; `12440 → R=0 F=1075 M=$25.1M`; `106559 → R=1 F=520`; `38327 → R=7 F=494`; `54601 → R=6 F=474`. Los líderes de frecuencia son revendedores/cuentas de ruta (ver §6).

### Distribución de Frequency (44,738 clientes)

```sql
SELECT CASE WHEN FREQUENCY=1 THEN '1'
            WHEN FREQUENCY BETWEEN 2 AND 3 THEN '2-3'
            WHEN FREQUENCY BETWEEN 4 AND 9 THEN '4-9'
            WHEN FREQUENCY BETWEEN 10 AND 49 THEN '10-49'
            ELSE '50+' END AS FREQ_BUCKET, COUNT(*) AS N_CLIENTES
FROM ( SELECT pv.CLIENTE_ID, COUNT(*) AS FREQUENCY FROM DOCTOS_PV pv
       WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
       GROUP BY pv.CLIENTE_ID ) t
GROUP BY 1 ORDER BY 1;
```

| Frequency | N clientes | % |
|-----------|-----------:|--:|
| 1 (one-shot) | 18,023 | 40.3 % |
| 2-3 | 10,117 | 22.6 % |
| 4-9 | 5,987 | 13.4 % |
| 10-49 | 8,848 | 19.8 % |
| 50+ | 1,763 | 3.9 % |

### Distribución de Recency (44,738 clientes)

```sql
SELECT CASE WHEN RECENCY_DAYS<=30 THEN '0-30d'
            WHEN RECENCY_DAYS<=90 THEN '31-90d'
            WHEN RECENCY_DAYS<=180 THEN '91-180d'
            WHEN RECENCY_DAYS<=365 THEN '181-365d'
            WHEN RECENCY_DAYS<=730 THEN '1-2a'
            ELSE '2a+' END AS REC_BUCKET, COUNT(*) AS N_CLIENTES
FROM ( SELECT pv.CLIENTE_ID, DATEDIFF(DAY FROM MAX(pv.FECHA) TO DATE '2026-06-11') AS RECENCY_DAYS
       FROM DOCTOS_PV pv WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
       GROUP BY pv.CLIENTE_ID ) t
GROUP BY 1 ORDER BY MIN(RECENCY_DAYS);
```

| Recency | N clientes | % |
|---------|-----------:|--:|
| 0-30d | 1,209 | 2.7 % |
| 31-90d | 2,110 | 4.7 % |
| 91-180d | 3,039 | 6.8 % |
| 181-365d | 4,799 | 10.7 % |
| 1-2a | 6,491 | 14.5 % |
| **2a+** | **27,090** | **60.6 %** |

> El 60 % de la base no compra hace 2+ años → enorme universo de winback (consistente con la estrategia de ventas-AI).

### Distribución de Monetary (44,738 clientes)

```sql
SELECT CASE WHEN MONETARY<2000 THEN '<2k'
            WHEN MONETARY<5000 THEN '2k-5k'
            WHEN MONETARY<15000 THEN '5k-15k'
            WHEN MONETARY<50000 THEN '15k-50k'
            WHEN MONETARY<150000 THEN '50k-150k'
            ELSE '150k+' END AS MON_BUCKET, COUNT(*) AS N_CLIENTES,
       SUM(MONETARY) AS IMPORTE_BUCKET
FROM ( SELECT pv.CLIENTE_ID, SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS MONETARY
       FROM DOCTOS_PV pv WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
       GROUP BY pv.CLIENTE_ID ) t
GROUP BY 1 ORDER BY MIN(MONETARY);
```

| Monetary | N clientes | Importe del bucket |
|----------|-----------:|-------------------:|
| <2k | 1,677 | $2.2 M |
| 2k-5k | 6,229 | $22.1 M |
| 5k-15k | 22,796 | $195.5 M |
| 15k-50k | 12,164 | $313.0 M |
| 50k-150k | 1,763 | $126.5 M |
| 150k+ | 109 | $128.3 M |

> Cola larga: 109 clientes (revendedores) concentran $128 M; 1,872 clientes (50k+) suman $254.8 M. La masa está en 5k–50k (34,960 clientes, $508 M).

---

## 3. Cohort split — el titular que el RFM de crédito perdía

¿Cuántos clientes compran **solo contado**, **solo crédito**, o **ambos**?

```sql
SELECT CASE WHEN N_CRED>0 AND N_CONT>0 THEN 'AMBOS'
            WHEN N_CRED>0 THEN 'SOLO_CREDITO'
            ELSE 'SOLO_CONTADO' END AS COHORTE, COUNT(*) AS N_CLIENTES
FROM (
  SELECT pv.CLIENTE_ID,
    SUM(CASE WHEN EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71) THEN 1 ELSE 0 END) AS N_CRED,
    SUM(CASE WHEN EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71) THEN 0 ELSE 1 END) AS N_CONT
  FROM DOCTOS_PV pv WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
  GROUP BY pv.CLIENTE_ID
) t
GROUP BY 1 ORDER BY 2 DESC;
```

| Cohorte | N clientes | % |
|---------|-----------:|--:|
| **SOLO_CREDITO** | 30,257 | 67.6 % |
| **AMBOS** | 13,677 | 30.6 % |
| **SOLO_CONTADO** | 804 | 1.8 % |
| **Total** | **44,738** | 100 % |

**Lo que el RFM de crédito perdía:**
- **804 clientes** (solo contado) eran **completamente invisibles** al análisis de crédito.
- **13,677 clientes "ambos"** tenían su Frequency/Recency/Monetary **subcontados**: el RFM de crédito solo veía sus compras a plazos, ignorando sus compras de contado.
- En total, **14,481 clientes (32.4 %)** estaban mal medidos por el RFM de crédito, y los **317,562 eventos de contado** (75 % de todas las ventas) no entraban en ninguna métrica de recencia/frecuencia.

---

## 4. Reconciliación del overlay de crédito (`DOCTOS_CC` concepto 5)

`DOCTOS_CC` **no tiene FK a PV** (no hay `DOCTO_PV_ID`). El enlace es por **`FOLIO + CLIENTE_ID + SISTEMA_ORIGEN`**. Validado en muestra (cada venta a crédito empata 1:1 con su cargo concepto 5):

```sql
SELECT FIRST 5 pv.FOLIO, pv.CLIENTE_ID, pv.IMPORTE_NETO,
  (SELECT COUNT(*) FROM DOCTOS_CC cc
   WHERE cc.FOLIO=pv.FOLIO AND cc.CLIENTE_ID=pv.CLIENTE_ID
     AND cc.SISTEMA_ORIGEN='PV' AND cc.CONCEPTO_CC_ID=5) AS CC_MATCH
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO='V' AND pv.ESTATUS='N'
  AND EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71)
ORDER BY pv.DOCTO_PV_ID DESC;
```

| FOLIO | CLIENTE_ID | IMPORTE_NETO | CC_MATCH |
|-------|-----------:|-------------:|---------:|
| AJ0002163 | 2782515 | 7,500.00 | 1 |
| AJ0002162 | 1267580 | 1,465.52 | 1 |
| AJ0002161 | 2629511 | 8,362.07 | 1 |
| AD0002717 | 1464283 | 9,900.00 | 1 |
| AD0002716 | 3092126 | 7,413.79 | 1 |

### Reconciliación 105,054 vs 108,077 vs 104,013

```sql
-- Cargos concepto 5 por estatus
SELECT cc.ESTATUS, COUNT(*) FROM DOCTOS_CC cc WHERE cc.CONCEPTO_CC_ID=5 GROUP BY cc.ESTATUS;
-- → ESTATUS 'N' : 108,077 (todos)

-- Cargos concepto 5 por sistema origen
SELECT cc.SISTEMA_ORIGEN, COUNT(*) FROM DOCTOS_CC cc WHERE cc.CONCEPTO_CC_ID=5 GROUP BY cc.SISTEMA_ORIGEN;
-- → PV : 108,076 ; CC : 1

-- Ventas PV con línea forma 71, SIN filtro de estatus, por TIPO/ESTATUS
SELECT pv.TIPO_DOCTO, pv.ESTATUS, COUNT(*) FROM DOCTOS_PV pv
WHERE EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71)
GROUP BY pv.TIPO_DOCTO, pv.ESTATUS;
```

Líneas de cobro forma-71 en PV (sin filtrar estatus):

| TIPO_DOCTO | ESTATUS | N | Interpretación |
|-----------|--------|--:|----------------|
| V | N | 104,013 | venta a crédito **viva** |
| V | C | 822 | venta a crédito **cancelada** |
| V | D | 203 | devolución sobre venta a crédito |
| D | N | 16 | docto devolución |
| | | **105,054** | = total cobros forma 71 (ground truth) |

**Cadena de reconciliación:**
- `104,013` (ventas crédito vivas, V+N) — la base limpia para RFM.
- `+822 +203 +16 = 105,054` cobros forma-71 totales → **cuadra con el ground truth de 105,054**.
- `108,077` cargos concepto-5 (`108,076` origen PV `+1` origen CC) > `105,054`. **Diff ≈ 3,022.** Origen probable: cargos CC de ventas a crédito cuya línea de cobro PV fue editada/borrada tras generar el cargo, planes de cargo múltiple, y 1 cargo de origen `CC` (manual). El rango de fechas de los cargos concepto-5 (2006-03-26 → 2026-06-11) coincide con PV → no son del módulo `DOCTOS_VE` abandonado.

### Dimensión de comportamiento de pago (saldo / % pagado)

`DOCTOS_CC` **no tiene columna `SALDO`** (su `IMPORTE_COBRO` está en 0 en la cabecera; el monto vive en la tabla de detalle de importes CC). El saldo se computa como cargos (concepto 5) menos abonos. Conceptos más frecuentes en `DOCTOS_CC` (`ESTATUS='N'`):

| CONCEPTO_CC_ID | N | Rol |
|---------------:|--:|-----|
| 87327 | 1,810,839 | abono/pago de cobranza (mayoritario) |
| 155 | 314,891 | abono |
| **5** | **108,077** | **cargo (venta a crédito)** |
| 24533 | 69,515 | abono/movimiento |
| 27969 | 60,500 | abono/movimiento |

> **Confirmado:** la dimensión saldo / % pagado / morosidad **sigue siendo válida**, pero aplica **solo a la cohorte de crédito** (43,934 clientes con al menos una venta a plazos = 30,257 solo-crédito + 13,677 ambos). **No se puede aplicar a los 804 clientes solo-contado** (no tienen cuenta CC) ni a la porción de contado de los 13,677 "ambos".

---

## 5. Productos sobre TODAS las ventas (no solo crédito)

Cadena validada end-to-end (`DOCTOS_PV → DOCTOS_PV_DET → ARTICULOS → LINEAS_ARTICULOS`). `DOCTOS_PV_DET` cubre **ambos** tipos de venta (validado: venta crédito `15545178 → LAVADORAS` y crédito `15545161 → LICUADORAS`; el detalle existe igual en contado). Importe de línea = `PRECIO_TOTAL_NETO`.

### Top 15 categorías por importe — TODAS las ventas

```sql
SELECT la.LINEA_ARTICULO_ID, la.NOMBRE,
  COUNT(*) AS N_LINEAS,
  SUM(CAST(d.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET d ON d.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = d.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
GROUP BY la.LINEA_ARTICULO_ID, la.NOMBRE
ORDER BY 4 DESC ROWS 15;
```

| # | Categoría | N líneas | Importe |
|--:|-----------|---------:|--------:|
| 1 | ROPEROS | 14,490 | $97.1 M |
| 2 | KIT CAMAS COMPLETAS | 9,643 | $76.4 M |
| 3 | LAVADORAS | 11,618 | $71.8 M |
| 4 | COLCHONES Y BOXES | 25,289 | $69.7 M |
| 5 | PANTALLAS | 8,300 | $62.8 M |
| 6 | REFRIGERADORES Y CONGELADORES | 5,455 | $55.6 M |
| 7 | ESTUFAS Y PARRILLAS | 3,741 | $27.9 M |
| 8 | ALACENAS | 3,842 | $23.1 M |
| 9 | BOCINAS PORTATILES | 5,558 | $21.6 M |
| 10 | SALAS Y ESTANCIAS | 1,589 | $21.3 M |
| 11 | TOCADORES | 3,478 | $17.1 M |
| 12 | CELULAR | 3,147 | $17.1 M |
| 13 | ENSERES DOMESTICOS | 5,427 | $12.3 M |
| 14 | CAMAS Y LITERAS | 14,232 | $10.8 M |
| 15 | COMODAS Y CAJONERAS | 2,421 | $10.0 M |

### Shift vs solo-contado (top 10 contado)

```sql
SELECT la.NOMBRE, COUNT(*) AS N_LINEAS,
  SUM(CAST(d.PRECIO_TOTAL_NETO AS DOUBLE PRECISION)) AS IMPORTE
FROM DOCTOS_PV pv
JOIN DOCTOS_PV_DET d ON d.DOCTO_PV_ID = pv.DOCTO_PV_ID
JOIN ARTICULOS a ON a.ARTICULO_ID = d.ARTICULO_ID
JOIN LINEAS_ARTICULOS la ON la.LINEA_ARTICULO_ID = a.LINEA_ARTICULO_ID
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
  AND NOT EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS c WHERE c.DOCTO_PV_ID=pv.DOCTO_PV_ID AND c.FORMA_COBRO_ID=71)
GROUP BY la.NOMBRE ORDER BY 3 DESC ROWS 10;
```

| Categoría | N líneas (contado) | Importe (contado) |
|-----------|-------------------:|------------------:|
| COLCHONES Y BOXES | 1,126 | $2.98 M |
| ROPEROS | 790 | $2.65 M |
| REFRIGERADORES Y CONGELADORES | 226 | $1.36 M |
| LAVADORAS | 384 | $1.35 M |
| PANTALLAS | 168 | $0.98 M |
| KIT CAMAS COMPLETAS | 200 | $0.97 M |
| CAMAS Y LITERAS | 658 | $0.80 M |
| ALACENAS | 238 | $0.68 M |
| BOCINAS PORTATILES | 212 | $0.67 M |
| ESTUFAS Y PARRILLAS | 153 | $0.60 M |

> **El mix de categorías casi no cambia** (mismas líderes), porque por valor el contado es ~16 % del total. El cambio relevante: **COLCHONES Y BOXES** sube a #1 en contado (compra de impulso/efectivo) frente a #4 en la base total, y hay mayor presencia relativa de **CAMAS Y LITERAS** y electrónica chica. La corrección de la base **no reordena materialmente el ranking de productos**; sí corrige el **conteo de eventos y de clientes** (§2-3).

### Manejo de enganche / devolución

- **Enganche:** no aparece como línea de producto en `DOCTOS_PV_DET`; es un movimiento de cobro/CC, no un artículo. No contamina las categorías.
- **Devolución:** se excluye por `TIPO_DOCTO IN ('V','P')` (excluye `D`) y por `ESTATUS='N'` (excluye `C`). El detalle tiene `UNIDADES_DEV` para devoluciones parciales sobre la misma línea; no se descuenta en este conteo (caveat menor).

---

## 6. Gaps corregidos y caveats reales

### Retractación explícita

- **RETRACTADO: el "gap de `CLIENTE_ID` nulo".** Es **FALSO**. `CLIENTE_ID` está 100 % poblado en `DOCTOS_PV` (423,284/423,284, 44,781 distintos). No hay ventas anónimas; toda venta (contado o crédito) es atribuible a un cliente.
- **CORREGIDO: el enganche NO es una línea efectivo (67) sobre la venta a crédito** en `DOCTOS_PV_COBROS`. La línea 71 lleva el monto financiado completo; el detalle de enganche vive en el ledger CC, no en el PV (§1).

### Caveats reales que quedan

1. **Cuentas de revendedor / ruta inflan la frecuencia.** Los líderes son personas reales pero operan como mayoristas/ruta (`12387` VICTORINO ENRIQUEZ: F=2,381, M=$32.7 M; `12440`: F=1,075). 109 clientes 150k+ concentran $128 M. Para RFM de cliente final conviene marcarlos/excluirlos (umbral p.ej. F>50 o M>150k).
2. **Separación enganche vs contado puro.** "Contado" aquí = venta sin línea 71. El monto financiado del crédito (forma 71, $733 M) ≠ `SUM(IMPORTE_NETO)` del crédito ($664.6 M); la diferencia incluye intereses/recargos del plan, no es comparable peso a peso con el contado.
3. **No hay FK PV → CC.** El enlace al ledger de crédito es heurístico (`FOLIO + CLIENTE_ID + SISTEMA_ORIGEN`), validado 1:1 en muestra pero con ~3,022 cargos CC sin venta-crédito viva exacta (cancelaciones/ediciones).
4. **Exclusión de cancelados/devoluciones.** Todo el análisis filtra `ESTATUS='N'` y `TIPO_DOCTO IN ('V','P')`. Cancelados (`C`) y devoluciones (`D`) se excluyen; las devoluciones parciales (`UNIDADES_DEV`) no se restan línea a línea (impacto menor en §5).
5. **Dimensión de pago solo para crédito.** Saldo / % pagado / morosidad (`DOCTOS_CC`) aplica solo a los 43,934 clientes con crédito; no a los 804 solo-contado ni a la porción contado de los 13,677 "ambos".

### Validación end-to-end del RFM (FULL vs CRÉDITO-ONLY)

```sql
SELECT pv.CLIENTE_ID,
  DATEDIFF(DAY FROM MAX(pv.FECHA) TO DATE '2026-06-11') AS R_FULL,
  COUNT(*) AS F_FULL,
  SUM(CAST(pv.IMPORTE_NETO AS DOUBLE PRECISION)) AS M_FULL,
  SUM(CASE WHEN EXISTS (SELECT 1 FROM DOCTOS_PV_COBROS x
       WHERE x.DOCTO_PV_ID=pv.DOCTO_PV_ID AND x.FORMA_COBRO_ID=71) THEN 1 ELSE 0 END) AS F_CRED_ONLY
FROM DOCTOS_PV pv
WHERE pv.TIPO_DOCTO IN ('V','P') AND pv.ESTATUS='N'
  AND pv.CLIENTE_ID IN (11515, 11749, 23649, 12387, 38327)
GROUP BY pv.CLIENTE_ID ORDER BY pv.CLIENTE_ID;
```

| CLIENTE_ID | Cohorte | R_FULL (d) | **F_FULL** | M_FULL | **F_CRED_ONLY** | Eventos perdidos por RFM-crédito |
|-----------:|---------|-----------:|-----------:|-------:|----------------:|---------------------------------:|
| 11515 (LAURA A. SANCHEZ) | ambos | 2,520 | **12** | $16,255 | 3 | **9 (75 %)** |
| 11749 (EMARIEL S. ANDRADE) | ambos | 2,380 | **12** | $12,293 | 2 | **10 (83 %)** |
| 23649 | solo contado | 2,973 | **2** | $15,103 | 0 | **2 (100 %) — invisible** |
| 12387 (VICTORINO ENRIQUEZ) | ambos/reseller | 0 | **2,381** | $32.7 M | 1,921 | 460 (19 %) |
| 38327 | ambos | 7 | **494** | $4.0 M | 259 | 235 (48 %) |

> El RFM de crédito subcontaba la frecuencia entre 19 % y 100 % por cliente y volvía invisibles a los solo-contado. La base corregida (`DOCTOS_PV`, contado+crédito) es la única atribución completa.
