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
