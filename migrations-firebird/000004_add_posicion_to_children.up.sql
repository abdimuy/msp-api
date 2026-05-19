-- ============================================================================
-- Migración 000004: POSICION en child tables (productos/combos/vendedores)
-- ============================================================================
-- Bug E: cuando una venta se crea en una sola transacción, todos los hijos
-- comparten exactamente el mismo CREATED_AT (microsecond resolution insuficiente
-- ante INSERTs consecutivos), por lo que la cláusula
-- ORDER BY CREATED_AT, ID en los SELECT cae al desempate por UUID
-- lexicográfico — un orden efectivamente aleatorio.
--
-- Fix: una columna POSICION INTEGER NOT NULL en cada tabla de hijos, asignada
-- secuencialmente (1,2,3…) por la app en el momento del INSERT según el orden
-- en que el cliente envió los items. Las queries SELECT cambian su ORDER BY
-- a POSICION.
--
-- Imagenes NO se incluye: se suben una a una via POSTs separados, por lo que
-- CREATED_AT sí da orden estable.
-- ============================================================================

-- ─── MSP_VENTAS_PRODUCTOS ────────────────────────────────────────────────────
ALTER TABLE MSP_VENTAS_PRODUCTOS ADD POSICION INTEGER;

UPDATE MSP_VENTAS_PRODUCTOS p
   SET POSICION = (
     SELECT COUNT(*) FROM MSP_VENTAS_PRODUCTOS p2
      WHERE p2.VENTA_ID = p.VENTA_ID
        AND (p2.CREATED_AT < p.CREATED_AT
             OR (p2.CREATED_AT = p.CREATED_AT AND p2.ID <= p.ID))
   );

ALTER TABLE MSP_VENTAS_PRODUCTOS ALTER COLUMN POSICION SET NOT NULL;

ALTER TABLE MSP_VENTAS_PRODUCTOS
  ADD CONSTRAINT UQ_MSP_VENTAS_PRODS_VENTA_POS UNIQUE (VENTA_ID, POSICION);

ALTER TABLE MSP_VENTAS_PRODUCTOS
  ADD CONSTRAINT CK_MSP_VENTAS_PRODS_POSICION_POS CHECK (POSICION > 0);

-- ─── MSP_VENTAS_COMBOS ───────────────────────────────────────────────────────
ALTER TABLE MSP_VENTAS_COMBOS ADD POSICION INTEGER;

UPDATE MSP_VENTAS_COMBOS c
   SET POSICION = (
     SELECT COUNT(*) FROM MSP_VENTAS_COMBOS c2
      WHERE c2.VENTA_ID = c.VENTA_ID
        AND (c2.CREATED_AT < c.CREATED_AT
             OR (c2.CREATED_AT = c.CREATED_AT AND c2.ID <= c.ID))
   );

ALTER TABLE MSP_VENTAS_COMBOS ALTER COLUMN POSICION SET NOT NULL;

ALTER TABLE MSP_VENTAS_COMBOS
  ADD CONSTRAINT UQ_MSP_VENTAS_COMBOS_VENTA_POS UNIQUE (VENTA_ID, POSICION);

ALTER TABLE MSP_VENTAS_COMBOS
  ADD CONSTRAINT CK_MSP_VENTAS_COMBOS_POSICION_POS CHECK (POSICION > 0);

-- ─── MSP_VENTAS_VENDEDORES ───────────────────────────────────────────────────
ALTER TABLE MSP_VENTAS_VENDEDORES ADD POSICION INTEGER;

UPDATE MSP_VENTAS_VENDEDORES v
   SET POSICION = (
     SELECT COUNT(*) FROM MSP_VENTAS_VENDEDORES v2
      WHERE v2.VENTA_ID = v.VENTA_ID
        AND (v2.CREATED_AT < v.CREATED_AT
             OR (v2.CREATED_AT = v.CREATED_AT AND v2.ID <= v.ID))
   );

ALTER TABLE MSP_VENTAS_VENDEDORES ALTER COLUMN POSICION SET NOT NULL;

ALTER TABLE MSP_VENTAS_VENDEDORES
  ADD CONSTRAINT UQ_MSP_VENTAS_VEND_VENTA_POS UNIQUE (VENTA_ID, POSICION);

ALTER TABLE MSP_VENTAS_VENDEDORES
  ADD CONSTRAINT CK_MSP_VENTAS_VEND_POSICION_POS CHECK (POSICION > 0);

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (4, '000004_add_posicion_to_children', CURRENT_TIMESTAMP);

COMMIT;
