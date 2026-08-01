-- ============================================================================
-- Rollback de la migración 000050: módulo de garantías (MSP_GA_GARANTIA,
-- MSP_GA_ARTICULO, MSP_GA_EVENTO, MSP_GA_IMAGEN, GEN_MSP_GA_FOLIO)
-- ============================================================================
-- Orden inverso al up: primero las tablas hijas por FK (imagen antes que
-- evento, evento y artículo antes que garantía), y al final la tabla raíz y
-- el generador. DROP TABLE ya elimina sus índices y constraints — por eso no
-- hay DROP INDEX ni ALTER TABLE ... DROP CONSTRAINT aquí: si el up falló a
-- la mitad, intentar dropear un objeto que nunca se creó aborta el rollback
-- y deja la base peor que antes.
-- ============================================================================

DROP TABLE MSP_GA_IMAGEN;

COMMIT;

DROP TABLE MSP_GA_EVENTO;

COMMIT;

DROP TABLE MSP_GA_ARTICULO;

COMMIT;

DROP TABLE MSP_GA_GARANTIA;

COMMIT;

DROP GENERATOR GEN_MSP_GA_FOLIO;

COMMIT;

-- ─── Quitar la migración del registro ───────────────────────────────────────
DELETE FROM MSP_MIGRATIONS WHERE ID = 50;

COMMIT;