-- ============================================================================
-- Rollback de la migración 000062: MSP_FLOTA_ASIGNACION_ACTUAL,
-- MSP_FLOTA_ASIGNACION_CAMBIOS y MSP_FLOTA_FOTOS
-- ============================================================================
-- No hay llaves foráneas entre las tres tablas (ver el encabezado del up: la
-- bitácora tiene que sobrevivir a la baja del usuario), así que el orden es
-- indiferente. DROP TABLE ya elimina sus índices y constraints — por eso no
-- hay DROP INDEX aquí: si el up falló a la mitad, intentar dropear un objeto
-- que nunca se creó aborta el rollback y deja la base peor que antes.
--
-- OJO: este rollback borra la bitácora de asignaciones. No existe otra copia
-- —Firestore no guarda historia, que es la razón de ser del módulo—, así que
-- lo que se pierda aquí no se recupera de ningún lado.
-- ============================================================================

DROP TABLE MSP_FLOTA_ASIGNACION_CAMBIOS;

COMMIT;

DROP TABLE MSP_FLOTA_ASIGNACION_ACTUAL;

COMMIT;

DROP TABLE MSP_FLOTA_FOTOS;

COMMIT;

-- ─── Quitar la migración del registro ───────────────────────────────────────
DELETE FROM MSP_MIGRATIONS WHERE ID = 62;

COMMIT;
