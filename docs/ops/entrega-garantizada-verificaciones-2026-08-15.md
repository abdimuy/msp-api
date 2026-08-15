# Verificaciones de datos para encender las banderas de entrega garantizada

Corridas contra el **clon local** `mueblera-firebird` (MUEBLERA.FDB, clon de prod
del 2026-07-31). Los conteos hay que repetirlos en producción antes de encender
banderas; la configuración y el catálogo cambian poco, los conteos sí.

## D0 — Cobertura de MSP_CFG_ZONA_CAJA

44 zonas configuradas / 46 en Microsip → **2 sin fila**:

| ZONA_CLIENTE_ID | NOMBRE | CLIENTES | ACTIVOS |
|---|---|---|---|
| 71190 | MEDIO MAYOREO | 14 | **9** |
| 2990694 | R/SUP RICARDO MORALES | 0 | 0 |

**No bloquea.** Ninguna de las dos vende desde la app:

- `MEDIO MAYOREO` (71190) es mayoreo, y el mayoreo **no se captura desde la
  app** (confirmado por el negocio el 2026-08-15). Sus 9 clientes activos se
  atienden por otra vía, así que la falta de caja nunca se ejercita. **No hay
  que sembrar su fila.**
- `R/SUP RICARDO MORALES` (2990694) está vacía y es inerte.

Por lo tanto `VENTAS_ZONA_OBLIGATORIA` sólo espera a que la adopción de la app
que exige zona sea ~total. La cobertura del catálogo ya no es una precondición.

Si algún día el mayoreo llegara a capturarse desde la app, hay que sembrar su
fila **antes** o sus ventas fallarán con `ErrZonaSinCaja`.

## D0 — MSP_CFG_APLICAR

En el clon local **no** están en NULL:

| ID | SUCURSAL | F.COBRO CONTADO | F.COBRO CREDITO | CAJA_CONTADO | CAJERO_CONTADO |
|---|---|---|---|---|---|
| 1 | 225490 | 67 | 71 | 12151 | 12266 |

El plan los reporta NULL **en producción**. El clon no puede confirmarlo, así que
el `UPDATE MSP_CFG_APLICAR` sigue siendo pendiente operativo de prod
(pendiente #1 del plan).

## E2 — Calidad del catálogo CIUDADES (70 filas)

Peor de lo que anticipaba el plan. Además de los casi-duplicados que ya listaba:

**Espacios finales** (rompen igualdad exacta):
- `COYOMEAPAN ` (25361)
- `ESPERANZA ` (35739)

**Casi-duplicados**:
- `TLACHICHUCA` (26220) vs `TLACHICHUCA, PUE` (209265) — ambos ESTADO_ID 337

**Sufijos de estado dentro del nombre**:
- `TECAMACHALCO, PUE.` (2844279)
- `TUXTLA GUTIERREZ, CHIAPAS` (12827)

**ESTADO_ID que contradice al nombre — no lo anticipaba el plan:**

| CIUDAD_ID | NOMBRE | ESTADO_ID | ESTADO GUARDADO | ESTADO REAL |
|---|---|---|---|---|
| 11364 | QUERETARO | 10904 | CIUDAD DE MEXICO | Querétaro |
| 3032478 | TECAMAC | 10904 | CIUDAD DE MEXICO | Estado de México |
| 2844279 | TECAMACHALCO, PUE. | 10904 | CIUDAD DE MEXICO | Puebla |
| 12827 | TUXTLA GUTIERREZ, CHIAPAS | 10904 | CIUDAD DE MEXICO | Chiapas |
| 11277 | CIUDAD DE HIDALGO | 10904 | CIUDAD DE MEXICO | (dudoso) |

También `TULANCIGO DE BRAVO` (2681013) está mal escrito (TULANCINGO).

**Consecuencia para E1/E2.** El diseño dice «la fila de la ciudad trae su
estado: nunca se elige aparte», y es correcto — pero en 4 filas el ESTADO_ID
guardado contradice al nombre. Adoptarlo tal cual propagaría esos estados
equivocados a los clientes nuevos. Hay que corregir esas filas en Microsip
antes de encender `VENTAS_CIUDAD_CATALOGO`. Es tarea de la oficina: `CIUDADES`
es tabla compartida y el plan prohíbe que la venta escriba en ella.

## E4 — Daño existente del default Tehuacán/Puebla

- `DIRS_CLIENTES` con `CIUDAD_ID = 338` (Tehuacán): **13,636** de 43,835. La
  gran mayoría es legítima — el negocio está en Tehuacán.
- Clientes **creados por el API** (con venta en `MSP_VENTAS`) que quedaron con
  Tehuacán: **5**.
- Ventas cuyo texto capturado en `MSP_VENTAS.CIUDAD` **no** es Tehuacán: **1**.

Daño acotado: el auto-alta es reciente. Repetir en producción antes de decidir
si vale la pena corregir a mano.
