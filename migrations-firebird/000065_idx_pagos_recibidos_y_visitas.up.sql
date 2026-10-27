-- 000065_idx_pagos_recibidos_y_visitas
--
-- Índices para el tablero de desempeño (internal/desempeno/infra/desempenofb/
-- actividades_repo.go) y el detalle de pago de clientes
-- (internal/clientes/infra/clientesfb/queries.go, pagoRecibidoDelAbono).
-- Sólo estructura: CLAUDE.md §1 permite índices; aquí no hay lógica.
--
-- 1. IDX_MSP_PAGOS_REC_IMPTE_ID (IMPTE_DOCTO_CC_ID, ID)
--    IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID (DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, ID)
--
--    Las dos subconsultas escalares que ligan un abono con la captura de la app
--
--      (SELECT MIN(pr2.ID) FROM MSP_PAGOS_RECIBIDOS pr2
--        WHERE pr2.IMPTE_DOCTO_CC_ID = ?)
--      (SELECT MIN(pr3.ID) FROM MSP_PAGOS_RECIBIDOS pr3
--        WHERE pr3.DOCTO_CC_ID = ? AND pr3.IMPTE_DOCTO_CC_ID IS NULL)
--
--    se resolvían en dev con PLAN (PR2 ORDER PK_MSP_PAGOS_RECIBIDOS INDEX (...)):
--    Firebird recorre la PK en orden buscando el primer ID que pase el filtro
--    y, cuando no hay ninguno (el caso común: pagos de oficina), recorre la PK
--    entera. Con la igualdad como prefijo y ID como último segmento, el MIN se
--    resuelve navegando sólo el rango del valor buscado: en dev el plan pasa a
--    PLAN (PR2 ORDER IDX_MSP_PAGOS_REC_IMPTE_ID) y
--    PLAN (PR3 ORDER IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID) (el IS NULL también
--    acota como segundo segmento), con resultados idénticos. Cifras y control
--    positivo: docs/evidencia/infra.md, E-INF-015. Recalcular estadísticas
--    solo NO cambió ese plan; hacen falta los índices.
--    No únicos a propósito: nada impide hoy dos capturas con el mismo importe.
--
-- 2. IDX_MSP_VISITAS_ZONA_FECHA (ZONA_CLIENTE_ID, FECHA)
--
--    IDX_MSP_VISITAS empieza por FECHA: para "zona = ? AND FECHA entre" sólo
--    acota por fecha y filtra la zona fila por fila sobre todas las zonas.
--
-- 3. SET STATISTICS
--
--    Firebird calcula la selectividad de un índice al crearlo y no la vuelve a
--    calcular sola. En dev IDX_MSP_PAGOS_RECIB_IMPTE (000054) tenía 0.333 y
--    IDX_MSP_PAGOS_REC_ESTADO / _PEND / _CARGO tenían 1.0: valores de cuando
--    la tabla estaba casi vacía. Se recalculan aquí, junto con
--    IDX_MSP_PAGOS_RECIBIDOS (FECHA), los índices nuevos y nada más: sólo
--    índices de MSP_PAGOS_RECIBIDOS que existen con el MISMO nombre en dev y en
--    producción (el esquema de índices de los dos NO coincide; p. ej. el de
--    DOCTO_CC_ID se llama distinto), para que la migración no falle allá.
--    El mantenimiento periódico está en docs/ops/estadisticas-indices.md.

CREATE INDEX IDX_MSP_PAGOS_REC_IMPTE_ID ON MSP_PAGOS_RECIBIDOS (IMPTE_DOCTO_CC_ID, ID);
CREATE INDEX IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID ON MSP_PAGOS_RECIBIDOS (DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, ID);
CREATE INDEX IDX_MSP_VISITAS_ZONA_FECHA ON MSP_VISITAS (ZONA_CLIENTE_ID, FECHA);
COMMIT;

SET STATISTICS INDEX IDX_MSP_PAGOS_RECIB_IMPTE;
SET STATISTICS INDEX IDX_MSP_PAGOS_REC_ESTADO;
SET STATISTICS INDEX IDX_MSP_PAGOS_REC_PEND;
SET STATISTICS INDEX IDX_MSP_PAGOS_REC_CARGO;
SET STATISTICS INDEX IDX_MSP_PAGOS_RECIBIDOS;
SET STATISTICS INDEX IDX_MSP_PAGOS_REC_IMPTE_ID;
SET STATISTICS INDEX IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID;
SET STATISTICS INDEX IDX_MSP_VISITAS_ZONA_FECHA;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (65, '000065_idx_pagos_recibidos_y_visitas', CURRENT_TIMESTAMP);
COMMIT;
