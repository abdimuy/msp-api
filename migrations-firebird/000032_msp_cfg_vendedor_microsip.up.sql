-- ============================================================================
-- Migración 000032: mapeo vendedor (usuario) → LISTA_ATRIB_ID de Microsip
-- ============================================================================
-- Propósito:
--   LIBRES_CARGOS_CC.VENDEDOR_1/2/3 son campos libres tipo lista del objeto
--   CARGOS_CC (atributos 19985 / 19986 / 19987). Los valores (personas) viven
--   en LISTAS_ATRIBUTOS y se identifican por LISTA_ATRIB_ID. NO existe un id
--   único cross-atributo: la misma persona tiene 3 LISTA_ATRIB_ID distintos
--   (uno por atributo) y la POSICION tampoco alinea. Por eso guardamos los
--   tres ids por vendedor: VENDEDOR_LISTA_ID_1 (atrib 19985), _2 (19986),
--   _3 (19987). El writer (aplicar venta) escribe VENDEDOR_k con el id del
--   vendedor que ocupa la posición k.
--
-- Propiedad:
--   Tabla propia de MSP (CLAUDE.md §1): sin defaults, sin triggers. Las
--   columnas de id son nullable; el centinela "sin mapeo" (-1) se resuelve en
--   Go al leer, no en la BD. La captura de los 3 ids por vendedor es un paso
--   de setup aparte (fuera de alcance de esta migración).
-- ============================================================================

CREATE TABLE MSP_CFG_VENDEDOR_MICROSIP (
  USUARIO_ID          CHAR(36) CHARACTER SET ASCII NOT NULL,
  VENDEDOR_LISTA_ID_1 INTEGER,
  VENDEDOR_LISTA_ID_2 INTEGER,
  VENDEDOR_LISTA_ID_3 INTEGER,
  CONSTRAINT PK_MSP_CFG_VENDEDOR_MICROSIP PRIMARY KEY (USUARIO_ID),
  CONSTRAINT FK_MSP_CFG_VEND_MSIP_USUARIO
    FOREIGN KEY (USUARIO_ID) REFERENCES MSP_USUARIOS (ID)
);
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (32, '000032_msp_cfg_vendedor_microsip', CURRENT_TIMESTAMP);
COMMIT;
