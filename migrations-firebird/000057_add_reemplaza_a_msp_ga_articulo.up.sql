-- ============================================================================
-- Migración 000057: MSP_GA_ARTICULO.REEMPLAZA_A — la cadena de reemplazos
-- ============================================================================
--
-- Por qué:
--   El diseño original (§3.2 del spec de garantías) asumía un solo cambio
--   físico por artículo: una fila ROL='original' y a lo sumo una fila
--   ROL='reemplazo'. Con ese supuesto no hacía falta enlazarlas: bastaba el
--   rol para saber cuál era cuál.
--
--   Ese supuesto no aguanta la realidad — un reemplazo también puede salir
--   defectuoso, y entonces se reemplaza otra vez. Y como un folio puede
--   traer varios artículos originales (un cliente reporta la mesa y el
--   sillón), con dos o más reemplazos en el mismo folio ya no hay forma de
--   saber cuál sustituye a cuál. El expediente pierde la genealogía justo
--   en el caso que más importa explicarle al cliente.
--
--   REEMPLAZA_A apunta al artículo que este sustituye; NULL en los
--   originales. La cadena queda explícita: original ← reemplazo ← reemplazo.
--
-- Qué NO resuelve, a propósito:
--   La invariante de que sólo un artículo de la cadena esté vivo a la vez ya
--   la garantiza la máquina de etapas: autorizar un cambio físico manda al
--   predecesor a `standby`, y desde `standby` no hay salida hacia el camino
--   del cliente. Esta columna es para el expediente, no para el control.
--
-- Riesgo de aplicación: nulo. MSP_GA_ARTICULO está vacía en todos los
-- entornos — el módulo de garantías todavía no tiene infraestructura de
-- escritura, sólo dominio.
--
-- Regla dura #1 de CLAUDE.md: sin DEFAULT, sin trigger, sin lógica. La
-- columna es estructural y el valor lo escribe Go.
-- ============================================================================

ALTER TABLE MSP_GA_ARTICULO
  ADD REEMPLAZA_A CHAR(36) CHARACTER SET ASCII;

COMMIT;

-- FK auto-referenciada: un reemplazo sólo puede apuntar a un artículo que
-- exista. Sin ON DELETE CASCADE — en este módulo no se borra nada (§3.5).
ALTER TABLE MSP_GA_ARTICULO
  ADD CONSTRAINT FK_MSP_GA_ARTICULO_REEMP
  FOREIGN KEY (REEMPLAZA_A) REFERENCES MSP_GA_ARTICULO (ID);

COMMIT;

-- Índice para recorrer la cadena hacia adelante ("¿qué reemplazó a este?"),
-- que es la dirección que consulta el expediente. La contraria ya la
-- resuelve la PK.
CREATE INDEX IDX_MSP_GA_ARTICULO_REEMP ON MSP_GA_ARTICULO (REEMPLAZA_A);

COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (57, '000057_add_reemplaza_a_msp_ga_articulo', CURRENT_TIMESTAMP);

COMMIT;
