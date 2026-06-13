-- ============================================================================
-- Migración 000034: agregar REVERSADO a MSP_VENTAS_TRASPASOS
-- ============================================================================
--
-- Por qué:
--   Los DOCTOS_IN de Microsip son inmutables. Para permitir editar una venta
--   más de una vez sin romper inventario, el módulo de inventario genera un
--   traspaso directo nuevo y cancela (revierte) el anterior. REVERSADO indica
--   si el traspaso directo ya fue revertido ('S') o sigue activo ('N').
--
--   Solo los traspasos de TIPO='directo' tienen relevancia en esta bandera;
--   los de TIPO='reverso' siempre son REVERSADO='N' (un reverso no se
--   revierte a sí mismo).
--
--   La columna se agrega en dos pasos (nullable primero, backfill, SET NOT
--   NULL) siguiendo el patrón establecido en las migraciones 000003 y 000007,
--   porque Firebird no hace backfill implícito al agregar una columna con
--   DEFAULT a filas existentes.
--
-- Requiere: migración 000027 aplicada (MSP_VENTAS_TRASPASOS debe existir).
-- ============================================================================

-- Paso 1: agregar la columna como nullable para que el ALTER no falle en
-- filas existentes.
ALTER TABLE MSP_VENTAS_TRASPASOS ADD REVERSADO CHAR(1) CHARACTER SET ASCII;
COMMIT;

-- Paso 2: backfill — todas las filas existentes son NOT reversadas todavía.
UPDATE MSP_VENTAS_TRASPASOS SET REVERSADO = 'N';
COMMIT;

-- Paso 3: marcar NOT NULL ahora que todas las filas tienen valor.
ALTER TABLE MSP_VENTAS_TRASPASOS ALTER COLUMN REVERSADO SET NOT NULL;
COMMIT;

-- Paso 4: guardarrail defensivo — la regla canónica vive en Go (CLAUDE.md §1).
ALTER TABLE MSP_VENTAS_TRASPASOS
  ADD CONSTRAINT CK_MSP_VTA_TRASP_REVERSADO CHECK (REVERSADO IN ('S', 'N'));
COMMIT;

-- Paso 5: índice compuesto para la consulta de "traspaso directo activo"
-- (VENTA_ID, TIPO, REVERSADO) que el app layer usará para encontrar el
-- directo vigente.
CREATE INDEX IDX_MSP_VTA_TRASP_REVERSADO ON MSP_VENTAS_TRASPASOS (VENTA_ID, TIPO, REVERSADO);
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (34, '000034_msp_ventas_traspasos_reversado', CURRENT_TIMESTAMP);
COMMIT;
